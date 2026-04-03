package eventrouter

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/icholy/xagent/internal/model"
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
	Log   *slog.Logger
	Store *store.Store
}

// Route finds all subscribed links matching the event URL for the given user,
// creates events per org, and starts the associated tasks. It returns the total
// number of tasks routed and any error finding links.
func (r *Router) Route(ctx context.Context, input InputEvent) (int, error) {
	if !strings.HasPrefix(strings.TrimSpace(input.Data), "xagent:") {
		return 0, nil
	}
	linksByOrg, err := r.find(ctx, input)
	if err != nil {
		return 0, err
	}
	var n int
	for orgID, links := range linksByOrg {
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
	matches, err := r.Store.FindSubscribedLinksByURLForUser(ctx, nil, input.URL, input.UserID)
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
