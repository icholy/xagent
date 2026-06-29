// Package lambdamicrovm implements the runner backend on AWS Lambda MicroVMs:
// AWS-managed Firecracker microVMs with no host hypervisor, kernel, or
// networking to operate. Each task runs in its own MicroVM launched from a
// pre-built image; the in-VM `xagent tool microvm-shim` receives the lifecycle
// hooks, fetches the task's spec bundle, runs the driver, and notifies the
// runner of the driver's exit over an SSE stream through AWS's managed proxy.
//
// See proposals/draft/lambda-microvm-backend.md. The lifecycle is symmetric
// with Docker: the driver exiting suspends the VM (state preserved, no compute),
// the next run resumes it (driver re-spawned against the preserved disk), and
// the VM is terminated only when the task is archived. All three control-plane
// verbs (suspend, resume, terminate) are the runner's — the guest holds no AWS
// credentials. The control plane is reached through the Cloud and Stager
// interfaces so the orchestration here is unit-tested against fakes.
package lambdamicrovm

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/icholy/xagent/internal/runner/backend"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
	"github.com/icholy/xagent/internal/x/sse"
)

// HandleType is the backend.Handle.Type the backend stamps on its handles
// (informational metadata persisted in the task record).
const HandleType = "lambda-microvm"

// Resource tag keys mirrored from the Docker backend's container labels.
const (
	tagMarker = "xagent"
	tagTask   = "xagent.task"
	tagRunner = "xagent.runner"
)

// shimPort is the port the in-VM shim's HTTP server (hooks + xagent control
// surface) listens on; the proxy auth token is scoped to it.
const shimPort = 8080

// xagent control-surface paths the runner reaches over the managed proxy.
const (
	lifecyclePath = "/xagent/lifecycle"
	stopPath      = "/xagent/stop"
)

// EventDriverExited is the SSE event type the shim emits when the supervised
// driver process exits. Its data is a JSON DriverExited. It is the only
// load-bearing event on the /xagent/lifecycle stream; the runner ignores all
// others (keep-alives, etc.).
const EventDriverExited = "driver-exited"

// DriverExited is the payload of an EventDriverExited SSE event: the driver's
// real process exit code, observed by the shim via proc.Wait().
type DriverExited struct {
	Code int `json:"code"`
}

// noIngressConnector is the Lambda-provided connector that disables inbound
// connectivity. The driver connects out to the C2, and the runner reaches the
// shim over the managed proxy (a separate path from ingress connectors), so
// MicroVMs need no ingress.
const noIngressConnector = "arn:aws:lambda:%s:aws:network-connector:aws-network-connector:NO_INGRESS"

// defaultMaxDuration is used when a workspace does not set max_duration_seconds.
// Lambda caps the value at 28800 (8h).
const defaultMaxDuration int64 = 14400

// defaultReconcile is the period of the ListMicrovms reconcile sweep, the
// backstop for VMs that went terminal while their SSE stream was down.
const defaultReconcile = 30 * time.Second

// tokenExpirationMinutes is the lifetime of a minted proxy auth token. The
// stream re-mints on expiry and reconnects, so a short lifetime bounds the blast
// radius of a leaked token without failing healthy tasks.
const tokenExpirationMinutes = 60

// Bundle is the task spec the backend stages and the in-VM shim fetches. It
// carries exactly what backend.Spec holds; File.Data is base64-encoded by the
// JSON marshaller.
type Bundle struct {
	Cmd        []string       `json:"cmd"`
	Env        []string       `json:"env"`
	Files      []backend.File `json:"files"`
	WorkingDir string         `json:"working_dir,omitempty"`
	User       string         `json:"user,omitempty"`
}

