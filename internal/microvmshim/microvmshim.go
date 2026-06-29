// Package microvmshim implements the in-MicroVM application that runs as the
// Lambda MicroVMs image entrypoint (`xagent tool microvm-shim`). It exposes two
// surfaces on one HTTP server (port 8080): the AWS lifecycle hooks under
// /aws/lambda-microvms/runtime/v1/ (Lambda → shim) and the xagent control
// surface under /xagent/ (runner → shim over the managed proxy).
//
// The shim decouples provisioning (the task's files, once) from spawning the
// driver (every run — the first /run and every /resume). It supervises the
// driver and notifies the runner of its exit over the /xagent/lifecycle SSE
// stream. It holds NO AWS credentials and makes NO control-plane calls: all
// suspend/resume/terminate authority lives with the runner. See
// proposals/draft/lambda-microvm-backend.md.
package microvmshim

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/icholy/xagent/internal/runner/backend/lambdamicrovm"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
	"github.com/icholy/xagent/internal/x/sse"
)

const (
	// provisionedMarker gates one-time file provisioning so a resumed VM does
	// not clobber the driver's setup markers.
	provisionedMarker = "/xagent/.provisioned"

	defaultGrace    = 30 * time.Second
	keepAlivePeriod = 15 * time.Second
	keepAliveEvent  = "keep-alive"
)

// Process is the driver process the shim supervises. It is an interface so the
// server can be unit-tested without spawning a real process.
type Process interface {
	// Wait blocks until the process exits, returning its exit code (or -1 if it
	// was killed by a signal / did not exit cleanly).
	Wait() (int, error)
	// Signal delivers a signal (SIGTERM for graceful stop).
	Signal(os.Signal) error
	// Kill force-kills the process (SIGKILL).
	Kill() error
}

// Server implements the MicroVM lifecycle hooks and the xagent control surface.
// The zero value (with optional fields set) is usable.
type Server struct {
	// Fetch downloads the staged bundle from the /run payload URL. Defaults to
	// an HTTP GET.
	Fetch func(ctx context.Context, url string) ([]byte, error)
	// Start launches the driver from the bundle. Defaults to exec.
	Start func(ctx context.Context, b lambdamicrovm.Bundle) (Process, error)
	// Provision writes the bundle's files (gated by the provisioned marker).
	// Defaults to writing to the local filesystem.
	Provision func(b lambdamicrovm.Bundle) error

	Grace time.Duration
	Log   *slog.Logger

	lc lifecycle

	mu      sync.Mutex
	started bool
	bundle  lambdamicrovm.Bundle // retained for /resume re-spawn (survives the snapshot)
	current *driverProc
}

// driverProc is a single supervised driver run.
type driverProc struct {
	proc Process
	done chan struct{} // closed when the supervise goroutine's Wait returns
}

func (s *Server) log() *slog.Logger { return cmp.Or(s.Log, slog.Default()) }

// Handler returns the HTTP handler exposing both surfaces. The AWS hooks are
// routed by awsmicrovm.Handler; the xagent control surface is served directly.
func (s *Server) Handler() http.Handler {
	hooks := &awsmicrovm.Handler{
		Run:       s.runHook,
		Resume:    s.resumeHook,
		Suspend:   s.suspendHook,
		Terminate: s.terminateHook,
	}
	mux := http.NewServeMux()
	mux.Handle(awsmicrovm.HookBase+"/", hooks)
	mux.HandleFunc("GET "+lambdamicrovmLifecyclePath, s.lifecycleHandler)
	mux.HandleFunc("POST "+lambdamicrovmStopPath, s.stopHandler)
	return mux
}

// Paths of the xagent control surface, kept in sync with the backend.
const (
	lambdamicrovmLifecyclePath = "/xagent/lifecycle"
	lambdamicrovmStopPath      = "/xagent/stop"
)

