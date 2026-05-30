package mcpserver

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"gotest.tools/v3/assert"
)

type recordingSender struct {
	got   []mcpchannel.Params
	calls int
}

func (r *recordingSender) SendChannel(_ context.Context, p mcpchannel.Params) error {
	r.got = append(r.got, p)
	r.calls++
	return nil
}

func TestForwardNotification_GatesOnEmptyChannelMessage(t *testing.T) {
	t.Parallel()
	sender := &recordingSender{}

	err := ForwardNotification(t.Context(), sender, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 1},
		},
		ChannelMessage: "",
	})
	assert.NilError(t, err)
	assert.Equal(t, sender.calls, 0)
}

func TestForwardNotification_RelaysChannelMessage(t *testing.T) {
	t.Parallel()
	sender := &recordingSender{}

	err := ForwardNotification(t.Context(), sender, model.Notification{
		Type:           "change",
		ChannelMessage: "Task 7 completed.",
	})
	assert.NilError(t, err)
	assert.Equal(t, sender.calls, 1)
	assert.DeepEqual(t, sender.got, []mcpchannel.Params{
		{Content: "Task 7 completed."},
	})
}
