package runner

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/runner/backend"
	dockerbackend "github.com/icholy/xagent/internal/runner/backend/docker"
	"github.com/icholy/xagent/internal/runner/taskstate"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/dockerx"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

// testStore opens a fresh taskstate store in a temp dir, optionally seeded with
// records.
func testStore(t *testing.T, recs ...taskstate.Record) *taskstate.Store {
	t.Helper()
	s, err := taskstate.Open(t.TempDir())
	assert.NilError(t, err)
	for _, rec := range recs {
		assert.NilError(t, s.Write(rec))
	}
	return s
}

func TestRunnerStart(t *testing.T) {
	abs, err := filepath.Abs("../../prebuilt")
	assert.NilError(t, err)
	t.Setenv("XAGENT_PREBUILT_DIR", abs)

	task := &model.Task{
		ID:        1,
		Name:      "test-task",
		Runner:    "test-runner",
		Workspace: "test",
		Status:    model.TaskStatusPending,
		Command:   model.TaskCommandStart,
		Version:   1,
	}

	// Create mock client. The driver and the injected MCP server now connect to
	// this server directly (over the host network), so it must answer the
	// driver's started/stopped events and the agent's get_my_task
	// (GetTaskDetails).
	mock := &xagentclient.ClientMock{
		CreateTaskTokenFunc: func(_ context.Context, req *xagentv1.CreateTaskTokenRequest) (*xagentv1.CreateTaskTokenResponse, error) {
			return &xagentv1.CreateTaskTokenResponse{Token: "test-token"}, nil
		},
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
		GetTaskDetailsFunc: func(_ context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: task.Proto("")}, nil
		},
	}

	// Create httptest server with the mock
	_, handler := xagentv1connect.NewXAgentServiceHandler(mock)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Create runner. The agent connects to the C2 directly over the network, so
	// the container shares the host network namespace (NetworkMode "host") to
	// reach the httptest server on 127.0.0.1, and ServerURL is the same URL.
	client := xagentclient.New(xagentclient.Options{BaseURL: ts.URL})
	be, err := dockerbackend.New(dockerbackend.Options{RunnerID: "test-runner"})
	assert.NilError(t, err)
	r, err := New(Options{
		Client:    client,
		Backend:   be,
		Store:     testStore(t),
		ServerURL: ts.URL,
		Queue:     NewEventQueue(EventQueueOptions{Client: client, Log: slog.Default()}),
		Workspaces: &workspace.Config{
			Workspaces: map[string]workspace.Workspace{
				"test": {
					Container: workspace.Container{
						Image:       "alpine:latest",
						NetworkMode: "host",
					},
					Agent: workspace.Agent{
						Type: "dummy",
						Dummy: &workspace.DummyConfig{
							Sleep: 1,
							ToolCalls: []agent.DummyToolCall{
								{Server: "xagent", Name: "get_my_task"},
							},
						},
					},
				},
			},
		},
		Concurrency: 1,
		RunnerID:    "test-runner",
	})
	assert.NilError(t, err)
	t.Cleanup(func() { r.Close() })

	docker, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	assert.NilError(t, err)
	defer docker.Close()

	// Remove any leftover container from a previous aborted run.
	_ = docker.ContainerRemove(t.Context(), "xagent-1", container.RemoveOptions{Force: true})

	// Start a task
	err = r.Start(t.Context(), task)
	assert.NilError(t, err)

	// The runner records the launched handle in the store.
	rec, ok, err := r.store.Read(task.ID)
	assert.NilError(t, err)
	assert.Equal(t, ok, true)
	assert.Equal(t, rec.Type, "docker")
	assert.Assert(t, rec.ID != "")

	// Wait for the container to exit
	err = dockerx.ContainerWait(t.Context(), docker, "xagent-1", container.WaitConditionNotRunning)
	assert.NilError(t, err)

	// Remove the container
	err = docker.ContainerRemove(t.Context(), "xagent-1", container.RemoveOptions{})
	assert.NilError(t, err)

	// Verify get_my_task was called
	assert.Equal(t, len(mock.GetTaskDetailsCalls()), 1)

	// Verify the driver reported its own lifecycle: started, then stopped.
	var events []string
	for _, call := range mock.SubmitRunnerEventsCalls() {
		for _, e := range call.SubmitRunnerEventsRequest.Events {
			events = append(events, e.Event)
		}
	}
	assert.DeepEqual(t, events, []string{"started", "stopped"})
}

