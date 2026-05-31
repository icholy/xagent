package githubserver

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-github/v88/github"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/githubx"
)

// WebhookHandler handles incoming GitHub App webhook events.
type WebhookHandler struct {
	Router        Router
	Store         Store
	WebhookSecret string
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	input := toInputEvent(webhookEvent)
	if input == nil {
		eventType := r.Header.Get("X-GitHub-Event")
		slog.Debug("ignoring GitHub webhook event", "event_type", eventType)
		fmt.Fprintf(w, "ignored")
		return
	}
	// toInputEvent always sets Meta to a GitHubMeta, so this
	// assertion is safe. It panics loudly if that invariant is ever broken.
	meta := input.Meta.(GitHubMeta)

	// Look up xagent owner by GitHub user ID
	user, err := h.Store.GetUserByGitHubUserID(r.Context(), nil, meta.AuthorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Info("no linked GitHub account", "github_user_id", meta.AuthorID)
			fmt.Fprintf(w, "no linked account")
			return
		}
		slog.Error("failed to look up GitHub account", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update cached username if it changed
	if meta.AuthorLogin != "" && meta.AuthorLogin != user.GitHubUsername {
		if err := h.Store.UpdateGitHubUsername(r.Context(), nil, user.GitHubUserID, meta.AuthorLogin); err != nil {
			slog.Warn("failed to update GitHub username", "error", err)
		}
	}

	// Route event to subscribed tasks
	input.UserID = user.ID
	totalRouted, err := h.Router.Route(r.Context(), *input)
	if err != nil {
		slog.Error("failed to route event", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("GitHub webhook processed", "url", input.URL, "tasks_routed", totalRouted)
	fmt.Fprintf(w, "processed")
}

func (h *WebhookHandler) handleInstallationEvent(w http.ResponseWriter, r *http.Request, event *github.InstallationEvent) {
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

// GitHubMeta is attached to an eventrouter.InputEvent's Meta field, carrying
// GitHub-native identity that the router does not interpret.
type GitHubMeta struct {
	AuthorID    int64
	AuthorLogin string

	// Owner, Repo, and CommentID locate the comment for GitHub's Reactions API.
	// Populated only for issue_comment and pull_request_review_comment events;
	// left zero for assignments and review submissions (which have no reactable
	// comment). Consumers key off InputEvent.Type, not these fields, to decide
	// whether and how to react.
	Owner     string
	Repo      string
	CommentID int64
}

// Event-type strings set on eventrouter.InputEvent.Type by toInputEvent. They
// form a contract between the extractor (producer) and any consumer that
// dispatches on InputEvent.Type.
const (
	EventTypeIssueComment             = "issue_comment"
	EventTypePullRequestReviewComment = "pull_request_review_comment"
	EventTypePullRequestReview        = "pull_request_review"
	EventTypeIssueAssigned            = "issue_assigned"
	EventTypePullRequestAssigned      = "pull_request_assigned"
)

func toInputEvent(webhookEvent any) *eventrouter.InputEvent {
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
		return &eventrouter.InputEvent{
			Source:      "github",
			Type:        EventTypeIssueComment,
			Description: description,
			Data:        body,
			URL:         *event.Issue.HTMLURL,
			Meta: GitHubMeta{
				AuthorID:    *event.Comment.User.ID,
				AuthorLogin: login,
				Owner:       event.GetRepo().GetOwner().GetLogin(),
				Repo:        event.GetRepo().GetName(),
				CommentID:   event.GetComment().GetID(),
			},
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
		return &eventrouter.InputEvent{
			Source:      "github",
			Type:        EventTypePullRequestReviewComment,
			Description: fmt.Sprintf("%s reviewed PR #%d", login, number),
			Data:        body,
			URL:         *event.PullRequest.HTMLURL,
			Meta: GitHubMeta{
				AuthorID:    *event.Comment.User.ID,
				AuthorLogin: login,
				Owner:       event.GetRepo().GetOwner().GetLogin(),
				Repo:        event.GetRepo().GetName(),
				CommentID:   event.GetComment().GetID(),
			},
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
		return &eventrouter.InputEvent{
			Source:      "github",
			Type:        EventTypePullRequestReview,
			Description: fmt.Sprintf("%s reviewed PR #%d", login, number),
			Data:        body,
			URL:         *event.PullRequest.HTMLURL,
			Meta:        GitHubMeta{AuthorID: *event.Review.User.ID, AuthorLogin: login},
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
		return &eventrouter.InputEvent{
			Source:      "github",
			Type:        EventTypeIssueAssigned,
			Description: fmt.Sprintf("%s assigned issue #%d to @%s", senderLogin, number, assigneeLogin),
			URL:         *event.Issue.HTMLURL,
			Assignee:    assigneeLogin,
			Meta:        GitHubMeta{AuthorID: *event.Sender.ID, AuthorLogin: senderLogin},
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
		return &eventrouter.InputEvent{
			Source:      "github",
			Type:        EventTypePullRequestAssigned,
			Description: fmt.Sprintf("%s assigned PR #%d to @%s", senderLogin, number, assigneeLogin),
			URL:         *event.PullRequest.HTMLURL,
			Assignee:    assigneeLogin,
			Meta:        GitHubMeta{AuthorID: *event.Sender.ID, AuthorLogin: senderLogin},
		}
	}

	return nil
}
