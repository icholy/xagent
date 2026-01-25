package xmcp

//go:generate go tool moq -pkg xmcp -out client_moq_test.go ../xagentclient Client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Server struct {
	client    xagentclient.Client
	taskID    int64
	runner    string
	workspace string
}

func NewServer(client xagentclient.Client, taskID int64, runner, workspace string) *Server {
	return &Server{
		client:    client,
		taskID:    taskID,
		runner:    runner,
		workspace: workspace,
	}
}

func (s *Server) log(ctx context.Context, format string, args ...any) {
	_, err := s.client.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: s.taskID,
		Entries: []*xagentv1.LogEntry{
			{Type: "mcp", Content: fmt.Sprintf(format, args...)},
		},
	})
	if err != nil {
		slog.Warn("failed to upload log", "error", err)
	}
}

func (s *Server) AddTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_link",
		Description: "Associate an external resource (PR, Jira ticket, etc.) with the current task",
	}, s.createLink)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report",
		Description: "Report a problem or log message for the current task",
	}, s.report)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_my_task",
		Description: "Get the current task instructions, links, events, and children",
	}, s.getMyTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_child_task",
		Description: "Create a child task of the current task",
	}, s.createChildTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_my_task",
		Description: "Update the current task's name",
	}, s.updateMyTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_child_tasks",
		Description: "Get details of child tasks spawned by the current task",
	}, s.listChildTasks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_child_task",
		Description: "Update a child task by adding an instruction, then start it",
	}, s.updateChildTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_child_task_logs",
		Description: "List logs for a child task",
	}, s.listChildTaskLogs)

}

type createLinkInput struct {
	Relevance string `json:"relevance" jsonschema:"Describe how this link is relevant to the task"`
	URL       string `json:"url" jsonschema:"URL of the external resource"`
	Title     string `json:"title,omitempty" jsonschema:"Optional display title for the link"`
	Notify    bool   `json:"notify,omitempty" jsonschema:"True to receive events for this link"`
}

func (s *Server) createLink(ctx context.Context, req *mcp.CallToolRequest, input createLinkInput) (*mcp.CallToolResult, any, error) {
	_, err := s.client.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    s.taskID,
		Relevance: input.Relevance,
		Url:       input.URL,
		Title:     input.Title,
		Notify:    input.Notify,
	})
	if err != nil {
		return errorResult("failed to create link: %v", err), nil, nil
	}

	s.log(ctx, "created link: %s", input.URL)
	return textResult("Link created: %s", input.URL), nil, nil
}

type reportInput struct {
	Message string `json:"message" jsonschema:"The message to report"`
}

func (s *Server) report(ctx context.Context, req *mcp.CallToolRequest, input reportInput) (*mcp.CallToolResult, any, error) {
	_, err := s.client.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: s.taskID,
		Entries: []*xagentv1.LogEntry{
			{Type: "llm", Content: input.Message},
		},
	})
	if err != nil {
		return errorResult("failed to upload log: %v", err), nil, nil
	}

	return textResult("Report submitted"), nil, nil
}

func (s *Server) getMyTask(ctx context.Context, req *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: s.taskID})
	if err != nil {
		return errorResult("failed to get task: %v", err), nil, nil
	}

	return jsonResult(taskDetailsToMap(resp)), nil, nil
}

type createChildTaskInput struct {
	Name        string `json:"name" jsonschema:"A short name for the task"`
	Instruction string `json:"instruction" jsonschema:"The instruction text for the task"`
	URL         string `json:"url,omitempty" jsonschema:"Optional URL associated with the instruction (e.g. GitHub issue Jira ticket)"`
}

func (s *Server) createChildTask(ctx context.Context, req *mcp.CallToolRequest, input createChildTaskInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      input.Name,
		Parent:    s.taskID,
		Runner:    s.runner,
		Workspace: s.workspace,
		Instructions: []*xagentv1.Instruction{
			{Text: input.Instruction, Url: input.URL},
		},
	})
	if err != nil {
		return errorResult("failed to create task: %v", err), nil, nil
	}

	s.log(ctx, "created child task: %d (%s)", resp.Task.Id, input.Name)
	return textResult("Task created: %d", resp.Task.Id), nil, nil
}

type updateMyTaskInput struct {
	Name string `json:"name" jsonschema:"The new name for the task"`
}

