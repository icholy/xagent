package server

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// publishRecorder is a test double for websocketServer that records Publish calls.
type publishRecorder struct {
	mu    sync.Mutex
	calls []publishCall
}

type publishCall struct {
	OrgID int64
	N     model.Notification
}

func (r *publishRecorder) Publish(_ context.Context, orgID int64, n model.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, publishCall{OrgID: orgID, N: n})
	return nil
}

func (r *publishRecorder) Handler() http.Handler {
	return http.NotFoundHandler()
}

func (r *publishRecorder) publishCalls() []publishCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]publishCall(nil), r.calls...)
}

func TestCreateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &publishRecorder{}
	st := teststore.New(t)
	srv := New(Options{Store: st, WSS: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	calls := pub.publishCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].N, model.Notification{
		Type:     "created",
		Resource: "task",
		ID:       resp.Task.Id,
		OrgID:    org.OrgID,
		Version:  1,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestUpdateTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &publishRecorder{}
	st := teststore.New(t)
	srv := New(Options{Store: st, WSS: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id: resp.Task.Id, Name: "updated",
	})
	assert.NilError(t, err)

	calls := pub.publishCalls()
	assert.Equal(t, len(calls), 2)
	assert.DeepEqual(t, calls[1].N, model.Notification{
		Type:     "updated",
		Resource: "task",
		ID:       resp.Task.Id,
		OrgID:    org.OrgID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestCancelTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &publishRecorder{}
	st := teststore.New(t)
	srv := New(Options{Store: st, WSS: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	_, err = srv.CancelTask(ctx, &xagentv1.CancelTaskRequest{Id: resp.Task.Id})
	assert.NilError(t, err)

	calls := pub.publishCalls()
	assert.Equal(t, len(calls), 2)
	assert.DeepEqual(t, calls[1].N, model.Notification{
		Type:     "updated",
		Resource: "task",
		ID:       resp.Task.Id,
		OrgID:    org.OrgID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestUploadLogs_Publishes(t *testing.T) {
	t.Parallel()

	pub := &publishRecorder{}
	st := teststore.New(t)
	srv := New(Options{Store: st, WSS: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	_, err = srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: resp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "hello"},
		},
	})
	assert.NilError(t, err)

	calls := pub.publishCalls()
	assert.Equal(t, len(calls), 2)
	assert.DeepEqual(t, calls[1].N, model.Notification{
		Type:     "appended",
		Resource: "log",
		ID:       resp.Task.Id,
		OrgID:    org.OrgID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestCreateLink_Publishes(t *testing.T) {
	t.Parallel()

	pub := &publishRecorder{}
	st := teststore.New(t)
	srv := New(Options{Store: st, WSS: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

	linkResp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId: resp.Task.Id,
		Url:    "https://example.com",
		Title:  "test",
	})
	assert.NilError(t, err)

	calls := pub.publishCalls()
	assert.Equal(t, len(calls), 2)
	assert.DeepEqual(t, calls[1].N, model.Notification{
		Type:     "created",
		Resource: "link",
		ID:       linkResp.Link.Id,
		OrgID:    org.OrgID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestAddEventTask_Publishes(t *testing.T) {
	t.Parallel()

	pub := &publishRecorder{}
	st := teststore.New(t)
	srv := New(Options{Store: st, WSS: pub})
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

	_, err = srv.AddEventTask(ctx, &xagentv1.AddEventTaskRequest{
		EventId: eventResp.Event.Id,
		TaskId:  taskResp.Task.Id,
	})
	assert.NilError(t, err)

	calls := pub.publishCalls()
	assert.Equal(t, len(calls), 2)
	assert.DeepEqual(t, calls[1].N, model.Notification{
		Type:     "created",
		Resource: "event",
		ID:       eventResp.Event.Id,
		OrgID:    org.OrgID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time"))
}

func TestSubmitRunnerEvents_Publishes(t *testing.T) {
	t.Parallel()

	pub := &publishRecorder{}
	st := teststore.New(t)
	srv := New(Options{Store: st, WSS: pub})
	org := teststore.CreateOrg(t, st, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name: "test", Runner: "r", Workspace: "w",
	})
	assert.NilError(t, err)

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

	calls := pub.publishCalls()
	assert.Equal(t, len(calls), 2)
	assert.DeepEqual(t, calls[1].N, model.Notification{
		Type:     "updated",
		Resource: "task",
		ID:       taskResp.Task.Id,
		OrgID:    org.OrgID,
	}, cmpopts.IgnoreFields(model.Notification{}, "Time", "Version"))
}
