package webhookserver

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/githubx"
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
	if event, ok := webhookEvent.(*github.InstallationEvent); ok {
		h.handleInstallationEvent(w, r, event)
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
	input := eventrouter.InputEvent{
		Source:      "github",
		Type:        extracted.eventType,
		Description: extracted.description,
		Data:        extracted.data,
		URL:         extracted.url,
		UserID:      user.ID,
		Assignee:    extracted.assignee,
	}
	totalRouted, err := h.Router.Route(r.Context(), input)
	if err != nil {
		slog.Error("failed to route event", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("GitHub webhook processed", "url", extracted.url, "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
}

func (h *GitHubHandler) handleInstallationEvent(w http.ResponseWriter, r *http.Request, event *github.InstallationEvent) {
	installation := event.GetInstallation()
	installationID := installation.GetID()
	action := event.GetAction()
	switch action {
	case "created":
		pending := &model.PendingIntegration{
			Type:       model.PendingIntegrationTypeGitHub,
			ExternalID: strconv.FormatInt(installationID, 10),
			Options: model.PendingIntegrationOptions{
				GitHub: &model.GitHubPendingIntegration{
					SenderGitHubUserID: event.GetSender().GetID(),
					AccountLogin:       installation.GetAccount().GetLogin(),
					AccountType:        installation.GetAccount().GetType(),
				},
			},
		}
		if err := h.Store.UpsertPendingIntegration(r.Context(), nil, pending); err != nil {
			slog.Error("failed to upsert pending github installation", "error", err, "installation_id", installationID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		slog.Info("github installation pending",
			"installation_id", installationID,
			"sender_id", event.GetSender().GetID(),
			"account", installation.GetAccount().GetLogin())
		fmt.Fprintf(w, "pending")
	case "deleted":
		if err := h.Store.DeletePendingIntegration(r.Context(), nil, model.PendingIntegrationTypeGitHub, strconv.FormatInt(installationID, 10)); err != nil {
			slog.Error("failed to delete pending github installation", "error", err, "installation_id", installationID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := h.Store.ClearGitHubInstallation(r.Context(), nil, installationID); err != nil {
			slog.Error("failed to clear github installation", "error", err, "installation_id", installationID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		slog.Info("github installation cleared", "installation_id", installationID)
		fmt.Fprintf(w, "cleared")
	default:
		slog.Info("github installation event", "action", action, "installation_id", installationID)
		fmt.Fprintf(w, "ignored")
	}
}

type githubWebhookEvent struct {
	eventType      string
	description    string
	data           string
	url            string
	githubUserID   int64
	githubUsername string
	assignee       string
}

func extractGitHubWebhookEvent(webhookEvent any) *githubWebhookEvent {
	switch event := webhookEvent.(type) {
	case *github.IssueCommentEvent:
		if action := event.GetAction(); action != "created" && action != "edited" {
			return nil
		}
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
			eventType:      "issue_comment",
			description:    description,
			data:           body,
			url:            *event.Issue.HTMLURL,
			githubUserID:   *event.Comment.User.ID,
			githubUsername: login,
		}

	case *github.PullRequestReviewCommentEvent:
		if action := event.GetAction(); action != "created" && action != "edited" {
			return nil
		}
		if event.Comment == nil || event.PullRequest == nil ||
			event.Comment.Body == nil || event.PullRequest.HTMLURL == nil ||
			event.Comment.User == nil || event.Comment.User.ID == nil {
			return nil
		}
		body := strings.TrimSpace(*event.Comment.Body)
		login := event.Comment.User.GetLogin()
		number := event.PullRequest.GetNumber()
		return &githubWebhookEvent{
			eventType:      "pull_request_review_comment",
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
			eventType:      "pull_request_review",
			description:    fmt.Sprintf("%s reviewed PR #%d", login, number),
			data:           body,
			url:            *event.PullRequest.HTMLURL,
			githubUserID:   *event.Review.User.ID,
			githubUsername: login,
		}

	case *github.IssuesEvent:
		if event.GetAction() != "assigned" ||
			event.Issue == nil || event.Issue.HTMLURL == nil ||
			event.Assignee == nil || event.Assignee.Login == nil ||
			event.Sender == nil || event.Sender.ID == nil {
			return nil
		}
		senderLogin := event.Sender.GetLogin()
		assigneeLogin := event.Assignee.GetLogin()
		number := event.Issue.GetNumber()
		return &githubWebhookEvent{
			eventType:      "issue_assigned",
			description:    fmt.Sprintf("%s assigned issue #%d to @%s", senderLogin, number, assigneeLogin),
			url:            *event.Issue.HTMLURL,
			githubUserID:   *event.Sender.ID,
			githubUsername: senderLogin,
			assignee:       assigneeLogin,
		}

	case *github.PullRequestEvent:
		if event.GetAction() != "assigned" ||
			event.PullRequest == nil || event.PullRequest.HTMLURL == nil ||
			event.Assignee == nil || event.Assignee.Login == nil ||
			event.Sender == nil || event.Sender.ID == nil {
			return nil
		}
		senderLogin := event.Sender.GetLogin()
		assigneeLogin := event.Assignee.GetLogin()
		number := event.PullRequest.GetNumber()
		return &githubWebhookEvent{
			eventType:      "pull_request_assigned",
			description:    fmt.Sprintf("%s assigned PR #%d to @%s", senderLogin, number, assigneeLogin),
			url:            *event.PullRequest.HTMLURL,
			githubUserID:   *event.Sender.ID,
			githubUsername: senderLogin,
			assignee:       assigneeLogin,
		}
	}

	return nil
}