// handleData is the backend-defined Handle.Data: everything the backend needs
// to reach and clean up a sandbox, but not for identity (the MicroVM id is the
// Handle.ID). Most importantly it carries the VM Endpoint so the runner can mint
// a token, open the SSE stream, POST /xagent/stop, and resume — without
// re-listing first. The runner persists it opaquely and hands it back on
// Destroy / reuse.
type handleData struct {
	Endpoint    string `json:"endpoint"` // VM proxy endpoint, for SSE + /xagent/stop
	ImageARN    string `json:"image_arn"`
	StageBucket string `json:"stage_bucket"` // staged spec bundle, cleaned on Destroy
	StageKey    string `json:"stage_key"`
}

// Backend runs task sandboxes as AWS Lambda MicroVMs.
type Backend struct {
	cloud     Cloud
	stager    Stager
	http      *http.Client
	runnerID  string
	region    string
	reconcile time.Duration
	log       *slog.Logger
}

type Options struct {
	Cloud  Cloud
	Stager Stager
	// HTTPClient makes the proxy requests (SSE stream, /xagent/stop). It must
	// not impose a timeout that would cut the long-lived SSE stream; defaults to
	// a timeout-free client.
	HTTPClient *http.Client
	RunnerID   string
	// Region is the single AWS region this runner's MicroVMs run in. A workspace
	// may not pin a different region (open question: multi-region).
	Region string
	// ReconcileInterval is the period of the ListMicrovms reconcile sweep.
	ReconcileInterval time.Duration
	Log               *slog.Logger
}

func New(opts Options) (*Backend, error) {
	if opts.Cloud == nil || opts.Stager == nil {
		return nil, fmt.Errorf("lambdamicrovm: Cloud and Stager are required")
	}
	return &Backend{
		cloud:     opts.Cloud,
		stager:    opts.Stager,
		http:      cmp.Or(opts.HTTPClient, &http.Client{}),
		runnerID:  opts.RunnerID,
		region:    opts.Region,
		reconcile: cmp.Or(opts.ReconcileInterval, defaultReconcile),
		log:       cmp.Or(opts.Log, slog.Default()),
	}, nil
}

func (b *Backend) Close() error { return nil }

// ValidateWorkspace requires the Lambda MicroVMs config a workspace needs to
// launch a sandbox. image_source build-on-demand is a documented follow-up, so
// a pre-built image_identifier is required for now.
func (b *Backend) ValidateWorkspace(ws *workspace.Workspace) error {
	cfg := ws.LambdaMicroVM
	if cfg == nil {
		return fmt.Errorf("lambda_microvm config is required")
	}
	if cfg.ImageIdentifier == "" {
		return fmt.Errorf("lambda_microvm.image_identifier is required")
	}
	if cfg.ExecutionRole == "" {
		return fmt.Errorf("lambda_microvm.execution_role is required")
	}
	if cfg.EgressConnector == "" {
		return fmt.Errorf("lambda_microvm.egress_connector is required")
	}
	if cfg.StagingBucket == "" {
		return fmt.Errorf("lambda_microvm.staging_bucket is required")
	}
	if cfg.MaxDurationSeconds < 0 || cfg.MaxDurationSeconds > 28800 {
		return fmt.Errorf("lambda_microvm.max_duration_seconds must be in (0, 28800]")
	}
	if cfg.Region != "" && b.region != "" && cfg.Region != b.region {
		return fmt.Errorf("lambda_microvm.region %q does not match runner region %q (multi-region is unsupported)", cfg.Region, b.region)
	}
	return nil
}

// Launch runs the task's sandbox. With a reuse handle identifying a suspended
// VM it resumes it (the /resume hook re-spawns the driver against the preserved
// disk — the MicroVM analog of reusing an exited Docker container). Otherwise it
// stages the spec bundle and runs a fresh MicroVM. The returned handle's id is
// the MicroVM id and its Data carries the endpoint + staging location.
func (b *Backend) Launch(ctx context.Context, spec *backend.Spec, reuse *backend.Handle) (backend.Handle, error) {
	if err := b.ValidateWorkspace(spec.Workspace); err != nil {
		return backend.Handle{}, err
	}

	// Reuse path: resume the suspended VM if it is still alive. If it is gone
	// (terminated / max_duration reaped), fall through to a fresh launch after
	// cleaning up the prior run's staged object.
	if reuse != nil && reuse.ID != "" {
		resumed, h, err := b.tryResume(ctx, reuse)
		if err != nil {
			return backend.Handle{}, err
		}
		if resumed {
			return h, nil
		}
		if prev, ok := decodeData(reuse.Data); ok {
			_ = b.stager.Remove(ctx, prev.StageBucket, prev.StageKey)
		}
	}

	return b.launchFresh(ctx, spec)
}

