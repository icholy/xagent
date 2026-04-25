package server

import (
	"context"
	"sync"
	"testing"

	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// testPublisher records published notifications for assertions.
type testPublisher struct {
	mu            sync.Mutex
	notifications []pubsub.Notification
}

func (p *testPublisher) Publish(_ context.Context, orgID int64, n pubsub.Notification) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notifications = append(p.notifications, n)
	return nil
}

func (p *testPublisher) get() []pubsub.Notification {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]pubsub.Notification(nil), p.notifications...)
}

func TestCreateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &testPublisher{}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	ns := pub.get()
	assert.Equal(t, len(ns), 1)
	assert.Equal(t, ns[0].Type, "created")
	assert.Equal(t, ns[0].Resource, "task")
	assert.Equal(t, ns[0].ID, resp.Task.Id)
	assert.Equal(t, ns[0].OrgID, org.OrgID)
}

func TestUpdateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &testPublisher{}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.mu.Lock()
	pub.notifications = nil
	pub.mu.Unlock()

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id: resp.Task.Id, Name: "updated",
	})
	assert.NilError(t, err)

	ns := pub.get()
	assert.Equal(t, len(ns), 1)
	assert.Equal(t, ns[0].Type, "updated")
	assert.Equal(t, ns[0].Resource, "task")
	assert.Equal(t, ns[0].ID, resp.Task.Id)
}

func TestCancelTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &testPublisher{}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.mu.Lock()
	pub.notifications = nil
	pub.mu.Unlock()

	_, err = srv.CancelTask(ctx, &xagentv1.CancelTaskRequest{Id: resp.Task.Id})
	assert.NilError(t, err)

	ns := pub.get()
	assert.Equal(t, len(ns), 1)
	assert.Equal(t, ns[0].Type, "updated")
	assert.Equal(t, ns[0].Resource, "task")
}

func TestUploadLogs_Publishes(t *testing.T) {
	t.Parallel()

	pub := &testPublisher{}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.mu.Lock()
	pub.notifications = nil
	pub.mu.Unlock()

	_, err = srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: resp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "hello"},
		},
	})
	assert.NilError(t, err)

	ns := pub.get()
	assert.Equal(t, len(ns), 1)
	assert.Equal(t, ns[0].Type, "appended")
	assert.Equal(t, ns[0].Resource, "log")
	assert.Equal(t, ns[0].ID, resp.Task.Id)
}

func TestCreateLink_Publishes(t *testing.T) {
	t.Parallel()

	pub := &testPublisher{}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.mu.Lock()
	pub.notifications = nil
	pub.mu.Unlock()

	linkResp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId: resp.Task.Id,
		Url:    "https://example.com",
		Title:  "test",
	})
	assert.NilError(t, err)

	ns := pub.get()
	assert.Equal(t, len(ns), 1)
	assert.Equal(t, ns[0].Type, "created")
	assert.Equal(t, ns[0].Resource, "link")
	assert.Equal(t, ns[0].ID, linkResp.Link.Id)
}

func TestAddEventTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &testPublisher{}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	eventResp, err := srv.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: "test event",
		Url:         "https://example.com",
	})
	assert.NilError(t, err)
	pub.mu.Lock()
	pub.notifications = nil
	pub.mu.Unlock()

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	ns := pub.get()
	assert.Equal(t, len(ns), 1)
	assert.Equal(t, ns[0].Type, "created")
	assert.Equal(t, ns[0].Resource, "event")
	assert.Equal(t, ns[0].ID, eventResp.Event.Id)
}

func TestSubmitRunnerEvents_Publishes(t *testing.T) {
	t.Parallel()

	pub := &testPublisher{}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.mu.Lock()
	pub.notifications = nil
	pub.mu.Unlock()

	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{
				TaskId:  taskResp.Task.Id,
				Event:   string(model.RunnerEventStarted),
				Version: 1,
			},
		},
	})
	assert.NilError(t, err)

	ns := pub.get()
	assert.Equal(t, len(ns), 1)
	assert.Equal(t, ns[0].Type, "updated")
	assert.Equal(t, ns[0].Resource, "task")
	assert.Equal(t, ns[0].ID, taskResp.Task.Id)
}
