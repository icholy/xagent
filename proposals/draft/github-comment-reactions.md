# Emoji Reactions for Matched GitHub Comments

Issue: https://github.com/icholy/xagent/issues/691

## Problem

When a GitHub comment matches one of an org's routing rules and gets routed to a task, the user has no immediate feedback in GitHub that the bot saw their comment. The agent might take 10–60s to spin up and post its first message; until then the comment sits unacknowledged. Users assume nothing happened and comment again.

GitHub's native [Reactions API](https://docs.github.com/en/rest/reactions) is a perfect fit for an instant ack: drop a 👀 on the comment the moment routing decides it matches.

## Design

### Overview

When `eventrouter.Router.Route()` finds that an `InputEvent` matches an org's routing rules, it hands the routing outcome to a set of registered `Reactor`s. The GitHub reactor recognizes the event's source-specific metadata and asynchronously adds a reaction to the originating comment using that org's GitHub App installation token. The reaction is a side-effect of *matching*, decoupled from the existing event/task attachment flow.

### 1. Carry source-specific metadata on `InputEvent`

`InputEvent` (`internal/eventrouter/eventrouter.go:17`) currently carries the issue/PR `URL` for link matching but not the comment ID needed to react. Add a single optional, source-agnostic metadata field that reactors interpret with a type switch:

```go
// internal/eventrouter/eventrouter.go

type InputEvent struct {
    Source      string
    Type        string
    Description string
    Data        string
    URL         string
    UserID      string

    // Meta carries optional, source-specific metadata about the event that the
    // generic router doesn't interpret. Reactors type-switch on it to decide
    // whether (and how) to act. nil for events with no extra metadata.
    Meta any
}

// Metadata carriers are plain data with no source-client imports, so eventrouter
// stays generic. Keeping them as distinct concrete types — rather than one
// struct with a discriminator field — lets a reactor dispatch with a single type
// switch that maps 1:1 to an API endpoint, and lets future sources (e.g. Jira)
// add their own types without touching this one.

type GitHubIssueComment struct {
    Owner     string // repository owner, e.g. "icholy"
    Repo      string // repository name, e.g. "xagent"
    CommentID int64  // ID of the issue comment
}

type GitHubPullRequestReviewComment struct {
    Owner     string
    Repo      string
    CommentID int64 // ID of the PR review comment
}
```

These are **value types**, not pointers, so an unpopulated `Meta` is a clean nil interface — a reactor's type switch falls through to its `default` (no-op) without the typed-nil-pointer pitfall.

The Atlassian path needs no change — Jira reactions are a separate follow-up (see Trade-offs), and would slot in as another `Meta` type plus a `case` in a Jira reactor.

### 2. Populate it in the webhook handler

`extractGitHubWebhookEvent` (`internal/server/webhookserver/github.go:145`) currently extracts only the URL, description, and user. Extend its return struct with a `meta any` field, and copy it into `InputEvent.Meta` in `ServeHTTP`:

```go
// internal/server/webhookserver/github.go

type githubWebhookEvent struct {
    description    string
    data           string
    url            string
    githubUserID   int64
    githubUsername string

    // meta is optional source-specific metadata, copied into InputEvent.Meta.
    // nil if the event has no reactable comment.
    meta any
}
```

For `IssueCommentEvent`:

```go
meta: eventrouter.GitHubIssueComment{
    Owner:     event.GetRepo().GetOwner().GetLogin(),
    Repo:      event.GetRepo().GetName(),
    CommentID: event.GetComment().GetID(),
},
```

For `PullRequestReviewCommentEvent`:

```go
meta: eventrouter.GitHubPullRequestReviewComment{
    Owner:     event.GetRepo().GetOwner().GetLogin(),
    Repo:      event.GetRepo().GetName(),
    CommentID: event.GetComment().GetID(),
},
```

For `PullRequestReviewEvent`: leave `meta` nil. GitHub's REST API has no reaction endpoint for review *submissions* (only for the individual review *comments*, which arrive on the separate `pull_request_review_comment` webhook).

### 3. `Reactor` hooks on the Router

