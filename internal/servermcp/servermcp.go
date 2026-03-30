package servermcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/icholy/xagent/internal/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is an MCP server that proxies tool calls to the xagent
// Connect RPC service. It exposes list_workspaces and create_task tools.
type Server struct {
	service xagentv1connect.XAgentServiceHandler
}

// New creates a new MCP server backed by the given service handler.
func New(service xagentv1connect.XAgentServiceHandler) *Server {
	return &Server{service: service}
}

// Handler returns an http.Handler that serves the MCP Streamable HTTP
// protocol. The handler expects auth middleware to have set UserInfo
// in the request context.
func (s *Server) Handler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "xagent",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List available workspaces",
	}, s.listWorkspaces)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a new task",
	}, s.createTask)

	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if apiauth.Caller(r.Context()) == nil {
			return nil
		}
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
}

type listWorkspacesInput struct{}

func (s *Server) listWorkspaces(ctx context.Context, req *mcp.CallToolRequest, input listWorkspacesInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.service.ListWorkspaces(ctx, &xagentv1.ListWorkspacesRequest{})
	if err != nil {
		return errorResult("failed to list workspaces: %v", err), nil, nil
	}
	type workspace struct {
		Name     string `json:"name"`
		RunnerID string `json:"runner_id"`
	}
	result := make([]workspace, len(resp.Workspaces))
	for i, ws := range resp.Workspaces {
		result[i] = workspace{
			Name:     ws.Name,
			RunnerID: ws.RunnerId,
		}
	}
	return jsonResult(result), nil, nil
}

type createTaskInput struct {
	Name        string `json:"name,omitempty" jsonschema:"A short name for the task"`
	Workspace   string `json:"workspace" jsonschema:"The workspace to run the task in"`
	Instruction string `json:"instruction" jsonschema:"The instruction text for the task"`
	Runner      string `json:"runner,omitempty" jsonschema:"Optional runner ID to target"`
}

func (s *Server) createTask(ctx context.Context, req *mcp.CallToolRequest, input createTaskInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.service.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      input.Name,
		Workspace: input.Workspace,
		Runner:    input.Runner,
		Instructions: []*xagentv1.Instruction{
			{Text: input.Instruction},
		},
	})
	if err != nil {
		return errorResult("failed to create task: %v", err), nil, nil
	}
	type taskResult struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
		Status    string `json:"status"`
	}
	return jsonResult(taskResult{
		ID:        resp.Task.Id,
		Name:      resp.Task.Name,
		Workspace: resp.Task.Workspace,
		Status:    resp.Task.Status.String(),
	}), nil, nil
}

func errorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult("failed to format response: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}
}
