package eventrouter

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

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

func TestRouter_AttachToRunningTaskStaysSilent(t *testing.T) {
	t.Parallel()

	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	url := "https://github.com/owner/repo/pull/1#issuecomment-1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Runner:    "r",
		Workspace: "w",
		Status:    model.TaskStatusRunning,
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

	// The notification still publishes with the usual resources so the FE
	// invalidates, but ChannelMessage is empty — PR #725's gate keeps the
	// agent channel silent on a no-transition attach.
	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.ChannelMessage, "")
	assert.Equal(t, len(calls[0].N.Resources), 3)
}

func TestRouteCreateRuleSpawnsTask(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source:  "github",
		Mention: "icholy-bot",
		Create: &model.CreateTaskAction{
			Workspace: "default",
			Runner:    "test-runner",
			Prompt:    "Triage this issue.",
		},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source:      "github",
		Type:        "issue_comment",
		Description: "alice commented on issue #1",
		Data:        "@icholy-bot please look at this",
		URL:         url,
		UserID:      org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	task := tasks[0]
	assert.Equal(t, task.Workspace, "default")
	assert.Equal(t, task.Runner, "test-runner")
	assert.Equal(t, task.Status, model.TaskStatusPending)
	assert.Equal(t, task.Command, model.TaskCommandStart)
	assert.Equal(t, len(task.Instructions), 2)
	// Preamble orients the agent with source/type only — URL, description,
	// and event body are all reachable via the subscribed link.
	assert.Assert(t, strings.Contains(task.Instructions[0].Text, "routing rule"))
	assert.Assert(t, !strings.Contains(task.Instructions[0].Text, url))
	assert.Assert(t, !strings.Contains(task.Instructions[0].Text, "alice commented"))
	assert.Assert(t, !strings.Contains(task.Instructions[0].Text, "please look at this"))
	assert.Equal(t, task.Instructions[0].URL, "")
	assert.Equal(t, task.Instructions[1].Text, "Triage this issue.")

	links, err := s.ListLinksByTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(links), 1)
	assert.Equal(t, links[0].URL, url)
	assert.Equal(t, links[0].Subscribe, true)
	assert.Equal(t, links[0].Title, "alice commented on issue #1")
}

func TestRouteCreateRuleAppliesArchiveAfter(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{
			Workspace:    "default",
			Runner:       "test-runner",
			ArchiveAfter: 24 * time.Hour,
		},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "hello",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	assert.Equal(t, tasks[0].ArchiveAfter, 24*time.Hour)
}

func TestRouteCreateRuleDefaultsArchiveAfterToNever(t *testing.T) {
	t.Parallel()

	// Arrange — a create rule without ArchiveAfter leaves the task at the
	// "never auto-archive" default (zero duration).
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/2"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "hello",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	assert.Equal(t, tasks[0].ArchiveAfter, time.Duration(0))
}

func TestRouteCreateRuleOmittedPromptUsesPreambleOnly(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/2"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "hello",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	assert.Equal(t, len(tasks[0].Instructions), 1)
}

func TestRouteSecondEventWakesCreatedTask(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	input := InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "first",
		URL:    url,
		UserID: org.UserID,
	}

	// Act: first event creates
	n1, err := r.Route(t.Context(), input)
	assert.NilError(t, err)
	assert.Equal(t, n1, 1)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	created := tasks[0]

	// Bring the task back to a wakeable state so the wake path is observable.
	created.Status = model.TaskStatusCompleted
	created.Command = model.TaskCommandNone
	assert.NilError(t, s.UpdateTask(t.Context(), nil, created))

	// Act: replay creates no new task and wakes the existing one
	input.Data = "second"
	n2, err := r.Route(t.Context(), input)
	assert.NilError(t, err)
	assert.Equal(t, n2, 1)

	tasks, err = s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	updated, err := s.GetTask(t.Context(), nil, created.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusPending)
	assert.Equal(t, updated.Command, model.TaskCommandStart)
}