Define a minimal interface in the eventrouter package so the Router stays source-agnostic, and register a slice of them. Each reactor is handed the full routing outcome and must be a **no-op if it doesn't recognize the event** — so reactors compose freely (a GitHub reactor and a future Jira reactor can both be registered; each ignores outcomes whose `Meta` isn't theirs):

```go
// internal/eventrouter/eventrouter.go

// Reactor performs a side-effect in response to a routing outcome (e.g. adding
// a GitHub reaction). React MUST be a no-op when it doesn't recognize the event
// (typically: its Meta is nil or of an unhandled type). Every registered Reactor
// is invoked for every matched outcome.
type Reactor interface {
    React(ctx context.Context, outcome RouteOutcome)
}

// RouteOutcome describes what the Router did with an InputEvent for one org.
// It gives reactors the routing context — which org matched, the persisted
// event, and which tasks the event was attached to — not just the raw input.
type RouteOutcome struct {
    Input   InputEvent   // the routed event, including its Meta
    OrgID   int64        // the org whose routing rules matched
    Event   *model.Event // the persisted event row
    TaskIDs []int64       // tasks the event was attached to (may be empty)
}

type Router struct {
    Log       *slog.Logger
    Store     *store.Store
    Publisher pubsub.Publisher
    Reactors  []Reactor // optional; each is invoked for every matched outcome
}
```

In `Router.Route()`, after the rule match passes and the event is attached to its tasks, fire every reactor in the background with the outcome:

```go
// internal/eventrouter/eventrouter.go (Route)

if !slices.ContainsFunc(rules, input.MatchRule) {
    continue
}
event := &model.Event{ ... }
if err := r.Store.CreateEvent(ctx, nil, event); err != nil {
    r.Log.Error("failed to create event", "org_id", orgID, "error", err)
    continue
}
var taskIDs []int64
for _, link := range links {
    if err := r.attach(ctx, link.TaskID, event); err != nil {
        r.Log.Error("failed to attach event to task", "event_id", event.ID, "task_id", link.TaskID, "error", err)
        continue
    }
    taskIDs = append(taskIDs, link.TaskID)
    n++
}
r.react(ctx, RouteOutcome{Input: input, OrgID: orgID, Event: event, TaskIDs: taskIDs})
```

```go
// react fans the outcome out to every registered reactor, each in its own
// goroutine so a slow source API never blocks routing or the webhook response.
func (r *Router) react(ctx context.Context, outcome RouteOutcome) {
    for _, reactor := range r.Reactors {
        go reactor.React(context.WithoutCancel(ctx), outcome)
    }
}
```

`context.WithoutCancel` keeps reactors alive after the webhook handler returns, since the source round-trip should not block the webhook response. Each reactor is responsible for its own timeout (a few seconds). A nil/empty `Reactors` slice simply means the `for` loop does nothing — no explicit guard needed.

### 4. Concrete reactor in `githubserver`

```go
// internal/server/githubserver/reactor.go

type Reactor struct {
    Server  *Server      // for CreateInstallationToken
    Store   *store.Store
    Log     *slog.Logger
    Content string       // "eyes", "+1", etc. Defaults to "eyes".
}

func (r *Reactor) React(ctx context.Context, outcome eventrouter.RouteOutcome) {
    // Recognize the event first, before any work (token mint, API call). Return
    // for anything this reactor doesn't own: a nil Meta (event with no reactable
    // comment) or a foreign type (e.g. a future Jira Meta). Because every
    // registered reactor sees every outcome, this no-op-on-unrecognized rule is
    // what lets reactors compose.
    var react func(ctx context.Context, client *github.Client, content string) error
    switch m := outcome.Input.Meta.(type) {
    case eventrouter.GitHubIssueComment:
        react = func(ctx context.Context, c *github.Client, content string) error {
            _, _, err := c.Reactions.CreateIssueCommentReaction(ctx, m.Owner, m.Repo, m.CommentID, content)
            return err
        }
    case eventrouter.GitHubPullRequestReviewComment:
        react = func(ctx context.Context, c *github.Client, content string) error {
            _, _, err := c.Reactions.CreatePullRequestCommentReaction(ctx, m.Owner, m.Repo, m.CommentID, content)
            return err
        }
    default:
        return // not ours — no-op
    }

    ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    org, err := r.Store.GetOrg(ctx, nil, outcome.OrgID)
    if err != nil || org.GitHubInstallationID == 0 {
        return
    }
    token, err := r.Server.CreateInstallationToken(ctx, org.GitHubInstallationID)
    if err != nil {
        r.Log.Warn("github reaction: failed to mint token", "org_id", outcome.OrgID, "error", err)
        return
    }
    client := github.NewClient(nil).WithAuthToken(token.Token)

    content := r.Content
    if content == "" {
        content = "eyes"
    }
    if err := react(ctx, client, content); err != nil {
        r.Log.Warn("github reaction failed",
            "org_id", outcome.OrgID, "url", outcome.Input.URL, "error", err)
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
            Reactors: []eventrouter.Reactor{
                &Reactor{Server: s, Store: s.store, Log: s.log},
            },
        },
        Store:         s.store,
        WebhookSecret: s.config.WebhookSecret,
    }
}
```

