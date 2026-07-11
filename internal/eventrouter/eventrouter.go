package eventrouter

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store"
)

// Attrs maps a dimension name to the event's values for that dimension.
// Single-valued dimensions are one-element slices. Webhook handlers populate it
// at extraction time and the attribute matcher (Match) reads from it. See
// proposals/draft/attribute-based-event-matching.md.
type Attrs map[string][]string

// InputEvent represents a parsed webhook event ready for routing. Its matchable
// dimensions travel in Attrs (populated by the webhook handlers), which the
// attribute matcher reads via Attr.
type InputEvent struct {
	Source      string
	Type        string
	Description string
	Data        string
	URL         string
	UserID      string
	// Orgs names the orgs this event belongs to, resolved by the webhook handler
	// independent of the actor's membership (GitHub: the orgs sharing the App
	// installation; Atlassian: the org in the webhook's ?org= param). It gates
	// non-member routing: a rule with Public can fire for one of these orgs even
	// when UserID is empty or the actor is not a member. The webhook wire-up that
	// populates it lands in later layers; until then it is nil and Route behaves
	// exactly as before (member-only).
	Orgs []int64
	// Attrs carries the event's matchable dimensions keyed by dimension name
	// (e.g. "mention", "assignee", "label"), which the attribute matcher reads.
	Attrs Attrs
	// Details is source-defined key/value context copied verbatim into the
	// persisted ExternalPayload for consumers (agent, UI). It is distinct from
	// Attrs: Attrs are matchable routing dimensions the matcher reads and the
	// router drops after routing; Details is persisted payload the router does
	// not interpret.
	Details map[string]string
	// Meta carries source-specific data that the router does not interpret. It
	// lets webhook handlers attach native identity (e.g. the GitHub author)
	// without leaking source-specific types into eventrouter.
	Meta any
}

// Attr returns the event's values for a dimension. The "body" and "url"
// dimensions are derived views over Data and URL so extractors don't
// duplicate them; any other key reads from Attrs (nil/absent -> nil).
func (e InputEvent) Attr(key string) []string {
	switch key {
	case "body":
		return []string{e.Data}
	case "url":
		return []string{e.URL}
	default:
		return e.Attrs[key]
	}
}

// RouteOutcome describes what the Router did with an InputEvent for one org. It
// gives the callback the routing context — which org matched, the rule, the
// affected tasks, and whether a task was created — so it can react differently
// per case:
//
//	Created                       -> a task was created
//	!Created && len(TaskIDs) > 0  -> existing task(s) were woken
//	!Created && len(TaskIDs) == 0 -> matched, but nothing was created or woken
type RouteOutcome struct {
	Input   InputEvent         // the routed event, including its Meta
	OrgID   int64              // the org whose routing rule matched
	Rule    *model.RoutingRule // the rule that matched
	TaskIDs []int64            // tasks created or woken
	Created bool               // whether a task was created (vs woken / matched-only)
}

// Router routes events to subscribed tasks via the store.
type Router struct {
	Log       *slog.Logger
	Store     *store.Store
	Publisher pubsub.Publisher

	// Registry supplies the schemas backing the ruleless-org default rules
	// (reg.DefaultRules()). When nil, Route falls back to DefaultSchemaRegistry —
	// the process-wide registry the producer packages populate from init — so
	// production construction sites need not set it. Tests inject an isolated
	// registry to control which schemas are present.
	Registry *SchemaRegistry

	// OnRouteOutcome, if set, is called synchronously once per matched org
	// after routing handles that org, with the request context. The Router
	// imposes no concurrency or lifetime policy — the callback owns that (e.g.
	// spawning its own goroutine). Optional — nil disables it (e.g. the
	// Atlassian router leaves it unset).
	OnRouteOutcome func(ctx context.Context, outcome RouteOutcome)
}

// RouteMatch is the read-only result of evaluating routing rules for one org:
// the rule that matched and its position in the org's configured list. It is
// what a dry run reports and what apply consumes. The subscribed-link lookup and
// the wake-vs-create decision are derived from this by the caller, not stored
// here.
type RouteMatch struct {
	OrgID     int64
	Rule      *model.RoutingRule
	RuleIndex int // index into the org's configured rules (-1 = shipped default)
}