// tryResume resumes the reuse handle's MicroVM if it is alive. It returns
// resumed=false (with no error) when the VM is gone or terminal, signalling a
// fresh launch. A running VM is adopted as-is; a suspended one is resumed.
func (b *Backend) tryResume(ctx context.Context, reuse *backend.Handle) (bool, backend.Handle, error) {
	out, err := b.cloud.GetMicrovm(ctx, &awsmicrovm.GetMicrovmInput{MicrovmID: reuse.ID})
	if awsmicrovm.IsNotFound(err) {
		return false, backend.Handle{}, nil
	}
	if err != nil {
		return false, backend.Handle{}, fmt.Errorf("get microvm for resume: %w", err)
	}
	mvm := out.Microvm
	switch mvm.State {
	case awsmicrovm.MicrovmStateRunning:
		// Already running (e.g. a restart that beat the suspend): adopt it.
	case awsmicrovm.MicrovmStateSuspended:
		b.log.Info("resuming microvm", "microvm", reuse.ID)
		if _, err := b.cloud.ResumeMicrovm(ctx, &awsmicrovm.ResumeMicrovmInput{MicrovmID: reuse.ID}); err != nil {
			return false, backend.Handle{}, fmt.Errorf("resume microvm: %w", err)
		}
	default: // TERMINATED / FAILED
		return false, backend.Handle{}, nil
	}

	hd, _ := decodeData(reuse.Data)
	if mvm.Endpoint != "" {
		hd.Endpoint = mvm.Endpoint
	}
	data, err := json.Marshal(hd)
	if err != nil {
		return false, backend.Handle{}, fmt.Errorf("marshal handle data: %w", err)
	}
	return true, backend.Handle{Type: HandleType, ID: reuse.ID, Data: data}, nil
}

// launchFresh stages the spec bundle and runs a brand-new MicroVM.
func (b *Backend) launchFresh(ctx context.Context, spec *backend.Spec) (backend.Handle, error) {
	cfg := spec.Workspace.LambdaMicroVM

	bundle := Bundle{
		Cmd:   spec.Cmd,
		Env:   append(cfg.Environ(), spec.Env...),
		Files: spec.Files,
	}
	data, err := json.Marshal(bundle)
	if err != nil {
		return backend.Handle{}, fmt.Errorf("marshal bundle: %w", err)
	}

	maxDuration := cmp.Or(cfg.MaxDurationSeconds, defaultMaxDuration)
	stageKey := fmt.Sprintf("%s/%d.json", b.runnerID, spec.TaskID)
	url, err := b.stager.Stage(ctx, cfg.StagingBucket, stageKey, data, maxDuration)
	if err != nil {
		return backend.Handle{}, fmt.Errorf("stage bundle: %w", err)
	}

	b.log.Info("running microvm", "task", spec.TaskID, "image", cfg.ImageIdentifier)
	res, err := b.cloud.RunMicrovm(ctx, &awsmicrovm.RunMicrovmInput{
		ImageIdentifier:          cfg.ImageIdentifier,
		ExecutionRoleArn:         cfg.ExecutionRole,
		EgressNetworkConnectors:  []string{cfg.EgressConnector},
		IngressNetworkConnectors: []string{fmt.Sprintf(noIngressConnector, b.region)},
		MaximumDurationInSeconds: maxDuration,
		RunHookPayload:           url,
		// Endpoint-idle auto-suspend must be disabled: the runner drives suspend
		// explicitly off the driver-exit signal, and auto-suspend would race that
		// stream and wrongly suspend a CPU-busy agent receiving no traffic (per
		// AWS's guidance for asynchronous applications).
		IdlePolicy: &awsmicrovm.IdlePolicy{AutoResumeEnabled: false},
		Tags: map[string]string{
			tagMarker: "true",
			tagTask:   strconv.FormatInt(spec.TaskID, 10),
			tagRunner: b.runnerID,
		},
	})
	if err != nil {
		_ = b.stager.Remove(ctx, cfg.StagingBucket, stageKey)
		return backend.Handle{}, fmt.Errorf("run microvm: %w", err)
	}

	hd, err := json.Marshal(handleData{
		Endpoint:    res.Endpoint,
		ImageARN:    cfg.ImageIdentifier,
		StageBucket: cfg.StagingBucket,
		StageKey:    stageKey,
	})
	if err != nil {
		return backend.Handle{}, fmt.Errorf("marshal handle data: %w", err)
	}
	return backend.Handle{Type: HandleType, ID: res.MicrovmID, Data: hd}, nil
}