The Atlassian webhook router just constructs its `Router` with an empty `Reactors` slice (or omits it), and the `for` loop in `react` is a no-op there. Registering reactors as a slice means another source's reactor can be appended later without changing the `Router` type.

### 6. Configuration

v1: hardcoded `"eyes"`. Two natural extension points if we want to customize later:

- **Per-org**: add a `reaction_emoji` column to `orgs` and read it in the Reactor. The `Reactor.Content` field accommodates this without API change.
- **Per-rule**: add an optional `Emoji string` field on `model.RoutingRule` and pass the matched rule (not just the event) to the reactor.

We don't need either for v1 — start simple and add config if users ask.

### 7. Tests

- `eventrouter_test.go`: register two fake `Reactor`s that record the `RouteOutcome`s they receive; assert both fire once per matched org with the right `OrgID`, `Event`, and `TaskIDs`, and that neither fires when no rule matches.
- `webhookserver/github_test.go`: assert `extractGitHubWebhookEvent` sets `meta` to a `GitHubIssueComment` for issue comments and a `GitHubPullRequestReviewComment` for PR review comments, and leaves it nil for review submissions.
- `githubserver/reactor_test.go`: stub the GitHub API with `httptest.Server` and verify the correct endpoint is hit for each `Meta` type. Verify that a nil `Meta`, a foreign `Meta` type, and an org with no installation ID are each no-ops (and mint no token).

### 8. Implementation order

1. Add the `Meta` types (`GitHubIssueComment`, `GitHubPullRequestReviewComment`), the `Meta any` field, and the `Reactor` interface + `RouteOutcome` struct to `eventrouter`.
2. Add the `Reactors []Reactor` field to `Router`, the `react` fan-out helper, and the call in `Route` (capturing `TaskIDs`).
3. Extend the webhook extractor to populate `meta`.
4. Implement `githubserver.Reactor` and register it in `WebhookHandler()`.
5. Tests.

No migrations, no proto changes, no UI changes.

## Trade-offs

**React on match, not after task start.** The whole point is fast feedback. Reactors fire the moment routing matches and the event is attached — within a webhook round-trip, rather than after the runner schedules a container. The outcome carries `TaskIDs`, so a reactor *could* choose to react only when tasks actually started, but the v1 GitHub reactor reacts on match regardless: a 👀 means "matched and accepted", and even if every attach failed the reaction is still a true signal that the bot saw the comment. (See Open Question 3.)

**Pass a `RouteOutcome`, not just the input.** The reactor gets the org that matched, the persisted event, and the attached task IDs — not only the raw `InputEvent`. This keeps the reactor from re-deriving routing context the Router already computed, and leaves room for richer reactor behavior later (e.g. reflect task state in the reaction) without changing the interface.

**Hook in the Router, not the webhook handler.** The webhook handler doesn't know which rules matched or which orgs the event was routed to — that's the Router's job. Putting the hook in the Router also means future event sources (e.g. another webhook handler that produces `InputEvent`s) get reactions for free as long as they populate `Meta`.

