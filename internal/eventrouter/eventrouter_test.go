package eventrouter

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// withTxNoop mocks WithTx by calling f with a nil tx. The production
// callback ends with tx.Commit() which panics on nil, so we recover from it.
func withTxNoop(_ context.Context, _ *sql.Tx, f func(tx *sql.Tx) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// tx.Commit() on nil *sql.Tx - expected in mock
		}
	}()
	return f(nil)
}

func TestRouteCreatesEventAndStartsTask(t *testing.T) {
	var createdEvent *model.Event
	var updatedTask *model.Task
	var createdLog *model.Log

	r := &Router{
		Log: slog.Default(),
		Store: &StoreMock{
			FindSubscribedLinksByURLForUserFunc: func(ctx context.Context, tx *sql.Tx, url string, userID string) ([]store.LinkWithOrg, error) {
				return []store.LinkWithOrg{
					{Link: &model.Link{TaskID: 10}, OrgID: 1},
				}, nil
			},
			CreateEventFunc: func(ctx context.Context, tx *sql.Tx, event *model.Event) error {
				event.ID = 100
				createdEvent = event
				return nil
			},
			WithTxFunc: withTxNoop,
			AddEventTaskFunc: func(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
				return nil
			},
			GetTaskForUpdateFunc: func(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (*model.Task, error) {
				return &model.Task{
					ID:      id,
					OrgID:   orgID,
					Status:  model.TaskStatusCompleted,
					Command: model.TaskCommandNone,
				}, nil
			},
			UpdateTaskFunc: func(ctx context.Context, tx *sql.Tx, task *model.Task) error {
				updatedTask = task
				return nil
			},
			CreateLogFunc: func(ctx context.Context, tx *sql.Tx, log *model.Log) error {
				createdLog = log
				return nil
			},
		},
	}

	n, err := r.Route(t.Context(), Event{
		Type:        EventTypeGitHub,
		Description: "testuser commented on PR #1",
		Data:        "xagent: fix tests",
		URL:         "https://github.com/owner/repo/pull/1",
		UserID:      "user-1",
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	// Event was created with correct fields
	assert.Equal(t, createdEvent.Description, "testuser commented on PR #1")
	assert.Equal(t, createdEvent.Data, "xagent: fix tests")
	assert.Equal(t, createdEvent.URL, "https://github.com/owner/repo/pull/1")
	assert.Equal(t, createdEvent.OrgID, int64(1))

	// Task was started
	assert.Equal(t, updatedTask.ID, int64(10))
	assert.Equal(t, updatedTask.Status, model.TaskStatusPending)

	// Audit log was created
	assert.Equal(t, createdLog.TaskID, int64(10))
	assert.Equal(t, createdLog.Type, "audit")
	assert.Equal(t, createdLog.Content, "github webhook started task")
}

func TestRouteMultipleOrgs(t *testing.T) {
	var startedTaskIDs []int64

	r := &Router{
		Log: slog.Default(),
		Store: &StoreMock{
			FindSubscribedLinksByURLForUserFunc: func(ctx context.Context, tx *sql.Tx, url string, userID string) ([]store.LinkWithOrg, error) {
				return []store.LinkWithOrg{
					{Link: &model.Link{TaskID: 10}, OrgID: 1},
					{Link: &model.Link{TaskID: 20}, OrgID: 2},
				}, nil
			},
			CreateEventFunc: func(ctx context.Context, tx *sql.Tx, event *model.Event) error {
				event.ID = event.OrgID * 100
				return nil
			},
			WithTxFunc: withTxNoop,
			AddEventTaskFunc: func(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
				return nil
			},
			GetTaskForUpdateFunc: func(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (*model.Task, error) {
				return &model.Task{
					ID:      id,
					OrgID:   orgID,
					Status:  model.TaskStatusCompleted,
					Command: model.TaskCommandNone,
				}, nil
			},
			UpdateTaskFunc: func(ctx context.Context, tx *sql.Tx, task *model.Task) error {
				startedTaskIDs = append(startedTaskIDs, task.ID)
				return nil
			},
			CreateLogFunc: func(ctx context.Context, tx *sql.Tx, log *model.Log) error {
				return nil
			},
		},
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    "https://github.com/owner/repo/pull/1",
		UserID: "user-1",
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 2)
	assert.Equal(t, len(startedTaskIDs), 2)
}

func TestRouteDeduplicatesTasksWithMultipleLinks(t *testing.T) {
	var updateCount int

	r := &Router{
		Log: slog.Default(),
		Store: &StoreMock{
			FindSubscribedLinksByURLForUserFunc: func(ctx context.Context, tx *sql.Tx, url string, userID string) ([]store.LinkWithOrg, error) {
				return []store.LinkWithOrg{
					{Link: &model.Link{TaskID: 10}, OrgID: 1},
					{Link: &model.Link{TaskID: 10}, OrgID: 1},
				}, nil
			},
			CreateEventFunc: func(ctx context.Context, tx *sql.Tx, event *model.Event) error {
				event.ID = 100
				return nil
			},
			WithTxFunc: withTxNoop,
			AddEventTaskFunc: func(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error {
				return nil
			},
			GetTaskForUpdateFunc: func(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (*model.Task, error) {
				return &model.Task{
					ID:      id,
					OrgID:   orgID,
					Status:  model.TaskStatusCompleted,
					Command: model.TaskCommandNone,
				}, nil
			},
			UpdateTaskFunc: func(ctx context.Context, tx *sql.Tx, task *model.Task) error {
				updateCount++
				return nil
			},
			CreateLogFunc: func(ctx context.Context, tx *sql.Tx, log *model.Log) error {
				return nil
			},
		},
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    "https://github.com/owner/repo/pull/1",
		UserID: "user-1",
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	assert.Equal(t, updateCount, 1)
}

func TestRouteNoMatchingLinks(t *testing.T) {
	r := &Router{
		Log: slog.Default(),
		Store: &StoreMock{
			FindSubscribedLinksByURLForUserFunc: func(ctx context.Context, tx *sql.Tx, url string, userID string) ([]store.LinkWithOrg, error) {
				return nil, nil
			},
		},
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    "https://github.com/owner/repo/pull/1",
		UserID: "user-1",
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteEmptyURL(t *testing.T) {
	r := &Router{
		Log: slog.Default(),
		Store: &StoreMock{
			FindSubscribedLinksByURLForUserFunc: func(ctx context.Context, tx *sql.Tx, url string, userID string) ([]store.LinkWithOrg, error) {
				t.Fatal("should not be called for empty URL")
				return nil, nil
			},
		},
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    "",
		UserID: "user-1",
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}
