# Emoji Reactions for Matched GitHub Comments

Issue: https://github.com/icholy/xagent/issues/691

## Problem

When a GitHub comment matches one of an org's routing rules and gets routed to a task, the user has no immediate feedback in GitHub that the bot saw their comment. The agent might take 10–60s to spin up and post its first message; until then the comment sits unacknowledged. Users assume nothing happened and comment again.

GitHub's native [Reactions API](https://docs.github.com/en/rest/reactions) is a perfect fit for an instant ack: drop a 👀 on the comment the moment routing decides it matches.

## Design

### Overview

When `eventrouter.Router.Route()` finds that an `InputEvent` matches an org's routing rules, asynchronously add a reaction to the originating GitHub comment using that org's GitHub App installation token. The reaction is a side-effect of *matching*, decoupled from the existing event/task attachment flow.

### 1. Carry the reaction target on `InputEvent`

`InputEvent` (`internal/eventrouter/eventrouter.go:17`) currently carries the issue/PR `URL` for link matching but not the comment ID needed to react. Add an optional GitHub-specific field:

```go
// internal/eventrouter/eventrouter.go

type InputEvent struct {
    Source      string
    Type        string
    Description string
    Data        string
    URL         string
    UserID      string

    // GitHub is set for GitHub webhook events that support reactions.
    // nil for non-GitHub events or GitHub events with no reaction API
    // (e.g. pull_request_review submissions).
    GitHub *GitHubReactionTarget
}

type GitHubReactionTarget struct {
    Owner     string // repository owner, e.g. "icholy"
    Repo      string // repository name, e.g. "xagent"
    CommentID int64  // ID of the issue comment or PR review comment
    Kind      GitHubCommentKind
}

type GitHubCommentKind string

const (
    GitHubIssueComment             GitHubCommentKind = "issue_comment"
    GitHubPullRequestReviewComment GitHubCommentKind = "pull_request_review_comment"
)
```

The Atlassian path needs no change — Jira reactions are a separate follow-up (see Trade-offs).

### 2. Populate it in the webhook handler

`extractGitHubWebhookEvent` (`internal/server/webhookserver/github.go:145`) currently extracts only the URL, description, and user. Extend its return struct so the GitHub-specific fields flow through, and populate `InputEvent.GitHub` in `ServeHTTP`:

```go
// internal/server/webhookserver/github.go

type githubWebhookEvent struct {
    description    string
    data           string
    url            string
    githubUserID   int64
    githubUsername string

    // For reaction support — nil if the event has no reactable comment.
    react *eventrouter.GitHubReactionTarget
}
```

For `IssueCommentEvent`:

```go
react: &eventrouter.GitHubReactionTarget{
    Owner:     event.GetRepo().GetOwner().GetLogin(),
    Repo:      event.GetRepo().GetName(),
    CommentID: event.GetComment().GetID(),
    Kind:      eventrouter.GitHubIssueComment,
},
```

For `PullRequestReviewCommentEvent`:

```go
react: &eventrouter.GitHubReactionTarget{
    Owner:     event.GetRepo().GetOwner().GetLogin(),
    Repo:      event.GetRepo().GetName(),
    CommentID: event.GetComment().GetID(),
    Kind:      eventrouter.GitHubPullRequestReviewComment,
},
```

For `PullRequestReviewEvent`: leave `react` nil. GitHub's REST API has no reaction endpoint for review *submissions* (only for the individual review *comments*, which arrive on the separate `pull_request_review_comment` webhook).

### 3. New `Reactor` hook on the Router

Define a minimal interface in the eventrouter package so the Router stays GitHub-agnostic, and wire a concrete implementation in `githubserver`:

```go
// internal/eventrouter/eventrouter.go

type Reactor interface {
    React(ctx context.Context, orgID int64, input InputEvent)
}

type Router struct {
    Log       *slog.Logger
    Store     *store.Store
    Publisher pubsub.Publisher
    Reactor   Reactor // optional; nil disables reactions
}
```

