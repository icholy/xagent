# Emoji Reactions for Matched GitHub Comments

Issue: https://github.com/icholy/xagent/issues/691

## Problem

When a GitHub comment matches one of an org's routing rules and gets routed to a task, the user has no immediate feedback in GitHub that the bot saw their comment. The agent might take 10–60s to spin up and post its first message; until then the comment sits unacknowledged. Users assume nothing happened and comment again.

GitHub's native [Reactions API](https://docs.github.com/en/rest/reactions) is a perfect fit for an instant ack: drop a 👀 on the comment the moment routing decides it matches.

## Design

### Overview

When `eventrouter.Router.Route()` finds that an `InputEvent` matches an org's routing rules, it hands the routing outcome to a set of registered `Reactor`s. The GitHub reactor recognizes the event's source-specific metadata and asynchronously adds a reaction to the originating comment using that org's GitHub App installation token. The reaction is a side-effect of *matching*, decoupled from the existing event/task flow.

This builds directly on the webhook refactor that just landed on master:

- **PR #775 ("parse GitHub webhooks directly into InputEvent")** removed the `githubWebhookEvent` intermediate struct. `extractGitHubWebhookEvent` now returns an `*eventrouter.InputEvent` directly. `InputEvent` already has a `Meta any` field, and `webhookserver` already defines `GithubUser{ID, Username}` and `GitHubMeta{Author GithubUser}`. The extractor sets `Meta = GitHubMeta{Author: ...}` on **every** GitHub event, and `ServeHTTP` resolves identity via `input.Meta.(GitHubMeta).Author`.
- **PR #774 ("ignore non-create/edit GitHub comment webhook actions")** means only `created`/`edited` comments route at all — `deleted` is dropped in the extractor before routing. So we never react to a comment that no longer exists; it's handled upstream, not a risk we need to guard against here.

Because `Meta` and `GitHubMeta` already exist, this proposal does **not** add a new field to `InputEvent` and does **not** put any GitHub-specific type in `eventrouter`. The reaction target rides along on the existing `GitHubMeta`, and the only generic additions to `eventrouter` are the source-agnostic `Reactor` interface and `RouteOutcome` struct.

### 1. Carry the reaction target on `webhookserver.GitHubMeta`

`GitHubMeta` (`internal/server/webhookserver/github.go`) already carries the GitHub-native author. Extend it with an optional reaction target, expressed as small `webhookserver`-local value types — one per Reactions endpoint:

```go
// internal/server/webhookserver/github.go

type GitHubMeta struct {
    Author GithubUser

    // Reaction, when non-nil, identifies a comment that supports GitHub's
    // Reactions API. Set only for issue_comment and pull_request_review_comment
    // events; nil for assignments and review submissions. The github reactor
    // type-switches on it. Holds one of the GitHub*Reaction types below.
    Reaction any
}

// Reaction targets are plain data — no github.Client dependency — so this
// stays a pure metadata package. One type per Reactions endpoint lets the
// reactor dispatch with a single type switch that maps 1:1 to an API call.

type GitHubIssueCommentReaction struct {
    Owner     string // repository owner, e.g. "icholy"
    Repo      string // repository name, e.g. "xagent"
    CommentID int64  // ID of the issue comment
}

type GitHubPullRequestReviewCommentReaction struct {
    Owner     string
    Repo      string
    CommentID int64 // ID of the PR review comment
}
```

`Reaction` is a value type held in an `any`, mirroring the existing `Meta any` idiom — an unpopulated target is a clean nil interface, so the reactor's nil check and type switch fall through to a no-op without the typed-nil-pointer pitfall.

Populate it in `extractGitHubWebhookEvent`, alongside the `Author` that's already set. Only the two comment events get a target.

For `*github.IssueCommentEvent`:

```go
Meta: GitHubMeta{
    Author: GithubUser{ID: *event.Comment.User.ID, Username: login},
    Reaction: GitHubIssueCommentReaction{
        Owner:     event.GetRepo().GetOwner().GetLogin(),
        Repo:      event.GetRepo().GetName(),
        CommentID: event.GetComment().GetID(),
    },
},
```

For `*github.PullRequestReviewCommentEvent`:

```go
Meta: GitHubMeta{
    Author: GithubUser{ID: *event.Comment.User.ID, Username: login},
    Reaction: GitHubPullRequestReviewCommentReaction{
        Owner:     event.GetRepo().GetOwner().GetLogin(),
        Repo:      event.GetRepo().GetName(),
        CommentID: event.GetComment().GetID(),
    },
},
```

For `*github.PullRequestReviewEvent` (submission) and the `assigned` `*github.IssuesEvent` / `*github.PullRequestEvent`: leave `Reaction` nil — keep setting `Author` exactly as today. GitHub's REST API has no reaction endpoint for review *submissions* (only for the individual review *comments*, which arrive on the separate `pull_request_review_comment` webhook), and assignments have no reactable comment.

### 2. Generic `Reactor` hook on the Router

