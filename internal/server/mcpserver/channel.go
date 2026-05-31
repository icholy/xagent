package mcpserver

import (
	"context"
	"slices"
	"strconv"
	"sync"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ChannelInstructions describes the mute-by-default channel behaviour to
// the agent. It is appended to the bridge's server instructions only when
// --channel is enabled, so an agent knows why completions don't arrive
// unless it opts in.
const ChannelInstructions = "Channel notifications about task status changes are MUTED BY DEFAULT. " +
	"To be notified when a task is queued, woken, completed, failed, or cancelled, " +
	"call watch_task(task_id) for each task you want situational awareness on. " +
	"A task stays watched until you call unwatch_task(task_id); use " +
	"list_watched_tasks to introspect."

// ChannelSender is the subset of *mcpchannel.Transport the bridge needs
// to push a channel notification. Defined here so the gate can be tested
// without a live stdio transport.
type ChannelSender interface {
	SendChannel(ctx context.Context, p mcpchannel.Params) error
}

// Channel owns the per-process subscription set and the mute-by-default
// forwarding gate for the local stdio bridge. One Channel is created per
// `xagent mcp --channel` process.
//
// The subscription set is in-memory, per-bridge-process state, not server
// state: it mirrors where the existing client-side channel filters live
// (the ChannelMessage gate and own-ClientID suppression). It depends on
// mcpchannel for the transport/Params primitives and speaks
// model.Notification on top.
type Channel struct {
	sender ChannelSender

	mu  sync.Mutex
	ids map[int64]struct{} // watched task ids; empty == muted
}

// NewChannel returns a Channel that forwards through sender with an empty
// (muted) subscription set.
func NewChannel(sender ChannelSender) *Channel {
	return &Channel{sender: sender, ids: map[int64]struct{}{}}
}

func (c *Channel) watching(id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.ids[id]
	return ok
}

func (c *Channel) watched() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]int64, 0, len(c.ids))
	for id := range c.ids {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// findTaskID returns the id of the first task resource in the
// notification, if any.
func findTaskID(n model.Notification) (int64, bool) {
	for _, r := range n.Resources {
		if r.Type == "task" {
			return r.ID, true
		}
	}
	return 0, false
}

// Forward applies the gate and pushes a channel notification when the
// notification is channel-worthy AND names a task this agent is watching.
// It returns nil when the notification is gated out and otherwise returns
// the result of sending it; the caller (the command glue) handles logging.
//
// Subscriptions are purely explicit: a task stays watched until
// unwatch_task is called. The gate carries no task-status awareness.
func (c *Channel) Forward(ctx context.Context, n model.Notification) error {
	if n.ChannelMessage == "" {
		return nil // summary gate: not channel-worthy
	}
	id, ok := findTaskID(n)
	if !ok || !c.watching(id) {
		return nil // mute-by-default: not a task this agent is watching
	}
	return c.sender.SendChannel(ctx, mcpchannel.Params{
		Content: n.ChannelMessage,
		Meta:    map[string]string{"resource": "task", "id": strconv.FormatInt(id, 10)},
	})
}

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
			"until you call unwatch_task.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		TaskID int64 `json:"task_id" jsonschema:"The task ID to watch"`
	}) (*mcp.CallToolResult, any, error) {
		c.mu.Lock()
		c.ids[in.TaskID] = struct{}{}
		c.mu.Unlock()
		return jsonResult(map[string]any{"watching": c.watched()}), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "unwatch_task",
		Description: "Stop receiving Claude Code channel notifications for a " +
			"task. Idempotent.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		TaskID int64 `json:"task_id" jsonschema:"The task ID to unwatch"`
	}) (*mcp.CallToolResult, any, error) {
		c.mu.Lock()
		delete(c.ids, in.TaskID)
		c.mu.Unlock()
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
