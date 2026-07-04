package mcpbridge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

// mutedIDsFromResult extracts the muted list a tool handler returns.
func mutedIDsFromResult(t *testing.T, res *mcp.CallToolResult) []int64 {
	t.Helper()
	assert.Assert(t, !res.IsError)
	assert.Equal(t, len(res.Content), 1)
	text, ok := res.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected *mcp.TextContent, got %T", res.Content[0])
	var out struct {
		Muted []int64 `json:"muted"`
	}
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &out))
	return out.Muted
}

// muteStateFromResult extracts the full mute state a tool handler returns.
func muteStateFromResult(t *testing.T, res *mcp.CallToolResult) (all bool, muted, unmuted []int64) {
	t.Helper()
	assert.Assert(t, !res.IsError)
	assert.Equal(t, len(res.Content), 1)
	text, ok := res.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected *mcp.TextContent, got %T", res.Content[0])
	var out struct {
		All     bool    `json:"all"`
		Muted   []int64 `json:"muted"`
		Unmuted []int64 `json:"unmuted"`
	}
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &out))
	return out.All, out.Muted, out.Unmuted
}

// TestForward_DefaultForwardsEverything is the byte-for-byte default: an
// empty mute set forwards every channel-worthy notification unchanged.
func TestForward_DefaultForwardsEverything(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 42}},
		ChannelMessage: "Task 42 completed.",
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
	assert.Equal(t, sender.SendChannelCalls()[0].P.Content, "Task 42 completed.")
	assert.Assert(t, sender.SendChannelCalls()[0].P.Meta == nil)
}

// TestForward_EmptyChannelMessage keeps the summary gate: notifications
// with no ChannelMessage are silent.
func TestForward_EmptyChannelMessage(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "updated", Type: "task", ID: 42}},
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 0)
}

// TestForward_MutedTaskDropped drops notifications for a muted task while
// still forwarding others.
func TestForward_MutedTaskDropped(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	ch.filter.Mute(42)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 42}},
		ChannelMessage: "Task 42 completed.",
	})
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 7}},
		ChannelMessage: "Task 7 completed.",
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
	assert.Equal(t, sender.SendChannelCalls()[0].P.Content, "Task 7 completed.")
}

// TestForward_NonTaskScopedAlwaysForwarded confirms a message with no task
// resource is delivered even when the mute set is non-empty (a blocklist
// can only drop ids it holds).
func TestForward_NonTaskScopedAlwaysForwarded(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	ch.filter.Mute(42)

	// Act
	ch.Forward(context.Background(), model.Notification{ChannelMessage: "System notice."})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
	assert.Equal(t, sender.SendChannelCalls()[0].P.Content, "System notice.")
}

// TestForward_Unmute restores delivery for a previously muted task.
func TestForward_Unmute(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	ch.filter.Mute(42)
	ch.filter.Unmute(42)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 42}},
		ChannelMessage: "Task 42 completed.",
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
}

// TestForward_MuteAllDropsEveryTask confirms mute-all suppresses
// task-scoped notifications for tasks that were never individually muted.
func TestForward_MuteAllDropsEveryTask(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	_, _, err := ch.muteTool(context.Background(), nil, muteInput{All: true})
	assert.NilError(t, err)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 42}},
		ChannelMessage: "Task 42 completed.",
	})
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 7}},
		ChannelMessage: "Task 7 completed.",
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 0)
}

// TestForward_MuteAllStillForwardsNonTaskScoped confirms mute-all only
// gates task-scoped notifications; a message with no task resource is not a
// channel and is still delivered.
func TestForward_MuteAllStillForwardsNonTaskScoped(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	_, _, err := ch.muteTool(context.Background(), nil, muteInput{All: true})
	assert.NilError(t, err)

	// Act
	ch.Forward(context.Background(), model.Notification{ChannelMessage: "System notice."})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
	assert.Equal(t, sender.SendChannelCalls()[0].P.Content, "System notice.")
}

// TestForward_MuteAllThenUnmuteAll confirms unmute-all lifts a mute-all and
// restores the subscribe-all default.
func TestForward_MuteAllThenUnmuteAll(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	_, _, err := ch.muteTool(context.Background(), nil, muteInput{All: true})
	assert.NilError(t, err)
	_, _, err = ch.unmuteTool(context.Background(), nil, unmuteInput{All: true})
	assert.NilError(t, err)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 42}},
		ChannelMessage: "Task 42 completed.",
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
}

