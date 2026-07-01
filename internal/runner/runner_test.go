package runner

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

	// Create runner. The agent connects to the server directly over the network, so
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
		// Start spawns a supervise goroutine; park it until the test's ctx ends.
		WaitFunc: func(ctx context.Context, _ backend.Handle) (backend.ExitCode, error) {
			<-ctx.Done()
			return 0, ctx.Err()
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

func TestRunnerSupervise_ReportLost(t *testing.T) {
	t.Parallel()
	// Arrange - Wait reports the exit as lost (non-zero code): the driver's
	// terminal report never reached the server, so the runner emits "failed".
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, _ *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	be := &backend.BackendMock{
		WaitFunc: func(_ context.Context, h backend.Handle) (backend.ExitCode, error) {
			assert.Equal(t, h.ID, "c2")
			return backend.ExitLost, nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: testStore(t), RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)
	assert.Assert(t, r.sem.TryAcquire(1)) // the slot supervise will release

	// Act
	r.supervise(t.Context(), 2, backend.Handle{ID: "c2"})

	// Assert - a failed event is emitted and the concurrency slot is released.
	events := submitted(t, mock, queue)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Event, "failed")
	assert.Equal(t, events[0].TaskId, int64(2))
	assert.Equal(t, events[0].Version, int64(0))
	assert.Assert(t, r.sem.TryAcquire(1)) // the slot was released
}

func TestRunnerSupervise_CleanExit(t *testing.T) {
	t.Parallel()
	// Arrange - Wait returns a clean exit (0): the driver already reported, so
	// no event is owed, but the slot is still released.
	mock := &xagentclient.ClientMock{}
	be := &backend.BackendMock{
		WaitFunc: func(_ context.Context, _ backend.Handle) (backend.ExitCode, error) {
			return 0, nil
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: testStore(t), RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)
	assert.Assert(t, r.sem.TryAcquire(1))

	// Act
	r.supervise(t.Context(), 3, backend.Handle{ID: "c3"})

	// Assert - no event, slot released.
	assert.Equal(t, queue.Len(), 0)
	assert.Assert(t, r.sem.TryAcquire(1))
}

func TestRunnerSupervise_Shutdown(t *testing.T) {
	t.Parallel()
	// Arrange - Wait returns context.Canceled: the runner is shutting down, the
	// sandbox stays alive for next-boot rehydration. No event, slot NOT released.
	mock := &xagentclient.ClientMock{}
	be := &backend.BackendMock{
		WaitFunc: func(_ context.Context, _ backend.Handle) (backend.ExitCode, error) {
			return 0, context.Canceled
		},
	}
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	r, err := New(Options{Client: mock, Backend: be, Store: testStore(t), RunnerID: "test-runner", Concurrency: 1, Queue: queue})
	assert.NilError(t, err)
	assert.Assert(t, r.sem.TryAcquire(1))

	// Act
	r.supervise(t.Context(), 4, backend.Handle{ID: "c4"})

	// Assert - no event, and the slot is still held (not released on shutdown).
	assert.Equal(t, queue.Len(), 0)
	assert.Assert(t, !r.sem.TryAcquire(1))
}

func TestRunnerLoad(t *testing.T) {
	t.Parallel()
	// Arrange - a running sandbox (re-attached), an exited husk whose task is
	// still running (lost-report backstop), and a gone sandbox whose record is
	// dropped.
	status := map[int64]xagentv1.TaskStatus{
		2: xagentv1.TaskStatus_RUNNING,
		3: xagentv1.TaskStatus_RUNNING,
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
		"c3": backend.StateGone,
	}
	waited := make(chan string, 1)
	be := &backend.BackendMock{
		ProbeFunc: func(_ context.Context, h backend.Handle) (backend.State, error) {
			return state[h.ID], nil
		},
		WaitFunc: func(ctx context.Context, h backend.Handle) (backend.ExitCode, error) {
			waited <- h.ID // the running sandbox got a supervise goroutine
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}
	store := testStore(t,
		taskstate.Record{TaskID: 1, Type: "docker", ID: "c1"},
		taskstate.Record{TaskID: 2, Type: "docker", ID: "c2"},
		taskstate.Record{TaskID: 3, Type: "docker", ID: "c3"},
	)
	queue := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 5, Queue: queue})
	assert.NilError(t, err)

	// Act
	err = r.Load(ctx)
	assert.NilError(t, err)

	// Assert - the exited husk and gone sandbox (both still-running tasks) each
	// emit "failed"; the gone record is dropped, the others kept.
	events := submitted(t, mock, queue)
	var failed []int64
	for _, e := range events {
		assert.Equal(t, e.Event, "failed")
		failed = append(failed, e.TaskId)
	}
	assert.DeepEqual(t, failed, []int64{2, 3})

	_, ok, err := store.Read(3)
	assert.NilError(t, err)
	assert.Equal(t, ok, false) // gone record dropped
	_, ok, err = store.Read(1)
	assert.NilError(t, err)
	assert.Equal(t, ok, true) // running record kept

	// The running sandbox was re-attached via a supervise goroutine.
	select {
	case id := <-waited:
		assert.Equal(t, id, "c1")
	case <-time.After(3 * time.Second):
		t.Fatal("running sandbox was not supervised")
	}

	// The semaphore was seeded with the one running sandbox.
	assert.Assert(t, r.sem.TryAcquire(4))
	assert.Assert(t, !r.sem.TryAcquire(1))
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

func TestRunnerStart_Gone(t *testing.T) {
	t.Parallel()
	// Arrange - a task whose recorded sandbox has vanished. Launch returns
	// ErrGone (it never creates a fresh sandbox on the reuse path), so Start
	// drops the dangling record and propagates ErrGone to its caller.
	task := &model.Task{ID: 8, Runner: "test-runner", Workspace: "test", Version: 1}
	store := testStore(t, taskstate.Record{TaskID: 8, Type: "docker", ID: "old-id"})
	be := &backend.BackendMock{
		ValidateWorkspaceFunc: func(_ *workspace.Workspace) error { return nil },
		ProbeFunc: func(_ context.Context, _ backend.Handle) (backend.State, error) {
			// Probe races Launch; report exited so the reuse handle is passed on
			// and Launch's ErrGone guard is the authoritative one.
			return backend.StateExited, nil
		},
		LaunchFunc: func(_ context.Context, _ *backend.Spec, reuse *backend.Handle) (backend.Handle, error) {
			assert.Assert(t, reuse != nil)
			return backend.Handle{}, backend.ErrGone
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

	// Assert - ErrGone surfaced (the caller emits failed + releases the slot) and
	// the dangling record was removed. No supervise goroutine was spawned.
	assert.Assert(t, errors.Is(err, backend.ErrGone))
	_, ok, err := store.Read(8)
	assert.NilError(t, err)
	assert.Equal(t, ok, false)
}

func TestRunnerStart_GoneViaProbe(t *testing.T) {
	t.Parallel()
	// Arrange - the pre-Launch Probe already reports the sandbox gone, so Start
	// short-circuits without minting a token or calling Launch.
	task := &model.Task{ID: 9, Runner: "test-runner", Workspace: "test", Version: 1}
	store := testStore(t, taskstate.Record{TaskID: 9, Type: "docker", ID: "old-id"})
	be := &backend.BackendMock{
		ProbeFunc: func(_ context.Context, _ backend.Handle) (backend.State, error) {
			return backend.StateGone, nil
		},
	}
	mock := &xagentclient.ClientMock{}
	r, err := New(Options{Client: mock, Backend: be, Store: store, RunnerID: "test-runner", Concurrency: 1, Queue: NewEventQueue(EventQueueOptions{Client: mock})})
	assert.NilError(t, err)

	// Act
	err = r.Start(t.Context(), task)

	// Assert - gone short-circuits: ErrGone, record dropped, no Launch, no token.
	assert.Assert(t, errors.Is(err, backend.ErrGone))
	assert.Equal(t, len(be.LaunchCalls()), 0)
	assert.Equal(t, len(mock.CreateTaskTokenCalls()), 0)
	_, ok, err := store.Read(9)
	assert.NilError(t, err)
	assert.Equal(t, ok, false)
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