// Probe reports the liveness of a handle from the control plane. A SUSPENDED VM
// (the normal post-completion state) reports StateExited so it looks like an
// exited-but-preserved container: the orchestrator then drives Start → Probe
// StateExited → Launch(reuse) → resume, and Prune-on-archive → Destroy →
// terminate, unchanged.
func (b *Backend) Probe(ctx context.Context, h backend.Handle) (backend.State, error) {
	out, err := b.cloud.GetMicrovm(ctx, &awsmicrovm.GetMicrovmInput{MicrovmID: h.ID})
	if awsmicrovm.IsNotFound(err) {
		return backend.StateExited, nil
	}
	if err != nil {
		return backend.StateUnknown, fmt.Errorf("get microvm: %w", err)
	}
	if out.Microvm.State == awsmicrovm.MicrovmStateRunning {
		return backend.StateRunning, nil
	}
	return backend.StateExited, nil // SUSPENDED / TERMINATED / FAILED
}

// Signal gracefully stops the driver: over the managed proxy it POSTs the shim's
// /xagent/stop endpoint (SIGTERM → grace → SIGKILL). The driver catches SIGTERM
// and owns its terminal report to the C2; its exit then drives the suspend like
// any other completion (Watch). It reports signalled=true if a running VM was
// reached, and does NOT terminate or suspend — that is Destroy's / Watch's job.
func (b *Backend) Signal(ctx context.Context, h backend.Handle) (bool, error) {
	hd, ok := decodeData(h.Data)
	if !ok || hd.Endpoint == "" {
		// No endpoint to reach: the driver already exited and the VM is
		// suspended/gone, so nothing is running to signal.
		return false, nil
	}
	token, err := b.mintToken(ctx, h.ID)
	if err != nil {
		if awsmicrovm.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("mint auth token: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := awsmicrovm.NewProxyRequest(reqCtx, hd.Endpoint, token, http.MethodPost, stopPath, nil)
	if err != nil {
		return false, err
	}
	resp, err := b.http.Do(req)
	if err != nil {
		// The VM could not be reached (suspended / network): the driver is not
		// running, so nothing was signalled.
		b.log.Warn("signal: stop request failed", "microvm", h.ID, "error", err)
		return false, nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 == 2 {
		return true, nil
	}
	return false, fmt.Errorf("stop returned status %d", resp.StatusCode)
}

// Destroy is the only terminate path, reached via Prune on task archive/delete:
// terminate the MicroVM (idempotent — a 404 means it is already gone) and delete
// the staged S3 object. A non-404 terminate failure is returned so Prune retries
// rather than leaking a billed VM.
func (b *Backend) Destroy(ctx context.Context, h backend.Handle) error {
	b.log.Info("terminating microvm", "microvm", h.ID)
	if _, err := b.cloud.TerminateMicrovm(ctx, &awsmicrovm.TerminateMicrovmInput{MicrovmID: h.ID}); err != nil && !awsmicrovm.IsNotFound(err) {
		return fmt.Errorf("terminate microvm: %w", err)
	}
	if hd, ok := decodeData(h.Data); ok {
		if err := b.stager.Remove(ctx, hd.StageBucket, hd.StageKey); err != nil {
			b.log.Warn("remove staged bundle", "microvm", h.ID, "error", err)
		}
	}
	return nil
}

// mintToken mints a short-lived proxy auth token scoped to the shim's port.
func (b *Backend) mintToken(ctx context.Context, microvmID string) (string, error) {
	out, err := b.cloud.CreateMicrovmAuthToken(ctx, &awsmicrovm.CreateMicrovmAuthTokenInput{
		MicrovmID:         microvmID,
		ExpirationMinutes: tokenExpirationMinutes,
		AllowedPorts:      []awsmicrovm.AllowedPort{{Port: shimPort}},
	})
	if err != nil {
		return "", err
	}
	return out.Token, nil
}

// listAll yields every MicroVM across all pages of ListMicrovms. Pagination is
// fallible, so it is an iter.Seq2 carrying a per-step error: a non-nil error is
// yielded once (with a zero Microvm) and iteration stops.
func (b *Backend) listAll(ctx context.Context) iter.Seq2[awsmicrovm.Microvm, error] {
	return func(yield func(awsmicrovm.Microvm, error) bool) {
		in := &awsmicrovm.ListMicrovmsInput{}
		for {
			out, err := b.cloud.ListMicrovms(ctx, in)
			if err != nil {
				yield(awsmicrovm.Microvm{}, fmt.Errorf("list microvms: %w", err))
				return
			}
			for _, m := range out.Microvms {
				if !yield(m, nil) {
					return
				}
			}
			if out.NextToken == "" {
				return
			}
			in.NextToken = out.NextToken
		}
	}
}

func decodeData(raw []byte) (handleData, bool) {
	if len(raw) == 0 {
		return handleData{}, false
	}
	var hd handleData
	if err := json.Unmarshal(raw, &hd); err != nil {
		return handleData{}, false
	}
	return hd, true
}

// Watch discovers this runner's MicroVMs (ListMicrovms tag-filtered) and
// maintains a per-VM SSE stream to /xagent/lifecycle over the managed proxy. On
// a clean driver-exited{code} event it suspends the VM itself (so the
// orchestrator sees a sandbox that merely "exited", Docker-identically) and
// reports the true exit code. A stream drop is arbitrated via GetMicrovm: a
// running VM reconnects (sticky replay covers a gap-exit) and emits nothing; a
// non-running VM emits one exit. A periodic ListMicrovms reconcile sweep is the
// final backstop for VMs reaped while disconnected. Watch runs until ctx is
// cancelled.
func (b *Backend) Watch(ctx context.Context, handle func(backend.HandleExit)) error {
	w := &watcher{
		b:        b,
		handle:   handle,
		streams:  make(map[string]context.CancelFunc),
		states:   make(map[string]awsmicrovm.MicrovmState),
		reported: make(map[string]bool),
	}
	defer w.wg.Wait()

	ticker := time.NewTicker(b.reconcile)
	defer ticker.Stop()
	w.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

// watcher holds the live state of a single Watch call: the per-VM stream
// goroutines and the bookkeeping that dedups exits across the SSE streams and
// the reconcile sweep.
type watcher struct {
	b      *Backend
	handle func(backend.HandleExit)
	wg     sync.WaitGroup

	mu      sync.Mutex
	streams map[string]context.CancelFunc // active stream per MicroVM id
	// states is the last sweep-observed state per id, used to detect a
	// SUSPENDED→RUNNING resume (which re-arms the next exit).
	states map[string]awsmicrovm.MicrovmState
	// reported guards against a duplicate exit for the same run. It is cleared
	// for an id only when the sweep observes it transition SUSPENDED→RUNNING (a
	// genuine resume), so the suspend-propagation window — where a sweep may see
	// a just-suspended VM still RUNNING and reconnect to its sticky
	// driver-exited — cannot manufacture a second exit.
	reported map[string]bool
}

// sweep lists this runner's VMs and reconciles streams: start one per RUNNING
// VM, emit a backstop exit for a terminal VM with no live stream, and re-arm an
// id that resumed. It is the only writer of states and the sweep-side reporter.
func (w *watcher) sweep(ctx context.Context) {
	var vms []awsmicrovm.Microvm
	for m, err := range w.b.listAll(ctx) {
		if err != nil {
			// A partial fleet view can't be reconciled; the next tick retries.
			w.b.log.Error("watch: list microvms", "error", err)
			return
		}
		if m.Tags[tagRunner] != w.b.runnerID {
			continue // another runner's MicroVM
		}
		vms = append(vms, m)
	}

	seen := make(map[string]bool, len(vms))
	var starts []awsmicrovm.Microvm
	var lost []string

	w.mu.Lock()
	for _, m := range vms {
		seen[m.MicrovmID] = true
		prev := w.states[m.MicrovmID]
		w.states[m.MicrovmID] = m.State
		switch {
		case m.State == awsmicrovm.MicrovmStateRunning:
			if prev == awsmicrovm.MicrovmStateSuspended {
				// Genuine resume: re-arm so the new run's exit emits.
				delete(w.reported, m.MicrovmID)
			}
			starts = append(starts, m)
		case m.State.Terminal():
			// A terminal VM with no live stream went terminal while disconnected
			// (e.g. a max_duration reap with no SSE): emit a "report lost" exit.
			// With a live stream, defer to its arbitration, which has the last
			// SSE code.
			if _, streaming := w.streams[m.MicrovmID]; !streaming && !w.reported[m.MicrovmID] {
				lost = append(lost, m.MicrovmID)
			}
		}
	}
	// Drop bookkeeping for ids Lambda has GC'd from the listing.
	for id := range w.states {
		if !seen[id] {
			delete(w.states, id)
			delete(w.reported, id)
		}
	}
	w.mu.Unlock()

	for _, m := range starts {
		w.ensureStream(ctx, m.MicrovmID, m.Endpoint)
	}
	for _, id := range lost {
		w.emit(id, -1)
	}
}

// ensureStream starts a stream goroutine for id unless one is already running.
func (w *watcher) ensureStream(ctx context.Context, id, endpoint string) {
	w.mu.Lock()
	if _, ok := w.streams[id]; ok {
		w.mu.Unlock()
		return
	}
	sctx, cancel := context.WithCancel(ctx)
	w.streams[id] = cancel
	w.mu.Unlock()

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer w.removeStream(id)
		w.stream(sctx, id, endpoint)
	}()
}

func (w *watcher) removeStream(id string) {
	w.mu.Lock()
	if cancel, ok := w.streams[id]; ok {
		cancel()
		delete(w.streams, id)
	}
	w.mu.Unlock()
}

// emit reports an exit for id at most once until the id is re-armed by a resume.
func (w *watcher) emit(id string, code int) {
	w.mu.Lock()
	if w.reported[id] {
		w.mu.Unlock()
		return
	}
	w.reported[id] = true
	w.mu.Unlock()
	w.handle(backend.HandleExit{ID: id, ExitCode: code})
}

// stream maintains the SSE lifecycle stream for one MicroVM until it observes a
// terminal outcome (a driver-exited event, or a drop arbitrated to non-RUNNING)
// or ctx is cancelled. A clean driver-exited suspends the VM and emits the true
// code; a drop with the VM still RUNNING reconnects without emitting.
func (w *watcher) stream(ctx context.Context, id, endpoint string) {
	backoff := 500 * time.Millisecond
	var lastCode *int
	for {
		if ctx.Err() != nil {
			return
		}
		token, err := w.b.mintToken(ctx, id)
		if err != nil {
			if awsmicrovm.IsNotFound(err) {
				w.emit(id, codeOr(lastCode, -1))
				return
			}
			w.b.log.Warn("watch: mint token", "microvm", id, "error", err)
			if w.arbitrate(ctx, id, lastCode, &endpoint) {
				return
			}
			if !sleep(ctx, &backoff) {
				return
			}
			continue
		}

		exited, code := w.readStream(ctx, id, endpoint, token)
		if exited {
			// Clean completion: suspend (preserve state, stop compute) and emit
			// the true exit code. The orchestrator never learns the VM is merely
			// suspended — it sees an exited sandbox, Docker-identically.
			if _, err := w.b.cloud.SuspendMicrovm(ctx, &awsmicrovm.SuspendMicrovmInput{MicrovmID: id}); err != nil {
				w.b.log.Warn("watch: suspend after driver exit", "microvm", id, "error", err)
			}
			w.emit(id, code)
			return
		}

		// Bare drop: the control plane is the liveness authority.
		if w.arbitrate(ctx, id, lastCode, &endpoint) {
			return
		}
		backoff = 500 * time.Millisecond // a confirmed-running reconnect resets backoff
		if !sleep(ctx, &backoff) {
			return
		}
	}
}

// readStream opens the SSE stream and reads until a driver-exited event (returns
// exited=true with the code) or the stream ends/errors (returns exited=false).
func (w *watcher) readStream(ctx context.Context, id, endpoint, token string) (bool, int) {
	req, err := awsmicrovm.NewProxyRequest(ctx, endpoint, token, http.MethodGet, lifecyclePath, nil)
	if err != nil {
		w.b.log.Warn("watch: build proxy request", "microvm", id, "error", err)
		return false, 0
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := w.b.http.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return false, 0
	}
	r := sse.NewReader(resp.Body)
	for {
		ev, err := r.Read()
		if err != nil {
			return false, 0
		}
		if ev.Event == "" && len(ev.Data) == 0 {
			return false, 0 // stream ended
		}
		if ev.Event == EventDriverExited {
			var de DriverExited
			if err := json.Unmarshal(ev.Data, &de); err != nil {
				w.b.log.Warn("watch: decode driver-exited", "microvm", id, "error", err)
				return true, -1
			}
			return true, de.Code
		}
		// Ignore keep-alives and unknown event types.
	}
}

// arbitrate consults the control plane after a stream drop. It returns done=true
// (the caller stops) when it emits an exit — the VM is non-RUNNING or gone — and
// false when the VM is still RUNNING (the caller reconnects; the sticky
// driver-exited covers an exit during the gap). A transient control-plane error
// is treated as "keep trying" (no exit emitted).
func (w *watcher) arbitrate(ctx context.Context, id string, lastCode *int, endpoint *string) bool {
	out, err := w.b.cloud.GetMicrovm(ctx, &awsmicrovm.GetMicrovmInput{MicrovmID: id})
	if awsmicrovm.IsNotFound(err) {
		w.emit(id, codeOr(lastCode, -1))
		return true
	}
	if err != nil {
		w.b.log.Warn("watch: arbitrate get microvm", "microvm", id, "error", err)
		return false
	}
	if out.Microvm.State == awsmicrovm.MicrovmStateRunning {
		if out.Microvm.Endpoint != "" {
			*endpoint = out.Microvm.Endpoint
		}
		return false
	}
	w.emit(id, codeOr(lastCode, -1))
	return true
}

func codeOr(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}

// sleep waits for the backoff to elapse (or ctx to cancel) and doubles it,
// capped. It returns false if ctx was cancelled.
func sleep(ctx context.Context, backoff *time.Duration) bool {
	t := time.NewTimer(*backoff)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
	}
	*backoff *= 2
	if *backoff > 30*time.Second {
		*backoff = 30 * time.Second
	}
	return true
}
