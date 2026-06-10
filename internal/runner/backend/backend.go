// Package backend defines the interface between the runner's orchestration
// logic and the runtime that hosts task sandboxes (Docker today; Kubernetes,
// ECS, Firecracker, AgentCore later). See
// proposals/accepted/runner-backend-interface.md.
package backend

//go:generate go tool moq -out backend_moq.go . Backend

import (
	"context"

	"github.com/icholy/xagent/internal/runner/workspace"
)

// BinaryPath is the path inside the sandbox where backends provision the
// xagent driver binary. Spec.Cmd and the injected MCP server reference it.
const BinaryPath = "/usr/local/bin/xagent"

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

// Sandbox is a point-in-time view of a task's sandbox.
type Sandbox struct {
	TaskID int64
	State  State
}

// Exit reports that a task's sandbox exited. ExitCode carries the
// driver-owned-events invariant: 0 means the driver reported its outcome,
// non-zero means the report was lost.
type Exit struct {
	TaskID   int64
	ExitCode int
}

// Backend runs task sandboxes on a concrete runtime.
type Backend interface {
	// Start ensures a sandbox exists for spec.TaskID and starts it. It may
	// reuse a previous sandbox (and its filesystem) for the same task; the
	// driver tolerates both fresh and reused filesystems via its
	// SetupCommandsCompleted marker.
	Start(ctx context.Context, spec *Spec) error

	// Stop gracefully stops the task's sandbox, escalating to a hard kill
	// after a backend-chosen grace period. It reports whether a running
	// sandbox was signalled — in that case the driver owns the terminal
	// event report.
	Stop(ctx context.Context, taskID int64) (signalled bool, err error)

	// Running reports whether the task's sandbox is currently running.
	Running(ctx context.Context, taskID int64) (bool, error)

	// List returns all sandboxes owned by this runner.
	List(ctx context.Context) ([]Sandbox, error)

	// Remove deletes the task's sandbox and its associated state.
	Remove(ctx context.Context, taskID int64) error

	// Watch invokes handle for every sandbox exit this backend observes,
	// at most once per exit, until ctx is cancelled or the underlying
	// stream fails. Missed exits (e.g. while the runner is down) are
	// covered by the orchestrator's Reconcile, not by Watch.
	Watch(ctx context.Context, handle func(Exit)) error

	Close() error
}