// Plan evaluates routing rules for the event and returns one RouteMatch per org
// whose rules matched, without any side effects and without touching links. When
// orgFilter is non-zero, only that org is evaluated (the test path scopes to
// caller.OrgID); zero preserves the webhook path's all-member-orgs behavior.
func (r *Router) Plan(ctx context.Context, input InputEvent, orgFilter int64) ([]RouteMatch, error) {
	orgs, err := r.Store.ListRoutingRulesForEvent(ctx, nil, input.UserID, input.Orgs)
	if err != nil {
		return nil, err
	}

	// The store returns conditions-native rules (legacy rows translated on read),
	// so matching runs directly through the attribute-based matcher against the
	// input event. Resolve the registry once for the default-rule fallback
	// (nil-defaulting to the process-wide registry).
	reg := cmp.Or(r.Registry, DefaultSchemaRegistry)

	// First matching rule per org; orgs with no match are dropped.
	var matches []RouteMatch
	for _, org := range orgs {
		// orgFilter scopes the simulation to a single org (the test path); zero
		// evaluates every returned org, preserving the webhook path's behavior.
		if orgFilter != 0 && org.OrgID != orgFilter {
			continue
		}
		rules := org.Rules
		// RuleIndex points into the org's configured rules; -1 flags a match
		// against the shipped default rules used for ruleless orgs.
		defaulted := false
		if len(rules) == 0 && org.IsMember {
			// Ruleless orgs fall back to the producers' per-type "xagent:"
			// body-prefix wakeup defaults, already conditions-native. The
			// fallback stays member-only: a ruleless non-member org routes
			// nothing, since non-member routing always requires an explicit
			// opt-in rule.
			rules = reg.DefaultRules()
			defaulted = true
		}
		for i, rule := range rules {
			// Member org: every rule is eligible. Non-member org (in input.Orgs
			// but not the actor's): only rules that opted in via Public.
			if !org.IsMember && !rule.Public {
				continue
			}
			if Match(rule, input) {
				index := i
				if defaulted {
					index = -1
				}
				matches = append(matches, RouteMatch{OrgID: org.OrgID, Rule: &rule, RuleIndex: index})
				break
			}
		}
	}
	return matches, nil
}

// Route evaluates routing rules for every org the event belongs to. An org's
// rules are eligible when the actor is a member of it (existing behavior — all
// the org's rules apply) or when the org appears in input.Orgs and the rule
// opted in via Public. For each org with a matching rule, it either wakes the
// subscribed task(s) for the event URL or — if the matched rule has a Create
// action — creates a new task in a single transaction. Returns the number of
// tasks woken or created.
func (r *Router) Route(ctx context.Context, input InputEvent) (int, error) {
	// An empty UserID no longer short-circuits: a non-member event (no linked
	// actor) can still route through input.Orgs. A URL is still required — it's
	// the routing key for the subscribed-link lookup below.
	if input.URL == "" {
		return 0, nil
	}

	// Plan is the shared read-only matcher; Route applies its result. The zero
	// orgFilter keeps Route evaluating every org the event belongs to.
	matches, err := r.Plan(ctx, input, 0)
	if err != nil {
		return 0, err
	}
	if len(matches) == 0 {
		return 0, nil
	}

	// Link lookup runs only for orgs that have a matching rule. Links are stored
	// with routing_key = model.RoutingKey(url), so derive the key the same way:
	// a non-canonical event URL (a comment URL, an API URL) resolves to the same
	// key as the stored link.
	orgIDs := make([]int64, 0, len(matches))
	for _, match := range matches {
		orgIDs = append(orgIDs, match.OrgID)
	}
	key := model.RoutingKey(input.URL)
	linksByOrg, err := r.Store.FindSubscribedLinksForOrgs(ctx, nil, key, orgIDs)
	if err != nil {
		return 0, err
	}

	// Wake if a subscribed link exists; otherwise create if the matched rule opts in.
	var n int
	for _, match := range matches {
		orgID, rule := match.OrgID, match.Rule
		// Built at the top, updated by whichever branch runs, passed to the
		// callback at the end. Defaults to "matched, nothing done" (no TaskIDs,
		// Created false).
		outcome := RouteOutcome{Input: input, OrgID: orgID, Rule: rule}

		if links := linksByOrg[orgID]; len(links) > 0 {
			// Events are task-scoped: fan the external event out as one event row
			// per subscribed task instead of a shared row plus junction rows.
			seen := map[int64]bool{}
			for _, link := range links {
				if seen[link.TaskID] {
					continue
				}
				seen[link.TaskID] = true
				if err := r.attach(ctx, link.TaskID, input, orgID, rule.Wakeup); err != nil {
					r.Log.Error("failed to attach event to task", "task_id", link.TaskID, "err", err)
					continue
				}
				outcome.TaskIDs = append(outcome.TaskIDs, link.TaskID)
				n++
			}
		} else if rule.Create != nil {
			taskID, err := r.create(ctx, input, orgID, rule)
			if err != nil {
				r.Log.Error("failed to create task from rule", "org_id", orgID, "err", err)
				continue
			}
			outcome.TaskIDs = []int64{taskID}
			outcome.Created = true
			n++
		}
		// else: matched a rule but no subscribed link and no create action —
		// outcome stays empty, and we still fire the callback below.

		if r.OnRouteOutcome != nil {
			r.OnRouteOutcome(ctx, outcome)
		}
	}
	return n, nil
}

