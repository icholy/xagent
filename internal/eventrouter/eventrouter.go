package eventrouter

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
)

// EventType identifies the source of a webhook event.
type EventType string

const (
	EventTypeGitHub    EventType = "github"
	EventTypeAtlassian EventType = "atlassian"
)

// Event represents a parsed webhook event ready for routing.
type Event struct {
	Type        EventType
	Description string
	Data        string
	URL         string
	UserID      string
}

// Router routes events to subscribed tasks via the store.
type Router struct {
	Log   *slog.Logger
	Store *store.Store
}

// Route finds all subscribed links matching the event URL for the given user,
// creates events per org, and starts the associated tasks. It returns the total
// number of tasks routed and any error finding links.
func (r *Router) Route(ctx context.Context, event Event) (int, error) {
	linksByOrg, err := r.find(ctx, event)
	if err != nil {
		return 0, err
	}
	var n int
	for orgID, links := range linksByOrg {
		me := &model.Event{
			Description: event.Description,
			Data:        event.Data,
			URL:         event.URL,
			OrgID:       orgID,
		}
		if err := r.Store.CreateEvent(ctx, nil, me); err != nil {
			r.Log.Error("failed to create event", "org_id", orgID, "error", err)
			continue
		}
		for _, link := range links {
			if err := r.attach(ctx, link.TaskID, me); err != nil {
				r.Log.Error("failed to attach event to task", "event_id", me.ID, "task_id", link.TaskID, "error", err)
				continue
			}
			n++
		}
	}
	return n, nil
}

// find queries all matching subscribed links for a URL across all
// the user's orgs and groups them by org ID.
func (r *Router) find(ctx context.Context, event Event) (map[int64][]*model.Link, error) {
	if event.URL == "" {
		return nil, nil
	}
	matches, err := r.Store.FindSubscribedLinksByURLForUser(ctx, nil, event.URL, event.UserID)
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
