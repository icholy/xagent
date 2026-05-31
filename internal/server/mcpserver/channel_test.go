package mcpserver

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"gotest.tools/v3/assert"
)

// fakeSender records the params it is asked to send.
type fakeSender struct {
	sent []mcpchannel.Params
}

func (f *fakeSender) SendChannel(ctx context.Context, p mcpchannel.Params) error {
	f.sent = append(f.sent, p)
	return nil
}

func taskNotification(id int64, msg string) model.Notification {
	return model.Notification{
		Type:           "change",
		Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: id}},
		ChannelMessage: msg,
	}
}

func TestForward_MutedByDefault(t *testing.T) {
	// Arrange
	sender := &fakeSender{}
	c := NewChannel(sender)

	// Act - no watch_task call has been made
	c.Forward(context.Background(), taskNotification(42, "Task 42 queued: start."))

	// Assert
	assert.Equal(t, len(sender.sent), 0)
}

func TestForward_WatchedTask(t *testing.T) {
	// Arrange
	sender := &fakeSender{}
	c := NewChannel(sender)
	c.watch(42)

	// Act
	c.Forward(context.Background(), taskNotification(42, "Task 42 queued: start."))

	// Assert
	assert.Equal(t, len(sender.sent), 1)
	assert.Equal(t, sender.sent[0].Content, "Task 42 queued: start.")
	assert.DeepEqual(t, sender.sent[0].Meta, map[string]string{"resource": "task", "id": "42"})
}

func TestForward_UnwatchedTask(t *testing.T) {
	// Arrange
	sender := &fakeSender{}
	c := NewChannel(sender)
	c.watch(42)

	// Act - a different task's notification arrives
	c.Forward(context.Background(), taskNotification(43, "Task 43 queued: start."))

	// Assert
	assert.Equal(t, len(sender.sent), 0)
}

func TestForward_EmptyChannelMessage(t *testing.T) {
	// Arrange
	sender := &fakeSender{}
	c := NewChannel(sender)
	c.watch(42)

	// Act - summary gate: silent notification
	c.Forward(context.Background(), taskNotification(42, ""))

	// Assert
	assert.Equal(t, len(sender.sent), 0)
}

func TestForward_NoTaskResource(t *testing.T) {
	// Arrange
	sender := &fakeSender{}
	c := NewChannel(sender)
	c.watch(42)

	// Act - channel-worthy message with no task resource
	c.Forward(context.Background(), model.Notification{
		Type:           "change",
		ChannelMessage: "something happened.",
	})

	// Assert
	assert.Equal(t, len(sender.sent), 0)
}

func TestForward_StaysWatchedAfterTerminal(t *testing.T) {
	// Arrange
	sender := &fakeSender{}
	c := NewChannel(sender)
	c.watch(42)

	// Act - subscriptions are purely explicit; a terminal notification does
	// not auto-unwatch, so a follow-up notification is still forwarded.
	c.Forward(context.Background(), taskNotification(42, "Task 42 completed."))
	c.Forward(context.Background(), taskNotification(42, "Task 42 archived."))

	// Assert
	assert.Equal(t, len(sender.sent), 2)
	assert.Equal(t, c.watching(42), true)
	assert.DeepEqual(t, c.watched(), []int64{42})
}

func TestWatchSet(t *testing.T) {
	// Arrange
	c := NewChannel(&fakeSender{})

	// Act / Assert - watch is idempotent and the set stays sorted
	c.watch(3)
	c.watch(1)
	c.watch(2)
	c.watch(1)
	assert.DeepEqual(t, c.watched(), []int64{1, 2, 3})

	// unwatch removes; unwatching a missing id is a no-op
	c.unwatch(2)
	c.unwatch(99)
	assert.DeepEqual(t, c.watched(), []int64{1, 3})
	assert.Equal(t, c.watching(2), false)
	assert.Equal(t, c.watching(1), true)
}

func TestPrimaryTaskID(t *testing.T) {
	tests := []struct {
		name      string
		resources []model.NotificationResource
		wantID    int64
		wantOK    bool
	}{
		{"none", nil, 0, false},
		{
			"single task",
			[]model.NotificationResource{{Type: "task", ID: 7}},
			7, true,
		},
		{
			"first task wins over later task",
			[]model.NotificationResource{{Type: "task", ID: 7}, {Type: "task", ID: 9}},
			7, true,
		},
		{
			"skips non-task resources",
			[]model.NotificationResource{{Type: "log", ID: 1}, {Type: "task", ID: 9}},
			9, true,
		},
		{
			"no task among others",
			[]model.NotificationResource{{Type: "log", ID: 1}, {Type: "link", ID: 2}},
			0, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := primaryTaskID(model.Notification{Resources: tt.resources})
			assert.Equal(t, ok, tt.wantOK)
			assert.Equal(t, id, tt.wantID)
		})
	}
}
