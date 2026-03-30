package servermcp

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/icholy/xagent/internal/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is an MCP server that proxies tool calls to the xagent
// Connect RPC service.
type Server struct {
	service xagentv1connect.XAgentServiceHandler
	baseURL string
}

// New creates a new MCP server backed by the given service handler.
func New(service xagentv1connect.XAgentServiceHandler, baseURL string) *Server {
	return &Server{service: service, baseURL: cmp.Or(baseURL, xagentclient.DefaultURL)}
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

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task",
		Description: "Get full details of a task including instructions, logs, links, and children",
	}, s.getTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List all tasks",
	}, s.listTasks)

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
		Name        string `json:"name"`
		Description string `json:"description"`
		RunnerID    string `json:"runner_id"`
	}
	result := make([]workspace, len(resp.Workspaces))
	for i, ws := range resp.Workspaces {
		result[i] = workspace{
			Name:        ws.Name,
			Description: ws.Description,
			RunnerID:    ws.RunnerId,
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
		URL       string `json:"url,omitempty"`
	}
	result := taskResult{
		ID:        resp.Task.Id,
		Name:      resp.Task.Name,
		Workspace: resp.Task.Workspace,
		Status:    resp.Task.Status.String(),
		URL:       fmt.Sprintf("%s/ui/tasks/%d", s.baseURL, resp.Task.Id),
	}
	return jsonResult(result), nil, nil
}

type getTaskInput struct {
	ID int64 `json:"id" jsonschema:"The task ID"`
}

func (s *Server) getTask(ctx context.Context, req *mcp.CallToolRequest, input getTaskInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.service.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{
		Id: input.ID,
	})
	if err != nil {
		return errorResult("failed to get task: %v", err), nil, nil
	}
	type instruction struct {
		Text string `json:"text"`
		URL  string `json:"url,omitempty"`
	}
	type logEntry struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	type link struct {
		ID        int64  `json:"id"`
		Relevance string `json:"relevance"`
		URL       string `json:"url"`
		Title     string `json:"title,omitempty"`
		Notify    bool   `json:"notify"`
	}
	type childTask struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
		Status    string `json:"status"`
	}
	type taskDetails struct {
		ID           int64         `json:"id"`
		Name         string        `json:"name"`
		Parent       int64         `json:"parent,omitempty"`
		Workspace    string        `json:"workspace"`
		Runner       string        `json:"runner,omitempty"`
		Status       string        `json:"status"`
		URL          string        `json:"url,omitempty"`
		Instructions []instruction `json:"instructions"`
		Logs         []logEntry    `json:"logs"`
		Links        []link        `json:"links"`
		Children     []childTask   `json:"children"`
	}
	task := resp.Task
	result := taskDetails{
		ID:        task.Id,
		Name:      task.Name,
		Parent:    task.Parent,
		Workspace: task.Workspace,
		Runner:    task.Runner,
		Status:    task.Status.String(),
		URL:       fmt.Sprintf("%s/ui/tasks/%d", s.baseURL, task.Id),
	}
	for _, inst := range task.Instructions {
		result.Instructions = append(result.Instructions, instruction{
			Text: inst.Text,
			URL:  inst.Url,
		})
	}
	// Fetch logs for the task
	logsResp, err := s.service.ListLogs(ctx, &xagentv1.ListLogsRequest{
		TaskId: input.ID,
	})
	if err == nil {
		for _, l := range logsResp.Entries {
			result.Logs = append(result.Logs, logEntry{
				Type:    l.Type,
				Content: l.Content,
			})
		}
	}
	for _, l := range resp.Links {
		result.Links = append(result.Links, link{
			ID:        l.Id,
			Relevance: l.Relevance,
			URL:       l.Url,
			Title:     l.Title,
			Notify:    l.Notify,
		})
	}
	for _, c := range resp.Children {
		result.Children = append(result.Children, childTask{
			ID:        c.Id,
			Name:      c.Name,
			Workspace: c.Workspace,
			Status:    c.Status.String(),
		})
	}
	return jsonResult(result), nil, nil
}

type listTasksInput struct{}

func (s *Server) listTasks(ctx context.Context, req *mcp.CallToolRequest, input listTasksInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.service.ListTasks(ctx, &xagentv1.ListTasksRequest{})
	if err != nil {
		return errorResult("failed to list tasks: %v", err), nil, nil
	}
	type taskSummary struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
		Status    string `json:"status"`
		URL       string `json:"url,omitempty"`
	}
	result := make([]taskSummary, len(resp.Tasks))
	for i, t := range resp.Tasks {
		result[i] = taskSummary{
			ID:        t.Id,
			Name:      t.Name,
			Workspace: t.Workspace,
			Status:    t.Status.String(),
			URL:       fmt.Sprintf("%s/ui/tasks/%d", s.baseURL, t.Id),
		}
	}
	return jsonResult(result), nil, nil
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
