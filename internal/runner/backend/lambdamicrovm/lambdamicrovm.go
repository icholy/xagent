// Package lambdamicrovm implements the runner backend on AWS Lambda MicroVMs:
// AWS-managed Firecracker microVMs with no host hypervisor, kernel, or
// networking to operate. Each task runs in its own MicroVM launched from a
// pre-built image; the in-VM `xagent tool microvm-shim` receives the lifecycle
// hooks, fetches the task's spec bundle, and runs the driver.
//
// See proposals/draft/lambda-microvm-backend.md. The backend does runtime work
// only over opaque Handles — it persists, discovers, and reconciles nothing;
// the runner owns the taskstate store. The AWS control plane is reached through
// the Cloud and Stager interfaces so the orchestration here is unit-tested
// against fakes; the live wire client lives in the awsmvm subpackage.
package lambdamicrovm

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/icholy/xagent/internal/runner/backend"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
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

// noIngressConnector is the Lambda-provided connector that disables inbound
// connectivity. The driver connects out to the C2, so MicroVMs need no ingress.
const noIngressConnector = "arn:aws:lambda:%s:aws:network-connector:aws-network-connector:NO_INGRESS"

// defaultMaxDuration is used when a workspace does not set max_duration_seconds.
// Lambda caps the value at 28800 (8h).
const defaultMaxDuration int64 = 14400

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
// to clean a sandbox up, but not for identity (the MicroVM id is the
// Handle.ID). The runner persists it opaquely and hands it back on Destroy /
// reuse.
type handleData struct {
	ImageARN    string `json:"image_arn"`
	StageBucket string `json:"stage_bucket"`
	StageKey    string `json:"stage_key"`
}

// Backend runs task sandboxes as AWS Lambda MicroVMs.
type Backend struct {
	cloud    Cloud
	stager   Stager
	runnerID string
	region   string
	poll     time.Duration
	log      *slog.Logger
}

type Options struct {
	Cloud    Cloud
	Stager   Stager
	RunnerID string
	// Region is the single AWS region this runner's MicroVMs run in. A
	// workspace may not pin a different region (open question: multi-region).
	Region       string
	PollInterval time.Duration
	Log          *slog.Logger
}

func New(opts Options) (*Backend, error) {
	if opts.Cloud == nil || opts.Stager == nil {
		return nil, fmt.Errorf("lambdamicrovm: Cloud and Stager are required")
	}
	return &Backend{
		cloud:    opts.Cloud,
		stager:   opts.Stager,
		runnerID: opts.RunnerID,
		region:   opts.Region,
		poll:     cmp.Or(opts.PollInterval, 10*time.Second),
		log:      cmp.Or(opts.Log, slog.Default()),
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

// Launch stages the task's spec bundle and runs a fresh MicroVM, returning the
// handle the runner persists (the MicroVM id, plus the staging location in
// Data). A MicroVM is terminated on Signal/Destroy and never adopted, so a
// reuse handle is only used to clean up the prior run's stale staged object.
func (b *Backend) Launch(ctx context.Context, spec *backend.Spec, reuse *backend.Handle) (backend.Handle, error) {
	if err := b.ValidateWorkspace(spec.Workspace); err != nil {
		return backend.Handle{}, err
	}
	cfg := spec.Workspace.LambdaMicroVM

	// A reuse handle means a prior (exited) MicroVM for this task: clean up its
	// staged object before launching fresh.
	if reuse != nil {
		if prev, ok := decodeData(reuse.Data); ok {
			_ = b.stager.Remove(ctx, prev.StageBucket, prev.StageKey)
		}
	}

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
		// The driver runs to completion and exposes no endpoint traffic, so
		// endpoint-idle auto-suspend must be disabled (per AWS's guidance for
		// asynchronous applications).
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
		ImageARN:    cfg.ImageIdentifier,
		StageBucket: cfg.StagingBucket,
		StageKey:    stageKey,
	})
	if err != nil {
		return backend.Handle{}, fmt.Errorf("marshal handle data: %w", err)
	}
	return backend.Handle{Type: HandleType, ID: res.MicrovmID, Data: hd}, nil
}

// Probe reports the liveness of a handle by looking its MicroVM up in a single
// ListMicrovms call. A vanished or terminal MicroVM is reported as exited.
func (b *Backend) Probe(ctx context.Context, h backend.Handle) (backend.State, error) {
	mvm, ok, err := b.find(ctx, h.ID)
	if err != nil {
		return backend.StateUnknown, err
	}
	if ok && mvm.Alive() {
		return backend.StateRunning, nil
	}
	return backend.StateExited, nil
}

