// Package microvmshim implements the in-MicroVM application that runs as the
// Lambda MicroVMs image entrypoint (`xagent tool microvm-shim`). It exposes the
// MicroVM lifecycle hooks, fetches the task's spec bundle on /run, provisions
// its files, and supervises the driver process — the in-VM half of the
// lambdamicrovm backend. See proposals/draft/lambda-microvm-backend.md.
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
)

const (
	// provisionedMarker gates one-time file provisioning so a resumed VM does
	// not clobber the driver's setup markers.
	provisionedMarker = "/xagent/.provisioned"

	defaultGrace = 30 * time.Second
)

// Process is the driver process the shim supervises. It is an interface so the
// server can be unit-tested without spawning a real process.
type Process interface {
	// Wait blocks until the process exits.
	Wait() error
	// Signal delivers a signal (SIGTERM for graceful stop).
	Signal(os.Signal) error
	// Kill force-kills the process (SIGKILL).
	Kill() error
}

// Server implements the MicroVM lifecycle hooks.
type Server struct {
	// Fetch downloads the staged bundle from the /run payload URL. Defaults to
	// an HTTP GET.
	Fetch func(ctx context.Context, url string) ([]byte, error)
	// Terminate terminates this MicroVM (self-termination once the driver
	// exits, and the /terminate fallback). Wired to the AWS client.
	Terminate func(ctx context.Context, microvmID string) error
	// Start launches the driver from the bundle. Defaults to exec.
	Start func(ctx context.Context, b lambdamicrovm.Bundle) (Process, error)
	// Provision writes the bundle's files. Defaults to writing to the local
	// filesystem (gated by the provisioned marker).
	Provision func(b lambdamicrovm.Bundle) error

	Grace time.Duration
	Log   *slog.Logger

	mu        sync.Mutex
	microvmID string
	proc      Process
	started   bool
}

func (s *Server) log() *slog.Logger { return cmp.Or(s.Log, slog.Default()) }

// Handler returns the HTTP handler exposing the lifecycle hooks. Routing and
// the wire format are owned by awsmicrovm.Handler; this server supplies only the
// behavior of the run and terminate hooks. suspend/resume are left nil — the
// run-to-completion model treats them as no-ops, which awsmicrovm answers 200.
func (s *Server) Handler() http.Handler {
	return &awsmicrovm.Handler{
		Run:       s.run,
		Terminate: s.terminate,
	}
}

// ListenAndServe serves the hooks on the default port until ctx is cancelled.
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

// run handles the /run lifecycle hook: fetch the staged bundle from the
// (opaque-to-awsmicrovm) payload, provision its files, and start the driver. It
// returns promptly — the driver outlives the request — so Lambda finishes
// starting the VM. A repeated /run (e.g. on resume) is a no-op.
func (s *Server) run(_ context.Context, req awsmicrovm.RunHookRequest) error {
	s.mu.Lock()
	s.microvmID = req.MicrovmID
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	fetch := s.Fetch
	if fetch == nil {
		fetch = httpFetch
	}
	data, err := fetch(context.Background(), req.Payload)
	if err != nil {
		s.log().Error("fetch bundle", "error", err)
		return fmt.Errorf("fetch bundle: %w", err)
	}
	var bundle lambdamicrovm.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		s.log().Error("decode bundle", "error", err)
		return fmt.Errorf("decode bundle: %w", err)
	}

	provision := s.Provision
	if provision == nil {
		provision = provisionFiles
	}
	if err := provision(bundle); err != nil {
		s.log().Error("provision files", "error", err)
		return fmt.Errorf("provision files: %w", err)
	}

	start := s.Start
	if start == nil {
		start = execStart
	}
	// Use a background context: the driver outlives this hook.
	proc, err := start(context.Background(), bundle)
	if err != nil {
		s.log().Error("start driver", "error", err)
		return fmt.Errorf("start driver: %w", err)
	}

	s.mu.Lock()
	s.proc = proc
	s.started = true
	microvmID := s.microvmID
	s.mu.Unlock()

	go s.supervise(proc, microvmID)
	return nil
}

// supervise waits for the driver to exit, then self-terminates the MicroVM so
// billing stops — the in-VM equivalent of a container exiting.
func (s *Server) supervise(proc Process, microvmID string) {
	err := proc.Wait()
	s.log().Info("driver exited", "error", err)
	if s.Terminate == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Terminate(ctx, microvmID); err != nil {
		s.log().Error("self-terminate microvm", "microvm", microvmID, "error", err)
	}
}

// terminate handles the /terminate lifecycle hook, called by Lambda before
// releasing resources. It mirrors the Docker backend's SIGTERM→SIGKILL so the
// driver catches the signal and owns its terminal report.
func (s *Server) terminate(_ context.Context, _ awsmicrovm.TerminateHookRequest) error {
	s.mu.Lock()
	proc := s.proc
	s.mu.Unlock()
	if proc != nil {
		s.stop(proc)
	}
	return nil
}

func (s *Server) stop(proc Process) {
	grace := cmp.Or(s.Grace, defaultGrace)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		s.log().Warn("SIGTERM driver", "error", err)
	}
	done := make(chan struct{})
	go func() { _ = proc.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(grace):
		s.log().Warn("driver did not exit after SIGTERM, sending SIGKILL")
		_ = proc.Kill()
	}
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

func (p *execProcess) Wait() error              { return p.cmd.Wait() }
func (p *execProcess) Signal(s os.Signal) error { return p.cmd.Process.Signal(s) }
func (p *execProcess) Kill() error              { return p.cmd.Process.Kill() }

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
