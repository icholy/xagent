package mcpbridge

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Instructions describes the mute-by-default channel behaviour to the
// agent. It is appended to the bridge's server instructions only when
// --channel is enabled, so an agent knows why completions don't arrive
// unless it opts in.
const Instructions = "Channel notifications about task status changes are MUTED BY DEFAULT. " +
	"To be notified when a task is queued, woken, completed, failed, or cancelled, " +
	"call watch_task(task_id) for each task you want situational awareness on. " +
	"A task stays watched until you call unwatch_task(task_id); use " +
	"list_watched_tasks to introspect. " +
	"This is separate from create_link(subscribe=true), which routes inbound external events to a task."

// AddTools registers the watch_task / unwatch_task / list_watched_tasks
// tools on server. Called by the bridge only when --channel is enabled
// (without channels there is nothing to subscribe to). These are bridge
// tools: they mutate the local subscription set and do not proxy to the
// C2 RPC API.
func (c *Channel) AddTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "watch_task",
		Description: "Receive Claude Code channel notifications for a task's " +
			"status changes (queued, woken, completed, failed, cancelled). " +
			"Channel notifications are muted by default — call this for each " +
			"task you want situational awareness on. A task stays watched " +
			"until you call unwatch_task. Distinct from " +
			"create_link(subscribe=true), which routes external events to a task.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		TaskID int64 `json:"task_id" jsonschema:"The task ID to watch"`
	}) (*mcp.CallToolResult, any, error) {
		c.watch(in.TaskID)
		return jsonResult(map[string]any{"watching": c.watched()}), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "unwatch_task",
		Description: "Stop receiving Claude Code channel notifications for a " +
			"task. Idempotent.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		TaskID int64 `json:"task_id" jsonschema:"The task ID to unwatch"`
	}) (*mcp.CallToolResult, any, error) {
		c.unwatch(in.TaskID)
		return jsonResult(map[string]any{"watching": c.watched()}), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_watched_tasks",
		Description: "List the task IDs currently watched for Claude Code " +
			"channel notifications.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return jsonResult(map[string]any{"watching": c.watched()}), nil, nil
	})
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "failed to format response: " + err.Error()},
			},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}
}
