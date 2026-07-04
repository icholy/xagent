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
	ch.mute(42)

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
	ch.mute(42)

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
	ch.mute(42)
	ch.unmute(42)

	// Act
	ch.Forward(context.Background(), model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 42}},
		ChannelMessage: "Task 42 completed.",
	})

	// Assert
	assert.Equal(t, len(sender.SendChannelCalls()), 1)
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
	ch.mute(1)
	ch.mute(2)
	ch.mute(3)

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
	ch.mute(1)
	ch.mute(2)

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
	ch.mute(5)
	ch.mute(1)

	// Act
	res, _, err := ch.mutedTool(context.Background(), nil, mutedInput{})

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, mutedIDsFromResult(t, res), []int64{1, 5})
}