func TestRouteRedeliveryDedup(t *testing.T) {
	t.Parallel()

	// Arrange: sequential replay. The first Route creates the task and a
	// subscribed link; the second Route sees that link via the routing-level
	// FindSubscribedLinksForOrgs lookup and takes the wake path instead of
	// creating a duplicate. (Genuinely-concurrent overlapping txns can still
	// produce duplicates — accepted as a v1 limitation.)
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/3"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	input := InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "trigger",
		URL:    url,
		UserID: org.UserID,
	}

	// Act: two back-to-back identical events
	_, err = r.Route(t.Context(), input)
	assert.NilError(t, err)
	_, err = r.Route(t.Context(), input)
	assert.NilError(t, err)

	// Assert: only one task and one subscribed link exist
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	links, err := s.FindSubscribedLinksForOrgs(t.Context(), nil, url, []int64{org.OrgID})
	assert.NilError(t, err)
	assert.Equal(t, len(links[org.OrgID]), 1)
}

func TestRouteFirstMatchingRuleWins(t *testing.T) {
	t.Parallel()

	// Arrange: wake-only rule ordered before create-rule shadows the create.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/4"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{
		{Source: "github"}, // wake-only, matches
		{Source: "github", Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"}},
	})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "hi",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 0)

	// Reorder so the create-rule comes first.
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{
		{Source: "github", Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"}},
		{Source: "github"},
	})
	assert.NilError(t, err)
	n, err = r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "hi",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	tasks, err = s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
}

func TestRoutePerOrgIsolation(t *testing.T) {
	t.Parallel()

	// Arrange: user in org A (matching link, no create rule) and org B (no link, create rule).
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)
	assert.NilError(t, s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID: orgB.OrgID, UserID: orgA.UserID, Role: "member",
	}))
	url := "https://github.com/owner/repo/issues/5"
	teststore.CreateTask(t, s, orgA, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	err := s.SetOrgRoutingRules(t.Context(), nil, orgB.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "xagent: do it", // matches org A's defaults; matches org B's source-only rule
		URL:    url,
		UserID: orgA.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 2)

	tasksA, err := s.ListTasks(t.Context(), nil, orgA.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasksA), 1)
	assert.Equal(t, tasksA[0].Status, model.TaskStatusPending)

	tasksB, err := s.ListTasks(t.Context(), nil, orgB.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasksB), 1)
	assert.Equal(t, tasksB[0].Status, model.TaskStatusPending)
}

func TestRouteLinkQueryScopedToMatchedOrgs(t *testing.T) {
	t.Parallel()

	// Arrange: user in orgs A, B, C. Only B has a matching rule. A has a link
	// at the URL, C has no rule and no link. A's link must NOT cause a wake
	// because no rule matched in A.
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)
	orgC := teststore.CreateOrg(t, s, nil)
	assert.NilError(t, s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID: orgB.OrgID, UserID: orgA.UserID, Role: "member",
	}))
	assert.NilError(t, s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID: orgC.OrgID, UserID: orgA.UserID, Role: "member",
	}))
	url := "https://github.com/owner/repo/issues/6"
	taskA := teststore.CreateTask(t, s, orgA, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	// Org A has rules that don't match this event (mention required, none present).
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, orgA.OrgID, []model.RoutingRule{
		{Source: "github", Mention: "someone-else"},
	}))
	// Org B has a matching create rule.
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, orgB.OrgID, []model.RoutingRule{
		{Source: "github", Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"}},
	}))
	// Org C has empty rules — defaultRules apply, prefix won't match.
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "no mention, no xagent prefix",
		URL:    url,
		UserID: orgA.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1) // only org B creates

	// Org A's task is untouched.
	updated, err := s.GetTask(t.Context(), nil, taskA.ID, orgA.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusCompleted)

	// Org B has the newly created task.
	tasksB, err := s.ListTasks(t.Context(), nil, orgB.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasksB), 1)

	// Org C has no tasks.
	tasksC, err := s.ListTasks(t.Context(), nil, orgC.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasksC), 0)
}

