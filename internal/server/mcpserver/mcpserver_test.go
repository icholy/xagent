package mcpserver

import (
	"context"
	"testing"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/x/mcptest"
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

	var got map[string]any
	mcptest.UnmarshalCallToolResult(t, result, &got)
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

	var got map[string]any
	mcptest.UnmarshalCallToolResult(t, result, &got)
	assert.DeepEqual(t, got, map[string]any{
		"id":        float64(42),
		"name":      "test",
		"workspace": "ws",
		"status":    "PENDING",
		"url":       "https://xagent.example.com/ui/tasks/42?org=7",
	})
}

func TestCreateTask_AutoArchiveParam(t *testing.T) {
	// With no server default: a provided param is parsed and forwarded, and
	// omitting it leaves auto_archive unset.
	var got *xagentv1.CreateTaskRequest
	client := &xagentclient.ClientMock{
		CreateTaskFunc: func(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
			got = req
			return &xagentv1.CreateTaskResponse{
				Task: &xagentv1.Task{Id: 1, Workspace: "ws", Status: xagentv1.TaskStatus_PENDING},
			}, nil
		},
	}
	session := setupSession(t, client)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"workspace":    "ws",
			"instruction":  "do it",
			"runner":       "r1",
			"auto_archive": "30m",
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)
	assert.Assert(t, got.AutoArchive != nil)
	assert.Equal(t, got.AutoArchive.AsDuration(), 30*time.Minute)

	got = nil
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"workspace":   "ws",
			"instruction": "do it",
			"runner":      "r1",
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)
	assert.Assert(t, got.AutoArchive == nil, "auto_archive should be unset when neither param nor default is set")
}

func TestCreateTask_DefaultAutoArchive(t *testing.T) {
	// With a server default: it applies when the param is omitted, and the
	// per-call param overrides it.
	var got *xagentv1.CreateTaskRequest
	client := &xagentclient.ClientMock{
		CreateTaskFunc: func(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
			got = req
			return &xagentv1.CreateTaskResponse{
				Task: &xagentv1.Task{Id: 1, Workspace: "ws", Status: xagentv1.TaskStatus_PENDING},
			}, nil
		},
	}
	session := setupSession(t, client, WithDefaultAutoArchive(time.Hour))

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
	assert.Assert(t, got.AutoArchive != nil)
	assert.Equal(t, got.AutoArchive.AsDuration(), time.Hour)

	got = nil
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "create_task",
		Arguments: map[string]any{
			"workspace":    "ws",
			"instruction":  "do it",
			"runner":       "r1",
			"auto_archive": "-1s",
		},
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)
	assert.Assert(t, got.AutoArchive != nil)
	assert.Equal(t, got.AutoArchive.AsDuration(), -time.Second)
}

func TestGetTask_EventNative(t *testing.T) {
	var eventFetches int
	client := &xagentclient.ClientMock{
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			assert.Equal(t, req.Id, int64(7))
			return &xagentv1.GetTaskResponse{
				Task: &xagentv1.Task{
					Id:        7,
					Name:      "test",
					Workspace: "ws",
					Runner:    "r1",
					Status:    xagentv1.TaskStatus_RUNNING,
					Url:       "https://xagent.example.com/ui/tasks/7?org=7",
				},
			}, nil
		},
		ListEventsByTaskFunc: func(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
			eventFetches++
			assert.Equal(t, req.TaskId, int64(7))
			// The full stream, all arms, no type filter.
			assert.Equal(t, len(req.Types), 0)
			return &xagentv1.ListEventsByTaskResponse{
				Events: []*xagentv1.Event{
					{Id: 1, TaskId: 7, Payload: &xagentv1.Event_Instruction{
						Instruction: &xagentv1.InstructionPayload{Text: "do it"},
					}},
					{Id: 2, TaskId: 7, Payload: &xagentv1.Event_Report{
						Report: &xagentv1.ReportPayload{Content: "working"},
					}},
				},
			}, nil
		},
		ListLinksFunc: func(ctx context.Context, req *xagentv1.ListLinksRequest) (*xagentv1.ListLinksResponse, error) {
			assert.Equal(t, req.TaskId, int64(7))
			return &xagentv1.ListLinksResponse{
				Links: []*xagentv1.TaskLink{
					{Id: 10, Relevance: "pr", Url: "https://example.com/pr/1", Subscribe: true},
				},
			}, nil
		},
	}
	session := setupSession(t, client)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "get_task",
		Arguments: map[string]any{"id": 7},
	})
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "unexpected error result: %v", result.Content)

	// The event stream is fetched exactly once — no separate brief fetch.
	assert.Equal(t, eventFetches, 1)

	var got map[string]any
	mcptest.UnmarshalCallToolResult(t, result, &got)

	// Header + links + raw event stream, no synthesized projections.
	_, hasInstructions := got["instructions"]
	assert.Assert(t, !hasInstructions, "instructions projection should be dropped")
	_, hasLogs := got["logs"]
	assert.Assert(t, !hasLogs, "logs projection should be dropped")

	assert.Equal(t, got["id"], float64(7))
	assert.Equal(t, got["name"], "test")
	assert.Equal(t, got["workspace"], "ws")
	assert.Equal(t, got["runner"], "r1")
	assert.Equal(t, got["status"], "RUNNING")

	links, ok := got["links"].([]any)
	assert.Assert(t, ok, "links should be a list")
	assert.Equal(t, len(links), 1)

	// The raw all-arms stream, in stream order (instruction then report).
	events, ok := got["events"].([]any)
	assert.Assert(t, ok, "events should be a list")
	assert.Equal(t, len(events), 2)
	first := events[0].(map[string]any)
	_, hasInstructionArm := first["instruction"]
	assert.Assert(t, hasInstructionArm, "first event should carry the instruction arm")
	second := events[1].(map[string]any)
	_, hasReportArm := second["report"]
	assert.Assert(t, hasReportArm, "second event should carry the report arm")
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

	var got []map[string]any
	mcptest.UnmarshalCallToolResult(t, result, &got)
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