func TestMuteTool_All(t *testing.T) {
	t.Parallel()
	// Arrange
	ch := NewChannel(&ChannelSenderMock{})
	ch.filter.Mute(7)

	// Act
	res, _, err := ch.muteTool(context.Background(), nil, muteInput{All: true})

	// Assert
	assert.NilError(t, err)
	all, _, unmuted := muteStateFromResult(t, res)
	assert.Assert(t, all)
	// mute-all subsumes and clears the exception set: nothing is delivered.
	assert.Equal(t, len(unmuted), 0)
}

// TestForward_MuteAllThenUnmuteOne is the maintainer-requested workflow:
// channel_mute(all=true) followed by channel_unmute(task_ids=[123]) mutes
// every task except 123, which keeps delivering.
func TestForward_MuteAllThenUnmuteOne(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	_, _, err := ch.muteTool(context.Background(), nil, muteInput{All: true})
	assert.NilError(t, err)
	_, _, err = ch.unmuteTool(context.Background(), nil, unmuteInput{TaskIDs: []int64{123}})
	assert.NilError(t, err)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 123}},
		ChannelMessage: "Task 123 completed.",
	})
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 7}},
		ChannelMessage: "Task 7 completed.",
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
	assert.Equal(t, sender.SendChannelCalls()[0].P.Content, "Task 123 completed.")
}

// TestUnmuteTool_AfterMuteAll reports the unmuted exceptions under mute-all.
func TestUnmuteTool_AfterMuteAll(t *testing.T) {
	t.Parallel()
	// Arrange
	ch := NewChannel(&ChannelSenderMock{})
	_, _, err := ch.muteTool(context.Background(), nil, muteInput{All: true})
	assert.NilError(t, err)

	// Act
	res, _, err := ch.unmuteTool(context.Background(), nil, unmuteInput{TaskIDs: []int64{123, 7}})

	// Assert
	assert.NilError(t, err)
	all, _, unmuted := muteStateFromResult(t, res)
	assert.Assert(t, all)
	assert.DeepEqual(t, unmuted, []int64{7, 123})
}

// TestMuteTool_RemuteUnderMuteAll confirms muting a task that was unmuted
// under mute-all removes the exception, so it is muted with the rest again.
func TestMuteTool_RemuteUnderMuteAll(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &ChannelSenderMock{
		SendChannelFunc: func(context.Context, mcpchannel.Params) error { return nil },
	}
	ch := NewChannel(sender)
	_, _, err := ch.muteTool(context.Background(), nil, muteInput{All: true})
	assert.NilError(t, err)
	_, _, err = ch.unmuteTool(context.Background(), nil, unmuteInput{TaskIDs: []int64{123}})
	assert.NilError(t, err)
	res, _, err := ch.muteTool(context.Background(), nil, muteInput{TaskIDs: []int64{123}})
	assert.NilError(t, err)

	// Assert: the exception is gone, so 123 is muted again.
	all, _, unmuted := muteStateFromResult(t, res)
	assert.Assert(t, all)
	assert.Equal(t, len(unmuted), 0)
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 123}},
		ChannelMessage: "Task 123 completed.",
	})
	assert.Equal(t, len(sender.SendChannelCalls()), 0)
}

func TestMuteTool(t *testing.T) {
	t.Parallel()
	// Arrange
	ch := NewChannel(&ChannelSenderMock{})

	// Act
	res, _, err := ch.muteTool(context.Background(), nil, muteInput{TaskIDs: []int64{9, 3, 3}})

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, mutedIDsFromResult(t, res), []int64{3, 9})
}

func TestUnmuteTool(t *testing.T) {
	t.Parallel()
	// Arrange
	ch := NewChannel(&ChannelSenderMock{})
	ch.filter.Mute(1)
	ch.filter.Mute(2)
	ch.filter.Mute(3)

	// Act
	res, _, err := ch.unmuteTool(context.Background(), nil, unmuteInput{TaskIDs: []int64{2}})

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, mutedIDsFromResult(t, res), []int64{1, 3})
}

func TestUnmuteTool_All(t *testing.T) {
	t.Parallel()
	// Arrange
	ch := NewChannel(&ChannelSenderMock{})
	ch.filter.Mute(1)
	ch.filter.Mute(2)

	// Act
	res, _, err := ch.unmuteTool(context.Background(), nil, unmuteInput{All: true})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(mutedIDsFromResult(t, res)), 0)
}

func TestMutedTool(t *testing.T) {
	t.Parallel()
	// Arrange
	ch := NewChannel(&ChannelSenderMock{})
	ch.filter.Mute(5)
	ch.filter.Mute(1)

	// Act
	res, _, err := ch.mutedTool(context.Background(), nil, mutedInput{})

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, mutedIDsFromResult(t, res), []int64{1, 5})
}
