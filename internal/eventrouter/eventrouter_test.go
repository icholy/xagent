package eventrouter

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store"

	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// taskInstructions reads a task's instruction events from the stream — the
// brief replacement for the old tasks.instructions column.
func taskInstructions(t *testing.T, s *store.Store, taskID, orgID int64) []*model.InstructionPayload {
	t.Helper()
	events, err := s.ListEventsByTask(t.Context(), nil, taskID, orgID, []string{model.EventTypeInstruction})
	assert.NilError(t, err)
	var out []*model.InstructionPayload
	for _, e := range events {
		if inst, ok := e.Payload.(*model.InstructionPayload); ok {
			out = append(out, inst)
		}
	}
	return out
}

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

func TestRouteNonCanonicalURLMatchesByRoutingKey(t *testing.T) {
	t.Parallel()

	// Arrange: a subscribed link stored under the canonical issue key. The
	// incoming event carries a non-canonical comment URL — the router must
	// derive the same routing key from it to match. A raw-URL lookup would miss.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	canonical := "https://github.com/o/r/issues/5"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: canonical, Subscribe: true}},
	})
	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	// Act: event URL is the comment permalink, not the canonical issue URL.
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: fix tests",
		URL:    "https://github.com/o/r/issues/5#issuecomment-9",
		UserID: org.UserID,
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

func TestRouteNoWakeAttachesEventWithoutRestart(t *testing.T) {
	t.Parallel()

	// Arrange: a matching rule that opts out of waking via Wakeup{Enable:false},
	// and a done task subscribed to the event URL.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	url := "https://github.com/owner/repo/pull/1#issuecomment-1"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Runner:    "r",
		Workspace: "w",
		Status:    model.TaskStatusCompleted,
		Links:     []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{
		{Source: "github", Wakeup: false},
	}))
	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	r := &Router{Log: slog.Default(), Store: s, Publisher: pub}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source:      "github",
		Description: "PR comment from alice",
		Data:        "anything",
		URL:         url,
		UserID:      org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	// The task is NOT restarted — its status stays done.
	updated, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusCompleted)

	// The event was still attached to the task.
	events, err := s.ListEventsByTask(t.Context(), nil, task.ID, org.OrgID, []string{model.EventTypeExternal})
	assert.NilError(t, err)
	assert.Equal(t, len(events), 1)

	// A channel notification is published unconditionally so the event isn't
	// silently swallowed, even though the task wasn't restarted.
	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	msg := calls[0].N.ChannelMessage
	assert.Assert(t, msg != "", "expected non-empty ChannelMessage, got empty")
	assert.Assert(t, strings.Contains(msg, "PR comment from alice"), "expected description in message: %q", msg)
	assert.Assert(t, strings.Contains(msg, url), "expected URL in message: %q", msg)
}