Define a minimal interface and outcome struct in `eventrouter` so the Router stays source-agnostic (no GitHub imports), and register a slice of reactors. Each reactor is handed the full routing outcome and must be a **no-op if it doesn't recognize the event** — so reactors compose freely (a GitHub reactor and a future Jira reactor can both be registered; each ignores outcomes whose `Meta` isn't theirs):

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
// It gives reactors the routing context — which org matched, the rule that
// matched, and which tasks were woken or created — not just the raw input.
type RouteOutcome struct {
    Input   InputEvent         // the routed event, including its Meta
    OrgID   int64              // the org whose routing rule matched
    Rule    *model.RoutingRule // the rule that matched
    TaskIDs []int64            // tasks woken or created for this event
}

type Router struct {
    Log       *slog.Logger
    Store     *store.Store
    Publisher pubsub.Publisher
    Reactors  []Reactor // optional; each is invoked for every matched outcome
}
```

`eventrouter` already imports `model`, so `RouteOutcome` needs no new import and carries nothing GitHub-specific.

In `Route()`, the matched rule per org is already computed (the `matched` map) and each org is then either woken or has a task created. Collect the woken-or-created task IDs per org and fan the outcome out to every reactor:

```go
// internal/eventrouter/eventrouter.go (Route, per matched org)

// ... after the wake/create handling for this org, with taskIDs collected ...
r.react(ctx, RouteOutcome{Input: input, OrgID: orgID, Rule: rule, TaskIDs: taskIDs})
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

`context.WithoutCancel` keeps reactors alive after the webhook handler returns, since the source round-trip should not block the webhook response. Each reactor owns its own timeout (a few seconds). A nil/empty `Reactors` slice simply means the `for` loop does nothing — no explicit guard needed, and the Atlassian router gets reactions-free for free.

### 3. Concrete reactor in `githubserver`

`githubserver` already imports `webhookserver` (it constructs `webhookserver.GitHubHandler` in `WebhookHandler`), so the reactor can read the concrete `webhookserver.GitHubMeta` and its reaction-target types with no import cycle:

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
    // for anything this reactor doesn't own: a non-GitHub Meta, a GitHub event
    // with no reactable comment (nil Reaction), or a target type we don't handle.
    meta, ok := outcome.Input.Meta.(webhookserver.GitHubMeta)
    if !ok || meta.Reaction == nil {
        return
    }
    var react func(ctx context.Context, c *github.Client, content string) error
    switch t := meta.Reaction.(type) {
    case webhookserver.GitHubIssueCommentReaction:
        react = func(ctx context.Context, c *github.Client, content string) error {
            _, _, err := c.Reactions.CreateIssueCommentReaction(ctx, t.Owner, t.Repo, t.CommentID, content)
            return err
        }
    case webhookserver.GitHubPullRequestReviewCommentReaction:
        react = func(ctx context.Context, c *github.Client, content string) error {
            _, _, err := c.Reactions.CreatePullRequestCommentReaction(ctx, t.Owner, t.Repo, t.CommentID, content)
            return err
        }
    default:
        return // unknown target type — no-op
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

GitHub's reaction endpoint is idempotent at the user level — a given GitHub user (the App's bot identity) can only have one of each reaction type on a given comment, so duplicate calls (e.g. an `edited` redelivery) are harmless: the API returns the existing reaction with 200.

### 4. Wiring

`githubserver.Server.WebhookHandler()` constructs the Router today as `&eventrouter.Router{Log, Store, Publisher}`. Register the reactor on it:

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

Registering reactors as a slice means another source's reactor can be appended later without changing the `Router` type, and an empty slice is a clean no-op.

### 5. Configuration

v1: hardcoded `"eyes"`. Two natural extension points if we want to customize later:

- **Per-org**: add a `reaction_emoji` column to `orgs` and read it in the Reactor. The `Reactor.Content` field accommodates this without an API change.
- **Per-rule**: `RouteOutcome` already carries the matched `*model.RoutingRule`, so a per-rule `Emoji string` field could be honored by the reactor with no signature change.

We don't need either for v1 — start simple and add config if users ask.

### 6. Tests