In `Router.Route()`, after the rule match passes but before any task work, fire the reactor in the background:

```go
// internal/eventrouter/eventrouter.go (Route, around line 59)

if !slices.ContainsFunc(rules, input.MatchRule) {
    continue
}
if r.Reactor != nil {
    go r.Reactor.React(context.WithoutCancel(ctx), orgID, input)
}
event := &model.Event{ ... }
// ... existing code unchanged
```

`context.WithoutCancel` keeps the reactor alive after the webhook handler returns, since the GitHub round-trip should not block the webhook response. The reactor itself is responsible for its own timeout (a few seconds).

### 4. Concrete reactor in `githubserver`

```go
// internal/server/githubserver/reactor.go

type Reactor struct {
    Server  *Server      // for CreateInstallationToken
    Store   *store.Store
    Log     *slog.Logger
    Content string       // "eyes", "+1", etc. Defaults to "eyes".
}

func (r *Reactor) React(ctx context.Context, orgID int64, input eventrouter.InputEvent) {
    if input.GitHub == nil {
        return
    }
    ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    org, err := r.Store.GetOrg(ctx, nil, orgID)
    if err != nil || org.GitHubInstallationID == 0 {
        return
    }
    token, err := r.Server.CreateInstallationToken(ctx, org.GitHubInstallationID)
    if err != nil {
        r.Log.Warn("github reaction: failed to mint token", "org_id", orgID, "error", err)
        return
    }
    client := github.NewClient(nil).WithAuthToken(token.Token)

    t := input.GitHub
    content := r.Content
    if content == "" {
        content = "eyes"
    }

    switch t.Kind {
    case eventrouter.GitHubIssueComment:
        _, _, err = client.Reactions.CreateIssueCommentReaction(
            ctx, t.Owner, t.Repo, t.CommentID, content)
    case eventrouter.GitHubPullRequestReviewComment:
        _, _, err = client.Reactions.CreatePullRequestCommentReaction(
            ctx, t.Owner, t.Repo, t.CommentID, content)
    default:
        return
    }
    if err != nil {
        r.Log.Warn("github reaction failed",
            "org_id", orgID, "url", input.URL, "kind", t.Kind, "error", err)
    }
}
```

GitHub's reaction endpoint is idempotent at the user level — a given GitHub user (the App's bot identity) can only have one of each reaction type on a given comment, so duplicate calls are harmless (the API returns the existing reaction with 200).

### 5. Wiring

`githubserver.Server.WebhookHandler()` (`internal/server/githubserver/githubserver.go:150`) constructs the Router today. Inject the reactor here:

```go
func (s *Server) WebhookHandler() http.Handler {
    return &webhookserver.GitHubHandler{
        Router: &eventrouter.Router{
            Log:       s.log,
            Store:     s.store,
            Publisher: s.publisher,
            Reactor: &Reactor{
                Server: s,
                Store:  s.store,
                Log:    s.log,
            },
        },
        Store:         s.store,
        WebhookSecret: s.config.WebhookSecret,
    }
}
```

When the GitHub App isn't configured at all, `WebhookHandler()` isn't called — so the `Reactor` field is never nil in the GitHub flow. The Atlassian webhook router doesn't get a reactor and the `if r.Reactor != nil` guard covers it.

### 6. Configuration

v1: hardcoded `"eyes"`. Two natural extension points if we want to customize later:

- **Per-org**: add a `reaction_emoji` column to `orgs` and read it in the Reactor. The `Reactor.Content` field accommodates this without API change.
- **Per-rule**: add an optional `Emoji string` field on `model.RoutingRule` and pass the matched rule (not just the event) to the reactor.

We don't need either for v1 — start simple and add config if users ask.

### 7. Tests

