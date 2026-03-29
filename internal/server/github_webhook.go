package server

import (
	"context"
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

	// Find matching notify links across all the user's orgs
	linksByOrg, err := s.findWebhookLinksByOrg(r.Context(), extracted.url, user.ID)
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
		if err := s.store.CreateEvent(r.Context(), nil, event); err != nil {
			slog.Error("failed to create event", "org_id", orgID, "error", err)
			continue
		}
		routed := s.routeEventToLinks(r.Context(), event.ID, links, orgID)
		totalRouted += routed
	}

	slog.Info("GitHub webhook processed", "url", extracted.url, "orgs", len(linksByOrg), "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
}

// findWebhookLinksByOrg queries all matching notify links for a URL across all
// the user's orgs and groups them by org ID.
func (s *Server) findWebhookLinksByOrg(ctx context.Context, url string, userID string) (map[int64][]*model.Link, error) {
	if url == "" {
		return nil, nil
	}
	matches, err := s.store.FindNotifyLinksByURLForUser(ctx, nil, url, userID)
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
func (s *Server) routeEventToLinks(ctx context.Context, eventID int64, links []*model.Link, orgID int64) int {
	taskIDs := map[int64]bool{}
	for _, link := range links {
		if taskIDs[link.TaskID] {
			continue
		}
		taskIDs[link.TaskID] = true
		if err := s.store.AddEventTask(ctx, nil, eventID, link.TaskID); err != nil {
			s.log.Warn("failed to add event task", "event_id", eventID, "task_id", link.TaskID, "error", err)
		}
		err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			task, err := s.store.GetTaskForUpdate(ctx, tx, link.TaskID, orgID)
			if err != nil {
				return err
			}
			task.Start()
			if err := s.store.UpdateTask(ctx, tx, task); err != nil {
				return err
			}
			return tx.Commit()
		})
		if err != nil {
			s.log.Warn("failed to start task", "task_id", link.TaskID, "error", err)
		}
	}
	return len(taskIDs)
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
