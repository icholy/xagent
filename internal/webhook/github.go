package webhook

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/githubx"
)

// GitHubHandler handles incoming GitHub App webhook events.
type GitHubHandler struct {
	Router        Router
	Store         Store
	WebhookSecret string
}

func (h *GitHubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	webhookEvent, err := githubx.ParseWebHook(body, r.Header, []byte(h.WebhookSecret))
	if err != nil {
		slog.Error("failed to parse GitHub webhook", "error", err)
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}
	extracted := extractGitHubWebhookEvent(webhookEvent)
	if extracted == nil {
		eventType := r.Header.Get("X-GitHub-Event")
		slog.Debug("ignoring GitHub webhook event", "event_type", eventType)
		fmt.Fprintf(w, "ignored")
		return
	}

	// Look up xagent owner by GitHub user ID
	user, err := h.Store.GetUserByGitHubUserID(r.Context(), nil, extracted.githubUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Info("no linked GitHub account", "github_user_id", extracted.githubUserID)
			fmt.Fprintf(w, "no linked account")
			return
		}
		slog.Error("failed to look up GitHub account", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update cached username if it changed
	if extracted.githubUsername != "" && extracted.githubUsername != user.GitHubUsername {
		if err := h.Store.UpdateGitHubUsername(r.Context(), nil, user.GitHubUserID, extracted.githubUsername); err != nil {
			slog.Warn("failed to update GitHub username", "error", err)
		}
	}

	// Route event to subscribed tasks
	event := eventrouter.Event{
		Type:        eventrouter.EventTypeGitHub,
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

	slog.Info("GitHub webhook processed", "url", extracted.url, "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
}

type githubWebhookEvent struct {
	description    string
	data           string
	url            string
	githubUserID   int64
	githubUsername string
}

func extractGitHubWebhookEvent(webhookEvent any) *githubWebhookEvent {
	switch event := webhookEvent.(type) {
	case *github.IssueCommentEvent:
		if event.Comment == nil || event.Issue == nil ||
			event.Comment.Body == nil || event.Issue.HTMLURL == nil ||
			event.Comment.User == nil || event.Comment.User.ID == nil {
			return nil
		}
		body := strings.TrimSpace(*event.Comment.Body)
		login := event.Comment.User.GetLogin()
		number := event.Issue.GetNumber()
		description := fmt.Sprintf("%s commented on issue #%d", login, number)
		if event.Issue.IsPullRequest() {
			description = fmt.Sprintf("%s commented on PR #%d", login, number)
		}
		return &githubWebhookEvent{
			description:    description,
			data:           body,
			url:            *event.Issue.HTMLURL,
			githubUserID:   *event.Comment.User.ID,
			githubUsername: login,
		}

	case *github.PullRequestReviewCommentEvent:
		if event.Comment == nil || event.PullRequest == nil ||
			event.Comment.Body == nil || event.PullRequest.HTMLURL == nil ||
			event.Comment.User == nil || event.Comment.User.ID == nil {
			return nil
		}
		body := strings.TrimSpace(*event.Comment.Body)
		login := event.Comment.User.GetLogin()
		number := event.PullRequest.GetNumber()
		return &githubWebhookEvent{
			description:    fmt.Sprintf("%s reviewed PR #%d", login, number),
			data:           body,
			url:            *event.PullRequest.HTMLURL,
			githubUserID:   *event.Comment.User.ID,
			githubUsername: login,
		}

	case *github.PullRequestReviewEvent:
		if event.Action == nil || *event.Action != "submitted" ||
			event.Review == nil || event.PullRequest == nil ||
			event.Review.Body == nil || event.PullRequest.HTMLURL == nil ||
			event.Review.User == nil || event.Review.User.ID == nil {
			return nil
		}
		body := strings.TrimSpace(*event.Review.Body)
		login := event.Review.User.GetLogin()
		number := event.PullRequest.GetNumber()
		return &githubWebhookEvent{
			description:    fmt.Sprintf("%s reviewed PR #%d", login, number),
			data:           body,
			url:            *event.PullRequest.HTMLURL,
			githubUserID:   *event.Review.User.ID,
			githubUsername: login,
		}
	}

	return nil
}
