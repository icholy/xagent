package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"syscall"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// testTaskVersion is the version returned by setupDriver's GetTask mock. The
// driver reads it once at the top of Run and stamps it on every runner event.
const testTaskVersion = 7

// setupDriver writes cfg for task 1 in a temporary config dir and returns a
// driver backed by a mock client whose SubmitRunnerEvents always acks.
func setupDriver(t *testing.T, cfg *Config) (*Driver, *xagentclient.ClientMock) {
	t.Helper()
	store := ConfigStore(t.TempDir())
	assert.NilError(t, store.Save(1, cfg))
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
		// run() forks on shell_session; an empty one takes the normal agent path.
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Version: testTaskVersion}}, nil
		},
	}
	return &Driver{TaskID: 1, Client: mock, Log: slog.Default(), Config: store}, mock
}

func TestDriverRun(t *testing.T) {
	t.Parallel()
	// Arrange
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})

	// Act
	err := driver.Run(t.Context())

	// Assert - started then stopped, both stamped with the fetched task version
	assert.NilError(t, err)
	assert.DeepEqual(t,
		mock.SubmittedRunnerEvents(),
		[]*xagentv1.RunnerEvent{
			{TaskId: 1, Version: testTaskVersion, Event: "started"},
			{TaskId: 1, Version: testTaskVersion, Event: "stopped"},
		},
		protocmp.Transform(),
	)
}

func TestDriverRun_AgentError(t *testing.T) {
	t.Parallel()
	// Arrange
	driver, mock := setupDriver(t, &Config{
		Type:  TypeDummy,
		Dummy: &DummyOptions{Commands: []string{"false"}},
	})

	// Act
	err := driver.Run(t.Context())

	// Assert - the failure was reported and acked, so the driver exits 0; both
	// events carry the fetched task version (the failed reason is the dynamic
	// agent error, ignored here).
	assert.NilError(t, err)
	assert.DeepEqual(t,
		mock.SubmittedRunnerEvents(),
		[]*xagentv1.RunnerEvent{
			{TaskId: 1, Version: testTaskVersion, Event: "started"},
			{TaskId: 1, Version: testTaskVersion, Event: "failed"},
		},
		protocmp.Transform(),
		protocmp.IgnoreFields(&xagentv1.RunnerEvent{}, "reason"),
	)
}

func TestDriverRun_AgentConfiguredError(t *testing.T) {
	t.Parallel()
	// Arrange - the dummy agent is configured to return an error string
	driver, mock := setupDriver(t, &Config{
		Type:  TypeDummy,
		Dummy: &DummyOptions{Error: "dummy agent failed on purpose"},
	})

	// Act
	err := driver.Run(t.Context())

	// Assert - the failure was reported and acked, so the driver exits 0
	assert.NilError(t, err)
	assert.DeepEqual(t,
		mock.SubmittedRunnerEvents(),
		[]*xagentv1.RunnerEvent{
			{TaskId: 1, Version: testTaskVersion, Event: "started"},
			{TaskId: 1, Version: testTaskVersion, Event: "failed"},
		},
		protocmp.Transform(),
		protocmp.IgnoreFields(&xagentv1.RunnerEvent{}, "reason"),
	)
}

func TestDriverRun_SetupCommandError(t *testing.T) {
	t.Parallel()
	// Arrange
	driver, mock := setupDriver(t, &Config{
		Type:     TypeDummy,
		Commands: []string{"false"},
	})

	// Act
	err := driver.Run(t.Context())

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t,
		mock.SubmittedRunnerEvents(),
		[]*xagentv1.RunnerEvent{
			{TaskId: 1, Version: testTaskVersion, Event: "started"},
			{TaskId: 1, Version: testTaskVersion, Event: "failed"},
		},
		protocmp.Transform(),
		protocmp.IgnoreFields(&xagentv1.RunnerEvent{}, "reason"),
	)
}

// TestDriverRun_Sigterm must not run in parallel: it delivers a process-wide
// SIGTERM via syscall.Kill, which Go dispatches to every driver's signal
// handler registered in Run. A parallel sibling would catch the stray signal
// and stop gracefully instead of taking its own path. Left serial, it runs in
// the sequential phase with no other Run active, so the signal reaches only its
// own driver.
func TestDriverRun_Sigterm(t *testing.T) {
	// Arrange - the dummy agent sleeps until the run context is cancelled
	driver, mock := setupDriver(t, &Config{
		Type:  TypeDummy,
		Dummy: &DummyOptions{Sleep: -1},
	})
	started := make(chan struct{})
	mock.SubmitRunnerEventsFunc = func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
		if req.Events[0].Event == "started" {
			close(started)
		}
		return &xagentv1.SubmitRunnerEventsResponse{}, nil
	}
	go func() {
		// Run's SIGTERM handler is registered before the started event is
		// submitted, so the signal cannot kill the test process.
		<-started
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	// Act
	err := driver.Run(t.Context())

	// Assert - a graceful stop is reported as stopped
	assert.NilError(t, err)
	assert.DeepEqual(t,
		mock.SubmittedRunnerEvents(),
		[]*xagentv1.RunnerEvent{
			{TaskId: 1, Version: testTaskVersion, Event: "started"},
			{TaskId: 1, Version: testTaskVersion, Event: "stopped"},
		},
		protocmp.Transform(),
	)
}

func TestDriverRun_StartedSubmitError(t *testing.T) {
	t.Parallel()
	// Arrange
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})
	mock.SubmitRunnerEventsFunc = func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
		return nil, errors.New("server unreachable")
	}

	// Act
	err := driver.Run(t.Context())

	// Assert
	assert.ErrorContains(t, err, "failed to submit started event")
}

func TestDriverRun_StoppedSubmitError(t *testing.T) {
	t.Parallel()
	// Arrange
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})
	mock.SubmitRunnerEventsFunc = func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
		if req.Events[0].Event == "stopped" {
			return nil, errors.New("server unreachable")
		}
		return &xagentv1.SubmitRunnerEventsResponse{}, nil
	}

	// Act
	err := driver.Run(t.Context())

	// Assert - an unacked terminal submit exits non-zero
	assert.ErrorContains(t, err, "failed to submit stopped event")
}

func TestDriverRun_FailedSubmitError(t *testing.T) {
	t.Parallel()
	// Arrange
	driver, mock := setupDriver(t, &Config{
		Type:  TypeDummy,
		Dummy: &DummyOptions{Commands: []string{"false"}},
	})
	mock.SubmitRunnerEventsFunc = func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
		if req.Events[0].Event == "failed" {
			return nil, errors.New("server unreachable")
		}
		return &xagentv1.SubmitRunnerEventsResponse{}, nil
	}

	// Act
	err := driver.Run(t.Context())

	// Assert - both the agent error and the lost report surface
	assert.ErrorContains(t, err, "dummy command failed")
	assert.ErrorContains(t, err, "failed to submit failed event")
}

func TestDriverRun_GetTaskError(t *testing.T) {
	t.Parallel()
	// Arrange - GetTask (hoisted to the top of Run) fails before any event
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})
	mock.GetTaskFunc = func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
		return nil, errors.New("server unreachable")
	}

	// Act
	err := driver.Run(t.Context())

	// Assert - the error is returned and no events were emitted; the runner's
	// supervise backstop reports the lost run via the non-zero exit code.
	assert.ErrorContains(t, err, "failed to get task")
	assert.Assert(t, cmp.Len(mock.SubmittedRunnerEvents(), 0))
}

func TestConfigPrompt(t *testing.T) {
	cfg := &Config{}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(got, "xagent:get_my_task"))
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
