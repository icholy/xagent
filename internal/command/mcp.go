package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/icholy/xagent/internal/mcpx"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/encoding/protojson"
)

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

		s := server.NewMCPServer(
			"xagent",
			"1.0.0",
			server.WithToolCapabilities(true),
		)

		s.AddTool(
			mcp.NewTool("create_link",
				mcp.WithDescription("Associate an external resource (PR, Jira ticket, etc.) with the current task"),
				mcp.WithString("relevance",
					mcp.Required(),
					mcp.Description("Describe how this link is relevant to the task"),
				),
				mcp.WithString("url",
					mcp.Required(),
					mcp.Description("URL of the external resource"),
				),
				mcp.WithString("title",
					mcp.Description("Optional display title for the link"),
				),
				mcp.WithBoolean("created",
					mcp.Description("True if this task created the resource, false if it's just related"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				relevance, _ := mcpx.StringArgument(req, "relevance")
				if relevance == "" {
					return mcp.NewToolResultErrorf("relevance is required"), nil
				}
				url, _ := mcpx.StringArgument(req, "url")
				if url == "" {
					return mcp.NewToolResultErrorf("url is required"), nil
				}
				title, _ := mcpx.StringArgument(req, "title")
				created, _ := mcpx.BoolArgument(req, "created")

				_, err := client.CreateLink(ctx, &xagentv1.CreateLinkRequest{
					TaskId:    taskID,
					Relevance: relevance,
					Url:       url,
					Title:     title,
					Created:   created,
				})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to create link: %v", err), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Link created: %s", url)), nil
			},
		)

		s.AddTool(
			mcp.NewTool("report",
				mcp.WithDescription("Report a problem or log message for the current task"),
				mcp.WithString("message",
					mcp.Required(),
					mcp.Description("The message to report"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				message, _ := mcpx.StringArgument(req, "message")
				if message == "" {
					return mcp.NewToolResultErrorf("message is required"), nil
				}
				_, err := client.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
					TaskId: taskID,
					Entries: []*xagentv1.LogEntry{
						{Type: "llm", Content: message},
					},
				})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to upload log: %v", err), nil
				}

				return mcp.NewToolResultText("Report submitted"), nil
			},
		)

		s.AddTool(
			mcp.NewTool("get_task",
				mcp.WithDescription("Get the current task instructions, links, events, and children"),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				resp, err := client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: taskID})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to get task: %v", err), nil
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
				return mcp.NewToolResultText(string(data)), nil
			},
		)

		s.AddTool(
			mcp.NewTool("create_child_task",
				mcp.WithDescription("Create a child task of the current task"),
				mcp.WithString("name",
					mcp.Required(),
					mcp.Description("A short name for the task"),
				),
				mcp.WithString("instruction",
					mcp.Required(),
					mcp.Description("The instruction text for the task"),
				),
				mcp.WithString("url",
					mcp.Description("Optional URL associated with the instruction (e.g., GitHub issue, Jira ticket)"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				name, _ := mcpx.StringArgument(req, "name")
				if name == "" {
					return mcp.NewToolResultErrorf("name is required"), nil
				}
				instruction, _ := mcpx.StringArgument(req, "instruction")
				if instruction == "" {
					return mcp.NewToolResultErrorf("instruction is required"), nil
				}
				url, _ := mcpx.StringArgument(req, "url")

				resp, err := client.CreateTask(ctx, &xagentv1.CreateTaskRequest{
					Name:      name,
					Parent:    taskID,
					Workspace: workspace,
					Instructions: []*xagentv1.Instruction{
						{Text: instruction, Url: url},
					},
				})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to create task: %v", err), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Task created: %d", resp.Task.Id)), nil
			},
		)

		s.AddTool(
			mcp.NewTool("update_task",
				mcp.WithDescription("Update the current task's name"),
				mcp.WithString("name",
					mcp.Required(),
					mcp.Description("The new name for the task"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				name, _ := mcpx.StringArgument(req, "name")
				if name == "" {
					return mcp.NewToolResultErrorf("name is required"), nil
				}
				_, err := client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
					Id:   taskID,
					Name: name,
				})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to update task: %v", err), nil
				}

				return mcp.NewToolResultText("Task updated"), nil
			},
		)

		s.AddTool(
			mcp.NewTool("list_child_tasks",
				mcp.WithDescription("List child tasks spawned by the current task"),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				resp, err := client.ListChildTasks(ctx, &xagentv1.ListChildTasksRequest{
					ParentId: taskID,
				})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to list children: %v", err), nil
				}

				data, _ := protojson.MarshalOptions{Indent: "  "}.Marshal(resp)
				return mcp.NewToolResultText(string(data)), nil
			},
		)

		s.AddTool(
			mcp.NewTool("add_child_task_instruction",
				mcp.WithDescription("Add an instruction to a child task and restart it"),
				mcp.WithNumber("task_id",
					mcp.Required(),
					mcp.Description("The child task ID"),
				),
				mcp.WithString("instruction",
					mcp.Required(),
					mcp.Description("The instruction text to add"),
				),
				mcp.WithString("url",
					mcp.Description("Optional URL associated with the instruction"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				childTaskID, ok := mcpx.Int64Argument(req, "task_id")
				if !ok {
					return mcp.NewToolResultErrorf("task_id is required"), nil
				}
				instruction, _ := mcpx.StringArgument(req, "instruction")
				if instruction == "" {
					return mcp.NewToolResultErrorf("instruction is required"), nil
				}
				url, _ := mcpx.StringArgument(req, "url")

				// Verify we are the parent
				childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: childTaskID})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to get child task: %v", err), nil
				}
				if childResp.Task.Parent != taskID {
					return mcp.NewToolResultErrorf("task is not a child of the current task"), nil
				}

				_, err = client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
					Id:     childTaskID,
					Status: "restarting",
					AddInstructions: []*xagentv1.Instruction{
						{Text: instruction, Url: url},
					},
				})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to add instruction: %v", err), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Instruction added to task %d", childTaskID)), nil
			},
		)

		s.AddTool(
			mcp.NewTool("list_child_task_logs",
				mcp.WithDescription("List logs for a child task"),
				mcp.WithNumber("task_id",
					mcp.Required(),
					mcp.Description("The child task ID"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				childTaskID, ok := mcpx.Int64Argument(req, "task_id")
				if !ok {
					return mcp.NewToolResultErrorf("task_id is required"), nil
				}

				// Verify we are the parent
				childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: childTaskID})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to get child task: %v", err), nil
				}
				if childResp.Task.Parent != taskID {
					return mcp.NewToolResultErrorf("task is not a child of the current task"), nil
				}

				logsResp, err := client.ListLogs(ctx, &xagentv1.ListLogsRequest{TaskId: childTaskID})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to list logs: %v", err), nil
				}

				data, _ := protojson.MarshalOptions{Indent: "  "}.Marshal(logsResp)
				return mcp.NewToolResultText(string(data)), nil
			},
		)

		s.AddTool(
			mcp.NewTool("list_child_task_links",
				mcp.WithDescription("List links for a child task"),
				mcp.WithNumber("task_id",
					mcp.Required(),
					mcp.Description("The child task ID"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				childTaskID, ok := mcpx.Int64Argument(req, "task_id")
				if !ok {
					return mcp.NewToolResultErrorf("task_id is required"), nil
				}

				// Verify we are the parent
				childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: childTaskID})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to get child task: %v", err), nil
				}
				if childResp.Task.Parent != taskID {
					return mcp.NewToolResultErrorf("task is not a child of the current task"), nil
				}

				linksResp, err := client.ListLinks(ctx, &xagentv1.ListLinksRequest{TaskId: childTaskID})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to list links: %v", err), nil
				}

				data, _ := protojson.MarshalOptions{Indent: "  "}.Marshal(linksResp)
				return mcp.NewToolResultText(string(data)), nil
			},
		)

		s.AddTool(
			mcp.NewTool("add_child_task_event",
				mcp.WithDescription("Add an event to a child task"),
				mcp.WithNumber("task_id",
					mcp.Required(),
					mcp.Description("The child task ID"),
				),
				mcp.WithNumber("event_id",
					mcp.Required(),
					mcp.Description("The event ID to add"),
				),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				childTaskID, ok := mcpx.Int64Argument(req, "task_id")
				if !ok {
					return mcp.NewToolResultErrorf("task_id is required"), nil
				}
				eventID, ok := mcpx.Int64Argument(req, "event_id")
				if !ok {
					return mcp.NewToolResultErrorf("event_id is required"), nil
				}

				// Verify we are the parent
				childResp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: childTaskID})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to get child task: %v", err), nil
				}
				if childResp.Task.Parent != taskID {
					return mcp.NewToolResultErrorf("task is not a child of the current task"), nil
				}

				_, err = client.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
					EventId: eventID,
					TaskId:  childTaskID,
				})
				if err != nil {
					return mcp.NewToolResultErrorf("failed to add event to task: %v", err), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Event %d added to task %d", eventID, childTaskID)), nil
			},
		)

		return server.ServeStdio(s)
	},
}
