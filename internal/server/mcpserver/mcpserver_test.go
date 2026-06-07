package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

func setupSession(t *testing.T, client *xagentclient.ClientMock, opts ...Option) *mcp.ClientSession {
	t.Helper()
	srv := NewServer(client, opts...)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	_, err := srv.Connect(t.Context(), serverTransport, nil)
	assert.NilError(t, err)

	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1.0.0"}, nil)
	session, err := c.Connect(t.Context(), clientTransport, nil)
	assert.NilError(t, err)
	t.Cleanup(func() { session.Close() })
	return session
}

func TestListTools(t *testing.T) {
	session := setupSession(t, &xagentclient.ClientMock{})

	resp, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
	assert.NilError(t, err)

	got := make([]string, len(resp.Tools))
	for i, tool := range resp.Tools {
		got[i] = tool.Name
	}
	assert.DeepEqual(t, got, []string{
		"archive_task",
		"create_task",
		"get_task",
		"list_tasks",
		"list_workspaces",
		"update_task",
	})
}

func TestArchiveTask(t *testing.T) {
	client := &xagentclient.ClientMock{
		ArchiveTaskFunc: func(ctx context.Context, req *xagentv1.ArchiveTaskRequest) (*xagentv1.ArchiveTaskResponse, error) {
			assert.Equal(t, req.Id, int64(42))
			return &xagentv1.ArchiveTaskResponse{}, nil
		},
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			assert.Equal(t, req.Id, int64(42))
			return &xagentv1.GetTaskResponse{
				Task: &xagentv1.Task{
					Id:        42,
					Name:      "test",
					Workspace: "ws",
					Status:    xagentv1.TaskStatus_COMPLETED,
					Url:       "https://xagent.example.com/ui/tasks/42?org=7",
				},
			}, nil
		},
	}
	session := setupSession(t, client)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "archive_task",
		Arguments: map[string]any{
			"id": 42,
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)

	text, ok := result.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok)
	var got map[string]any
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.DeepEqual(t, got, map[string]any{
		"id":        float64(42),
		"name":      "test",
		"workspace": "ws",
		"status":    "COMPLETED",
		"url":       "https://xagent.example.com/ui/tasks/42?org=7",
	})
}

func TestCreateTask_UsesServerURL(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateTaskFunc: func(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
			assert.Equal(t, req.Workspace, "ws")
			assert.Equal(t, req.Runner, "r1")
			assert.Equal(t, len(req.Instructions), 1)
			assert.Equal(t, req.Instructions[0].Text, "do it")
			return &xagentv1.CreateTaskResponse{
				Task: &xagentv1.Task{
					Id:        42,
					Name:      "test",
					Workspace: "ws",
					Status:    xagentv1.TaskStatus_PENDING,
					Url:       "https://xagent.example.com/ui/tasks/42?org=7",
				},
			}, nil
		},
	}
	session := setupSession(t, client)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"workspace":   "ws",
			"instruction": "do it",
			"runner":      "r1",
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)

	text, ok := result.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok)
	var got map[string]any
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.DeepEqual(t, got, map[string]any{
		"id":        float64(42),
		"name":      "test",
		"workspace": "ws",
		"status":    "PENDING",
		"url":       "https://xagent.example.com/ui/tasks/42?org=7",
	})
}

// createTaskCapture returns a ClientMock whose CreateTaskFunc records the
// request it received and returns a minimal valid task.
func createTaskCapture(got **xagentv1.CreateTaskRequest) *xagentclient.ClientMock {
	return &xagentclient.ClientMock{
		CreateTaskFunc: func(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
			*got = req
			return &xagentv1.CreateTaskResponse{
				Task: &xagentv1.Task{
					Id:        1,
					Name:      "t",
					Workspace: "ws",
					Status:    xagentv1.TaskStatus_PENDING,
				},
			}, nil
		},
	}
}

func callCreateTask(t *testing.T, session *mcp.ClientSession, args map[string]any) {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "create_task",
		Arguments: args,
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)
}

func TestCreateTask_ForwardsArchiveAfter(t *testing.T) {
	var got *xagentv1.CreateTaskRequest
	session := setupSession(t, createTaskCapture(&got))

	callCreateTask(t, session, map[string]any{
		"workspace":     "ws",
		"instruction":   "do it",
		"runner":        "r1",
		"archive_after": "30m",
	})

	assert.Assert(t, got.ArchiveAfter != nil)
	assert.Equal(t, got.ArchiveAfter.AsDuration(), 30*time.Minute)
}

func TestCreateTask_AppliesServerDefaultArchiveAfter(t *testing.T) {
	var got *xagentv1.CreateTaskRequest
	session := setupSession(t, createTaskCapture(&got), WithDefaultArchiveAfter(time.Hour))

	callCreateTask(t, session, map[string]any{
		"workspace":   "ws",
		"instruction": "do it",
		"runner":      "r1",
	})

	assert.Assert(t, got.ArchiveAfter != nil)
	assert.Equal(t, got.ArchiveAfter.AsDuration(), time.Hour)
}

func TestCreateTask_ParamOverridesDefaultArchiveAfter(t *testing.T) {
	var got *xagentv1.CreateTaskRequest
	session := setupSession(t, createTaskCapture(&got), WithDefaultArchiveAfter(time.Hour))

	callCreateTask(t, session, map[string]any{
		"workspace":     "ws",
		"instruction":   "do it",
		"runner":        "r1",
		"archive_after": "-1s",
	})

	assert.Assert(t, got.ArchiveAfter != nil)
	assert.Equal(t, got.ArchiveAfter.AsDuration(), -time.Second)
}

func TestCreateTask_NoArchiveAfterWhenUnset(t *testing.T) {
	var got *xagentv1.CreateTaskRequest
	session := setupSession(t, createTaskCapture(&got))

	callCreateTask(t, session, map[string]any{
		"workspace":   "ws",
		"instruction": "do it",
		"runner":      "r1",
	})

	assert.Assert(t, got.ArchiveAfter == nil, "archive_after should be unset when neither param nor default is set")
}

func TestCreateTask_InvalidArchiveAfter(t *testing.T) {
	var got *xagentv1.CreateTaskRequest
	session := setupSession(t, createTaskCapture(&got))

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"workspace":     "ws",
			"instruction":   "do it",
			"runner":        "r1",
			"archive_after": "not-a-duration",
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, result.IsError, "expected error result for invalid duration")
	assert.Assert(t, got == nil, "CreateTask should not be called when archive_after is invalid")
}

func TestListTasks_UsesTaskURLFromResponse(t *testing.T) {
	client := &xagentclient.ClientMock{
		ListTasksFunc: func(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
			return &xagentv1.ListTasksResponse{
				Tasks: []*xagentv1.Task{
					{
						Id:        1,
						Name:      "t1",
						Workspace: "ws",
						Status:    xagentv1.TaskStatus_RUNNING,
						Url:       "https://xagent.example.com/ui/tasks/1?org=7",
					},
				},
			}, nil
		},
	}
	session := setupSession(t, client)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_tasks",
		Arguments: map[string]any{},
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)

	text, ok := result.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok)
	var got []map[string]any
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.DeepEqual(t, got, []map[string]any{
		{
			"id":        float64(1),
			"name":      "t1",
			"workspace": "ws",
			"status":    "RUNNING",
			"url":       "https://xagent.example.com/ui/tasks/1?org=7",
		},
	})
}
