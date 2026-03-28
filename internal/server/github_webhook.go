package server

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/githubx"
	"github.com/icholy/xagent/internal/model"
)

// handleGitHubWebhook handles incoming GitHub App webhook events.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	webhookEvent, err := githubx.ParseWebHook(body, r.Header, []byte(s.github.WebhookSecret))
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
	user, err := s.store.GetUserByGitHubUserID(r.Context(), nil, extracted.githubUserID)
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
		if err := s.store.UpdateGitHubUsername(r.Context(), nil, user.GitHubUserID, extracted.githubUsername); err != nil {
			slog.Warn("failed to update GitHub username", "error", err)
		}
	}

	// Create event in store
	event := &model.Event{
		Description: extracted.description,
		Data:        extracted.data,
		URL:         extracted.url,
		OrgID:       user.DefaultOrgID,
	}
	if err := s.store.CreateEvent(r.Context(), nil, event); err != nil {
		slog.Error("failed to create event", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Route to matching tasks
	ids, err := s.processEventInternal(r.Context(), event.ID, event.URL, user.DefaultOrgID)
	if err != nil {
		slog.Error("failed to process event", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("GitHub webhook processed", "event_id", event.ID, "url", event.URL, "tasks_routed", len(ids))
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
		if !strings.HasPrefix(body, "xagent:") {
			return nil
		}
		description := "A comment was made on an issue"
		if event.Issue.IsPullRequest() {
			description = "A comment was made on a pull request"
		}
		return &githubWebhookEvent{
			description:    description,
			data:           body,
			url:            *event.Issue.HTMLURL,
			githubUserID:   *event.Comment.User.ID,
			githubUsername: event.Comment.User.GetLogin(),
		}

	case *github.PullRequestReviewCommentEvent:
		if event.Comment == nil || event.PullRequest == nil ||
			event.Comment.Body == nil || event.PullRequest.HTMLURL == nil ||
			event.Comment.User == nil || event.Comment.User.ID == nil {
			return nil
		}
		body := strings.TrimSpace(*event.Comment.Body)
		if !strings.HasPrefix(body, "xagent:") {
			return nil
		}
		return &githubWebhookEvent{
			description:    "A review comment was made on a pull request",
			data:           body,
			url:            *event.PullRequest.HTMLURL,
			githubUserID:   *event.Comment.User.ID,
			githubUsername: event.Comment.User.GetLogin(),
		}

	case *github.PullRequestReviewEvent:
		if event.Action == nil || *event.Action != "submitted" ||
			event.Review == nil || event.PullRequest == nil ||
			event.Review.Body == nil || event.PullRequest.HTMLURL == nil ||
			event.Review.User == nil || event.Review.User.ID == nil {
			return nil
		}
		body := strings.TrimSpace(*event.Review.Body)
		if !strings.HasPrefix(body, "xagent:") {
			return nil
		}
		return &githubWebhookEvent{
			description:    "A review was submitted on a pull request",
			data:           body,
			url:            *event.PullRequest.HTMLURL,
			githubUserID:   *event.Review.User.ID,
			githubUsername: event.Review.User.GetLogin(),
		}
	}

	return nil
}
