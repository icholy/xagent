package xmcp

import (
	"context"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExternalServer provides MCP tools for external agents not managed by xagent.
// Unlike Server, it doesn't bind to a specific task at startup.
type ExternalServer struct {
	client xagentclient.Client
}

// NewExternalServer creates a new ExternalServer.
func NewExternalServer(client xagentclient.Client) *ExternalServer {
	return &ExternalServer{client: client}
}

// AddTools registers the external mode tools with the MCP server.
func (s *ExternalServer) AddTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a new task in a workspace",
	}, s.createTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task",
		Description: "Get task details by ID including instructions, links, events, and children",
	}, s.getTask)
}

type createTaskInput struct {
	Name        string `json:"name" jsonschema:"A short name for the task"`
	Runner      string `json:"runner" jsonschema:"The runner ID to assign the task to"`
	Workspace   string `json:"workspace" jsonschema:"The workspace to create the task in"`
	Instruction string `json:"instruction" jsonschema:"The instruction text for the task"`
	URL         string `json:"url,omitempty" jsonschema:"Optional URL associated with the instruction"`
}

func (s *ExternalServer) createTask(ctx context.Context, req *mcp.CallToolRequest, input createTaskInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      input.Name,
		Runner:    input.Runner,
		Workspace: input.Workspace,
		Instructions: []*xagentv1.Instruction{
			{Text: input.Instruction, Url: input.URL},
		},
	})
	if err != nil {
		return errorResult("failed to create task: %v", err), nil, nil
	}

	return jsonResult(map[string]any{
		"id":        resp.Task.Id,
		"name":      resp.Task.Name,
		"runner":    resp.Task.Runner,
		"workspace": resp.Task.Workspace,
		"status":    resp.Task.Status,
	}), nil, nil
}

type getTaskInput struct {
	TaskID int64 `json:"task_id" jsonschema:"The task ID to retrieve"`
}

func (s *ExternalServer) getTask(ctx context.Context, req *mcp.CallToolRequest, input getTaskInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: input.TaskID})
	if err != nil {
		return errorResult("failed to get task: %v", err), nil, nil
	}

	return jsonResult(taskDetailsToMap(resp)), nil, nil
}
