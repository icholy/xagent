package runner

import (
	"cmp"
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/dockerx"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

func TestRunnerStart(t *testing.T) {
	// Skip if no prebuilt binaries
	prebuiltDir := cmp.Or(os.Getenv("TEST_PREBUILT_DIR"), "../../prebuilt")
	if _, err := os.Stat(prebuiltDir); os.IsNotExist(err) {
		t.Skip("prebuilt directory not found")
	}

	ctx := t.Context()

	// Create mock client
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}

	// Create httptest server with the mock
	_, handler := xagentv1connect.NewXAgentServiceHandler(mock)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Create workspace config with dummy agent
	workspaces := &workspace.Config{
		Workspaces: map[string]workspace.Workspace{
			"test": {
				Container: workspace.Container{
					Image: "alpine:latest",
				},
				Agent: workspace.Agent{
					Type: "dummy",
					Dummy: &workspace.DummyConfig{
						Sleep: 1,
					},
				},
			},
		},
	}

	// Create a temporary secret file
	secretFile := t.TempDir() + "/secret.key"

	// Create runner
	r, err := New(Options{
		ServerURL:   ts.URL,
		PrebuiltDir: prebuiltDir,
		SecretFile:  secretFile,
		Workspaces:  workspaces,
		Concurrency: 1,
		RunnerID:    "test-runner",
	})
	assert.NilError(t, err)
	t.Cleanup(func() { r.Close() })

	// Create a task
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

	// Start the task
	err = r.Start(ctx, task)
	assert.NilError(t, err)

	// Wait for the container to exit
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	assert.NilError(t, err)
	defer docker.Close()

	err = dockerx.ContainerWait(ctx, docker, "xagent-1", container.WaitConditionNotRunning)
	assert.NilError(t, err)

	// Remove the container
	err = docker.ContainerRemove(ctx, "xagent-1", container.RemoveOptions{})
	assert.NilError(t, err)
}