func TestRouteWakeEnabledRestartsTask(t *testing.T) {
	t.Parallel()

	// Arrange: same setup but with Wakeup:true — the task IS restarted, the
	// current behavior.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	url := "https://github.com/owner/repo/pull/1#issuecomment-1"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Runner:    "r",
		Workspace: "w",
		Status:    model.TaskStatusCompleted,
		Links:     []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{
		{Source: "github", Wakeup: true},
	}))
	r := &Router{Log: slog.Default(), Store: s}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source:      "github",
		Description: "PR comment from alice",
		Data:        "anything",
		URL:         url,
		UserID:      org.UserID,
	})

	// Assert: the task was restarted.
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
	updated, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusPending)

	// Waking a done task records both the external trigger and the RESTARTED
	// lifecycle transition (done -> pending) in the same stream, so the
	// materialized status doesn't flip without a source-of-truth event behind it.
	events, err := s.ListEventsByTask(t.Context(), nil, task.ID, org.OrgID, nil)
	assert.NilError(t, err)
	var external, restarted int
	for _, e := range events {
		switch p := e.Payload.(type) {
		case *model.ExternalPayload:
			external++
		case *model.LifecyclePayload:
			if p.Kind == model.LifecycleKindRestarted {
				assert.Equal(t, p.Actor, model.RouterActor)
				assert.Equal(t, p.FromStatus, "Completed")
				assert.Equal(t, p.ToStatus, "Pending")
				restarted++
			}
		}
	}
	assert.Equal(t, external, 1)
	assert.Equal(t, restarted, 1)
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
	// A custom prompt replaces the default preamble entirely — the task gets
	// exactly the configured prompt and nothing else.
	insts := taskInstructions(t, s, task.ID, org.OrgID)
	assert.Equal(t, len(insts), 1)
	assert.Equal(t, insts[0].Text, "Triage this issue.")
	assert.Equal(t, insts[0].URL, "")

	links, err := s.ListLinksByTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(links), 1)
	assert.Equal(t, links[0].URL, url)
	assert.Equal(t, links[0].Subscribe, true)
	assert.Equal(t, links[0].Title, "alice commented on issue #1")

	// The create path appends a link event mirroring the task_links row (the
	// timeline source of truth; task_links is the projection), alongside the
	// instruction event it already seeds.
	linkEvents, err := s.ListEventsByTask(t.Context(), nil, task.ID, org.OrgID, []string{model.EventTypeLink})
	assert.NilError(t, err)
	assert.Equal(t, len(linkEvents), 1)
	linkPayload, ok := linkEvents[0].Payload.(*model.LinkPayload)
	assert.Assert(t, ok)
	assert.Equal(t, linkPayload.LinkID, links[0].ID)
	assert.Equal(t, linkPayload.URL, url)
	assert.Equal(t, linkPayload.Relevance, "trigger")
	assert.Equal(t, linkPayload.Title, "alice commented on issue #1")
	assert.Equal(t, linkPayload.Subscribe, true)
	assert.Equal(t, linkEvents[0].Wake, false)

	// The timeline (ordered by event id) must read
	// External -> Created -> Link -> Instruction: the trigger event that caused
	// the task to exist first (the same way a woken task surfaces its trigger),
	// then the lifecycle event, the link that triggered the task, and finally the
	// prompt the agent acts on.
	allEvents, err := s.ListEventsByTask(t.Context(), nil, task.ID, org.OrgID, nil)
	assert.NilError(t, err)
	var types []string
	for _, e := range allEvents {
		types = append(types, e.Payload.Type())
	}
	assert.DeepEqual(t, types, []string{
		model.EventTypeExternal,
		model.EventTypeLifecycle,
		model.EventTypeLink,
		model.EventTypeInstruction,
	})

	// The trigger surfaces as an external event carrying the webhook payload,
	// mirroring the wake path (attach).
	externalEvents, err := s.ListEventsByTask(t.Context(), nil, task.ID, org.OrgID, []string{model.EventTypeExternal})
	assert.NilError(t, err)
	assert.Equal(t, len(externalEvents), 1)
	externalPayload, ok := externalEvents[0].Payload.(*model.ExternalPayload)
	assert.Assert(t, ok)
	assert.Equal(t, externalPayload.Description, "alice commented on issue #1")
	assert.Equal(t, externalPayload.URL, url)
	assert.Equal(t, externalPayload.Data, "@icholy-bot please look at this")
}

func TestRouteCreateRuleWithoutPromptUsesDefaultPreamble(t *testing.T) {
	t.Parallel()

	// Arrange — a create rule with no custom prompt falls back to the default
	// preamble orienting the agent with the event source/type.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{Workspace: "default", Runner: "test-runner"},
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
	insts := taskInstructions(t, s, task.ID, org.OrgID)
	assert.Equal(t, len(insts), 1)
	assert.Assert(t, strings.Contains(insts[0].Text, "routing rule"))
	assert.Assert(t, strings.Contains(insts[0].Text, "github"))
	assert.Assert(t, strings.Contains(insts[0].Text, "issue_comment"))
}

