package eventrouter

import (
	"log/slog"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// createTestTask creates a completed task in the given org.
func createTestTask(t *testing.T, s *store.Store, orgID int64, workspace string) *model.Task {
	t.Helper()
	task := &model.Task{
		Name:      "test-task",
		Workspace: workspace,
		Status:    model.TaskStatusCompleted,
		Command:   model.TaskCommandNone,
		OrgID:     orgID,
	}
	err := s.CreateTask(t.Context(), nil, task)
	assert.NilError(t, err)
	return task
}

// createSubscribedLink creates a subscribed link on a task.
func createSubscribedLink(t *testing.T, s *store.Store, taskID int64, url string) *model.Link {
	t.Helper()
	link := &model.Link{
		TaskID:    taskID,
		URL:       url,
		Title:     "test link",
		Subscribe: true,
		CreatedAt: time.Now(),
	}
	err := s.CreateLink(t.Context(), nil, link)
	assert.NilError(t, err)
	return link
}

func TestRouteCreatesEventAndStartsTask(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s)

	task := createTestTask(t, s, org.OrgID, "test")
	createSubscribedLink(t, s, task.ID, "https://github.com/owner/repo/pull/1")

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), Event{
		Type:        EventTypeGitHub,
		Description: "testuser commented on PR #1",
		Data:        "xagent: fix tests",
		URL:         "https://github.com/owner/repo/pull/1",
		UserID:      org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	// Task was started
	updated, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusPending)
}

func TestRouteMultipleOrgs(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s)
	orgB := teststore.CreateOrg(t, s)

	// Add user A as member of org B so FindSubscribedLinksByURLForUser finds both
	err := s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  orgB.OrgID,
		UserID: orgA.UserID,
		Role:   "member",
	})
	assert.NilError(t, err)

	taskA := createTestTask(t, s, orgA.OrgID, "test")
	taskB := createTestTask(t, s, orgB.OrgID, "test")
	url := "https://github.com/owner/repo/pull/1"
	createSubscribedLink(t, s, taskA.ID, url)
	createSubscribedLink(t, s, taskB.ID, url)

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    url,
		UserID: orgA.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 2)
}

func TestRouteDeduplicatesTasksWithMultipleLinks(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s)

	task := createTestTask(t, s, org.OrgID, "test")
	url := "https://github.com/owner/repo/pull/1"
	createSubscribedLink(t, s, task.ID, url)
	createSubscribedLink(t, s, task.ID, url)

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
}

func TestRouteNoMatchingLinks(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s)

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    "https://github.com/owner/repo/pull/1",
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteEmptyURL(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s)

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), Event{
		Type:   EventTypeGitHub,
		URL:    "",
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}