func (r *Router) publish(ctx context.Context, n model.Notification) {
	if n.Ignore {
		return
	}
	if r.Publisher == nil {
		return
	}
	if err := r.Publisher.Publish(ctx, n); err != nil {
		r.Log.Warn("failed to publish notification", "err", err)
	}
}

// attach creates a task-scoped event for a task and publishes a change
// notification. Both wake modes share the event-create + publish; they differ
// only on whether the task is restarted.
//
// When wake is true (the default behavior), it starts the task, logs the
// action, and sets the wake ChannelMessage only when the attach restarts a
// task that had finished its run (IsDone); an empty ChannelMessage keeps the
// agent channel silent (PR #725's gate) for already-running / already-queued
// tasks while the FE still receives the same notification.
//
// When wake is false (a rule with Wakeup: false), it leaves the task untouched
// — no task.Start(), no audit log — but still emits a channel notification
// unconditionally so the event isn't silently swallowed.
func (r *Router) attach(ctx context.Context, taskID int64, input InputEvent, orgID int64, wake bool) error {
	event := &model.Event{
		TaskID: taskID,
		OrgID:  orgID,
		Wake:   wake,
		Payload: &model.ExternalPayload{
			Description: input.Description,
			URL:         input.URL,
			Data:        input.Data,
			Details:     input.Details,
		},
	}
	notification := model.Notification{
		Type:  "change",
		OrgID: orgID,
		Time:  time.Now(),
	}
	err := r.Store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := r.Store.CreateEvent(ctx, tx, event); err != nil {
			return err
		}
		if !wake {
			// No-wake path: the event is attached and the FE is notified, but the
			// task is not restarted. Emit a channel message unconditionally —
			// otherwise the event would reach the task with no signal at all.
			notification.Resources = []model.NotificationResource{
				{Action: "updated", Type: "task", ID: taskID},
				{Action: "updated", Type: "event", ID: event.ID},
			}
			notification.ChannelMessage = fmt.Sprintf("Task %d: %s (%s)", taskID, input.Description, input.URL)
			return tx.Commit()
		}
		task, err := r.Store.GetTaskForUpdate(ctx, tx, taskID, event.OrgID)
		if err != nil {
			return err
		}
		wasDone := task.IsDone()
		task.Start()
		if err := r.Store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		// The external event above already records the trigger that restarted the
		// task, so a router-emitted RESTARTED lifecycle event would be redundant:
		// the timeline already shows why the task woke. Restarted lifecycle events
		// are reserved for user-initiated restarts (see RestartTask), where there's
		// no external event to explain the transition.
		notification.Resources = []model.NotificationResource{
			{Action: "updated", Type: "task", ID: task.ID},
			{Action: "updated", Type: "event", ID: event.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		}
		notification.Runner = task.PendingRunner()
		if wasDone {
			notification.ChannelMessage = fmt.Sprintf("Task %d woken by event %d: %s (%s)", task.ID, event.ID, input.Description, input.URL)
		}
		return tx.Commit()
	})
	if err != nil {
		return err
	}
	r.publish(ctx, notification)
	return nil
}