**`Meta any` resolved by a type switch, not a typed field.** Rather than a GitHub-named field (`GitHub *GitHubReactionTarget`), `InputEvent` carries an opaque `Meta any` and each `Reactor` type-switches on it. This keeps `InputEvent` — and the whole `eventrouter` package — free of any source-specific shape: a Jira reactor adds its own `Meta` type and `case` without ever touching `InputEvent`. The cost is that dispatch is runtime- rather than compile-checked, but the surface is tiny (one switch per reactor with a defensive `default`), and each `Meta` type maps 1:1 to an API endpoint, so there's no discriminator/`Kind` enum to keep in sync.

**A slice of reactors, each a no-op on unrecognized events.** The Router holds `[]Reactor` and invokes every one for every matched outcome; a reactor that doesn't recognize the `Meta` returns immediately (before minting any token). This composes cleanly — registering a Jira reactor alongside the GitHub one needs no dispatch logic in the Router, and an empty slice (the Atlassian router) is simply a no-op. The cost is that every reactor is invoked for every match, but the unrecognized-event check is a cheap type switch, so the overhead is negligible.

**Async, fire-and-forget, one goroutine per reactor.** Source API round-trips can be hundreds of milliseconds. Calling synchronously from the webhook handler would slow webhook responses and risk timeouts, so the Router spawns a goroutine per reactor with `context.WithoutCancel`. Failures are logged but don't block routing — a missed reaction is a degraded experience, not a broken one.

**Installation token, not OAuth user token.** Reactions posted via the installation token appear under the GitHub App's bot identity (e.g. `xagent-app[bot]`), which is the correct attribution — it's the bot acknowledging the comment, not the user who triggered it. OAuth tokens also tie to a specific linked user and would fail if that user un-links.

**Hardcoded emoji in v1.** 👀 is the most idiomatic "I see this and am working on it" reaction (used by github-actions[bot], Linear, many CI bots). Per-org or per-rule customization is easy to add later without breaking the v1 contract.

**Skip PR review submissions.** GitHub's REST API supports reactions on individual review comments but not on review submissions themselves. We could fall back to a reaction on the underlying PR (a different content kind), but a 👀 on a whole PR is confusing — it would imply we noticed the PR, not the review. Better to silently skip review submissions; the individual review comments still get reactions via the `pull_request_review_comment` webhook.

**Reactor interface in eventrouter.** Defining the interface in the eventrouter keeps that package free of GitHub imports. The concrete `githubserver.Reactor` carries the GitHub client and installation-token plumbing where it belongs. This mirrors the existing split between `eventrouter` (generic) and `webhookserver` (source-specific).

## Open Questions

1. **Should we react to atlassian/Jira matches too?** Jira issue comments support reactions via the [Atlassian REST API](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comments/) — `POST /rest/api/3/issue/{issueIdOrKey}/comment/{id}/reactions`. With `Meta any` and a slice of reactors, this slots in cleanly: add a `JiraComment` `Meta` type, populate it in the Jira webhook extractor, and handle it with a `case` in a Jira reactor appended to the Atlassian router's `Reactors`. No change to `InputEvent`, `Router`, or the existing GitHub reactor. Worth doing in a v2 once the GitHub side is proven.

2. **Should the reaction get removed when the task finishes?** Some bots (e.g. github-actions) update reactions to reflect state: 👀 while running, 🎉 on success, 👎 on failure. This is appealing but adds a lot of state management: we'd need to track the reaction ID per comment, plumb task-completion events back to the reactor, and handle deletion failures. Not worth it in v1; can be added later if there's demand.

3. **What about "I matched but couldn't start the task" cases?** If `Router.attach()` fails for *all* matching tasks, we'll have reacted to a comment that produced no work. Acceptable — the reaction means "I matched", not "I succeeded". Errors are already logged and visible in xagent.

4. **Rate limits?** GitHub Apps get 5,000 requests/hour per installation, shared across all reads/writes. A reaction is one extra request per matched comment. At realistic comment volumes this is negligible, but worth keeping in mind if an org has a high-volume routing rule.
