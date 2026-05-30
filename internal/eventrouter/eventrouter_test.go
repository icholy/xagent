package eventrouter

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"

	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestRouteCreatesEventAndStartsTask(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source:      "github",
		Description: "testuser commented on PR #1",
		Data:        "xagent: fix tests",
		URL:         url,
		UserID:      org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	updated, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusPending)
}

func TestRouteMultipleOrgs(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, orgA, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	teststore.CreateTask(t, s, orgB, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	err := s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  orgB.OrgID,
		UserID: orgA.UserID,
		Role:   "member",
	})
	assert.NilError(t, err)
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    url,
		UserID: orgA.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 2)
}

func TestRouteDeduplicatesTasksWithMultipleLinks(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links: []teststore.LinkOptions{
			{URL: url, Subscribe: true},
			{URL: url, Subscribe: true},
		},
	})
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
}

func TestRouteNoMatchingLinks(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    "https://github.com/owner/repo/pull/1",
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteEmptyURL(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    "",
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteSkipsEventsWithoutXAgentPrefix(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "just a regular comment",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouter_AttachSetsWakeMessage(t *testing.T) {
	t.Parallel()

	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	url := "https://github.com/owner/repo/pull/1#issuecomment-1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Runner:    "r",
		Workspace: "w",
		Status:    model.TaskStatusCompleted,
		Links:     []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	r := &Router{
		Log:       slog.Default(),
		Store:     s,
		Publisher: pub,
	}

	n, err := r.Route(t.Context(), InputEvent{
		Source:      "github",
		Description: "PR comment from alice",
		Data:        "xagent: fix tests",
		URL:         url,
		UserID:      org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	msg := calls[0].N.ChannelMessage
	assert.Assert(t, msg != "", "expected non-empty ChannelMessage, got empty")
	assert.Assert(t, strings.Contains(msg, "Task "), "expected task id in message: %q", msg)
	assert.Assert(t, strings.Contains(msg, "woken by event"), "expected wake phrase in message: %q", msg)
	assert.Assert(t, strings.Contains(msg, "PR comment from alice"), "expected description in message: %q", msg)
	assert.Assert(t, strings.Contains(msg, url), "expected URL in message: %q", msg)
}

func TestRouteOrgRulesOverrideDefaults(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{
		{Prefix: "bot:"},
	})
	assert.NilError(t, err)
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act - "xagent:" prefix should NOT match because the org overrode the defaults
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
	updated, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusCompleted)
}
