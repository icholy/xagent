package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/icholy/xagent/internal/auth/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Instructions is the prompt shown to MCP clients describing what the
// xagent server offers.
const Instructions = "xagent is an async agent orchestrator that runs AI coding agents in parallel inside Docker containers.\n" +
	"Use it to create and manage tasks that execute in isolated workspaces.\n" +
	"Workspaces define the container image, volumes, environment variables, and MCP servers available to agents.\n" +
	"Each task runs an AI coding agent with access to the codebase and configured tools.\n" +
	"Agents attach links to their tasks for external resources they create, such as GitHub PRs or Jira issues."

// Option configures the behaviour of the registered tools.
type Option func(*handlers)

// WithDefaultArchiveAfter sets the default archive_after applied to tasks
// created via create_task when the call omits the archive_after param. See
// Task.archive_after for the value semantics (zero/unset = never, negative
// = archive immediately on terminal status, positive = delay).
func WithDefaultArchiveAfter(d time.Duration) Option {
	return func(h *handlers) {
		h.defaultArchiveAfter = durationpb.New(d)
	}
}

// NewServer builds an MCP server with the user-facing xagent tools
// registered. The same setup is used by the HTTP handler and by the
// local stdio command.
func NewServer(service xagentv1connect.XAgentServiceHandler, opts ...Option) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "xagent",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Instructions: Instructions,
	})
	AddTools(server, service, opts...)
	return server
}

// AddTools registers the user-facing xagent tools on the given MCP server.
// Tool handlers proxy to the supplied service, which can be either an
// in-process XAgentServiceHandler (server-side) or the generated Connect
// client (remote) since both interfaces share the same method signatures.
func AddTools(server *mcp.Server, service xagentv1connect.XAgentServiceHandler, opts ...Option) {
	h := &handlers{service: service}
	for _, opt := range opts {
		opt(h)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List available workspaces",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, h.listWorkspaces)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a new task",
	}, h.createTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task",
		Description: "Get full details of a task including instructions, logs, links, and children",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, h.getTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tasks",
		Description: "List all tasks",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, h.listTasks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_task",
		Description: "Add an instruction to a task, optionally start it",
	}, h.updateTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "archive_task",
		Description: "Archive a task. The task must be in a terminal state (completed, failed, or cancelled) — running or pending tasks cannot be archived.",
	}, h.archiveTask)
}

// Handler returns an http.Handler that serves the MCP Streamable HTTP
// protocol. The handler expects auth middleware to have set UserInfo in
// the request context.
func Handler(service xagentv1connect.XAgentServiceHandler) http.Handler {
	server := NewServer(service)
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if apiauth.Caller(r.Context()) == nil {
			return nil
		}
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
}

type handlers struct {
	service xagentv1connect.XAgentServiceHandler
	// defaultArchiveAfter is applied to create_task when the call omits the
	// archive_after param. nil means no default is sent (server behavior
	// unchanged).
	defaultArchiveAfter *durationpb.Duration
}

type listWorkspacesInput struct{}

func (h *handlers) listWorkspaces(ctx context.Context, req *mcp.CallToolRequest, input listWorkspacesInput) (*mcp.CallToolResult, any, error) {
	resp, err := h.service.ListWorkspaces(ctx, &xagentv1.ListWorkspacesRequest{})
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
	Name         string `json:"name,omitempty" jsonschema:"A short name for the task"`
	Workspace    string `json:"workspace" jsonschema:"The workspace to run the task in"`
	Instruction  string `json:"instruction" jsonschema:"The instruction text for the task"`
	Runner       string `json:"runner" jsonschema:"Runner ID to target"`
	ArchiveAfter string `json:"archive_after,omitempty" jsonschema:"Auto-archive the task this long after it reaches a terminal status, as a Go duration string (e.g. \"30m\", \"1h\"). \"0\" = never, a negative value like \"-1s\" = archive immediately on terminal status, positive = delay. When omitted, the server default is used."`
}

