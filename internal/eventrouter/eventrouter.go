package eventrouter

import (
	"context"
	"database/sql"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store"
)

// InputEvent represents a parsed webhook event ready for routing.
type InputEvent struct {
	Source      string
	Type       string
	Description string
	Data        string
	URL         string
	UserID      string
}

// Router routes events to subscribed tasks via the store.
type Router struct {
	Log       *slog.Logger
	Store     *store.Store
	Publisher pubsub.Publisher
}

// defaultRules is the fallback when an org has no custom routing rules configured.
var defaultRules = []model.RoutingRule{
	{Prefix: "xagent:"},
}

// Route finds all subscribed links matching the event URL for the given user,
// creates events per org, and starts the associated tasks. It returns the total
// number of tasks routed and any error finding links.
func (r *Router) Route(ctx context.Context, input InputEvent) (int, error) {
	linksByOrg, err := r.find(ctx, input)
	if err != nil {
		return 0, err
	}
	if len(linksByOrg) == 0 {
		return 0, nil
	}
	orgRules, err := r.Store.GetRoutingRulesByOrgs(ctx, nil, slices.Collect(maps.Keys(linksByOrg)))
	if err != nil {
		return 0, err
	}
	var n int
	for orgID, links := range linksByOrg {
		rules := orgRules[orgID]
		if len(rules) == 0 {
			rules = defaultRules
		}
		if !slices.ContainsFunc(rules, input.MatchRule) {
			continue
		}
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
		for _, link := range links {
			if err := r.attach(ctx, link.TaskID, event); err != nil {
				r.Log.Error("failed to attach event to task", "event_id", event.ID, "task_id", link.TaskID, "error", err)
				continue
			}
			r.publish(ctx, model.Notification{
				Type: "change",
				Resources: []model.NotificationResource{
					{Action: "updated", Type: "task", ID: link.TaskID},
					{Action: "updated", Type: "event", ID: event.ID},
				},
				OrgID: orgID,
				Time:  time.Now(),
			})
			n++
		}
	}
	return n, nil
}

// find queries all matching subscribed links for a URL across all
// the user's orgs and groups them by org ID.
func (r *Router) find(ctx context.Context, input InputEvent) (map[int64][]*model.Link, error) {
	if input.URL == "" {
		return nil, nil
	}
	matches, err := r.Store.FindSubscribedLinksForUser(ctx, nil, input.URL, input.UserID)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	result := map[int64][]*model.Link{}
	for _, m := range matches {
		if seen[m.Link.TaskID] {
			continue
		}
		seen[m.Link.TaskID] = true
		result[m.OrgID] = append(result[m.OrgID], m.Link)
	}
	return result, nil
}

func (r *Router) publish(ctx context.Context, n model.Notification) {
	if r.Publisher == nil {
		return
	}
	if err := r.Publisher.Publish(ctx, n); err != nil {
		r.Log.Warn("failed to publish notification", "error", err)
	}
}

// attach associates an event with a task, starts the task, and logs the action.
func (r *Router) attach(ctx context.Context, taskID int64, event *model.Event) error {
	return r.Store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := r.Store.AddEventTask(ctx, tx, event.ID, taskID); err != nil {
			return err
		}
		task, err := r.Store.GetTaskForUpdate(ctx, tx, taskID, event.OrgID)
		if err != nil {
			return err
		}
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
		return tx.Commit()
	})
}
