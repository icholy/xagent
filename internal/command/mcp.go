package command

import (
	"context"
	"encoding/json"
	"fmt"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/urfave/cli/v3"
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
		&cli.StringFlag{
			Name:     "task",
			Aliases:  []string{"t"},
			Usage:    "Task ID",
			Required: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		taskID := cmd.String("task")
		client := xagentclient.New(cmd.String("server"))

		s := server.NewMCPServer(
			"xagent",
			"1.0.0",
			server.WithToolCapabilities(true),
		)

		s.AddTool(
			mcp.NewTool("create_link",
				mcp.WithDescription("Associate an external resource (PR, Jira ticket, etc.) with the current task"),
				mcp.WithString("type",
					mcp.Required(),
					mcp.Description("Type of link: 'pr', 'jira', 'issue', etc."),
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
				args := req.GetArguments()
				linkType, _ := args["type"].(string)
				url, _ := args["url"].(string)
				title, _ := args["title"].(string)
				created, _ := args["created"].(bool)

				if linkType == "" || url == "" {
					return mcp.NewToolResultError("type and url are required"), nil
				}

				_, err := client.CreateLink(ctx, &xagentv1.CreateLinkRequest{
					TaskId:  taskID,
					Type:    linkType,
					Url:     url,
					Title:   title,
					Created: created,
				})
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to create link: %v", err)), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Link created: %s (%s)", url, linkType)), nil
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
				args := req.GetArguments()
				message, _ := args["message"].(string)

				if message == "" {
					return mcp.NewToolResultError("message is required"), nil
				}

				_, err := client.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
					TaskId: taskID,
					Entries: []*xagentv1.LogEntry{
						{Type: "llm", Content: message},
					},
				})
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to upload log: %v", err)), nil
				}

				return mcp.NewToolResultText("Report submitted"), nil
			},
		)

		s.AddTool(
			mcp.NewTool("get_task",
				mcp.WithDescription("Get the current task instructions and links"),
			),
			func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				resp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to get task: %v", err)), nil
				}

				links, _ := client.ListLinks(ctx, &xagentv1.ListLinksRequest{TaskId: taskID})

				instructions := make([]map[string]string, len(resp.Task.Instructions))
				for i, inst := range resp.Task.Instructions {
					instructions[i] = map[string]string{
						"text":   inst.Text,
						"origin": inst.Origin,
					}
				}

				task := map[string]any{
					"instructions": instructions,
					"links":        links.GetLinks(),
				}

				data, _ := json.MarshalIndent(task, "", "  ")
				return mcp.NewToolResultText(string(data)), nil
			},
		)

		return server.ServeStdio(s)
	},
}
