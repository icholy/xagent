// Package mcpbridge implements the local stdio MCP bridge: it forwards
// task-change notifications from the server's per-org SSE stream to the
// host Claude Code session as channel events, subject to a per-process,
// per-task mute set.
//
// The mute set is a blocklist: by default (empty set) every
// channel-worthy notification is forwarded, exactly as the bridge did
// before this package existed. An agent mutes specific tasks it no longer
// cares about via the channel_mute / channel_unmute / channel_muted MCP
// tools; muted tasks stay muted until unmuted or until the bridge process
// restarts (the set is in-memory only).
package mcpbridge

//go:generate go tool moq -pkg mcpbridge -out channel_sender_moq_test.go . ChannelSender

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ChannelSender is the subset of *mcpchannel.Transport the bridge needs to
// push a channel notification. Defined as an interface so the forwarding
// gate can be tested without a live stdio transport.
type ChannelSender interface {
	SendChannel(ctx context.Context, p mcpchannel.Params) error
}

// Channel owns the per-process mute set and the mute-aware forwarding
// gate. One Channel is created per `xagent mcp --channel` process.
type Channel struct {
	sender ChannelSender

	mu    sync.Mutex
	muted map[int64]struct{} // muted task ids; empty == forward everything
}

// NewChannel returns a Channel that forwards through sender with an empty
// mute set (subscribe-all by default).
func NewChannel(sender ChannelSender) *Channel {
	return &Channel{sender: sender, muted: map[int64]struct{}{}}
}

func (c *Channel) mute(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.muted[id] = struct{}{}
}

func (c *Channel) unmute(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.muted, id)
}

func (c *Channel) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.muted)
}

func (c *Channel) isMuted(id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.muted[id]
	return ok
}

// mutedIDs returns the currently-muted task ids, sorted ascending.
func (c *Channel) mutedIDs() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]int64, 0, len(c.muted))
	for id := range c.muted {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// primaryTaskID returns the id of the first task resource in the
// notification, if any.
func primaryTaskID(n model.Notification) (int64, bool) {
	for _, r := range n.Resources {
		if r.Type == "task" {
			return r.ID, true
		}
	}
	return 0, false
}

// Forward applies the summary gate and the mute set, then pushes the
// channel notification. It is suitable as the xagentclient
// NotificationClient handler.
//
// With an empty mute set this is identical to the bridge's original inline
// handler: the same ChannelMessage gate and the same SendChannel call. A
// notification naming a muted task is dropped; a notification with no task
// resource is always forwarded (a blocklist can only drop ids it holds).
func (c *Channel) Forward(ctx context.Context, n model.Notification) {
	if n.ChannelMessage == "" {
		return // summary gate: not channel-worthy
	}
	if id, ok := primaryTaskID(n); ok && c.isMuted(id) {
		return // this task has been muted by the agent
	}
	if err := c.sender.SendChannel(ctx, mcpchannel.Params{Content: n.ChannelMessage}); err != nil {
		slog.Warn("xagent channel: failed to send", "err", err)
	}
}

// AddTools registers channel_mute / channel_unmute / channel_muted on
// server. Called by the bridge only when --channel is enabled, so the
// tools never appear when there is no channel stream to mute.
func (c *Channel) AddTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "channel_mute",
		Description: "Stop receiving xagent channel notifications (queued, woken, " +
			"completed, failed, cancelled, archived) for the given tasks. You are " +
			"subscribed to every task by default; call this to mute tasks you no " +
			"longer care about. Re-enable with channel_unmute. Muting is per bridge " +
			"session and resets when the session restarts. Distinct from " +
			"create_link(subscribe=true), which routes external events INTO a task.",
	}, c.muteTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "channel_unmute",
		Description: "Resume receiving xagent channel notifications for tasks " +
			"previously muted with channel_mute. Pass task_ids to unmute specific " +
			"tasks, or all=true to clear the whole mute set and go back to the " +
			"subscribe-all default.",
	}, c.unmuteTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "channel_muted",
		Description: "List the task ids currently muted for this session. All " +
			"tasks not listed are delivered.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, c.mutedTool)
}

type muteInput struct {
	TaskIDs []int64 `json:"task_ids" jsonschema:"Task IDs to mute"`
}

func (c *Channel) muteTool(_ context.Context, _ *mcp.CallToolRequest, in muteInput) (*mcp.CallToolResult, any, error) {
	for _, id := range in.TaskIDs {
		c.mute(id)
	}
	return c.mutedResult(), nil, nil
}

type unmuteInput struct {
	TaskIDs []int64 `json:"task_ids,omitempty" jsonschema:"Task IDs to unmute"`
	All     bool    `json:"all,omitempty" jsonschema:"Unmute every task, clearing the whole mute set"`
}

func (c *Channel) unmuteTool(_ context.Context, _ *mcp.CallToolRequest, in unmuteInput) (*mcp.CallToolResult, any, error) {
	if in.All {
		c.clear()
	}
	for _, id := range in.TaskIDs {
		c.unmute(id)
	}
	return c.mutedResult(), nil, nil
}

type mutedInput struct{}

func (c *Channel) mutedTool(_ context.Context, _ *mcp.CallToolRequest, _ mutedInput) (*mcp.CallToolResult, any, error) {
	return c.mutedResult(), nil, nil
}

// mutedResult renders the current mute set for a tool response.
func (c *Channel) mutedResult() *mcp.CallToolResult {
	return jsonResult(struct {
		Muted []int64 `json:"muted"`
		Note  string  `json:"note"`
	}{
		Muted: c.mutedIDs(),
		Note:  "Channel notifications for all tasks except these are delivered. Muting is per bridge session and resets on restart.",
	})
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("failed to format response: %v", err)}},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}
