package apiserver

import (
	"context"
	"strings"
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
		Runner: "r",
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "ChannelMessage"))
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
		Runner: "r",
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "ChannelMessage"))
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
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "ChannelMessage"))
}

func TestArchiveTask_Publishes(t *testing.T) {
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
	// Archive requires a terminal status; mark the task COMPLETED first.
	dbTask, err := st.GetTask(ctx, nil, resp.Task.Id, org.OrgID)
	assert.NilError(t, err)
	dbTask.Status = 5 // COMPLETED
	dbTask.Command = 0
	assert.NilError(t, st.UpdateTask(ctx, nil, dbTask))
	pub.ResetCalls()

	_, err = srv.ArchiveTask(ctx, &xagentv1.ArchiveTaskRequest{Id: resp.Task.Id})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "archived", Type: "task", ID: resp.Task.Id},
			{Action: "appended", Type: "task_logs", ID: resp.Task.Id},
		},
		OrgID:  org.OrgID,
		UserID: org.UserID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "ChannelMessage"))
	msg := calls[0].N.ChannelMessage
	assert.Assert(t, strings.Contains(msg, "archived"), "expected archived in message, got %q", msg)
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
			{Type: "llm", Content: "hello"},
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
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "ChannelMessage"))
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
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "ChannelMessage"))
}

func TestUpdateTask_ChannelMessage_QueuedOnStart(t *testing.T) {
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
	// Move past the queued/start state so the next UpdateTask works from
	// a non-queued task; then re-queue via Start: true.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: resp.Task.Id, Event: string(model.RunnerEventStarted), Version: 1},
			{TaskId: resp.Task.Id, Event: string(model.RunnerEventStopped), Version: 1},
		},
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id: resp.Task.Id, Start: true,
	})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	msg := calls[0].N.ChannelMessage
	assert.Assert(t, strings.Contains(msg, "queued"), "expected queued in message, got %q", msg)
}

func TestUpdateTask_NoChannelMessage_NameOnly(t *testing.T) {
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
	// Transition to Running so PendingRunner == "" and the helper stays silent
	// on a name-only update.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: resp.Task.Id, Event: string(model.RunnerEventStarted), Version: 1},
		},
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id: resp.Task.Id, Name: "renamed",
	})
	assert.NilError(t, err)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.ChannelMessage, "")
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
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "ChannelMessage"))
}

func TestSubmitRunnerEvents_NotApplied_DoesNotPublish(t *testing.T) {
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

	// Stale version: ApplyRunnerEvent returns false and the notification's
	// Ignore field is set inside the tx, so publish is a no-op.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: taskResp.Task.Id, Event: string(model.RunnerEventStarted), Version: 999},
		},
	})
	assert.NilError(t, err)
	assert.Equal(t, len(pub.PublishCalls()), 0)
}

func TestServerPublish_IgnoreSuppressesDelivery(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	srv := New(Options{Publisher: pub})

	srv.publish(model.Notification{Type: "change", OrgID: 1, Ignore: true})
	assert.Equal(t, len(pub.PublishCalls()), 0)

	srv.publish(model.Notification{Type: "change", OrgID: 1})
	assert.Equal(t, len(pub.PublishCalls()), 1)
}
