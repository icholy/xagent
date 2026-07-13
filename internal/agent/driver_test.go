package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
		// A first (non-wake) run seeds the event cursor from the tail token.
		ListEventsByTaskFunc: func(_ context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
			return &xagentv1.ListEventsByTaskResponse{NextPageToken: "tail-token"}, nil
		},
		// A first (non-wake) run fetches the full task brief for the prompt.
		GetTaskDetailsFunc: func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: &xagentv1.Task{Id: req.GetId()}}, nil
		},
	}
	// Log is required; tests that don't inspect output use the discard log.
	// TestDriverRun_LogsToSink overrides it with a file-backed one.
	return &Driver{TaskID: 1, Client: mock, Log: DiscardDriverLog, Config: store}, mock
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

// capturePromptLog returns a DriverLog that records structured records to buf as
// JSON lines, so a test can recover the exact prompt the dummy agent logged (it
// logs the prompt under the "text" attr of its "received prompt" line).
func capturePromptLog(buf *bytes.Buffer) *DriverLog {
	return &DriverLog{Logger: *slog.New(slog.NewJSONHandler(buf, nil)), sink: io.Discard}
}

// lastPrompt returns the prompt text from the dummy agent's "received prompt"
// log line captured in buf.
func lastPrompt(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	prompt, found := "", false
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec struct {
			Msg  string `json:"msg"`
			Text string `json:"text"`
		}
		assert.NilError(t, json.Unmarshal([]byte(line), &rec))
		if rec.Msg == "dummy agent received prompt" {
			prompt, found = rec.Text, true
		}
	}
	assert.Assert(t, found, "no prompt was logged")
	return prompt
}

func TestDriverRun_FirstRunInjectsBrief(t *testing.T) {
	t.Parallel()
	// Arrange - a first run whose GetTaskDetails returns a field-complete brief.
	driver, mock := setupDriver(t, &Config{Type: TypeDummy})
	mock.GetTaskDetailsFunc = func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
		return &xagentv1.GetTaskDetailsResponse{
			Task: &xagentv1.Task{
				Id:   req.GetId(),
				Name: "brief-task-name",
				Url:  "https://xagent.example/ui/tasks/1",
			},
			Events: []*xagentv1.Event{
				{Id: 1, Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{Text: "do the briefed thing"}}},
			},
		}, nil
	}
	var buf bytes.Buffer
	driver.Log = capturePromptLog(&buf)

	// Act
	assert.NilError(t, driver.Run(t.Context()))

	// Assert - the fetched brief's fields reached the prompt and the get_my_task
	// bootstrap instruction is gone; the first run fetched the brief exactly once.
	prompt := lastPrompt(t, &buf)
	assert.Assert(t, cmp.Contains(prompt, "brief-task-name"))
	assert.Assert(t, cmp.Contains(prompt, "https://xagent.example/ui/tasks/1"))
	assert.Assert(t, cmp.Contains(prompt, "do the briefed thing"))
	assert.Assert(t, !strings.Contains(prompt, "Use xagent:get_my_task to fetch your task instructions"))
	assert.Assert(t, cmp.Len(mock.GetTaskDetailsCalls(), 1))
}

func TestDriverRun_WakePromptUnchanged(t *testing.T) {
	t.Parallel()
	// Arrange - a wake run (already Started) with no pending events. The brief
	// fetch is first-run-only, so a wake must not call GetTaskDetails and its
	// prompt must be exactly the wake-branch text it was before this change.
	driver, mock := setupDriver(t, &Config{Type: TypeDummy, Started: true})
	var buf bytes.Buffer
	driver.Log = capturePromptLog(&buf)

	// Act
	assert.NilError(t, driver.Run(t.Context()))

	// Assert - the unchanged wake prompt, and no first-run brief fetch.
	assert.Equal(t, lastPrompt(t, &buf), "The task was updated. Continue.")
	assert.Assert(t, cmp.Len(mock.GetTaskDetailsCalls(), 0))
}

func TestDriverRun_DrainsEventsToTail(t *testing.T) {
	t.Parallel()
	// Arrange - a run whose event stream spans three pages: two with More=true
	// then a final one (More=false) that marks the tail. The drain must follow
	// next_page_token across all three and persist the final one.
	store := ConfigStore(t.TempDir())
	assert.NilError(t, store.Save(1, &Config{Type: TypeDummy}))
	pages := map[string]*xagentv1.ListEventsByTaskResponse{
		"":   {NextPageToken: "p1", More: true},
		"p1": {NextPageToken: "p2", More: true},
		"p2": {NextPageToken: "tail"}, // More defaults to false → tail reached
	}
	// The iterator mutates the request's PageToken in place, so the recorded call
	// requests all alias one pointer — capture the token at call time instead.
	var seenTokens []string
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Version: testTaskVersion}}, nil
		},
		ListEventsByTaskFunc: func(_ context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
			seenTokens = append(seenTokens, req.GetPageToken())
			return pages[req.GetPageToken()], nil
		},
		GetTaskDetailsFunc: func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: &xagentv1.Task{Id: req.GetId()}}, nil
		},
	}
	driver := &Driver{TaskID: 1, Client: mock, Log: DiscardDriverLog, Config: store}

	// Act
	assert.NilError(t, driver.Run(t.Context()))

	// Assert - the three pages were walked in order and the final tail token was
	// persisted as the new cursor.
	assert.DeepEqual(t, seenTokens, []string{"", "p1", "p2"})
	cfg, err := store.Load(1)
	assert.NilError(t, err)
	assert.Equal(t, cfg.NextEventToken, "tail")
}

