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
		// run() forks on shell_session; an empty one takes the normal agent path.
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id}}, nil
		},
		// The bootstrap brief renders from the same details get_my_task uses.
		GetTaskDetailsFunc: func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: &xagentv1.Task{Id: req.Id}}, nil
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

func TestDriverRun_AgentConfiguredError(t *testing.T) {
	// Arrange - the dummy agent is configured to return an error string
	driver, mock := setupDriver(t, &Config{
		Type:  TypeDummy,
		Dummy: &DummyOptions{Error: "dummy agent failed on purpose"},
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

func TestDriverRun_BriefError(t *testing.T) {
	// Arrange - the details fetch fails; the driver falls back to the pull flow
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})
	mock.GetTaskDetailsFunc = func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
		return nil, errors.New("server unreachable")
	}

	// Act
	err := driver.Run(t.Context())

	// Assert - the run still completes on the get_my_task fallback
	assert.NilError(t, err)
	assert.DeepEqual(t, submittedEvents(mock), []string{"started", "stopped"})
}

func TestDriverRun_BriefPersistsLastEventID(t *testing.T) {
	// Arrange - the task stream carries two events
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})
	mock.GetTaskDetailsFunc = func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
		return &xagentv1.GetTaskDetailsResponse{
			Task: &xagentv1.Task{Id: req.Id},
			Events: []*xagentv1.Event{
				{Id: 3, Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{Text: "Do the thing"}}},
				{Id: 7, Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{Description: "PR comment"}}},
			},
		}, nil
	}

	// Act
	err := driver.Run(t.Context())

	// Assert - the delivery mark is persisted alongside Started
	assert.NilError(t, err)
	cfg, err := LoadConfig(1)
	assert.NilError(t, err)
	assert.Equal(t, cfg.Started, true)
	assert.Equal(t, cfg.LastEventID, int64(7))
}

func TestBuildBrief(t *testing.T) {
	// Arrange - a fresh session with one already-tracked id (recreated
	// container edge: LastEventID only matters once Started is set)
	cfg := &Config{}
	resp := &xagentv1.GetTaskDetailsResponse{
		Task: &xagentv1.Task{Id: 1, Name: "Test"},
		Events: []*xagentv1.Event{
			{Id: 3, Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{Text: "Do the thing"}}},
		},
	}

	// Act
	brief := buildBrief(cfg, resp)

	// Assert - full context, and the mark advances past everything seen
	assert.Assert(t, strings.Contains(brief, "Do the thing"))
	assert.Equal(t, cfg.LastEventID, int64(3))
}

func TestBuildBrief_ResumeSkipsSeenEvents(t *testing.T) {
	// Arrange - a resumed session that already injected events up to id 3
	cfg := &Config{Started: true, LastEventID: 3}
	resp := &xagentv1.GetTaskDetailsResponse{
		Task: &xagentv1.Task{Id: 1, Name: "Test"},
		Events: []*xagentv1.Event{
			{Id: 3, Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{Text: "Do the thing"}}},
			{Id: 7, Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{Description: "PR comment arrived"}}},
		},
	}

	// Act
	brief := buildBrief(cfg, resp)

	// Assert - only the new event is rendered, and the mark advances
	assert.Assert(t, strings.Contains(brief, "PR comment arrived"))
	assert.Assert(t, !strings.Contains(brief, "Do the thing"))
	assert.Equal(t, cfg.LastEventID, int64(7))
}

func TestBuildBrief_ResumeNothingNew(t *testing.T) {
	// Arrange - a resumed session with no events above the mark
	cfg := &Config{Started: true, LastEventID: 7}
	resp := &xagentv1.GetTaskDetailsResponse{
		Task: &xagentv1.Task{Id: 1, Name: "Test"},
		Events: []*xagentv1.Event{
			{Id: 7, Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{Description: "PR comment"}}},
		},
	}

	// Act
	brief := buildBrief(cfg, resp)

	// Assert - an empty brief falls back to the get_my_task prompt
	assert.Equal(t, brief, "")
	assert.Equal(t, cfg.LastEventID, int64(7))
}

func TestConfigPrompt(t *testing.T) {
	cfg := &Config{}
	got, err := cfg.prompt("")
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(got, "Use xagent:get_my_task to fetch your task instructions"))
}

func TestConfigPrompt_Started(t *testing.T) {
	cfg := &Config{Started: true}
	got, err := cfg.prompt("")
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Check xagent:get_my_task and continue.")
}

func TestConfigPrompt_WorkspacePromptAppended(t *testing.T) {
	cfg := &Config{Started: true, Prompt: "Custom workspace instructions."}
	got, err := cfg.prompt("")
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Check xagent:get_my_task and continue.\n\nCustom workspace instructions.")
}

func TestConfigPrompt_Brief(t *testing.T) {
	cfg := &Config{}
	got, err := cfg.prompt("# Task 1: test\n\n## Instructions\n\n1. Do the thing")
	assert.NilError(t, err)
	// The brief replaces the "fetch your task instructions" bootstrap, but
	// get_my_task stays available as the mid-run refresh.
	assert.Assert(t, strings.Contains(got, "Your task brief is below."))
	assert.Assert(t, strings.Contains(got, "Use xagent:get_my_task to check for new instructions"))
	assert.Assert(t, strings.Contains(got, "1. Do the thing"))
	assert.Assert(t, !strings.Contains(got, "Use xagent:get_my_task to fetch your task instructions"))
}

func TestConfigPrompt_StartedBrief(t *testing.T) {
	cfg := &Config{Started: true}
	got, err := cfg.prompt("# Task 1: test — new activity")
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Handle the new activity in the brief below and continue.\n\n# Task 1: test — new activity")
}

func TestConfigPrompt_BriefAndWorkspacePrompt(t *testing.T) {
	cfg := &Config{Started: true, Prompt: "Custom workspace instructions."}
	got, err := cfg.prompt("brief text")
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Handle the new activity in the brief below and continue.\n\nbrief text\n\nCustom workspace instructions.")
}
