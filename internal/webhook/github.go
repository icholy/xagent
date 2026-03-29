package webhook

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
	"github.com/icholy/xagent/internal/store"
)

// GitHubHandler handles incoming GitHub App webhook events.
type GitHubHandler struct {
	Log           *slog.Logger
	Store         *store.Store
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

	slog.Info("GitHub webhook processed", "url", extracted.url, "orgs", len(linksByOrg), "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
}

// findLinksByOrg queries all matching notify links for a URL across all
// the user's orgs and groups them by org ID.
func (h *GitHubHandler) findLinksByOrg(ctx context.Context, url string, userID string) (map[int64][]*model.Link, error) {
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
func (h *GitHubHandler) routeEventToLinks(ctx context.Context, eventID int64, links []*model.Link, orgID int64) int {
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
				Content: "github webhook started task",
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
		if !strings.HasPrefix(body, "xagent:") {
			return nil
		}
		login := event.Comment.User.GetLogin()
		number := event.PullRequest.GetNumber()
		return &githubWebhookEvent{
			description:    fmt.Sprintf("%s commented on PR #%d review", login, number),
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
		if !strings.HasPrefix(body, "xagent:") {
			return nil
		}
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
