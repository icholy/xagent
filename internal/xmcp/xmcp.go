package xmcp

import (
	"context"
	"encoding/json"
	"fmt"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Server struct {
	client    xagentclient.Client
	taskID    int64
	workspace string
}

func NewServer(client xagentclient.Client, taskID int64, workspace string) *Server {
	return &Server{
		client:    client,
		taskID:    taskID,
		workspace: workspace,
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
		Name:        "get_task",
		Description: "Get the current task instructions, links, events, and children",
	}, s.getTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_child_task",
		Description: "Create a child task of the current task",
	}, s.createChildTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_task",
		Description: "Update the current task's name",
	}, s.updateTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_child_tasks",
		Description: "List child tasks spawned by the current task",
	}, s.listChildTasks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_child_task",
		Description: "Update a child task by adding an instruction and/or events, then start it",
	}, s.updateChildTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_child_task_logs",
		Description: "List logs for a child task",
	}, s.listChildTaskLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_child_task_links",
		Description: "List links for a child task",
	}, s.listChildTaskLinks)
}

type createLinkInput struct {
	Relevance string `json:"relevance" jsonschema:"description=Describe how this link is relevant to the task,required"`
	URL       string `json:"url" jsonschema:"description=URL of the external resource,required"`
	Title     string `json:"title,omitempty" jsonschema:"description=Optional display title for the link"`
	Created   bool   `json:"created,omitempty" jsonschema:"description=True if this task created the resource false if it's just related"`
}

func (s *Server) createLink(ctx context.Context, req *mcp.CallToolRequest, input createLinkInput) (*mcp.CallToolResult, any, error) {
	_, err := s.client.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    s.taskID,
		Relevance: input.Relevance,
		Url:       input.URL,
		Title:     input.Title,
		Created:   input.Created,
	})
	if err != nil {
		return errorResult("failed to create link: %v", err), nil, nil
	}

	return textResult("Link created: %s", input.URL), nil, nil
}

type reportInput struct {
	Message string `json:"message" jsonschema:"description=The message to report,required"`
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

func (s *Server) getTask(ctx context.Context, req *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: s.taskID})
	if err != nil {
		return errorResult("failed to get task: %v", err), nil, nil
	}

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

	return jsonResult(map[string]any{
		"name":         resp.Task.Name,
		"instructions": instructions,
		"links":        links,
		"events":       events,
		"children":     children,
	}), nil, nil
}

type createChildTaskInput struct {
	Name        string `json:"name" jsonschema:"description=A short name for the task,required"`
	Instruction string `json:"instruction" jsonschema:"description=The instruction text for the task,required"`
	URL         string `json:"url,omitempty" jsonschema:"description=Optional URL associated with the instruction (e.g. GitHub issue Jira ticket)"`
}

func (s *Server) createChildTask(ctx context.Context, req *mcp.CallToolRequest, input createChildTaskInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      input.Name,
		Parent:    s.taskID,
		Workspace: s.workspace,
		Instructions: []*xagentv1.Instruction{
			{Text: input.Instruction, Url: input.URL},
		},
	})
	if err != nil {
		return errorResult("failed to create task: %v", err), nil, nil
	}

	return textResult("Task created: %d", resp.Task.Id), nil, nil
}

type updateTaskInput struct {
	Name string `json:"name" jsonschema:"description=The new name for the task,required"`
}

func (s *Server) updateTask(ctx context.Context, req *mcp.CallToolRequest, input updateTaskInput) (*mcp.CallToolResult, any, error) {
	_, err := s.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:   s.taskID,
		Name: input.Name,
	})
	if err != nil {
		return errorResult("failed to update task: %v", err), nil, nil
	}

	return textResult("Task updated"), nil, nil
}

func (s *Server) listChildTasks(ctx context.Context, req *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.ListChildTasks(ctx, &xagentv1.ListChildTasksRequest{
		ParentId: s.taskID,
	})
	if err != nil {
		return errorResult("failed to list children: %v", err), nil, nil
	}

	return protojsonResult(resp), nil, nil
}

type updateChildTaskInput struct {
	TaskID      int64   `json:"task_id" jsonschema:"description=The child task ID,required"`
	Instruction string  `json:"instruction,omitempty" jsonschema:"description=Optional instruction text to add"`
	URL         string  `json:"url,omitempty" jsonschema:"description=Optional URL associated with the instruction"`
	EventIDs    []int64 `json:"event_ids,omitempty" jsonschema:"description=Optional array of event IDs to add to the task"`
}

func (s *Server) updateChildTask(ctx context.Context, req *mcp.CallToolRequest, input updateChildTaskInput) (*mcp.CallToolResult, any, error) {
	if input.Instruction == "" && len(input.EventIDs) == 0 {
		return errorResult("instruction or event_ids is required"), nil, nil
	}

	// Verify we are the parent
	childResp, err := s.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
	if err != nil {
		return errorResult("failed to get child task: %v", err), nil, nil
	}
	if childResp.Task.Parent != s.taskID {
		return errorResult("task is not a child of the current task"), nil, nil
	}

	// Add events
	for _, eventID := range input.EventIDs {
		_, err = s.client.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
			EventId: eventID,
			TaskId:  input.TaskID,
		})
		if err != nil {
			return errorResult("failed to add event %d: %v", eventID, err), nil, nil
		}
	}

	// Add instruction and restart
	updateReq := &xagentv1.UpdateTaskRequest{
		Id:     input.TaskID,
		Status: "restarting",
	}
	if input.Instruction != "" {
		updateReq.AddInstructions = []*xagentv1.Instruction{
			{Text: input.Instruction, Url: input.URL},
		}
	}

	_, err = s.client.UpdateTask(ctx, updateReq)
	if err != nil {
		return errorResult("failed to update task: %v", err), nil, nil
	}

	return textResult("Task %d updated and started", input.TaskID), nil, nil
}

type listChildTaskLogsInput struct {
	TaskID int64 `json:"task_id" jsonschema:"description=The child task ID,required"`
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

type listChildTaskLinksInput struct {
	TaskID int64 `json:"task_id" jsonschema:"description=The child task ID,required"`
}

func (s *Server) listChildTaskLinks(ctx context.Context, req *mcp.CallToolRequest, input listChildTaskLinksInput) (*mcp.CallToolResult, any, error) {
	// Verify we are the parent
	childResp, err := s.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
	if err != nil {
		return errorResult("failed to get child task: %v", err), nil, nil
	}
	if childResp.Task.Parent != s.taskID {
		return errorResult("task is not a child of the current task"), nil, nil
	}

	linksResp, err := s.client.ListLinks(ctx, &xagentv1.ListLinksRequest{TaskId: input.TaskID})
	if err != nil {
		return errorResult("failed to list links: %v", err), nil, nil
	}

	return protojsonResult(linksResp), nil, nil
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
