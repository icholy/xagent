package webhook

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/icholy/xagent/internal/atlassian"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/store"
)

// AtlassianHandler handles incoming Atlassian (Jira) webhook events.
type AtlassianHandler struct {
	Router Router
	Store  *store.Store
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

	// Route event to subscribed tasks
	event := eventrouter.Event{
		Type:        eventrouter.EventTypeAtlassian,
		Description: extracted.description,
		Data:        extracted.data,
		URL:         extracted.url,
		UserID:      user.ID,
	}
	totalRouted, err := h.Router.Route(r.Context(), event)
	if err != nil {
		slog.Error("failed to route event", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("Atlassian webhook processed", "url", extracted.url, "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
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

		url := payload.Issue.BrowseURL()
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
