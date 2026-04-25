package apiserver

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestCreateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
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
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "task", ID: resp.Task.Id},
			{Action: "appended", Type: "task_logs", ID: resp.Task.Id},
		},
		OrgID:  org.OrgID,
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestUpdateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id: resp.Task.Id, Name: "updated",
	})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: resp.Task.Id},
			{Action: "appended", Type: "task_logs", ID: resp.Task.Id},
		},
		OrgID:  org.OrgID,
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestCancelTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.CancelTask(ctx, &xagentv1.CancelTaskRequest{Id: resp.Task.Id})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "cancelled", Type: "task", ID: resp.Task.Id},
			{Action: "appended", Type: "task_logs", ID: resp.Task.Id},
		},
		OrgID:  org.OrgID,
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestUploadLogs_Publishes(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: resp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "hello"},
		},
	})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "appended", Type: "task_logs", ID: resp.Task.Id}},
		OrgID:     org.OrgID,
		UserID:    org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestCreateLink_Publishes(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	linkResp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId: resp.Task.Id,
		Url:    "https://example.com",
		Title:  "test",
	})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "task_links", ID: resp.Task.Id},
			{Action: "created", Type: "link", ID: linkResp.Link.Id},
		},
		OrgID:  org.OrgID,
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestAddEventTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
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
	pub.ResetCalls()

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: taskResp.Task.Id},
			{Action: "updated", Type: "event", ID: eventResp.Event.Id},
		},
		OrgID:  org.OrgID,
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestSubmitRunnerEvents_Publishes(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	st := teststore.New(t)
	srv := New(Options{Store: st, Publisher: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)
	pub.ResetCalls()

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

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: taskResp.Task.Id},
			{Action: "appended", Type: "task_logs", ID: taskResp.Task.Id},
		},
		OrgID:  org.OrgID,
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}