func TestRouteRuleLessOrgUsesDefaultRules(t *testing.T) {
	t.Parallel()

	// Arrange: org with no routing_rules; the membership-grounded query must
	// still return it so defaultRules applies.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/7"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	r := &Router{Log: slog.Default(), Store: s}

	// Act: matching prefix wakes
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do it",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	updated, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusPending)

	// Act: non-matching prefix is a no-op
	updated.Status = model.TaskStatusCompleted
	updated.Command = model.TaskCommandNone
	assert.NilError(t, s.UpdateTask(t.Context(), nil, updated))
	n, err = r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "just a regular comment",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteCreateRuleThatDoesNotMatch(t *testing.T) {
	t.Parallel()

	// Arrange: a create-rule that requires a mention, an event without one.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/8"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source:  "github",
		Mention: "bot",
		Create:  &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "no mention here",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 0)
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

func TestRouteAssignmentCreatesTaskAndLink(t *testing.T) {
	t.Parallel()

	// Arrange: create-rule gated on assignee for pull_request_assigned events.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/9"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source:   "github",
		Type:     "pull_request_assigned",
		Assignee: "icholy-bot",
		Create:   &model.CreateTaskAction{Workspace: "default", Runner: "r", Prompt: "Review it."},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source:      "github",
		Type:        "pull_request_assigned",
		Description: "alice assigned PR #9 to @icholy-bot",
		URL:         url,
		UserID:      org.UserID,
		Assignee:    "icholy-bot",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	assert.Equal(t, tasks[0].Workspace, "default")
	assert.Equal(t, tasks[0].Runner, "r")

	links, err := s.FindSubscribedLinksForOrgs(t.Context(), nil, url, []int64{org.OrgID})
	assert.NilError(t, err)
	assert.Equal(t, len(links[org.OrgID]), 1)
	assert.Equal(t, links[org.OrgID][0].URL, url)
}

func TestRouteAssignmentCreateThenCommentWakes(t *testing.T) {
	t.Parallel()

	// Arrange: two rules — an assignment create-rule and a comment wake-rule.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/9"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{
		{
			Source:   "github",
			Type:     "pull_request_assigned",
			Assignee: "icholy-bot",
			Create:   &model.CreateTaskAction{Workspace: "default", Runner: "r"},
		},
		{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
	})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act: first event creates the task and a subscribed link.
	n, err := r.Route(t.Context(), InputEvent{
		Source:   "github",
		Type:     "pull_request_assigned",
		URL:      url,
		UserID:   org.UserID,
		Assignee: "icholy-bot",
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	created := tasks[0]

	// Bring the task back to a wakeable state so the wake path is observable.
	created.Status = model.TaskStatusCompleted
	created.Command = model.TaskCommandNone
	assert.NilError(t, s.UpdateTask(t.Context(), nil, created))

	// Act: a subsequent comment on the same URL wakes the existing task —
	// no second task is created.
	n, err = r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "xagent: please look",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	tasks, err = s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 1)
	updated, err := s.GetTask(t.Context(), nil, created.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusPending)
	assert.Equal(t, updated.Command, model.TaskCommandStart)
}

func TestRouteAssignmentWrongAssigneeIsNoOp(t *testing.T) {
	t.Parallel()

	// Arrange: create-rule gated on a specific assignee.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/9"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source:   "github",
		Type:     "pull_request_assigned",
		Assignee: "icholy-bot",
		Create:   &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}})
	assert.NilError(t, err)
	r := &Router{Log: slog.Default(), Store: s}

	// Act: an assignment event for a different assignee does not match.
	n, err := r.Route(t.Context(), InputEvent{
		Source:   "github",
		Type:     "pull_request_assigned",
		URL:      url,
		UserID:   org.UserID,
		Assignee: "someone-else",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 0)
}

func TestRouterPublish_IgnoreSuppressesDelivery(t *testing.T) {
	t.Parallel()

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	r := &Router{Log: slog.Default(), Publisher: pub}

	r.publish(t.Context(), model.Notification{Type: "change", OrgID: 1, Ignore: true})
	assert.Equal(t, len(pub.PublishCalls()), 0)

	r.publish(t.Context(), model.Notification{Type: "change", OrgID: 1})
	assert.Equal(t, len(pub.PublishCalls()), 1)
}