// submitted drains the queue through the mock client and returns the events
// it delivered.
func submitted(t *testing.T, mock *xagentclient.ClientMock, queue *EventQueue) []*xagentv1.RunnerEvent {
	t.Helper()
	assert.NilError(t, queue.Drain(t.Context()))
	var events []*xagentv1.RunnerEvent
	for _, call := range mock.SubmitRunnerEventsCalls() {
		events = append(events, call.SubmitRunnerEventsRequest.Events...)
	}
	return events
}

func TestRunnerStart_Idempotent(t *testing.T) {
	t.Parallel()
	// Arrange - a task with an already-running tracked sandbox.
	task := &model.Task{ID: 5, Runner: "test-runner", Workspace: "test", Version: 1}
	store := testStore(t, taskstate.Record{TaskID: 5, Type: "docker", ID: "c5"})
	be := &backend.BackendMock{
		ProbeFunc: func(_ context.Context, h backend.Handle) (backend.State, error) {
			assert.Equal(t, h.ID, "c5")
			return backend.StateRunning, nil
		},
	}
	mock := &xagentclient.ClientMock{}
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: NewEventQueue(EventQueueOptions{Client: mock})})
	assert.NilError(t, err)

	// Act
	err = r.Start(t.Context(), task)

	// Assert - the running sandbox is left alone: no Launch, no token minted.
	assert.NilError(t, err)
	assert.Equal(t, len(be.LaunchCalls()), 0)
	assert.Equal(t, len(mock.CreateTaskTokenCalls()), 0)
}

func TestRunnerStart_AdoptReuse(t *testing.T) {
	t.Parallel()
	// Arrange - a task whose tracked sandbox has exited; Start must pass the
	// prior handle to Launch as reuse and persist the returned handle.
	task := &model.Task{ID: 6, Runner: "test-runner", Workspace: "test", Version: 1}
	store := testStore(t, taskstate.Record{TaskID: 6, Type: "docker", ID: "old-id"})
	be := &backend.BackendMock{
		ValidateWorkspaceFunc: func(_ *workspace.Workspace) error { return nil },
		ProbeFunc: func(_ context.Context, h backend.Handle) (backend.State, error) {
			return backend.StateExited, nil
		},
		LaunchFunc: func(_ context.Context, _ *backend.Spec, reuse *backend.Handle) (backend.Handle, error) {
			assert.Assert(t, reuse != nil)
			assert.Equal(t, reuse.ID, "old-id")
			return backend.Handle{ID: "new-id"}, nil
		},
	}
	mock := &xagentclient.ClientMock{
		CreateTaskTokenFunc: func(_ context.Context, _ *xagentv1.CreateTaskTokenRequest) (*xagentv1.CreateTaskTokenResponse, error) {
			return &xagentv1.CreateTaskTokenResponse{Token: "t"}, nil
		},
	}
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: NewEventQueue(EventQueueOptions{Client: mock}), Workspaces: testWorkspaces()})
	assert.NilError(t, err)

	// Act
	err = r.Start(t.Context(), task)

	// Assert - the new handle replaced the old record.
	assert.NilError(t, err)
	assert.Equal(t, len(be.LaunchCalls()), 1)
	rec, ok, err := store.Read(6)
	assert.NilError(t, err)
	assert.Equal(t, ok, true)
	assert.Equal(t, rec.ID, "new-id")
}

func TestRunnerList(t *testing.T) {
	t.Parallel()
	// Arrange - two tracked records; List maps each to a Sandbox via Probe.
	store := testStore(t,
		taskstate.Record{TaskID: 1, Type: "docker", ID: "c1"},
		taskstate.Record{TaskID: 2, Type: "docker", ID: "c2"},
	)
	state := map[string]backend.State{"c1": backend.StateRunning, "c2": backend.StateExited}
	be := &backend.BackendMock{
		ProbeFunc: func(_ context.Context, h backend.Handle) (backend.State, error) {
			return state[h.ID], nil
		},
	}
	mock := &xagentclient.ClientMock{}
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: NewEventQueue(EventQueueOptions{Client: mock})})
	assert.NilError(t, err)

	// Act
	sandboxes, err := r.List(t.Context())

	// Assert
	assert.NilError(t, err)
	got := map[int64]backend.State{}
	for _, sb := range sandboxes {
		got[sb.TaskID] = sb.State
	}
	assert.DeepEqual(t, got, map[int64]backend.State{1: backend.StateRunning, 2: backend.StateExited})
}