- `eventrouter_test.go`: add a fake `Reactor` that records calls; assert it fires once per matched (org, event) pair and is skipped when no rule matches.
- `webhookserver/github_test.go`: assert `extractGitHubWebhookEvent` populates `react` for issue comments and PR review comments, and leaves it nil for review submissions.
- `githubserver/reactor_test.go`: stub the GitHub API with `httptest.Server` and verify the correct endpoint is hit for each `Kind`. Verify that an org with no installation ID is a no-op.

### 8. Implementation order

1. Add `GitHubReactionTarget` and `Reactor` interface to `eventrouter`.
2. Add the `Reactor` field to `Router` and the fire-and-forget call in `Route`.
3. Extend the webhook extractor to populate the target.
4. Implement `githubserver.Reactor` and wire it in `WebhookHandler()`.
5. Tests.

No migrations, no proto changes, no UI changes.

## Trade-offs

**React on match, not after task start.** The whole point is fast feedback. Reacting on match means the user sees 👀 within a webhook round-trip rather than after the runner schedules a container. The cost is that we might react to a comment that then fails to start a task (e.g. the runner is down) — but in that case the user will see the agent never reply and can re-trigger, and the reaction is still a true signal that the comment was *matched and accepted*.

**Hook in the Router, not the webhook handler.** The webhook handler doesn't know which rules matched or which orgs the event was routed to — that's the Router's job. Putting the hook in the Router also means future event sources (e.g. another webhook handler that produces `InputEvent`s) get reactions for free as long as they populate `GitHub`.

**Async, fire-and-forget.** GitHub API round-trips can be hundreds of milliseconds. Synchronously calling from the webhook handler would slow webhook responses and risk timeouts. Failures are logged but don't block routing — a missed reaction is a degraded experience, not a broken one.

**Installation token, not OAuth user token.** Reactions posted via the installation token appear under the GitHub App's bot identity (e.g. `xagent-app[bot]`), which is the correct attribution — it's the bot acknowledging the comment, not the user who triggered it. OAuth tokens also tie to a specific linked user and would fail if that user un-links.

**Hardcoded emoji in v1.** 👀 is the most idiomatic "I see this and am working on it" reaction (used by github-actions[bot], Linear, many CI bots). Per-org or per-rule customization is easy to add later without breaking the v1 contract.

**Skip PR review submissions.** GitHub's REST API supports reactions on individual review comments but not on review submissions themselves. We could fall back to a reaction on the underlying PR (a different content kind), but a 👀 on a whole PR is confusing — it would imply we noticed the PR, not the review. Better to silently skip review submissions; the individual review comments still get reactions via the `pull_request_review_comment` webhook.

**Reactor interface in eventrouter.** Defining the interface in the eventrouter keeps that package free of GitHub imports. The concrete `githubserver.Reactor` carries the GitHub client and installation-token plumbing where it belongs. This mirrors the existing split between `eventrouter` (generic) and `webhookserver` (source-specific).

## Open Questions

1. **Should we react to atlassian/Jira matches too?** Jira issue comments support reactions via the [Atlassian REST API](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comments/) — `POST /rest/api/3/issue/{issueIdOrKey}/comment/{id}/reactions`. The same `Reactor` interface accommodates a second implementation, but the Jira webhook payload would need similar extraction changes. Worth doing in a v2 once the GitHub side is proven.

2. **Should the reaction get removed when the task finishes?** Some bots (e.g. github-actions) update reactions to reflect state: 👀 while running, 🎉 on success, 👎 on failure. This is appealing but adds a lot of state management: we'd need to track the reaction ID per comment, plumb task-completion events back to the reactor, and handle deletion failures. Not worth it in v1; can be added later if there's demand.

3. **What about "I matched but couldn't start the task" cases?** If `Router.attach()` fails for *all* matching tasks, we'll have reacted to a comment that produced no work. Acceptable — the reaction means "I matched", not "I succeeded". Errors are already logged and visible in xagent.

4. **Rate limits?** GitHub Apps get 5,000 requests/hour per installation, shared across all reads/writes. A reaction is one extra request per matched comment. At realistic comment volumes this is negligible, but worth keeping in mind if an org has a high-volume routing rule.