// Signal terminates the handle's MicroVM if it is alive. Termination fires the
// shim's /terminate hook, which SIGTERMs the driver (escalating to SIGKILL), so
// the driver owns the terminal event report — the same contract as Docker's
// SIGTERM.
func (b *Backend) Signal(ctx context.Context, h backend.Handle) (bool, error) {
	mvm, ok, err := b.find(ctx, h.ID)
	if err != nil {
		return false, err
	}
	if !ok || !mvm.Alive() {
		return false, nil
	}
	b.log.Info("terminating microvm", "microvm", h.ID)
	if _, err := b.cloud.TerminateMicrovm(ctx, &awsmicrovm.TerminateMicrovmInput{MicrovmID: h.ID}); err != nil {
		return true, err
	}
	return true, nil
}

// Destroy terminates the handle's MicroVM (if still alive) and deletes its
// staged bundle. It is idempotent.
func (b *Backend) Destroy(ctx context.Context, h backend.Handle) error {
	if mvm, ok, err := b.find(ctx, h.ID); err == nil && ok && mvm.Alive() {
		if _, err := b.cloud.TerminateMicrovm(ctx, &awsmicrovm.TerminateMicrovmInput{MicrovmID: h.ID}); err != nil {
			b.log.Warn("terminate during destroy failed", "microvm", h.ID, "error", err)
		}
	}
	if hd, ok := decodeData(h.Data); ok {
		_ = b.stager.Remove(ctx, hd.StageBucket, hd.StageKey)
	}
	return nil
}

// Watch polls ListMicrovms and reports each of this runner's MicroVMs' exits at
// most once, keyed by MicroVM id. Lambda surfaces VM state, not the driver's
// process exit code: a clean TERMINATED maps to ExitCode 0 ("driver reported"),
// anything else — including a MicroVM that vanishes while still believed alive —
// to -1 ("report lost"), which the state machine's status guard reconciles. The
// runner resolves id→task via the store and ignores ids it doesn't track.
func (b *Backend) Watch(ctx context.Context, handle func(backend.HandleExit)) error {
	reported := make(map[string]bool)
	prevAlive := make(map[string]bool)
	ticker := time.NewTicker(b.poll)
	defer ticker.Stop()
	for {
		mvms, err := b.listAll(ctx)
		if err != nil {
			b.log.Error("watch: list microvms", "error", err)
		} else {
			curr := make(map[string]awsmicrovm.Microvm, len(mvms))
			for _, m := range mvms {
				if m.Tags[tagRunner] != b.runnerID {
					continue // another runner's MicroVM
				}
				curr[m.MicrovmID] = m
				if m.Alive() || reported[m.MicrovmID] {
					continue
				}
				code := -1
				if m.State == awsmicrovm.MicrovmStateTerminated {
					code = 0
				}
				reported[m.MicrovmID] = true
				handle(backend.HandleExit{ID: m.MicrovmID, ExitCode: code})
			}
			// A MicroVM seen alive last poll but now absent vanished without an
			// observed terminal state: report it lost.
			for id := range prevAlive {
				if _, stillListed := curr[id]; !stillListed && !reported[id] {
					reported[id] = true
					handle(backend.HandleExit{ID: id, ExitCode: -1})
				}
			}
			prevAlive = make(map[string]bool, len(curr))
			for id, m := range curr {
				if m.Alive() {
					prevAlive[id] = true
				}
			}
			// Drop bookkeeping for ids no longer listed (terminal VMs Lambda has
			// GC'd); their exit was already reported. MicroVM ids are unique, so
			// a dropped id never recurs.
			for id := range reported {
				if _, stillListed := curr[id]; !stillListed {
					delete(reported, id)
				}
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// find looks up a MicroVM by id across all pages of ListMicrovms.
func (b *Backend) find(ctx context.Context, microvmID string) (awsmicrovm.Microvm, bool, error) {
	if microvmID == "" {
		return awsmicrovm.Microvm{}, false, nil
	}
	mvms, err := b.listAll(ctx)
	if err != nil {
		return awsmicrovm.Microvm{}, false, err
	}
	for _, m := range mvms {
		if m.MicrovmID == microvmID {
			return m, true, nil
		}
	}
	return awsmicrovm.Microvm{}, false, nil
}

// listAll pages through ListMicrovms until NextToken is empty, so callers see
// every MicroVM rather than just the first page. Truncating here would make
// find/Probe miss live VMs on later pages and Watch drop their exits.
func (b *Backend) listAll(ctx context.Context) ([]awsmicrovm.Microvm, error) {
	var all []awsmicrovm.Microvm
	in := &awsmicrovm.ListMicrovmsInput{}
	for {
		out, err := b.cloud.ListMicrovms(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("list microvms: %w", err)
		}
		all = append(all, out.Microvms...)
		if out.NextToken == "" {
			break
		}
		in.NextToken = out.NextToken
	}
	return all, nil
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