func TestRunnerPoll_StopWithoutSandbox(t *testing.T) {
	t.Parallel()
	// Arrange - task being cancelled with no tracked sandbox.
	task := &model.Task{
		ID:        7,
		Runner:    "test-runner",
		Workspace: "test",
		Status:    model.TaskStatusCancelling,
		Command:   model.TaskCommandStop,
		Version:   3,
	}
	mock := &xagentclient.ClientMock{
		ListRunnerTasksFunc: func(_ context.Context, _ *xagentv1.ListRunnerTasksRequest) (*xagentv1.ListRunnerTasksResponse, error) {
			return &xagentv1.ListRunnerTasksResponse{Tasks: []*xagentv1.Task{task.Proto("")}}, nil
		},
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	be := &backend.BackendMock{
		SignalFunc: func(_ context.Context, _ backend.Handle) (bool, error) {
			return false, nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: testStore(t), RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Poll(t.Context())
	assert.NilError(t, err)

	// Assert - no record, so the backend is never signalled and the runner
	// emits "stopped" itself.
	assert.Equal(t, len(be.SignalCalls()), 0)
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "stopped")
	assert.Equal(t, events[0].TaskId, int64(7))
	assert.Equal(t, events[0].Version, int64(3))
}

func TestRunnerPoll_StopSignalled(t *testing.T) {
	t.Parallel()
	// Arrange - task being cancelled with a tracked, running sandbox.
	task := &model.Task{
		ID:        7,
		Runner:    "test-runner",
		Workspace: "test",
		Status:    model.TaskStatusCancelling,
		Command:   model.TaskCommandStop,
		Version:   3,
	}
	mock := &xagentclient.ClientMock{
		ListRunnerTasksFunc: func(_ context.Context, _ *xagentv1.ListRunnerTasksRequest) (*xagentv1.ListRunnerTasksResponse, error) {
			return &xagentv1.ListRunnerTasksResponse{Tasks: []*xagentv1.Task{task.Proto("")}}, nil
		},
	}
	be := &backend.BackendMock{
		SignalFunc: func(_ context.Context, h backend.Handle) (bool, error) {
			assert.Equal(t, h.ID, "c7")
			return true, nil
		},
	}
	store := testStore(t, taskstate.Record{TaskID: 7, Type: "docker", ID: "c7"})
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Poll(t.Context())
	assert.NilError(t, err)

	// Assert - the driver was signalled and owns the terminal report.
	assert.Equal(t, len(be.SignalCalls()), 1)
	assert.Equal(t, queue.Len(), 0)
}

func TestRunnerMonitor(t *testing.T) {
	t.Parallel()
	// Arrange - two tracked sandboxes plus one untracked exit the runner must
	// ignore.
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	be := &backend.BackendMock{
		WatchFunc: func(_ context.Context, handle func(backend.HandleExit)) error {
			handle(backend.HandleExit{ID: "c1", ExitCode: 0})
			handle(backend.HandleExit{ID: "c2", ExitCode: 137})
			handle(backend.HandleExit{ID: "unknown", ExitCode: 1})
			return nil
		},
	}
	store := testStore(t,
		taskstate.Record{TaskID: 1, Type: "docker", ID: "c1"},
		taskstate.Record{TaskID: 2, Type: "docker", ID: "c2"},
	)
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Monitor(t.Context())
	assert.NilError(t, err)

	// Assert - c1 exit 0 means the driver already reported; c2 non-zero means
	// the report was lost so the runner emits "failed"; the untracked id is
	// ignored.
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "failed")
	assert.Equal(t, events[0].TaskId, int64(2))
	assert.Equal(t, events[0].Version, int64(0))
}

func TestRunnerReconcile(t *testing.T) {
	t.Parallel()
	// Arrange
	status := map[int64]xagentv1.TaskStatus{
		2: xagentv1.TaskStatus_RUNNING,
		3: xagentv1.TaskStatus_COMPLETED,
	}
	mock := &xagentclient.ClientMock{
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Status: status[req.Id]}}, nil
		},
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	state := map[string]backend.State{
		"c1": backend.StateRunning,
		"c2": backend.StateExited,
		"c3": backend.StateExited,
	}
	be := &backend.BackendMock{
		ProbeFunc: func(_ context.Context, h backend.Handle) (backend.State, error) {
			return state[h.ID], nil
		},
	}
	store := testStore(t,
		taskstate.Record{TaskID: 1, Type: "docker", ID: "c1"},
		taskstate.Record{TaskID: 2, Type: "docker", ID: "c2"},
		taskstate.Record{TaskID: 3, Type: "docker", ID: "c3"},
	)
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Reconcile(t.Context())
	assert.NilError(t, err)

	// Assert - only the exited sandbox whose task is still running means the
	// driver's report was lost.
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "failed")
	assert.Equal(t, events[0].TaskId, int64(2))
}

