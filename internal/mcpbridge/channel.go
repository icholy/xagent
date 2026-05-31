// Package mcpbridge implements the local stdio MCP bridge: it re-exposes
// the user-facing xagent tools and forwards task change notifications to
// the host Claude Code session as channel events, gated by an explicit
// per-task subscription set (mute-by-default).
//
// The subscription set, the watch tools, and the forwarding gate live
// here rather than in internal/command (which stays a thin wiring layer)
// or internal/x/mcpchannel (which is xagent-agnostic and knows only the
// channel protocol). mcpbridge depends on mcpchannel for the transport
// and Params primitives and speaks model.Notification on top.
package mcpbridge

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
)

// ChannelSender is the subset of *mcpchannel.Transport the bridge needs
// to push a channel notification. Defined here so the gate can be tested
// without a live stdio transport.
type ChannelSender interface {
	SendChannel(ctx context.Context, p mcpchannel.Params) error
}

// Channel owns the per-process subscription set and the mute-by-default
// forwarding gate. One Channel is created per `xagent mcp --channel`
// process.
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

func (c *Channel) watch(id int64)   { c.mu.Lock(); defer c.mu.Unlock(); c.ids[id] = struct{}{} }
func (c *Channel) unwatch(id int64) { c.mu.Lock(); defer c.mu.Unlock(); delete(c.ids, id) }

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

// isTerminal reports whether a channel message announces a terminal task
// status (completed, failed, or cancelled). The publishing sites format
// these as "Task N completed." / "Task N failed." / "Task N cancelled."
// (see apiserver/runner.go and apiserver/task.go); matching the suffix
// keeps the bridge self-contained with no server or model.Notification
// change. If the wording of those messages ever drifts, this is the one
// place to update.
func isTerminal(channelMessage string) bool {
	for _, suffix := range []string{"completed.", "failed.", "cancelled."} {
		if strings.HasSuffix(channelMessage, suffix) {
			return true
		}
	}
	return false
}

// Forward applies the gate and pushes a channel notification when the
// notification is channel-worthy AND names a task this agent is watching.
// It is suitable as a NotificationClient handler.
//
// After forwarding a terminal-status notification, the task is removed
// from the watch set: the agent has received the result it was waiting
// for and almost never wants further events about it. The agent can
// always re-watch.
func (c *Channel) Forward(ctx context.Context, n model.Notification) {
	if n.ChannelMessage == "" {
		return // summary gate: not channel-worthy
	}
	id, ok := primaryTaskID(n)
	if !ok || !c.watching(id) {
		return // mute-by-default: not a task this agent is watching
	}
	if err := c.sender.SendChannel(ctx, mcpchannel.Params{
		Content: n.ChannelMessage,
		Meta:    map[string]string{"resource": "task", "id": strconv.FormatInt(id, 10)},
	}); err != nil {
		slog.Warn("xagent channel: failed to send", "error", err)
	}
	if isTerminal(n.ChannelMessage) {
		c.unwatch(id)
	}
}