// create handles the create-task branch of routing. It creates the task, a
// subscribed link, and an audit log in a single transaction. Dedup for
// sequential redeliveries comes from the routing-level link lookup in
// Route: once this tx commits, the next event for the same URL sees the
// subscribed link and takes the wake path. Genuinely-concurrent
// overlapping txns can still produce duplicates — accepted as a v1
// limitation.
func (r *Router) create(ctx context.Context, input InputEvent, orgID int64, rule *model.RoutingRule) (int64, error) {
	var notification model.Notification
	var taskID int64
	err := r.Store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		// A custom prompt replaces the default preamble rather than supplementing
		// it — use one or the other, never both.
		prompt := rule.Create.Prompt
		if prompt == "" {
			prompt = fmt.Sprintf("You were created by a routing rule in response to a %s %s event.", input.Source, input.Type)
		}
		task := &model.Task{
			Runner:      rule.Create.Runner,
			Workspace:   rule.Create.Workspace,
			Status:      model.TaskStatusPending,
			Command:     model.TaskCommandStart,
			Version:     1,
			OrgID:       orgID,
			AutoArchive: rule.Create.AutoArchive,
		}
		if err := r.Store.CreateTask(ctx, tx, task); err != nil {
			return err
		}
		taskID = task.ID
		// Emit the external (trigger) event first so it leads the timeline (ordered
		// by event id), the same way it appears when an event wakes an existing task
		// (see attach). The event that caused the task to exist should precede the
		// "Created" lifecycle event it triggered.
		if err := r.Store.CreateEvent(ctx, tx, &model.Event{
			TaskID: task.ID,
			OrgID:  orgID,
			Payload: &model.ExternalPayload{
				Description: input.Description,
				URL:         input.URL,
				Data:        input.Data,
				Details:     input.Details,
			},
		}); err != nil {
			return err
		}
		// Record the creation as a lifecycle event. The router (not a user) created
		// the task, so the actor is the routing-rule actor; a freshly created task
		// has no prior status. Emit it after the external trigger but before the
		// link and instruction events so the timeline reads
		// External -> Created -> Link -> Instruction.
		if err := r.Store.CreateEvent(ctx, tx, &model.Event{
			TaskID: task.ID,
			OrgID:  task.OrgID,
			Payload: &model.LifecyclePayload{
				Kind:     model.LifecycleKindCreated,
				Actor:    model.RouterActor,
				ToStatus: task.Status.Label(),
			},
		}); err != nil {
			return err
		}
		// task_links is the subscription/list projection; the link event is the
		// timeline source of truth. Upsert the projection and append the event in
		// the same tx so they can't drift (mirrors the apiserver CreateLink path).
		// Emit the link (the trigger) before the instruction so the timeline
		// (ordered by event id) shows what triggered the task before its prompt.
		//
		// Leave the title empty: the triggering event's description now lives on
		// the external event above, so the link no longer has to double as the
		// trigger's label.
		link := &model.Link{
			TaskID:     task.ID,
			URL:        input.URL,
			RoutingKey: model.RoutingKey(input.URL),
			Relevance:  "trigger",
			Subscribe:  true,
			CreatedAt:  time.Now().UTC(),
		}
		if err := r.Store.CreateLink(ctx, tx, link); err != nil {
			return err
		}
		if err := r.Store.CreateEvent(ctx, tx, &model.Event{
			TaskID:  task.ID,
			OrgID:   orgID,
			Payload: link.EventPayload(),
		}); err != nil {
			return err
		}
		// Seed the stream with the rule's prompt as the initial instruction event
		// (replacing the old tasks.instructions column). The task already starts
		// via Command=Start; instruction events always wake.
		if err := r.Store.CreateEvent(ctx, tx, &model.Event{
			TaskID:  task.ID,
			OrgID:   orgID,
			Wake:    true,
			Payload: &model.InstructionPayload{Text: prompt},
		}); err != nil {
			return err
		}
		notification = model.Notification{
			Type:  "change",
			OrgID: orgID,
			Time:  time.Now(),
			Resources: []model.NotificationResource{
				{Action: "created", Type: "task", ID: task.ID},
				{Action: "appended", Type: "task_logs", ID: task.ID},
			},
			Runner:         task.PendingRunner(),
			ChannelMessage: fmt.Sprintf("Task %d created by routing rule for event: %s (%s)", task.ID, input.Description, input.URL),
		}
		return tx.Commit()
	})
	if err != nil {
		return 0, err
	}
	r.publish(ctx, notification)
	return taskID, nil
}
