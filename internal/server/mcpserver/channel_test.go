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

func TestForwardNotification(t *testing.T) {
	t.Parallel()
	// Arrange
	sender := &recordingSender{}

	// Act
	err := ForwardNotification(t.Context(), sender, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "task", ID: 1},
			{Action: "updated", Type: "task", ID: 2},
		},
	})
	assert.NilError(t, err)

	// Assert
	assert.DeepEqual(t, sender.got, []mcpchannel.Params{
		{Content: "task 1 was created.", Meta: map[string]string{"resource": "task", "id": "1"}},
		{Content: "task 2 was updated.", Meta: map[string]string{"resource": "task", "id": "2"}},
	})
}
