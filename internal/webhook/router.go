package webhook

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
)

// Event represents a parsed webhook event ready for routing.
type Event struct {
	Description string
	Data        string
	URL         string
}

// EventRouter routes webhook events to subscribed tasks.
type EventRouter struct {
	Log   *slog.Logger
	Store *store.Store
}

// Route finds all subscribed links matching the event URL for the given user,
// creates events per org, and starts the associated tasks. It returns the total
// number of tasks routed and any error finding links.
func (r *EventRouter) Route(ctx context.Context, event Event, userID string, auditMessage string) (int, error) {
	linksByOrg, err := r.findLinksByOrg(ctx, event.URL, userID)
	if err != nil {
		return 0, err
	}
	var totalRouted int
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
		totalRouted += r.routeEventToLinks(ctx, me.ID, links, orgID, auditMessage)
	}
	return totalRouted, nil
}

// findLinksByOrg queries all matching subscribed links for a URL across all
// the user's orgs and groups them by org ID.
func (r *EventRouter) findLinksByOrg(ctx context.Context, url string, userID string) (map[int64][]*model.Link, error) {
	if url == "" {
		return nil, nil
	}
	matches, err := r.Store.FindSubscribedLinksByURLForUser(ctx, nil, url, userID)
	if err != nil {
		return nil, err
	}
	result := map[int64][]*model.Link{}
	for _, m := range matches {
		result[m.OrgID] = append(result[m.OrgID], m.Link)
	}
	return result, nil
}

// routeEventToLinks routes an event to the tasks referenced by the given links.
// It returns the number of tasks successfully routed.
func (r *EventRouter) routeEventToLinks(ctx context.Context, eventID int64, links []*model.Link, orgID int64, auditMessage string) int {
	taskIDs := map[int64]bool{}
	for _, link := range links {
		if taskIDs[link.TaskID] {
			continue
		}
		taskIDs[link.TaskID] = true
		err := r.Store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if err := r.Store.AddEventTask(ctx, tx, eventID, link.TaskID); err != nil {
				return err
			}
			task, err := r.Store.GetTaskForUpdate(ctx, tx, link.TaskID, orgID)
			if err != nil {
				return err
			}
			task.Start()
			if err := r.Store.UpdateTask(ctx, tx, task); err != nil {
				return err
			}
			if err := r.Store.CreateLog(ctx, tx, &model.Log{
				TaskID:  link.TaskID,
				Type:    "audit",
				Content: auditMessage,
			}); err != nil {
				return err
			}
			return tx.Commit()
		})
		if err != nil {
			r.Log.Warn("failed to route event to task", "event_id", eventID, "task_id", link.TaskID, "error", err)
		}
	}
	return len(taskIDs)
}
