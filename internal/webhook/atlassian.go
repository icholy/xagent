package webhook

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/icholy/xagent/internal/atlassian"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
)

// AtlassianHandler handles incoming Atlassian (Jira) webhook events.
type AtlassianHandler struct {
	Log   *slog.Logger
	Store *store.Store
}

func (h *AtlassianHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract org ID from query parameter
	orgIDStr := r.URL.Query().Get("org")
	if orgIDStr == "" {
		http.Error(w, "missing org query parameter", http.StatusBadRequest)
		return
	}
	orgID, err := strconv.ParseInt(orgIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid org query parameter", http.StatusBadRequest)
		return
	}

	// Look up the org's webhook secret
	secret, err := h.Store.GetOrgAtlassianWebhookSecret(r.Context(), nil, orgID)
	if err != nil {
		slog.Error("failed to get Atlassian webhook secret", "org_id", orgID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if secret == "" {
		slog.Warn("no Atlassian webhook secret configured", "org_id", orgID)
		http.Error(w, "webhook not configured", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify HMAC-SHA256 signature
	if err := atlassian.VerifyWebhook(body, r.Header.Get("X-Hub-Signature"), secret); err != nil {
		slog.Error("failed to verify Atlassian webhook signature", "error", err)
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	// Parse and extract the event
	extracted, err := extractAtlassianWebhookEvent(body)
	if err != nil {
		slog.Error("failed to parse Atlassian webhook payload", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if extracted == nil {
		slog.Debug("ignoring Atlassian webhook event")
		fmt.Fprintf(w, "ignored")
		return
	}

	// Look up xagent owner by Atlassian account ID
	user, err := h.Store.GetUserByAtlassianAccountID(r.Context(), nil, extracted.atlassianAccountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Info("no linked Atlassian account", "atlassian_account_id", extracted.atlassianAccountID)
			fmt.Fprintf(w, "no linked account")
			return
		}
		slog.Error("failed to look up Atlassian account", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Find matching notify links across all the user's orgs
	linksByOrg, err := h.findLinksByOrg(r.Context(), extracted.url, user.ID)
	if err != nil {
		slog.Error("failed to find matching links", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Create events and route to tasks per org
	var totalRouted int
	for orgID, links := range linksByOrg {
		event := &model.Event{
			Description: extracted.description,
			Data:        extracted.data,
			URL:         extracted.url,
			OrgID:       orgID,
		}
		if err := h.Store.CreateEvent(r.Context(), nil, event); err != nil {
			slog.Error("failed to create event", "org_id", orgID, "error", err)
			continue
		}
		routed := h.routeEventToLinks(r.Context(), event.ID, links, orgID)
		totalRouted += routed
	}

	slog.Info("Atlassian webhook processed", "url", extracted.url, "orgs", len(linksByOrg), "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
}

// findLinksByOrg queries all matching notify links for a URL across all
// the user's orgs and groups them by org ID.
func (h *AtlassianHandler) findLinksByOrg(ctx context.Context, url string, userID string) (map[int64][]*model.Link, error) {
	if url == "" {
		return nil, nil
	}
	matches, err := h.Store.FindNotifyLinksByURLForUser(ctx, nil, url, userID)
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
func (h *AtlassianHandler) routeEventToLinks(ctx context.Context, eventID int64, links []*model.Link, orgID int64) int {
	taskIDs := map[int64]bool{}
	for _, link := range links {
		if taskIDs[link.TaskID] {
			continue
		}
		taskIDs[link.TaskID] = true
		err := h.Store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			if err := h.Store.AddEventTask(ctx, tx, eventID, link.TaskID); err != nil {
				return err
			}
			task, err := h.Store.GetTaskForUpdate(ctx, tx, link.TaskID, orgID)
			if err != nil {
				return err
			}
			task.Start()
			if err := h.Store.UpdateTask(ctx, tx, task); err != nil {
				return err
			}
			if err := h.Store.CreateLog(ctx, tx, &model.Log{
				TaskID:  link.TaskID,
				Type:    "audit",
				Content: "atlassian webhook started task",
			}); err != nil {
				return err
			}
			return tx.Commit()
		})
		if err != nil {
			h.Log.Warn("failed to route event to task", "event_id", eventID, "task_id", link.TaskID, "error", err)
		}
	}
	return len(taskIDs)
}

type atlassianWebhookEvent struct {
	description        string
	data               string
	url                string
	atlassianAccountID string
}

func extractAtlassianWebhookEvent(body []byte) (*atlassianWebhookEvent, error) {
	payload, err := atlassian.ParseWebhook(body)
	if err != nil {
		return nil, err
	}

	switch payload.WebhookEvent {
	case "comment_created":
		if payload.Comment == nil || payload.Issue == nil {
			return nil, nil
		}
		commentBody := strings.TrimSpace(payload.Comment.Body)
		if !strings.HasPrefix(commentBody, "xagent:") {
			return nil, nil
		}

		accountID := payload.Comment.Author.AccountID
		displayName := payload.Comment.Author.DisplayName
		if accountID == "" {
			return nil, nil
		}

		url := atlassian.IssueURL(payload.Issue.Self, payload.Issue.Key)
		if url == "" {
			return nil, nil
		}

		description := fmt.Sprintf("%s commented on %s", displayName, payload.Issue.Key)

		return &atlassianWebhookEvent{
			description:        description,
			data:               commentBody,
			url:                url,
			atlassianAccountID: accountID,
		}, nil
	}

	return nil, nil
}