// ListenAndServe serves both surfaces on addr until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// runHook handles the AWS /run hook: fetch the staged bundle, provision its
// files once, retain the bundle for resume, and spawn the driver in the
// background, returning promptly so Lambda finishes starting the VM. A repeated
// /run is a no-op.
func (s *Server) runHook(_ context.Context, req awsmicrovm.RunHookRequest) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	data, err := s.fetch(context.Background(), req.Payload)
	if err != nil {
		s.log().Error("fetch bundle", "error", err)
		return fmt.Errorf("fetch bundle: %w", err)
	}
	var bundle lambdamicrovm.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		s.log().Error("decode bundle", "error", err)
		return fmt.Errorf("decode bundle: %w", err)
	}

	if err := s.provision(bundle); err != nil {
		s.log().Error("provision files", "error", err)
		return fmt.Errorf("provision files: %w", err)
	}

	s.mu.Lock()
	s.bundle = bundle
	s.started = true
	s.mu.Unlock()

	return s.spawn(bundle)
}

// resumeHook handles the AWS /resume hook — load-bearing, not a no-op. The VM
// thawed with the previous driver already exited (that exit is what suspended
// it), so re-spawn the driver against the preserved disk. Files are NOT
// re-provisioned.
func (s *Server) resumeHook(_ context.Context, _ awsmicrovm.ResumeHookRequest) error {
	s.mu.Lock()
	bundle := s.bundle
	started := s.started
	s.mu.Unlock()
	if !started {
		// Never ran; nothing to resume. (Shouldn't happen — resume follows a
		// suspend, which follows a run.)
		s.log().Warn("resume before run; ignoring")
		return nil
	}
	s.log().Info("resume: re-spawning driver")
	return s.spawn(bundle)
}

// suspendHook handles the AWS /suspend hook, fired before the snapshot. By now
// the driver has already exited (its exit is what triggered the suspend), so
// this is a flush seam. It clears the sticky driver-exited so a stream that
// reconnects after the resume does not replay the previous run's exit.
func (s *Server) suspendHook(_ context.Context, _ awsmicrovm.SuspendHookRequest) error {
	s.lc.reset()
	return nil
}

// terminateHook handles the AWS /terminate hook (AWS-only), called by Lambda
// before releasing resources on a real terminate-microvm. It is a last-chance
// SIGTERM of the driver if it is somehow still running. The runner never POSTs
// it; graceful stop is /xagent/stop.
func (s *Server) terminateHook(_ context.Context, _ awsmicrovm.TerminateHookRequest) error {
	s.stopDriver()
	return nil
}

// stopHandler handles POST /xagent/stop — the runner's graceful stop over the
// proxy: SIGTERM → grace → SIGKILL the driver. Its exit then drives the suspend
// like any other completion.
func (s *Server) stopHandler(w http.ResponseWriter, _ *http.Request) {
	s.stopDriver()
	w.WriteHeader(http.StatusOK)
}

// lifecycleHandler handles GET /xagent/lifecycle — the SSE stream. It replays
// the sticky driver-exited immediately (so an exit during a runner disconnect is
// delivered on reconnect) and then streams live events plus keep-alives.
func (s *Server) lifecycleHandler(w http.ResponseWriter, r *http.Request) {
	sw, err := sse.NewServerWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ch, sticky, unsub := s.lc.subscribe()
	defer unsub()

	// Flush the SSE headers immediately so the runner's request unblocks: replay
	// the sticky driver-exited if there is one, else an initial keep-alive.
	first := sse.Event{Event: keepAliveEvent}
	if sticky != nil {
		first = *sticky
	}
	if err := sw.Write(first); err != nil {
		return
	}
	ka := time.NewTicker(keepAlivePeriod)
	defer ka.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if err := sw.Write(ev); err != nil {
				return
			}
		case <-ka.C:
			if err := sw.Write(sse.Event{Event: keepAliveEvent}); err != nil {
				return
			}
		}
	}
}

