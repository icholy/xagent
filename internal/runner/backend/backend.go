// Package backend defines the interface between the runner's orchestration
// logic and the runtime that hosts task sandboxes (Docker today; Kubernetes,
// ECS, Firecracker, AgentCore later). See
// proposals/accepted/runner-backend-interface.md and
// proposals/draft/shared-runner-taskstate.md.
//
// Backends do runtime work only — launch, probe, signal, destroy, watch — over
// opaque Handles. They never persist, discover, or reconcile task→sandbox
// state: the runner owns the taskstate store and is the only writer. It
// translates between a backend.Handle and a taskstate.Record at the boundary.
package backend

//go:generate go tool moq -out backend_moq.go . Backend

import (
	"context"
	"encoding/json"

	"github.com/icholy/xagent/internal/runner/workspace"
)

// BinaryPath is the path inside the sandbox where backends provision the
// xagent driver binary. Spec.Cmd and the injected MCP server reference it.
const BinaryPath = "/usr/local/bin/xagent"

// Handle identifies a backend's sandbox. ID is the index key (a container id,
// an AWS microVM id, ...) — unique and stable for the sandbox's lifetime — and
// is the legitimate handle the runner persists and reverse-indexes. Data is
// backend-defined (whatever the backend needs for cleanup but not for
// identity) and is never decoded by the store or the runner.
type Handle struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data,omitempty"`
}

// HandleExit reports a sandbox exit keyed by handle id. It carries only the id
// — enough for the runner's id→task lookup, and all a poll-based Watch can
// produce — plus the exit code.
type HandleExit struct {
	ID       string
	ExitCode int
}

// File is a file provisioned into the sandbox before it starts.
type File struct {
	Path string // absolute path inside the sandbox
	Data []byte
	Mode int64
	Dir  bool // if true, create a directory entry (Data is ignored)
}

// Spec describes the sandbox for a single task. The orchestrator owns
// everything in it; backends interpret the workspace's runtime-specific
// config section (container: for Docker) and provision the rest.
type Spec struct {
	TaskID    int64
	Workspace *workspace.Workspace
	Cmd       []string // driver invocation
	Env       []string // XAGENT_TASK_ID / XAGENT_TOKEN / XAGENT_SERVER
	Files     []File   // agent config; the driver binary is the backend's job
}

// State is the orchestrator's view of a sandbox's lifecycle. Runtime states
// that don't map to running or exited (e.g. a created-but-never-started
// container) are reported as StateUnknown and ignored by the orchestrator.
type State int

const (
	StateUnknown State = iota
	StateRunning
	StateExited
)

// Sandbox is a point-in-time view of a task's sandbox. The runner composes it
// from a taskstate record and a backend Probe; it is not produced by the
// backend.
type Sandbox struct {
	TaskID int64
	State  State
}

// Exit reports that a task's sandbox exited. ExitCode carries the
// driver-owned-events invariant: 0 means the driver reported its outcome,
// non-zero means the report was lost. The runner builds it from a HandleExit
// after resolving the handle id back to a task.
type Exit struct {
	TaskID   int64
	ExitCode int
}

// Backend runs task sandboxes on a concrete runtime. Every method does runtime
// work only and takes/returns Handles; none of them touch the taskstate store.
type Backend interface {
	// ValidateWorkspace reports whether the workspace's runtime config is
	// usable by this backend (Docker checks the container: section).
	ValidateWorkspace(ws *workspace.Workspace) error

	// Launch ensures a sandbox exists for spec and starts it, returning the
	// Handle the RUNNER persists. If reuse is non-nil the backend may adopt the
	// sandbox it identifies (preserving its filesystem) instead of creating
	// fresh, or clean up its stale resources. The backend persists nothing.
	Launch(ctx context.Context, spec *Spec, reuse *Handle) (Handle, error)

	// Probe reports the liveness of a single handle.
	Probe(ctx context.Context, h Handle) (State, error)

	// Signal gracefully stops the sandbox identified by h (SIGTERM → SIGKILL),
	// reporting whether a running sandbox was signalled — in that case the
	// driver owns the terminal event report.
	Signal(ctx context.Context, h Handle) (signalled bool, err error)

	// Destroy deletes the sandbox identified by h. It is idempotent: destroying
	// an absent sandbox is not an error.
	Destroy(ctx context.Context, h Handle) error

	// Watch streams sandbox exits keyed by handle id until ctx is cancelled or
	// the underlying stream fails. It reports only the id (not the full Handle);
	// the runner resolves id→task via the store, dedups, and ignores untracked
	// ids. Watch performs no persistence. Missed exits (e.g. while the runner is
	// down) are covered by the orchestrator's Reconcile, not by Watch.
	Watch(ctx context.Context, handle func(HandleExit)) error

	Close() error
}
