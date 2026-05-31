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

	// Parse and extract the events. A single webhook can yield multiple events
	// (e.g. one jira:issue_updated payload adding several labels).
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
	// toInputEvents always sets Meta to an AtlassianMeta on every event, and
	// all events from one payload share the same actor, so this assertion is
	// safe. It panics loudly if that invariant is ever broken.
	meta := inputs[0].Meta.(AtlassianMeta)

	// Look up xagent owner by Atlassian account ID. The actor is the same for
	// every event from a single payload, so the lookup runs once.
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

	// Route every event to subscribed tasks.
	var totalRouted int
	for _, input := range inputs {
		input.UserID = user.ID
		n, err := h.Router.Route(r.Context(), *input)
		if err != nil {
			slog.Error("failed to route event", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		totalRouted += n
	}

	slog.Info("Atlassian webhook processed", "events", len(inputs), "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
}

// AtlassianMeta is attached to an eventrouter.InputEvent's Meta field, carrying
// Atlassian-native identity that the router does not interpret.
type AtlassianMeta struct {
	AuthorAccountID   string
	AuthorDisplayName string
}

// Event-type strings set on eventrouter.InputEvent.Type by toInputEvent. They
// form a contract between the extractor (producer) and any consumer that
// dispatches on InputEvent.Type.
const (
	EventTypeCommentCreated = "comment_created"
	EventTypeLabelAdded     = "label_added"
)

// JiraEventIssueUpdated is the wire webhookEvent value Jira Cloud sends when an
// issue field changes (including label additions). Unlike comment_created, it
// carries the "jira:" prefix on the wire.
const JiraEventIssueUpdated = "jira:issue_updated"

// toInputEvents parses a Jira webhook payload and converts it into zero or more
// routable events. A comment yields a single comment_created event; an issue
// update yields one label_added event per added label. Every returned event has
// its Meta set to an AtlassianMeta carrying the actor's identity.
func toInputEvents(body []byte) ([]*eventrouter.InputEvent, error) {
	payload, err := atlassian.ParseWebhook(body)
	if err != nil {
		return nil, err
	}

	switch payload.WebhookEvent {
	case EventTypeCommentCreated:
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

		return []*eventrouter.InputEvent{{
			Source:      "atlassian",
			Type:        EventTypeCommentCreated,
			Description: description,
			Data:        commentBody,
			URL:         url,
			Meta:        AtlassianMeta{AuthorAccountID: accountID, AuthorDisplayName: displayName},
		}}, nil

	case JiraEventIssueUpdated:
		if payload.Issue == nil || payload.User == nil {
			return nil, nil
		}
		added := payload.Changelog.AddedLabels()
		if len(added) == 0 {
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

		// One event per added label so each can be routed and matched against a
		// rule's Prefix independently.
		events := make([]*eventrouter.InputEvent, 0, len(added))
		for _, label := range added {
			events = append(events, &eventrouter.InputEvent{
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