// spawn clears any stale sticky exit, starts the driver, and supervises it.
func (s *Server) spawn(bundle lambdamicrovm.Bundle) error {
	// Clear a previous run's sticky exit so a stream opened against the new run
	// waits for the new driver's exit rather than replaying the old one.
	s.lc.reset()

	proc, err := s.start(context.Background(), bundle)
	if err != nil {
		s.log().Error("start driver", "error", err)
		return fmt.Errorf("start driver: %w", err)
	}
	d := &driverProc{proc: proc, done: make(chan struct{})}
	s.mu.Lock()
	s.current = d
	s.mu.Unlock()

	go s.supervise(d)
	return nil
}

// supervise waits for the driver to exit and publishes driver-exited with its
// real exit code. The shim takes NO lifecycle action of its own — the runner,
// watching this stream, suspends the VM.
func (s *Server) supervise(d *driverProc) {
	code, err := d.proc.Wait()
	close(d.done)
	s.log().Info("driver exited", "code", code, "error", err)
	payload, _ := json.Marshal(lambdamicrovm.DriverExited{Code: code})
	s.lc.publish(sse.Event{Event: lambdamicrovm.EventDriverExited, Data: payload})
}

// stopDriver SIGTERMs the current driver, waits a grace period for it to exit,
// then SIGKILLs it. It waits for supervise to finish so the exit is published.
func (s *Server) stopDriver() {
	s.mu.Lock()
	d := s.current
	s.mu.Unlock()
	if d == nil {
		return
	}
	grace := cmp.Or(s.Grace, defaultGrace)
	if err := d.proc.Signal(syscall.SIGTERM); err != nil {
		s.log().Warn("SIGTERM driver", "error", err)
	}
	select {
	case <-d.done:
	case <-time.After(grace):
		s.log().Warn("driver did not exit after SIGTERM; sending SIGKILL")
		_ = d.proc.Kill()
		<-d.done
	}
}

func (s *Server) fetch(ctx context.Context, url string) ([]byte, error) {
	if s.Fetch != nil {
		return s.Fetch(ctx, url)
	}
	return httpFetch(ctx, url)
}

func (s *Server) provision(b lambdamicrovm.Bundle) error {
	if s.Provision != nil {
		return s.Provision(b)
	}
	return provisionFiles(b)
}

func (s *Server) start(ctx context.Context, b lambdamicrovm.Bundle) (Process, error) {
	if s.Start != nil {
		return s.Start(ctx, b)
	}
	return execStart(ctx, b)
}

func httpFetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch bundle: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// provisionFiles writes the bundle's files to disk once, gated by a marker so a
// resumed VM keeps the driver's setup state.
func provisionFiles(b lambdamicrovm.Bundle) error {
	if _, err := os.Stat(provisionedMarker); err == nil {
		return nil
	}
	for _, f := range b.Files {
		if f.Dir {
			if err := os.MkdirAll(f.Path, os.FileMode(f.Mode)); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(f.Path, f.Data, os.FileMode(f.Mode)); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(provisionedMarker), 0755); err != nil {
		return err
	}
	return os.WriteFile(provisionedMarker, nil, 0644)
}

// execProcess adapts *exec.Cmd to Process.
type execProcess struct{ cmd *exec.Cmd }

func (p *execProcess) Wait() (int, error) {
	err := p.cmd.Wait()
	if p.cmd.ProcessState != nil {
		return p.cmd.ProcessState.ExitCode(), err
	}
	return -1, err
}
func (p *execProcess) Signal(sig os.Signal) error { return p.cmd.Process.Signal(sig) }
func (p *execProcess) Kill() error                { return p.cmd.Process.Kill() }

func execStart(_ context.Context, b lambdamicrovm.Bundle) (Process, error) {
	if len(b.Cmd) == 0 {
		return nil, fmt.Errorf("bundle has no command")
	}
	cmd := exec.Command(b.Cmd[0], b.Cmd[1:]...)
	cmd.Env = append(os.Environ(), b.Env...)
	cmd.Dir = b.WorkingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd}, nil
}
