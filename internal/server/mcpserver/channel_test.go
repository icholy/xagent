package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"gotest.tools/v3/assert"
)

func TestNotificationToChannels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   model.Notification
		want []mcpchannel.Params
	}{
		{
			name: "ready dropped",
			in:   model.Notification{Type: "ready", OrgID: 1},
			want: nil,
		},
		{
			name: "change with no resources",
			in:   model.Notification{Type: "change", OrgID: 1},
			want: nil,
		},
		{
			name: "task updated",
			in: model.Notification{
				Type:  "change",
				OrgID: 1,
				Resources: []model.NotificationResource{
					{Action: "updated", Type: "task", ID: 42},
				},
			},
			want: []mcpchannel.Params{{
				Content: "task 42 was updated.",
				Meta: map[string]string{
					"action":   "updated",
					"resource": "task",
					"id":       "42",
				},
			}},
		},
		{
			name: "all allowlisted resource types",
			in: model.Notification{
				Type:  "change",
				OrgID: 1,
				Resources: []model.NotificationResource{
					{Action: "created", Type: "task", ID: 1},
					{Action: "appended", Type: "log", ID: 2},
					{Action: "appended", Type: "task_logs", ID: 3},
					{Action: "created", Type: "link", ID: 4},
					{Action: "created", Type: "event", ID: 5},
				},
			},
			want: []mcpchannel.Params{
				{Content: "task 1 was created.", Meta: map[string]string{"action": "created", "resource": "task", "id": "1"}},
				{Content: "log 2 was appended.", Meta: map[string]string{"action": "appended", "resource": "log", "id": "2"}},
				{Content: "task_logs 3 was appended.", Meta: map[string]string{"action": "appended", "resource": "task_logs", "id": "3"}},
				{Content: "link 4 was created.", Meta: map[string]string{"action": "created", "resource": "link", "id": "4"}},
				{Content: "event 5 was created.", Meta: map[string]string{"action": "created", "resource": "event", "id": "5"}},
			},
		},
		{
			name: "unknown resource type dropped",
			in: model.Notification{
				Type:  "change",
				OrgID: 1,
				Resources: []model.NotificationResource{
					{Action: "updated", Type: "task", ID: 1},
					{Action: "created", Type: "runner", ID: 99},
					{Action: "updated", Type: "workspace", ID: 100},
				},
			},
			want: []mcpchannel.Params{
				{Content: "task 1 was updated.", Meta: map[string]string{"action": "updated", "resource": "task", "id": "1"}},
			},
		},
		{
			name: "unknown notification type dropped",
			in: model.Notification{
				Type:  "heartbeat",
				OrgID: 1,
				Time:  time.Now(),
				Resources: []model.NotificationResource{
					{Action: "updated", Type: "task", ID: 1},
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notificationToChannels(tt.in)
			assert.DeepEqual(t, got, tt.want)
		})
	}
}

type recordingSender struct {
	got     []mcpchannel.Params
	errOn   map[int]error
	callIdx int
}

func (r *recordingSender) SendChannel(_ context.Context, p mcpchannel.Params) error {
	r.got = append(r.got, p)
	defer func() { r.callIdx++ }()
	if err, ok := r.errOn[r.callIdx]; ok {
		return err
	}
	return nil
}

func TestForwardNotification_LogAndContinue(t *testing.T) {
	t.Parallel()
	// Arrange — notification with three forwardable resources;
	// the sender will fail on the first call.
	sender := &recordingSender{
		errOn: map[int]error{0: errors.New("broken pipe")},
	}
	n := model.Notification{
		Type:  "change",
		OrgID: 1,
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 1},
			{Action: "appended", Type: "log", ID: 2},
			{Action: "created", Type: "link", ID: 3},
		},
	}

	// Act
	forwardNotification(context.Background(), sender, n)

	// Assert — all three were attempted despite the first error.
	assert.Equal(t, len(sender.got), 3)
	assert.Equal(t, sender.got[0].Meta["id"], "1")
	assert.Equal(t, sender.got[1].Meta["id"], "2")
	assert.Equal(t, sender.got[2].Meta["id"], "3")
}

func TestForwardNotification_DropsNonChange(t *testing.T) {
	t.Parallel()
	sender := &recordingSender{}
	forwardNotification(context.Background(), sender, model.Notification{Type: "ready", OrgID: 1})
	assert.Equal(t, len(sender.got), 0)
}