func (h *handlers) createTask(ctx context.Context, req *mcp.CallToolRequest, input createTaskInput) (*mcp.CallToolResult, any, error) {
	archiveAfter := h.defaultArchiveAfter
	if input.ArchiveAfter != "" {
		d, err := time.ParseDuration(input.ArchiveAfter)
		if err != nil {
			return errorResult("invalid archive_after %q: %v", input.ArchiveAfter, err), nil, nil
		}
		archiveAfter = durationpb.New(d)
	}
	resp, err := h.service.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      input.Name,
		Workspace: input.Workspace,
		Runner:    input.Runner,
		Instructions: []*xagentv1.Instruction{
			{Text: input.Instruction},
		},
		ArchiveAfter: archiveAfter,
	})
	if err != nil {
		return errorResult("failed to create task: %v", err), nil, nil
	}
	return jsonResult(taskSummaryOf(resp.Task)), nil, nil
}

type getTaskInput struct {
	ID int64 `json:"id" jsonschema:"The task ID"`
}

func (h *handlers) getTask(ctx context.Context, req *mcp.CallToolRequest, input getTaskInput) (*mcp.CallToolResult, any, error) {
	resp, err := h.service.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{
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
		Subscribe bool   `json:"subscribe"`
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
		URL:       task.Url,
	}
	for _, inst := range task.Instructions {
		result.Instructions = append(result.Instructions, instruction{
			Text: inst.Text,
			URL:  inst.Url,
		})
	}
	logsResp, err := h.service.ListLogs(ctx, &xagentv1.ListLogsRequest{
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
			Subscribe: l.Subscribe,
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

func (h *handlers) listTasks(ctx context.Context, req *mcp.CallToolRequest, input listTasksInput) (*mcp.CallToolResult, any, error) {
	resp, err := h.service.ListTasks(ctx, &xagentv1.ListTasksRequest{})
	if err != nil {
		return errorResult("failed to list tasks: %v", err), nil, nil
	}
	result := make([]taskSummary, len(resp.Tasks))
	for i, t := range resp.Tasks {
		result[i] = taskSummaryOf(t)
	}
	return jsonResult(result), nil, nil
}

type updateTaskInput struct {
	ID          int64  `json:"id" jsonschema:"The task ID to update"`
	Instruction string `json:"instruction" jsonschema:"Instruction text to add to the task"`
	URL         string `json:"url,omitempty" jsonschema:"Optional URL associated with the instruction (e.g. GitHub issue, Jira ticket)"`
	Start       bool   `json:"start,omitempty" jsonschema:"Start the task (non-interrupting if already running)"`
}

func (h *handlers) updateTask(ctx context.Context, req *mcp.CallToolRequest, input updateTaskInput) (*mcp.CallToolResult, any, error) {
	_, err := h.service.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:    input.ID,
		Start: input.Start,
		AddInstructions: []*xagentv1.Instruction{
			{Text: input.Instruction, Url: input.URL},
		},
	})
	if err != nil {
		return errorResult("failed to update task: %v", err), nil, nil
	}
	resp, err := h.service.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.ID})
	if err != nil {
		return errorResult("failed to get updated task: %v", err), nil, nil
	}
	return jsonResult(taskSummaryOf(resp.Task)), nil, nil
}

type archiveTaskInput struct {
	ID int64 `json:"id" jsonschema:"The task ID to archive"`
}

func (h *handlers) archiveTask(ctx context.Context, req *mcp.CallToolRequest, input archiveTaskInput) (*mcp.CallToolResult, any, error) {
	_, err := h.service.ArchiveTask(ctx, &xagentv1.ArchiveTaskRequest{
		Id: input.ID,
	})
	if err != nil {
		return errorResult("failed to archive task: %v", err), nil, nil
	}
	resp, err := h.service.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.ID})
	if err != nil {
		return errorResult("failed to get archived task: %v", err), nil, nil
	}
	return jsonResult(taskSummaryOf(resp.Task)), nil, nil
}

type taskSummary struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	Status    string `json:"status"`
	URL       string `json:"url,omitempty"`
}

func taskSummaryOf(t *xagentv1.Task) taskSummary {
	return taskSummary{
		ID:        t.Id,
		Name:      t.Name,
		Workspace: t.Workspace,
		Status:    t.Status.String(),
		URL:       t.Url,
	}
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
