package command

import (
	"context"
	"encoding/json"
	"fmt"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/encoding/protojson"
)

// Input types for each tool

type CreateLinkInput struct {
	Relevance string `json:"relevance" jsonschema:"description=Describe how this link is relevant to the task,required"`
	URL       string `json:"url" jsonschema:"description=URL of the external resource,required"`
	Title     string `json:"title,omitempty" jsonschema:"description=Optional display title for the link"`
	Created   bool   `json:"created,omitempty" jsonschema:"description=True if this task created the resource false if it's just related"`
}

type ReportInput struct {
	Message string `json:"message" jsonschema:"description=The message to report,required"`
}

type CreateChildTaskInput struct {
	Name        string `json:"name" jsonschema:"description=A short name for the task,required"`
	Instruction string `json:"instruction" jsonschema:"description=The instruction text for the task,required"`
	URL         string `json:"url,omitempty" jsonschema:"description=Optional URL associated with the instruction (e.g. GitHub issue Jira ticket)"`
}

type UpdateTaskInput struct {
	Name string `json:"name" jsonschema:"description=The new name for the task,required"`
}

type AddChildTaskInstructionInput struct {
	TaskID      int64  `json:"task_id" jsonschema:"description=The child task ID,required"`
	Instruction string `json:"instruction" jsonschema:"description=The instruction text to add,required"`
	URL         string `json:"url,omitempty" jsonschema:"description=Optional URL associated with the instruction"`
}

type ListChildTaskLogsInput struct {
	TaskID int64 `json:"task_id" jsonschema:"description=The child task ID,required"`
}

type ListChildTaskLinksInput struct {
	TaskID int64 `json:"task_id" jsonschema:"description=The child task ID,required"`
}

type AddChildTaskEventInput struct {
	TaskID  int64 `json:"task_id" jsonschema:"description=The child task ID,required"`
	EventID int64 `json:"event_id" jsonschema:"description=The event ID to add,required"`
}