func TestRunnerPrune(t *testing.T) {
	t.Parallel()
	// Arrange
	archived := map[int64]bool{1: true, 2: false}
	mock := &xagentclient.ClientMock{
		GetTaskFunc: func(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			if req.Id == 4 {
				return nil, connect.NewError(connect.CodeNotFound, errors.New("task not found"))
			}
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Archived: archived[req.Id]}}, nil
		},
	}
	state := map[string]backend.State{
		"c1": backend.StateExited,  // archived -> removed
		"c2": backend.StateExited,  // not archived -> kept
		"c3": backend.StateRunning, // running -> not considered
		"c4": backend.StateExited,  // deleted -> removed
	}
	be := &backend.BackendMock{
		ProbeFunc: func(_ context.Context, h backend.Handle) (backend.State, error) {
			return state[h.ID], nil
		},
		DestroyFunc: func(_ context.Context, _ backend.Handle) error {
			return nil
		},
	}
	store := testStore(t,
		taskstate.Record{TaskID: 1, Type: "docker", ID: "c1"},
		taskstate.Record{TaskID: 2, Type: "docker", ID: "c2"},
		taskstate.Record{TaskID: 3, Type: "docker", ID: "c3"},
		taskstate.Record{TaskID: 4, Type: "docker", ID: "c4"},
	)
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Prune(t.Context())
	assert.NilError(t, err)

	// Assert - archived/deleted tasks' sandboxes are destroyed and their
	// records removed.
	var destroyed []string
	for _, call := range be.DestroyCalls() {
		destroyed = append(destroyed, call.H.ID)
	}
	assert.DeepEqual(t, destroyed, []string{"c1", "c4"})
	for _, id := range []int64{1, 4} {
		_, ok, err := store.Read(id)
		assert.NilError(t, err)
		assert.Equal(t, ok, false)
	}
	// Kept records survive.
	_, ok, err := store.Read(2)
	assert.NilError(t, err)
	assert.Equal(t, ok, true)
}

func TestRunnerStart_ExitDuringLaunch(t *testing.T) {
	t.Parallel()
	// Arrange - a task whose container exits in the window between Launch
	// returning and the runner writing its record. Monitor must still resolve
	// the exit (not drop it) because both halves take launchMu.
	task := &model.Task{ID: 8, Runner: "test-runner", Workspace: "test", Version: 1}
	store := testStore(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	be := &backend.BackendMock{
		ValidateWorkspaceFunc: func(_ *workspace.Workspace) error { return nil },
		LaunchFunc: func(_ context.Context, _ *backend.Spec, _ *backend.Handle) (backend.Handle, error) {
			// Park inside Launch (holding launchMu) until the test releases us.
			close(entered)
			<-release
			return backend.Handle{ID: "c8"}, nil
		},
		WatchFunc: func(_ context.Context, fn func(backend.HandleExit)) error {
			fn(backend.HandleExit{ID: "c8", ExitCode: 1})
			return nil
		},
	}
	mock := &xagentclient.ClientMock{
		CreateTaskTokenFunc: func(_ context.Context, _ *xagentv1.CreateTaskTokenRequest) (*xagentv1.CreateTaskTokenResponse, error) {
			return &xagentv1.CreateTaskTokenResponse{Token: "t"}, nil
		},
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: queue, Workspaces: testWorkspaces()})
	assert.NilError(t, err)

	// Act - Start parks inside Launch holding launchMu; the die event arrives
	// while it is parked. Monitor's handler blocks on launchMu (or, if it runs
	// after the unlock, finds the committed record), so ByID resolves either
	// way and the exit is never dropped.
	startErr := make(chan error, 1)
	go func() { startErr <- r.Start(t.Context(), task) }()
	<-entered

	monErr := make(chan error, 1)
	go func() { monErr <- r.Monitor(t.Context()) }()

	close(release)
	assert.NilError(t, <-startErr)
	assert.NilError(t, <-monErr)

	// Assert - the exit resolved to task 8 and produced a failed event.
	rec, ok, err := store.Read(8)
	assert.NilError(t, err)
	assert.Equal(t, ok, true)
	assert.Equal(t, rec.ID, "c8")
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "failed")
	assert.Equal(t, events[0].TaskId, int64(8))
}

// testWorkspaces returns a minimal workspace config with a "test" workspace.
func testWorkspaces() *workspace.Config {
	return &workspace.Config{
		Workspaces: map[string]workspace.Workspace{
			"test": {
				Container: workspace.Container{Image: "alpine:latest"},
				Agent:     workspace.Agent{Type: "dummy", Dummy: &workspace.DummyConfig{}},
			},
		},
	}
}