func TestRouteCreateRuleAppliesAutoArchive(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Create: &model.CreateTaskAction{
			Workspace:   "default",
			Runner:      "test-runner",
			AutoArchive: 24 * time.Hour,
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
	assert.Equal(t, tasks[0].AutoArchive, 24*time.Hour)
}

func TestRouteCreateRuleDefaultsAutoArchiveToNever(t *testing.T) {
	t.Parallel()

	// Arrange — a create rule without AutoArchive leaves the task at the
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
	assert.Equal(t, tasks[0].AutoArchive, time.Duration(0))
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
	assert.Equal(t, len(taskInstructions(t, s, tasks[0].ID, org.OrgID)), 1)
}

func TestRouteSecondEventWakesCreatedTask(t *testing.T) {
	t.Parallel()

	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{{
		Source: "github",
		Wakeup: true,
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
	// The router derives routing_key so the created link is matchable; for an
	// already-canonical URL it equals url.
	assert.Equal(t, links[org.OrgID][0].RoutingKey, url)
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
		{Source: "github", Type: "issue_comment", Prefix: "xagent:", Wakeup: true},
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

func TestRouteOnRouteOutcomeFiresOnCreate(t *testing.T) {
	t.Parallel()

	// Arrange: a create-rule with no subscribed link — the create branch runs.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	rule := model.RoutingRule{
		Source: "github",
		Create: &model.CreateTaskAction{Workspace: "default", Runner: "r"},
	}
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{rule}))

	var outcomes []RouteOutcome
	r := &Router{
		Log:   slog.Default(),
		Store: s,
		OnRouteOutcome: func(_ context.Context, o RouteOutcome) {
			outcomes = append(outcomes, o)
		},
	}

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

	assert.Equal(t, len(outcomes), 1)
	got := outcomes[0]
	assert.Equal(t, got.OrgID, org.OrgID)
	assert.Equal(t, got.Created, true)
	assert.Equal(t, got.Rule.Source, "github")
	assert.Assert(t, got.Rule.Create != nil)
	assert.DeepEqual(t, got.TaskIDs, []int64{tasks[0].ID})
	assert.Equal(t, got.Input.URL, url)
}

func TestRouteOnRouteOutcomeFiresOnWake(t *testing.T) {
	t.Parallel()

	// Arrange: a subscribed link exists, so the wake branch runs.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	var outcomes []RouteOutcome
	r := &Router{
		Log:   slog.Default(),
		Store: s,
		OnRouteOutcome: func(_ context.Context, o RouteOutcome) {
			outcomes = append(outcomes, o)
		},
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: fix tests",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	assert.Equal(t, len(outcomes), 1)
	got := outcomes[0]
	assert.Equal(t, got.OrgID, org.OrgID)
	assert.Equal(t, got.Created, false)
	assert.DeepEqual(t, got.TaskIDs, []int64{task.ID})
}

func TestRouteOnRouteOutcomeFiresOnMatchOnly(t *testing.T) {
	t.Parallel()

	// Arrange: a wake-only rule matches but there is no subscribed link at the
	// URL and the rule has no Create action — matched, nothing done.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, []model.RoutingRule{
		{Source: "github"},
	}))

	var outcomes []RouteOutcome
	r := &Router{
		Log:   slog.Default(),
		Store: s,
		OnRouteOutcome: func(_ context.Context, o RouteOutcome) {
			outcomes = append(outcomes, o)
		},
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Type:   "issue_comment",
		Data:   "no link, no create",
		URL:    url,
		UserID: org.UserID,
	})

	// Assert: the callback still fires, but nothing was woken or created.
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
	tasks, err := s.ListTasks(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 0)

	assert.Equal(t, len(outcomes), 1)
	got := outcomes[0]
	assert.Equal(t, got.OrgID, org.OrgID)
	assert.Equal(t, got.Created, false)
	assert.Equal(t, len(got.TaskIDs), 0)
}

func TestRouteOnRouteOutcomeDoesNotFireWhenNoMatch(t *testing.T) {
	t.Parallel()

	// Arrange: the default "xagent:" prefix rule applies, but the event has no
	// matching prefix — no rule matches, so the callback never fires.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/issues/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	var outcomes []RouteOutcome
	r := &Router{
		Log:   slog.Default(),
		Store: s,
		OnRouteOutcome: func(_ context.Context, o RouteOutcome) {
			outcomes = append(outcomes, o)
		},
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
	assert.Equal(t, len(outcomes), 0)
}

func TestRouteOnRouteOutcomeFiresOncePerMatchedOrg(t *testing.T) {
	t.Parallel()

	// Arrange: user in two orgs, both with a subscribed link at the URL.
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	taskA := teststore.CreateTask(t, s, orgA, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	taskB := teststore.CreateTask(t, s, orgB, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	assert.NilError(t, s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  orgB.OrgID,
		UserID: orgA.UserID,
		Role:   "member",
	}))

	byOrg := map[int64]RouteOutcome{}
	r := &Router{
		Log:   slog.Default(),
		Store: s,
		OnRouteOutcome: func(_ context.Context, o RouteOutcome) {
			byOrg[o.OrgID] = o
		},
	}

	// Act
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    url,
		UserID: orgA.UserID,
	})

	// Assert: fired once per matched org with the right woken task.
	assert.NilError(t, err)
	assert.Equal(t, n, 2)
	assert.Equal(t, len(byOrg), 2)
	assert.DeepEqual(t, byOrg[orgA.OrgID].TaskIDs, []int64{taskA.ID})
	assert.DeepEqual(t, byOrg[orgB.OrgID].TaskIDs, []int64{taskB.ID})
	assert.Equal(t, byOrg[orgA.OrgID].Created, false)
	assert.Equal(t, byOrg[orgB.OrgID].Created, false)
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