func (s *Server) updateMyTask(ctx context.Context, req *mcp.CallToolRequest, input updateMyTaskInput) (*mcp.CallToolResult, any, error) {
	_, err := s.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:   s.taskID,
		Name: input.Name,
	})
	if err != nil {
		return errorResult("failed to update task: %v", err), nil, nil
	}

	s.log(ctx, "updated task name: %s", input.Name)
	return textResult("Task updated"), nil, nil
}

func (s *Server) listChildTasks(ctx context.Context, req *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.ListChildTasks(ctx, &xagentv1.ListChildTasksRequest{
		ParentId: s.taskID,
	})
	if err != nil {
		return errorResult("failed to list children: %v", err), nil, nil
	}

	children := make([]map[string]any, 0, len(resp.Tasks))
	for _, task := range resp.Tasks {
		details, err := s.client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: task.Id})
		if err != nil {
			return errorResult("failed to get child task %d: %v", task.Id, err), nil, nil
		}
		children = append(children, taskDetailsToMap(details))
	}

	return jsonResult(children), nil, nil
}

type updateChildTaskInput struct {
	TaskID      int64  `json:"task_id" jsonschema:"The child task ID"`
	Instruction string `json:"instruction" jsonschema:"Instruction text to add"`
	URL         string `json:"url,omitempty" jsonschema:"Optional URL associated with the instruction"`
}

func (s *Server) updateChildTask(ctx context.Context, req *mcp.CallToolRequest, input updateChildTaskInput) (*mcp.CallToolResult, any, error) {
	// Verify we are the parent
	childResp, err := s.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
	if err != nil {
		return errorResult("failed to get child task: %v", err), nil, nil
	}
	if childResp.Task.Parent != s.taskID {
		return errorResult("task is not a child of the current task"), nil, nil
	}
	if childResp.Task.Status == "archived" {
		return errorResult("cannot update archived task"), nil, nil
	}

	_, err = s.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:    input.TaskID,
		Start: true,
		AddInstructions: []*xagentv1.Instruction{
			{Text: input.Instruction, Url: input.URL},
		},
	})
	if err != nil {
		return errorResult("failed to update task: %v", err), nil, nil
	}

	s.log(ctx, "updated child task: %d", input.TaskID)
	return textResult("Task %d updated and started", input.TaskID), nil, nil
}

type listChildTaskLogsInput struct {
	TaskID int64 `json:"task_id" jsonschema:"The child task ID"`
}

func (s *Server) listChildTaskLogs(ctx context.Context, req *mcp.CallToolRequest, input listChildTaskLogsInput) (*mcp.CallToolResult, any, error) {
	// Verify we are the parent
	childResp, err := s.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
	if err != nil {
		return errorResult("failed to get child task: %v", err), nil, nil
	}
	if childResp.Task.Parent != s.taskID {
		return errorResult("task is not a child of the current task"), nil, nil
	}

	logsResp, err := s.client.ListLogs(ctx, &xagentv1.ListLogsRequest{TaskId: input.TaskID})
	if err != nil {
		return errorResult("failed to list logs: %v", err), nil, nil
	}

	return protojsonResult(logsResp), nil, nil
}

func textResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
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

func protojsonResult(m proto.Message) *mcp.CallToolResult {
	data, err := protojson.MarshalOptions{Indent: "  "}.Marshal(m)
	if err != nil {
		return errorResult("failed to format response: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
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

// taskDetailsToMap converts a GetTaskDetailsResponse to a map for JSON output.
func taskDetailsToMap(resp *xagentv1.GetTaskDetailsResponse) map[string]any {
	marshalOpts := protojson.MarshalOptions{Indent: "  "}

	instructions := make([]json.RawMessage, len(resp.Task.Instructions))
	for i, inst := range resp.Task.Instructions {
		instructions[i], _ = marshalOpts.Marshal(inst)
	}

	links := make([]json.RawMessage, len(resp.GetLinks()))
	for i, link := range resp.GetLinks() {
		links[i], _ = marshalOpts.Marshal(link)
	}

	events := make([]json.RawMessage, len(resp.GetEvents()))
	for i, event := range resp.GetEvents() {
		events[i], _ = marshalOpts.Marshal(event)
	}

	children := make([]json.RawMessage, len(resp.GetChildren()))
	for i, child := range resp.GetChildren() {
		children[i], _ = marshalOpts.Marshal(child)
	}

	return map[string]any{
		"id":           resp.Task.Id,
		"name":         resp.Task.Name,
		"status":       resp.Task.Status,
		"workspace":    resp.Task.Workspace,
		"instructions": instructions,
		"links":        links,
		"events":       events,
		"children":     children,
	}
}
