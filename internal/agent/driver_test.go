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
	"gotest.tools/v3/assert"
)

// setupDriver writes cfg for task 1 in a temporary config dir and returns a
// driver backed by a mock client whose SubmitRunnerEvents always acks.
// The tests mutate the global ConfigDir, so they must not run in parallel.
func setupDriver(t *testing.T, cfg *Config) (*Driver, *xagentclient.ClientMock) {
	t.Helper()
	dir := ConfigDir
	ConfigDir = t.TempDir()
	t.Cleanup(func() { ConfigDir = dir })
	assert.NilError(t, SaveConfig(1, cfg))
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
		// The normal agent path forks on an empty shell_session.
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id}}, nil
		},
	}
	return &Driver{TaskID: 1, Client: mock, Log: slog.Default()}, mock
}

// submittedEvents returns the runner events submitted to the mock, in order.
func submittedEvents(mock *xagentclient.ClientMock) []string {
	var events []string
	for _, call := range mock.SubmitRunnerEventsCalls() {
		for _, e := range call.SubmitRunnerEventsRequest.Events {
			events = append(events, e.Event)
		}
	}
	return events
}

func TestDriverRun(t *testing.T) {
	// Arrange
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})

	// Act
	err := driver.Run(t.Context())

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, submittedEvents(mock), []string{"started", "stopped"})
}

func TestDriverRun_AgentError(t *testing.T) {
	// Arrange
	driver, mock := setupDriver(t, &Config{
		Type:  TypeDummy,
		Dummy: &DummyOptions{Commands: []string{"false"}},
	})

	// Act
	err := driver.Run(t.Context())

	// Assert - the failure was reported and acked, so the driver exits 0
	assert.NilError(t, err)
	assert.DeepEqual(t, submittedEvents(mock), []string{"started", "failed"})
}

func TestDriverRun_SetupCommandError(t *testing.T) {
	// Arrange
	driver, mock := setupDriver(t, &Config{
		Type:     TypeDummy,
		Commands: []string{"false"},
	})

	// Act
	err := driver.Run(t.Context())

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, submittedEvents(mock), []string{"started", "failed"})
}

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
	assert.DeepEqual(t, submittedEvents(mock), []string{"started", "stopped"})
}

func TestDriverRun_StartedSubmitError(t *testing.T) {
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