- `eventrouter_test.go`: register two fake `Reactor`s that record the `RouteOutcome`s they receive; assert both fire once per matched org with the right `OrgID`, `Rule`, and `TaskIDs`, and that neither fires when no rule matches.
- `webhookserver/github_test.go`: assert `extractGitHubWebhookEvent` sets `Meta.Reaction` to a `GitHubIssueCommentReaction` for issue comments and a `GitHubPullRequestReviewCommentReaction` for PR review comments, and leaves it nil for review submissions and assignments. (The existing action-filtering test from PR #774 already covers `deleted` comments being dropped before routing.)
- `githubserver/reactor_test.go`: stub the GitHub API with `httptest.Server` and verify the correct endpoint is hit for each reaction-target type. Verify that a non-GitHub `Meta`, a nil `Reaction`, an unknown target type, and an org with no installation ID are each no-ops (and the last few mint no token).

### 7. Implementation order

1. Add the `Reaction` field and the two reaction-target types to `webhookserver.GitHubMeta`; populate them in `extractGitHubWebhookEvent` for the two comment events.
2. Add the `Reactor` interface, `RouteOutcome` struct, and `Reactors []Reactor` field to `eventrouter`, plus the `react` fan-out and the call in `Route` (collecting `TaskIDs`).
3. Implement `githubserver.Reactor` and register it in `WebhookHandler()`.
4. Tests.

No migrations, no proto changes, no UI changes.

## Trade-offs

**Reaction target lives in `webhookserver`, not `eventrouter`.** The original draft put `GitHubIssueComment`/`GitHubPullRequestReviewComment` in `eventrouter`; that was the main objection. With PR #775, the right home is obvious: `webhookserver` already owns `GitHubMeta` (the GitHub-native metadata bag) and is where the webhook is parsed, so the reaction target is just another field on it. `eventrouter` stays free of GitHub types — it only gains the generic `Reactor`/`RouteOutcome`. `githubserver` already depends on `webhookserver`, so the concrete reactor reads those types with no import cycle.

**React on match, not after task start.** The whole point is fast feedback. Reactors fire the moment routing matches and wakes/creates the task — within a webhook round-trip, rather than after the runner schedules a container. The outcome carries `TaskIDs`, so a reactor *could* react only when tasks actually started, but the v1 GitHub reactor reacts on match regardless: a 👀 means "matched and accepted". (See Open Questions.)

**Pass a `RouteOutcome`, not just the input.** The reactor gets the org that matched, the matched rule, and the woken/created task IDs — not only the raw `InputEvent`. This keeps the reactor from re-deriving routing context the Router already computed, and leaves room for richer behavior later (per-rule emoji, reflecting task state) without changing the interface.

**A slice of reactors, each a no-op on unrecognized events.** The Router holds `[]Reactor` and invokes every one for every matched outcome; a reactor that doesn't recognize the `Meta` returns immediately, before minting any token. This composes cleanly — registering a Jira reactor alongside the GitHub one needs no dispatch logic in the Router, and an empty slice (the Atlassian router) is simply a no-op. The cost is that every reactor is invoked for every match, but the recognition check is a cheap type assertion, so the overhead is negligible.

**Async, fire-and-forget, one goroutine per reactor.** Source API round-trips can be hundreds of milliseconds. Calling synchronously from the webhook handler would slow webhook responses and risk timeouts, so the Router spawns a goroutine per reactor with `context.WithoutCancel`. Failures are logged but don't block routing — a missed reaction is a degraded experience, not a broken one.

**Installation token, not OAuth user token.** Reactions posted via the installation token appear under the GitHub App's bot identity (e.g. `xagent-app[bot]`), which is the correct attribution — it's the bot acknowledging the comment, not the user who triggered it. OAuth tokens also tie to a specific linked user and would fail if that user un-links.

**Hardcoded emoji in v1.** 👀 is the most idiomatic "I see this and am working on it" reaction (used by github-actions[bot], Linear, many CI bots). Per-org or per-rule customization is easy to add later without breaking the v1 contract.

**Skip PR review submissions.** GitHub's REST API supports reactions on individual review comments but not on review submissions themselves. We could fall back to a reaction on the underlying PR (a different content kind), but a 👀 on a whole PR is confusing — it would imply we noticed the PR, not the review. Better to silently skip review submissions (leave `Reaction` nil); the individual review comments still get reactions via the `pull_request_review_comment` webhook.

**`Reaction any` resolved by a type switch.** Rather than a discriminator enum, each reaction target is its own value type and the reactor type-switches on it. Each type maps 1:1 to an API endpoint, so there's nothing to keep in sync; adding an endpoint is a new type plus a `case`. Dispatch is runtime- rather than compile-checked, but the surface is one switch with a defensive `default`.

## Open Questions

1. **Should we react to atlassian/Jira matches too?** Jira issue comments support reactions via the [Atlassian REST API](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comments/) — `POST /rest/api/3/issue/{issueIdOrKey}/comment/{id}/reactions`. With the generic `Reactor`/`RouteOutcome` and a slice of reactors, this slots in cleanly: give the Jira webhook its own `Meta` type carrying the comment ref, add a Jira reactor that type-switches on it, and append it to the Atlassian router's `Reactors`. No change to `InputEvent`, `Router`, `eventrouter`, or the GitHub reactor. Worth doing in a v2 once the GitHub side is proven.

2. **Should the reaction get removed/updated when the task finishes?** Some bots (e.g. github-actions) update reactions to reflect state: 👀 while running, 🎉 on success, 👎 on failure. Appealing, but it adds a lot of state management: we'd track the reaction ID per comment, plumb task-completion events back to the reactor, and handle deletion failures. Not worth it in v1; can be added later if there's demand.

3. **What about "I matched but couldn't start the task" cases?** If routing matches but every wake/create fails, we'll have reacted to a comment that produced no work. Acceptable — the reaction means "I matched", not "I succeeded", and errors are already logged and visible in xagent. A reactor that wanted stricter semantics could inspect `RouteOutcome.TaskIDs` and skip when empty.

4. **Rate limits?** GitHub Apps get 5,000 requests/hour per installation, shared across all reads/writes. A reaction is one extra request per matched comment. At realistic comment volumes this is negligible, but worth keeping in mind if an org has a high-volume routing rule.
