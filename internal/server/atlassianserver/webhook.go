package atlassianserver

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/x/atlassian"
)

// WebhookHandler handles incoming Atlassian (Jira) webhook events.
type WebhookHandler struct {
	Router Router
	Store  Store
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	// Parse and extract the events. A single webhook may yield multiple events
	// (e.g. several labels added at once), all from the same actor.
	inputs, err := toInputEvents(body)
	if err != nil {
		slog.Error("failed to parse Atlassian webhook payload", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if len(inputs) == 0 {
		slog.Debug("ignoring Atlassian webhook event")
		fmt.Fprintf(w, "ignored")
		return
	}
	// toInputEvents always sets Meta to an AtlassianMeta, so this assertion is
	// safe. It panics loudly if that invariant is ever broken. All events from a
	// single webhook share the same actor.
	meta := inputs[0].Meta.(AtlassianMeta)

	// Look up xagent owner by Atlassian account ID
	user, err := h.Store.GetUserByAtlassianAccountID(r.Context(), nil, meta.AuthorAccountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Info("no linked Atlassian account", "atlassian_account_id", meta.AuthorAccountID)
			fmt.Fprintf(w, "no linked account")
			return
		}
		slog.Error("failed to look up Atlassian account", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Route events to subscribed tasks
	for _, input := range inputs {
		input.UserID = user.ID
		routed, err := h.Router.Route(r.Context(), input)
		if err != nil {
			slog.Error("failed to route event", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		slog.Info("Atlassian webhook processed", "url", input.URL, "type", input.Type, "tasks_routed", routed)
	}

	fmt.Fprintf(w, "processed")
}

// AtlassianMeta is attached to an eventrouter.InputEvent's Meta field, carrying
// Atlassian-native identity that the router does not interpret.
type AtlassianMeta struct {
	AuthorAccountID   string
	AuthorDisplayName string
}

// Event-type strings set on eventrouter.InputEvent.Type by toInputEvents. They
// form a contract between the extractor (producer) and any consumer that
// dispatches on InputEvent.Type.
const (
	EventTypeCommentCreated = "comment_created"
	EventTypeLabelAdded     = "label_added"
)

// toInputEvents extracts zero or more routable events from a Jira webhook
// payload. A single webhook can yield multiple events (e.g. one per label added
// in an issue update). It returns an empty slice for events that should be
// ignored.
func toInputEvents(body []byte) ([]eventrouter.InputEvent, error) {
	payload, err := atlassian.ParseWebhook(body)
	if err != nil {
		return nil, err
	}

	switch payload.WebhookEvent {
	case atlassian.WebhookEventCommentCreated:
		if payload.Comment == nil || payload.Issue == nil {
			return nil, nil
		}
		commentBody := strings.TrimSpace(payload.Comment.Body)

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

		return []eventrouter.InputEvent{{
			Source:      "atlassian",
			Type:        EventTypeCommentCreated,
			Description: description,
			Data:        commentBody,
			URL:         url,
			Meta:        AtlassianMeta{AuthorAccountID: accountID, AuthorDisplayName: displayName},
		}}, nil

	case atlassian.WebhookEventIssueUpdated:
		added := payload.AddedLabels()
		if len(added) == 0 || payload.Issue == nil || payload.User == nil {
			return nil, nil
		}

		accountID := payload.User.AccountID
		displayName := payload.User.DisplayName
		if accountID == "" {
			return nil, nil
		}

		url := payload.Issue.BrowseURL()
		if url == "" {
			return nil, nil
		}

		// Emit one event per added label so routing rules can match a specific
		// label via the Data prefix.
		events := make([]eventrouter.InputEvent, 0, len(added))
		for _, label := range added {
			events = append(events, eventrouter.InputEvent{
				Source:      "atlassian",
				Type:        EventTypeLabelAdded,
				Description: fmt.Sprintf("%s added label %q to %s", displayName, label, payload.Issue.Key),
				Data:        label,
				URL:         url,
				Meta:        AtlassianMeta{AuthorAccountID: accountID, AuthorDisplayName: displayName},
			})
		}
		return events, nil
	}

	return nil, nil
}
