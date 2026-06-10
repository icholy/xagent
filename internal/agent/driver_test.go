package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/auth/agentauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

// eventRecorder records the runner events a driver under test submits.
// When failOn is set, submits of that event type return an error.
type eventRecorder struct {
	mu     sync.Mutex
	events []string
	failOn string
}

func (r *eventRecorder) client() *xagentclient.ClientMock {
	return &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			for _, ev := range req.Events {
				if ev.Event == r.failOn {
					return nil, errors.New("submit failed")
				}
				r.events = append(r.events, ev.Event)
			}
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
}

func (r *eventRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// waitLen blocks until at least n events have been recorded.
func (r *eventRecorder) waitLen(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.mu.Lock()
		l := len(r.events)
		r.mu.Unlock()
		if l >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d events, have %d", n, l)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func setupDriver(t *testing.T, cfg *Config) (*Driver, *eventRecorder) {
	t.Helper()
	ConfigDir = t.TempDir()
	cfg.Type = TypeDummy
	assert.NilError(t, SaveConfig(1, cfg))
	rec := &eventRecorder{}
	return &Driver{TaskID: 1, Client: rec.client(), Log: slog.Default()}, rec
}

func TestDriverRun(t *testing.T) {
	d, rec := setupDriver(t, &Config{})

	err := d.Run(t.Context())

	assert.NilError(t, err)
	assert.DeepEqual(t, rec.snapshot(), []string{"started", "stopped"})
	cfg, err := LoadConfig(1)
	assert.NilError(t, err)
	assert.Assert(t, cfg.Started, "config should be saved with Started=true")
}

func TestDriverRun_AgentError(t *testing.T) {
	d, rec := setupDriver(t, &Config{
		Dummy: &DummyOptions{Commands: []string{"exit 1"}},
	})

	err := d.Run(t.Context())

	// An acked failed event fulfils the reporting duty: exit 0.
	assert.NilError(t, err)
	assert.DeepEqual(t, rec.snapshot(), []string{"started", "failed"})
}

func TestDriverRun_SetupError(t *testing.T) {
	d, rec := setupDriver(t, &Config{Commands: []string{"exit 1"}})

	err := d.Run(t.Context())

	assert.NilError(t, err)
	assert.DeepEqual(t, rec.snapshot(), []string{"started", "failed"})
}

func TestDriverRun_FailedEventNotAcked(t *testing.T) {
	d, rec := setupDriver(t, &Config{
		Dummy: &DummyOptions{Commands: []string{"exit 1"}},
	})
	rec.failOn = "failed"

	err := d.Run(t.Context())

	// The failure could not be reported: exit non-zero so the monitor's
	// failed fallback fires.
	assert.ErrorContains(t, err, "dummy command failed")
	assert.DeepEqual(t, rec.snapshot(), []string{"started"})
}

func TestDriverRun_Stop(t *testing.T) {
	d, rec := setupDriver(t, &Config{
		Dummy: &DummyOptions{Sleep: -1},
	})

	result := make(chan error, 1)
	go func() { result <- d.Run(t.Context()) }()
	rec.waitLen(t, 1) // started: the signal handler is installed
	assert.NilError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	assert.NilError(t, <-result)
	assert.DeepEqual(t, rec.snapshot(), []string{"started", "stopped"})
}

func TestDriverRun_Reload(t *testing.T) {
	d, rec := setupDriver(t, &Config{
		Dummy: &DummyOptions{Sleep: -1},
	})

	result := make(chan error, 1)
	go func() { result <- d.Run(t.Context()) }()
	rec.waitLen(t, 1) // started: the signal handler is installed
	assert.NilError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))
	rec.waitLen(t, 2) // started again: the reload completed
	assert.NilError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

	assert.NilError(t, <-result)
	assert.DeepEqual(t, rec.snapshot(), []string{"started", "started", "stopped"})
}

func TestConfigPrompt_WithoutChildTasksCapability(t *testing.T) {
	cfg := &Config{}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(got, "xagent:get_my_task"))
	assert.Assert(t, !strings.Contains(got, "update_child_task"), "child task tools should not be mentioned without the child-tasks capability")
	assert.Assert(t, !strings.Contains(got, "create_child_task"), "child task tools should not be mentioned without the child-tasks capability")
}

func TestConfigPrompt_WithChildTasksCapability(t *testing.T) {
	cfg := &Config{Capabilities: []string{agentauth.CapabilityChildTasks}}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(got, "Use xagent:update_child_task to delegate work to child tasks."))
	assert.Assert(t, strings.Contains(got, "Only use xagent:create_child_task when explicitly instructed to create a new task."))
}

func TestConfigPrompt_Started(t *testing.T) {
	cfg := &Config{Started: true}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Check xagent:get_my_task and continue.")
}

func TestConfigPrompt_WorkspacePromptAppended(t *testing.T) {
	cfg := &Config{Started: true, Prompt: "Custom workspace instructions."}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Check xagent:get_my_task and continue.\n\nCustom workspace instructions.")
}