var McpCommand = &cli.Command{
	Name:  "mcp",
	Usage: "Run an MCP server that provides xagent tools",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
		&cli.Int64Flag{
			Name:     "task",
			Aliases:  []string{"t"},
			Usage:    "Task ID",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "workspace",
			Aliases:  []string{"w"},
			Usage:    "Workspace name",
			Required: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		taskID := cmd.Int64("task")
		workspace := cmd.String("workspace")
		client := xagentclient.New(cmd.String("server"))

		s := mcp.NewServer(&mcp.Implementation{
			Name:    "xagent",
			Version: "1.0.0",
		}, nil)

		mcp.AddTool(s, &mcp.Tool{
			Name:        "create_link",
			Description: "Associate an external resource (PR, Jira ticket, etc.) with the current task",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input CreateLinkInput) (*mcp.CallToolResult, any, error) {
			if input.Relevance == "" {
				return errorResult("relevance is required"), nil, nil
			}
			if input.URL == "" {
				return errorResult("url is required"), nil, nil
			}

			_, err := client.CreateLink(ctx, &xagentv1.CreateLinkRequest{
				TaskId:    taskID,
				Relevance: input.Relevance,
				Url:       input.URL,
				Title:     input.Title,
				Created:   input.Created,
			})
			if err != nil {
				return errorResult("failed to create link: %v", err), nil, nil
			}

			return textResult("Link created: %s", input.URL), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "report",
			Description: "Report a problem or log message for the current task",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input ReportInput) (*mcp.CallToolResult, any, error) {
			if input.Message == "" {
				return errorResult("message is required"), nil, nil
			}
			_, err := client.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
				TaskId: taskID,
				Entries: []*xagentv1.LogEntry{
					{Type: "llm", Content: input.Message},
				},
			})
			if err != nil {
				return errorResult("failed to upload log: %v", err), nil, nil
			}

			return textResult("Report submitted"), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_task",
			Description: "Get the current task instructions, links, events, and children",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
			resp, err := client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: taskID})
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

			task := map[string]any{
				"name":         resp.Task.Name,
				"instructions": instructions,
				"links":        links,
				"events":       events,
				"children":     children,
			}

			data, _ := json.MarshalIndent(task, "", "  ")
			return textResult(string(data)), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "create_child_task",
			Description: "Create a child task of the current task",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input CreateChildTaskInput) (*mcp.CallToolResult, any, error) {
			if input.Name == "" {
				return errorResult("name is required"), nil, nil
			}
			if input.Instruction == "" {
				return errorResult("instruction is required"), nil, nil
			}

			resp, err := client.CreateTask(ctx, &xagentv1.CreateTaskRequest{
				Name:      input.Name,
				Parent:    taskID,
				Workspace: workspace,
				Instructions: []*xagentv1.Instruction{
					{Text: input.Instruction, Url: input.URL},
				},
			})
			if err != nil {
				return errorResult("failed to create task: %v", err), nil, nil
			}

			return textResult("Task created: %d", resp.Task.Id), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "update_task",
			Description: "Update the current task's name",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input UpdateTaskInput) (*mcp.CallToolResult, any, error) {
			if input.Name == "" {
				return errorResult("name is required"), nil, nil
			}
			_, err := client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
				Id:   taskID,
				Name: input.Name,
			})
			if err != nil {
				return errorResult("failed to update task: %v", err), nil, nil
			}

			return textResult("Task updated"), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_child_tasks",
			Description: "List child tasks spawned by the current task",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
			resp, err := client.ListChildTasks(ctx, &xagentv1.ListChildTasksRequest{
				ParentId: taskID,
			})
			if err != nil {
				return errorResult("failed to list children: %v", err), nil, nil
			}

			data, _ := protojson.MarshalOptions{Indent: "  "}.Marshal(resp)
			return textResult(string(data)), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "add_child_task_instruction",
			Description: "Add an instruction to a child task and restart it",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input AddChildTaskInstructionInput) (*mcp.CallToolResult, any, error) {
			if input.TaskID == 0 {
				return errorResult("task_id is required"), nil, nil
			}
			if input.Instruction == "" {
				return errorResult("instruction is required"), nil, nil
			}

			// Verify we are the parent
			childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
			if err != nil {
				return errorResult("failed to get child task: %v", err), nil, nil
			}
			if childResp.Task.Parent != taskID {
				return errorResult("task is not a child of the current task"), nil, nil
			}

			_, err = client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
				Id:     input.TaskID,
				Status: "restarting",
				AddInstructions: []*xagentv1.Instruction{
					{Text: input.Instruction, Url: input.URL},
				},
			})
			if err != nil {
				return errorResult("failed to add instruction: %v", err), nil, nil
			}

			return textResult("Instruction added to task %d", input.TaskID), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_child_task_logs",
			Description: "List logs for a child task",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input ListChildTaskLogsInput) (*mcp.CallToolResult, any, error) {
			if input.TaskID == 0 {
				return errorResult("task_id is required"), nil, nil
			}

			// Verify we are the parent
			childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
			if err != nil {
				return errorResult("failed to get child task: %v", err), nil, nil
			}
			if childResp.Task.Parent != taskID {
				return errorResult("task is not a child of the current task"), nil, nil
			}

			logsResp, err := client.ListLogs(ctx, &xagentv1.ListLogsRequest{TaskId: input.TaskID})
			if err != nil {
				return errorResult("failed to list logs: %v", err), nil, nil
			}

			data, _ := protojson.MarshalOptions{Indent: "  "}.Marshal(logsResp)
			return textResult(string(data)), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "list_child_task_links",
			Description: "List links for a child task",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input ListChildTaskLinksInput) (*mcp.CallToolResult, any, error) {
			if input.TaskID == 0 {
				return errorResult("task_id is required"), nil, nil
			}

			// Verify we are the parent
			childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
			if err != nil {
				return errorResult("failed to get child task: %v", err), nil, nil
			}
			if childResp.Task.Parent != taskID {
				return errorResult("task is not a child of the current task"), nil, nil
			}

			linksResp, err := client.ListLinks(ctx, &xagentv1.ListLinksRequest{TaskId: input.TaskID})
			if err != nil {
				return errorResult("failed to list links: %v", err), nil, nil
			}

			data, _ := protojson.MarshalOptions{Indent: "  "}.Marshal(linksResp)
			return textResult(string(data)), nil, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "add_child_task_event",
			Description: "Add an event to a child task",
		}, func(ctx context.Context, req *mcp.CallToolRequest, input AddChildTaskEventInput) (*mcp.CallToolResult, any, error) {
			if input.TaskID == 0 {
				return errorResult("task_id is required"), nil, nil
			}
			if input.EventID == 0 {
				return errorResult("event_id is required"), nil, nil
			}

			// Verify we are the parent
			childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: input.TaskID})
			if err != nil {
				return errorResult("failed to get child task: %v", err), nil, nil
			}
			if childResp.Task.Parent != taskID {
				return errorResult("task is not a child of the current task"), nil, nil
			}

			_, err = client.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
				EventId: input.EventID,
				TaskId:  input.TaskID,
			})
			if err != nil {
				return errorResult("failed to add event to task: %v", err), nil, nil
			}

			return textResult("Event %d added to task %d", input.EventID, input.TaskID), nil, nil
		})

		return s.Run(ctx, &mcp.StdioTransport{})
	},
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