func TestDrainEvents_RequestsServerSideTypeFilter(t *testing.T) {
	t.Parallel()
	// Arrange - the drain pushes the instruction + external filter to the server
	// via the request's Types field (the server does the filtering), then returns
	// the events it gets back and the tail token.
	page := &xagentv1.ListEventsByTaskResponse{
		NextPageToken: "tail",
		Events: []*xagentv1.Event{
			{Id: 1, Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{Text: "do the thing"}}},
			{Id: 3, Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{Description: "PR comment"}}},
		},
	}
	var gotTypes []string
	mock := &xagentclient.ClientMock{
		ListEventsByTaskFunc: func(_ context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
			gotTypes = req.GetTypes()
			return page, nil
		},
	}
	driver := &Driver{TaskID: 1, Client: mock, Log: DiscardDriverLog}

	// Act
	events, token, err := driver.drainEvents(t.Context(), &Config{})

	// Assert - the request carried the type filter, and the returned events and
	// tail token passed through.
	assert.NilError(t, err)
	assert.DeepEqual(t, gotTypes, []string{"instruction", "external"})
	assert.Equal(t, token, "tail")
	assert.Assert(t, cmp.Len(events, 2))
	assert.Equal(t, events[0].GetId(), int64(1))
	assert.Equal(t, events[1].GetId(), int64(3))
}

func TestDriverRun_EventTokenNotAdvancedOnError(t *testing.T) {
	t.Parallel()
	// Arrange - a run whose agent errors. The drain computes a fresh tail token,
	// but a failed run must leave the saved cursor untouched (at-least-once: the
	// next run re-fetches from the same position).
	store := ConfigStore(t.TempDir())
	assert.NilError(t, store.Save(1, &Config{
		Type:           TypeDummy,
		NextEventToken: "old-token",
		Dummy:          &DummyOptions{Error: "dummy agent failed on purpose"},
	}))
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Version: testTaskVersion}}, nil
		},
		ListEventsByTaskFunc: func(_ context.Context, _ *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
			return &xagentv1.ListEventsByTaskResponse{NextPageToken: "new-token"}, nil
		},
		GetTaskDetailsFunc: func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: &xagentv1.Task{Id: req.GetId()}}, nil
		},
	}
	driver := &Driver{TaskID: 1, Client: mock, Log: DiscardDriverLog, Config: store}

	// Act - the driver reports the failure and exits 0, but the run did not succeed.
	assert.NilError(t, driver.Run(t.Context()))

	// Assert - the cursor was NOT advanced past its pre-run value.
	cfg, err := store.Load(1)
	assert.NilError(t, err)
	assert.Equal(t, cfg.NextEventToken, "old-token")
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

func TestDriverRun_LogsToSink(t *testing.T) {
	t.Parallel()
	// Arrange - a driver teeing into a real append-only log file, wired as the
	// command does in production via OpenDriverLog.
	driver, _ := setupDriver(t, &Config{Type: TypeDummy})
	logPath := filepath.Join(t.TempDir(), "log")
	driver.Log = OpenDriverLog(logPath)
	t.Cleanup(func() { _ = driver.Log.Close() })

	// Act - two runs against the same file
	assert.NilError(t, driver.Run(t.Context()))
	assert.NilError(t, driver.Run(t.Context()))

	// Assert - the run delimiter, the driver's slog lines, and the agent's own
	// slog lines (the logger is threaded through to the agent) all reach the
	// log, and the second run appended a second delimiter below the first.
	got, err := os.ReadFile(logPath)
	assert.NilError(t, err)
	assert.Assert(t, cmp.Contains(string(got), "==== run version=7 pid="))
	assert.Assert(t, cmp.Contains(string(got), "loaded config"))
	assert.Assert(t, cmp.Contains(string(got), "dummy agent received prompt"))
	assert.Equal(t, strings.Count(string(got), "==== run version="), 2)
}

func TestDriverRun_SetupCommandOutputTeed(t *testing.T) {
	t.Parallel()
	// Arrange - a setup command that writes to stdout and stderr then fails,
	// with the driver teeing into a real append-only log file.
	driver, _ := setupDriver(t, &Config{
		Type:     TypeDummy,
		Commands: []string{"echo out-marker; echo err-marker >&2; false"},
	})
	logPath := filepath.Join(t.TempDir(), "log")
	driver.Log = OpenDriverLog(logPath)
	t.Cleanup(func() { _ = driver.Log.Close() })

	// Act
	assert.NilError(t, driver.Run(t.Context()))

	// Assert - the failing command's stdout and stderr land in the log next to
	// the setup-failure the operator would otherwise see with no output.
	got, err := os.ReadFile(logPath)
	assert.NilError(t, err)
	assert.Assert(t, cmp.Contains(string(got), "out-marker"))
	assert.Assert(t, cmp.Contains(string(got), "err-marker"))
}
