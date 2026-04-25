package server

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func newTestPublisher() *pubsub.PublisherMock {
	return &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ int64, _ pubsub.Notification) error {
			return nil
		},
	}
}

func TestCreateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := newTestPublisher()
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.Type, "created")
	assert.Equal(t, calls[0].N.Resource, "task")
	assert.Equal(t, calls[0].N.ID, resp.Task.Id)
	assert.Equal(t, calls[0].OrgID, org.OrgID)
}

func TestUpdateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := newTestPublisher()
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	// Reset calls after CreateTask
	pub2 := newTestPublisher()
	srv.publisher = pub2

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id: resp.Task.Id, Name: "updated",
	})
	assert.NilError(t, err)

	calls := pub2.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.Type, "updated")
	assert.Equal(t, calls[0].N.Resource, "task")
	assert.Equal(t, calls[0].N.ID, resp.Task.Id)
}

func TestCancelTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := newTestPublisher()
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	pub2 := newTestPublisher()
	srv.publisher = pub2

	_, err = srv.CancelTask(ctx, &xagentv1.CancelTaskRequest{Id: resp.Task.Id})
	assert.NilError(t, err)

	calls := pub2.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.Type, "updated")
	assert.Equal(t, calls[0].N.Resource, "task")
}

func TestUploadLogs_Publishes(t *testing.T) {
	t.Parallel()

	pub := newTestPublisher()
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	pub2 := newTestPublisher()
	srv.publisher = pub2

	_, err = srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: resp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "hello"},
		},
	})
	assert.NilError(t, err)

	calls := pub2.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.Type, "appended")
	assert.Equal(t, calls[0].N.Resource, "log")
	assert.Equal(t, calls[0].N.ID, resp.Task.Id)
}

func TestCreateLink_Publishes(t *testing.T) {
	t.Parallel()

	pub := newTestPublisher()
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	pub2 := newTestPublisher()
	srv.publisher = pub2

	linkResp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId: resp.Task.Id,
		Url:    "https://example.com",
		Title:  "test",
	})
	assert.NilError(t, err)

	calls := pub2.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.Type, "created")
	assert.Equal(t, calls[0].N.Resource, "link")
	assert.Equal(t, calls[0].N.ID, linkResp.Link.Id)
}

func TestAddEventTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := newTestPublisher()
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

	pub2 := newTestPublisher()
	srv.publisher = pub2

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	calls := pub2.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.Type, "created")
	assert.Equal(t, calls[0].N.Resource, "event")
	assert.Equal(t, calls[0].N.ID, eventResp.Event.Id)
}

func TestSubmitRunnerEvents_Publishes(t *testing.T) {
	t.Parallel()

	pub := newTestPublisher()
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	pub2 := newTestPublisher()
	srv.publisher = pub2

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

	calls := pub2.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.Type, "updated")
	assert.Equal(t, calls[0].N.Resource, "task")
	assert.Equal(t, calls[0].N.ID, taskResp.Task.Id)
}
