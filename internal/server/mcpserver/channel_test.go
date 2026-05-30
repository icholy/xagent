package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"gotest.tools/v3/assert"
)

type fakeTasks struct {
	byID map[int64]xagentv1.TaskStatus
	err  error
}

func (f *fakeTasks) GetTask(_ context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	status, ok := f.byID[req.Id]
	if !ok {
		return nil, errors.New("task not found")
	}
	return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: req.Id, Status: status}}, nil
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

func newChannel(sender ChannelSender, tasks taskGetter) *NotificationChannel {
	return &NotificationChannel{sender: sender, tasks: tasks}
}

func TestForward_TerminalStatusesSent(t *testing.T) {
	t.Parallel()
	// Arrange
	tasks := &fakeTasks{byID: map[int64]xagentv1.TaskStatus{
		1: xagentv1.TaskStatus_COMPLETED,
		2: xagentv1.TaskStatus_FAILED,
		3: xagentv1.TaskStatus_CANCELLED,
	}}
	sender := &recordingSender{}
	c := newChannel(sender, tasks)
	n := model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 1},
			{Action: "updated", Type: "task", ID: 2},
			{Action: "updated", Type: "task", ID: 3},
		},
	}

	// Act
	c.forward(context.Background(), n)

	// Assert
	want := []mcpchannel.Params{
		{Content: "task 1 completed.", Meta: map[string]string{"resource": "task", "status": "completed", "id": "1"}},
		{Content: "task 2 failed.", Meta: map[string]string{"resource": "task", "status": "failed", "id": "2"}},
		{Content: "task 3 cancelled.", Meta: map[string]string{"resource": "task", "status": "cancelled", "id": "3"}},
	}
	assert.DeepEqual(t, sender.got, want)
}

func TestForward_NonTerminalStatusesDropped(t *testing.T) {
	t.Parallel()
	// Arrange
	tasks := &fakeTasks{byID: map[int64]xagentv1.TaskStatus{
		1: xagentv1.TaskStatus_PENDING,
		2: xagentv1.TaskStatus_RUNNING,
		3: xagentv1.TaskStatus_RESTARTING,
		4: xagentv1.TaskStatus_CANCELLING,
	}}
	sender := &recordingSender{}
	c := newChannel(sender, tasks)
	n := model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 1},
			{Action: "updated", Type: "task", ID: 2},
			{Action: "updated", Type: "task", ID: 3},
			{Action: "updated", Type: "task", ID: 4},
		},
	}

	// Act
	c.forward(context.Background(), n)

	// Assert
	assert.Equal(t, len(sender.got), 0)
}

func TestForward_NonTaskResourcesDropped(t *testing.T) {
	t.Parallel()
	// Arrange — only the task resource should be forwarded.
	tasks := &fakeTasks{byID: map[int64]xagentv1.TaskStatus{
		7: xagentv1.TaskStatus_COMPLETED,
	}}
	sender := &recordingSender{}
	c := newChannel(sender, tasks)
	n := model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "appended", Type: "log", ID: 100},
			{Action: "appended", Type: "task_logs", ID: 101},
			{Action: "created", Type: "link", ID: 102},
			{Action: "created", Type: "event", ID: 103},
			{Action: "updated", Type: "task", ID: 7},
		},
	}

	// Act
	c.forward(context.Background(), n)

	// Assert
	assert.Equal(t, len(sender.got), 1)
	assert.Equal(t, sender.got[0].Content, "task 7 completed.")
}

func TestForward_NonChangeDropped(t *testing.T) {
	t.Parallel()
	sender := &recordingSender{}
	c := newChannel(sender, &fakeTasks{})
	c.forward(context.Background(), model.Notification{Type: "ready"})
	assert.Equal(t, len(sender.got), 0)
}

func TestForward_GetTaskErrorSkipsButContinues(t *testing.T) {
	t.Parallel()
	// Arrange — task 1 returns "not found", task 2 returns COMPLETED.
	// The first should be dropped without aborting the batch.
	tasks := &fakeTasks{byID: map[int64]xagentv1.TaskStatus{
		2: xagentv1.TaskStatus_COMPLETED,
	}}
	sender := &recordingSender{}
	c := newChannel(sender, tasks)
	n := model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 1},
			{Action: "updated", Type: "task", ID: 2},
		},
	}

	// Act
	c.forward(context.Background(), n)

	// Assert
	assert.Equal(t, len(sender.got), 1)
	assert.Equal(t, sender.got[0].Meta["id"], "2")
}

func TestForward_SendChannelErrorLogAndContinue(t *testing.T) {
	t.Parallel()
	// Arrange — both tasks terminal, first SendChannel fails.
	tasks := &fakeTasks{byID: map[int64]xagentv1.TaskStatus{
		1: xagentv1.TaskStatus_COMPLETED,
		2: xagentv1.TaskStatus_FAILED,
	}}
	sender := &recordingSender{errOn: map[int]error{0: errors.New("broken pipe")}}
	c := newChannel(sender, tasks)
	n := model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 1},
			{Action: "updated", Type: "task", ID: 2},
		},
	}

	// Act
	c.forward(context.Background(), n)

	// Assert — both were attempted despite the first error.
	assert.Equal(t, len(sender.got), 2)
	assert.Equal(t, sender.got[0].Meta["id"], "1")
	assert.Equal(t, sender.got[1].Meta["id"], "2")
}
