package runner

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/x/dockerx"
	"github.com/icholy/xagent/internal/x/wakeup"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

func TestRunnerStart(t *testing.T) {
	abs, err := filepath.Abs("../../prebuilt")
	assert.NilError(t, err)
	t.Setenv("XAGENT_PREBUILT_DIR", abs)

	task := &model.Task{
		ID:        1,
		Name:      "test-task",
		Runner:    "test-runner",
		Workspace: "test",
		Instructions: []model.Instruction{
			{Text: "Hello from test"},
		},
		Status:  model.TaskStatusPending,
		Command: model.TaskCommandStart,
		Version: 1,
	}

	// Create mock client
	mock := &xagentclient.ClientMock{
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

	privateKey, err := agentauth.CreatePrivateKey()
	assert.NilError(t, err)

	// Create runner
	client := xagentclient.New(xagentclient.Options{BaseURL: ts.URL})
	r, err := New(Options{
		Client:     client,
		PrivateKey: privateKey,
		Queue:      NewEventQueue(EventQueueOptions{Client: client, Log: slog.Default()}),
		Workspaces: &workspace.Config{
			Workspaces: map[string]workspace.Workspace{
				"test": {
					Container: workspace.Container{
						Image: "alpine:latest",
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
		SocketPath:  filepath.Join(t.TempDir(), "socket"),
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

	// Wait for the container to exit
	err = dockerx.ContainerWait(t.Context(), docker, "xagent-1", container.WaitConditionNotRunning)
	assert.NilError(t, err)

	// Remove the container
	err = docker.ContainerRemove(t.Context(), "xagent-1", container.RemoveOptions{})
	assert.NilError(t, err)

	// Verify get_my_task was called
	assert.Equal(t, len(mock.GetTaskDetailsCalls()), 1)
}

func TestRunnerWakeChannel(t *testing.T) {
	r := &Runner{wake: wakeup.New()}

	// No signal yet → WakeC should be empty.
	select {
	case <-r.WakeC():
		t.Fatal("WakeC fired before Wake was called")
	default:
	}

	// A single Wake delivers one value.
	r.wake.Wake()
	select {
	case <-r.WakeC():
	default:
		t.Fatal("WakeC did not fire after Wake")
	}

	// Bursts coalesce into a single delivery.
	r.wake.Wake()
	r.wake.Wake()
	r.wake.Wake()
	select {
	case <-r.WakeC():
	default:
		t.Fatal("WakeC did not fire after burst Wake")
	}
	select {
	case <-r.WakeC():
		t.Fatal("WakeC fired more than once for a coalesced burst")
	default:
	}
}
