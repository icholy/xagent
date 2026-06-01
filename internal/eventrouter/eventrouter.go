package eventrouter

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store"
)

// InputEvent represents a parsed webhook event ready for routing.
type InputEvent struct {
	Source      string
	Type        string
	Description string
	Data        string
	URL         string
	UserID      string
	Assignee    string
	// Values is a bag of discrete matchable tokens the event provides (e.g.
	// added Jira labels). RoutingRule.Value matches by membership. This is
	// internal/transient — it is not part of the proto and is not persisted.
	Values []string
	// Meta carries source-specific data that the router does not interpret. It
	// lets webhook handlers attach native identity (e.g. the GitHub author)
	// without leaking source-specific types into eventrouter.
	Meta any
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

	// OnRouteOutcome, if set, is called synchronously once per matched org
	// after routing handles that org, with the request context. The Router
	// imposes no concurrency or lifetime policy — the callback owns that (e.g.
	// spawning its own goroutine). Optional — nil disables it (e.g. the
	// Atlassian router leaves it unset).
	OnRouteOutcome func(ctx context.Context, outcome RouteOutcome)
}

// defaultRules is the fallback when an org has no custom routing rules configured.
var defaultRules = []model.RoutingRule{
	{Prefix: "xagent:"},
}

// Route evaluates routing rules for every org the user belongs to. For each
// org with a matching rule, it either wakes the subscribed task(s) for the
// event URL or — if the matched rule has a Create action — creates a new
// task in a single transaction. Returns the number of tasks woken or created.
func (r *Router) Route(ctx context.Context, input InputEvent) (int, error) {
	if input.URL == "" || input.UserID == "" {
		return 0, nil
	}

	rulesByOrg, err := r.Store.ListRoutingRulesForUser(ctx, nil, input.UserID)
	if err != nil {
		return 0, err
	}

	// First matching rule per org; orgs with no match are dropped.
	matched := map[int64]*model.RoutingRule{}
	for orgID, rules := range rulesByOrg {
		if len(rules) == 0 {
			rules = defaultRules
		}
		for i := range rules {
			if input.MatchRule(rules[i]) {
				matched[orgID] = &rules[i]
				break
			}
		}
	}
	if len(matched) == 0 {
		return 0, nil
	}

	// Link lookup runs only for orgs that have a matching rule.
	orgIDs := make([]int64, 0, len(matched))
	for orgID := range matched {
		orgIDs = append(orgIDs, orgID)
	}
	linksByOrg, err := r.links(ctx, input.URL, orgIDs)
	if err != nil {
		return 0, err
	}

	// Wake if a subscribed link exists; otherwise create if the matched rule opts in.
	var n int
	for orgID, rule := range matched {
		// Built at the top, updated by whichever branch runs, passed to the
		// callback at the end. Defaults to "matched, nothing done" (no TaskIDs,
		// Created false).
		outcome := RouteOutcome{Input: input, OrgID: orgID, Rule: rule}

		if links := linksByOrg[orgID]; len(links) > 0 {
			event := &model.Event{
				Description: input.Description,
				Data:        input.Data,
				URL:         input.URL,
				OrgID:       orgID,
			}
			if err := r.Store.CreateEvent(ctx, nil, event); err != nil {
				r.Log.Error("failed to create event", "org_id", orgID, "error", err)
				continue
			}
			seen := map[int64]bool{}
			for _, link := range links {
				if seen[link.TaskID] {
					continue
				}
				seen[link.TaskID] = true
				if err := r.attach(ctx, link.TaskID, event); err != nil {
					r.Log.Error("failed to attach event to task", "event_id", event.ID, "task_id", link.TaskID, "error", err)
					continue
				}
				outcome.TaskIDs = append(outcome.TaskIDs, link.TaskID)
				n++
			}
		} else if rule.Create != nil {
			taskID, err := r.create(ctx, input, orgID, rule)
			if err != nil {
				r.Log.Error("failed to create task from rule", "org_id", orgID, "error", err)
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

// links queries subscribed links matching the URL, scoped to the given orgs,
// grouped by org ID. Subscribe-filtering happens in SQL.
func (r *Router) links(ctx context.Context, url string, orgIDs []int64) (map[int64][]*model.Link, error) {
	if url == "" || len(orgIDs) == 0 {
		return nil, nil
	}
	return r.Store.FindSubscribedLinksForOrgs(ctx, nil, url, orgIDs)
}

func (r *Router) publish(ctx context.Context, n model.Notification) {
	if n.Ignore {
		return
	}
	if r.Publisher == nil {
		return
	}
	if err := r.Publisher.Publish(ctx, n); err != nil {
		r.Log.Warn("failed to publish notification", "error", err)
	}
}

// attach associates an event with a task, starts the task, logs the action,
// and publishes a change notification. The wake ChannelMessage is set only
// when the attach restarts a task that had finished its run (IsDone); an
// empty ChannelMessage keeps the agent channel silent (PR #725's gate) for
// already-running / already-queued tasks while the FE still receives the
// same notification.
func (r *Router) attach(ctx context.Context, taskID int64, event *model.Event) error {
	notification := model.Notification{
		Type:  "change",
		OrgID: event.OrgID,
		Time:  time.Now(),
	}
	err := r.Store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := r.Store.AddEventTask(ctx, tx, event.ID, taskID); err != nil {
			return err
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
		if err := r.Store.CreateLog(ctx, tx, &model.Log{
			TaskID:  taskID,
			Type:    "audit",
			Content: "webhook started task",
		}); err != nil {
			return err
		}
		notification.Resources = []model.NotificationResource{
			{Action: "updated", Type: "task", ID: task.ID},
			{Action: "updated", Type: "event", ID: event.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		}
		notification.Runner = task.PendingRunner()
		if wasDone {
			notification.ChannelMessage = fmt.Sprintf("Task %d woken by event %d: %s (%s)", task.ID, event.ID, event.Description, event.URL)
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
			Runner:    rule.Create.Runner,
			Workspace: rule.Create.Workspace,
			Instructions: []model.Instruction{{
				Text: prompt,
			}},
			Status:       model.TaskStatusPending,
			Command:      model.TaskCommandStart,
			Version:      1,
			OrgID:        orgID,
			ArchiveAfter: rule.Create.ArchiveAfter,
		}
		if err := r.Store.CreateTask(ctx, tx, task); err != nil {
			return err
		}
		taskID = task.ID
		if err := r.Store.CreateLink(ctx, tx, &model.Link{
			TaskID:     task.ID,
			URL:        input.URL,
			RoutingURL: model.RoutingURL(input.URL),
			Title:      input.Description,
			Relevance:  "trigger",
			Subscribe:  true,
			CreatedAt:  time.Now().UTC(),
		}); err != nil {
			return err
		}
		if err := r.Store.CreateLog(ctx, tx, &model.Log{
			TaskID:  task.ID,
			Type:    "audit",
			Content: "webhook created task",
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
