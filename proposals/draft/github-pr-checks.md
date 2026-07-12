# GitHub PR checks for linked tasks

Issue: https://github.com/icholy/xagent/issues/886

## Problem

> It would be cool if the status of linked tasks were displayed as github PR checks.

When a task is working on a PR, its status lives only in the xagent web UI. A
reviewer looking at the PR on GitHub has no inline signal that a task is running
against it, whether it finished, or whether it failed. The ask is to surface a
task's lifecycle (running / completed / failed) directly on the PR it is linked
to, using GitHub's native check UI so it shows up in the PR's checks list and
merge box.

Today xagent already talks to GitHub in the outbound direction after routing an
inbound webhook: `githubserver.Server.react` posts an emoji reaction to the
resource that triggered a route (`internal/server/githubserver/reactions.go`),
authenticated as the App's installation. This proposal reuses that exact
machinery — installation-token client, async off the hot path — but flips the
direction: a **task status transition** drives a write to the PR, instead of an
inbound event driving a reaction.

## Decision up front: Check Runs API, not commit statuses

This is the load-bearing decision, so it is stated first for review.

**Use the Checks API (check runs), not the legacy commit statuses API.**

| | Check Runs API | Commit Statuses API |
|---|---|---|
| Permission | `checks: write` | `statuses: write` |
| Attaches to | a commit SHA (rendered against the PR whose head is that SHA) | a commit SHA |
| States | `queued`, `in_progress`, `completed` + `conclusion` (`success`/`failure`/`neutral`/`cancelled`/`timed_out`/…) | `pending`, `success`, `failure`, `error` only |
| Distinct "running" state | yes (`in_progress`) | no — only `pending` |
| Rich body | yes — `output.title` + `output.summary` markdown, updatable | no — single `description` line |
| Deep link | `details_url` per run (→ the xagent task UI) | `target_url` |
| Update in place | yes — `PATCH .../check-runs/{id}` by id | re-`POST` the same `context` string |
| Who can write | **GitHub Apps only** | Apps or tokens |

The deciding factors:

1. **A real `in_progress` state.** The whole point of #886 is showing a task's
   *progress*, not just its outcome. Check runs model `queued → in_progress →
   completed`, which maps cleanly onto `PENDING → RUNNING → COMPLETED/FAILED`
   (`model.TaskStatus`). Commit statuses only have `pending`, so a running task
   and a not-yet-started task would look identical.
2. **We are already a GitHub App.** Check runs can *only* be created by an App
   installation — which is exactly what xagent is (`ghinstallation` transport,
   `githubx.AppTokenCache`). There is no auth downside; the token we already
   mint for reactions can also write checks once the permission is granted.
3. **Updatable rich output + per-run deep link.** A check run's `output.summary`
   can carry the task name, latest `report` line, and a `details_url` that links
   straight to `https://<baseURL>/ui/tasks/{id}`. Statuses give one short line
   and no updatable body.
4. **Update semantics fit multiple tasks per PR.** Each task is its own check
   run, updated in place by its stored `check_run_id`. With statuses we would
   have to encode the task into the `context` string and re-POST; workable but
   strictly less capable.

**Trade-off accepted:** check runs are the heavier API (create returns an id we
must persist to update later; they bind to a specific head SHA and must be
re-created when the PR head advances). Commit statuses are simpler and idempotent
on `(sha, context)`. We take the extra bookkeeping in exchange for the
`in_progress` state and rich, deep-linkable output, which are core to the
feature. See [Trade-offs](#trade-offs) for the rejected alternative in full.

### Permission the App needs (currently missing)

The App is **read-only today**. `github-app-manifest.json` grants:

```json
"default_permissions": { "issues": "read", "metadata": "read", "pull_requests": "read" }
```

Check runs require **`checks: write`**, which is not present. This proposal adds
it to the manifest. Because tightening/adding permissions on an installed App
requires the installing account to **approve the new permission**, this is a
rollout step, not just a code change: existing installations stay read-only (and
this feature silently no-ops) until an admin accepts the request. That is called
out in the Implementation Plan as its own slice so it can land and be approved
before any check-writing code ships.

`pull_requests: read` (already granted) is sufficient to fetch a PR's head SHA;
no `contents` permission is needed.

## Design

### What a check looks like

One check run **per linked task, per PR**. Naming and content:

- **name**: `xagent: <task name>` (falls back to `xagent: task #<id>` when the
  task is unnamed). Distinct names let multiple tasks coexist on one PR without
  collapsing into one entry.
- **external_id**: the task id (string). This is how we correlate an inbound
  `check_run` re-request webhook (future) back to a task.
- **details_url**: `<baseURL>/ui/tasks/{id}` — the existing task URL
  (`task.url`, e.g. `https://xagent.choly.ca/ui/tasks/1290`).
- **head_sha**: the linked PR's current head commit SHA (see below).
- **status / conclusion**: mapped from `model.TaskStatus` (see the state table).
- **output.title / output.summary**: short status line + the task's latest
  `report` message when available (optional in v1; the title alone is enough to
  ship).

### Which commit the check attaches to

Check runs attach to a **commit SHA**, and GitHub renders them on whichever PR
has that SHA as its head. We attach to the **linked PR's current head commit**.

The task↔PR relationship is discovered through the **existing link machinery**.
A task links to a PR via `create_link` (`internal/agentmcp/xmcp.go` →
`CreateLink` RPC), which stores a row in `task_links` with a normalized
`routing_key` (`model.RoutingKey`, `internal/model/url.go`) that collapses
comment/API URLs to the canonical `https://github.com/<owner>/<repo>/pull/<n>`
web URL. To find a task's PRs we:

1. Load the task's links with the existing
   `store.ListLinksByTask(ctx, tx, taskID, orgID)`
   (`internal/store/link.go`).
2. Keep links whose `routing_key` matches a GitHub PR URL. Add a small parser
   (extends `internal/model/url.go`), e.g.
   `model.ParseGitHubPR(url) (owner, repo string, number int, ok bool)`, that
   recognizes `github.com/<owner>/<repo>/pull/<n>`.
3. For each matched PR, resolve the head SHA with go-github (v88, already a
   dependency): `client.PullRequests.Get(ctx, owner, repo, number)` →
   `pr.GetHead().GetSHA()`. The client is the same installation-authenticated
   one used by reactions: `github.NewClient(s.tokens.Client(installationID))`
   where `installationID = org.GitHubInstallationID`.

Note: `create_link` is already documented to set `subscribe=true` for resources
the agent creates (PRs), and PR links commonly already exist because the agent
subscribes to the PR to receive review events. This feature does **not** require
`subscribe=true` — it reads all of a task's PR links regardless — but in practice
the link is already there.

### What drives updates (keeping the check live)

The trigger is the **lifecycle event write path**, mirroring how `react` hangs
off `OnRouteOutcome`. Task status transitions already produce
`LifecyclePayload` events at well-defined points:

- **Runner transitions** — `internal/server/apiserver/runner.go`
  `SubmitRunnerEvents`: after `task.ApplyRunnerEvent`, it calls
  `event.LifecycleEvent(task, original.Status)` and `store.CreateEvent`,
  producing `SANDBOX_STARTED` (→ `RUNNING`), `SANDBOX_EXITED` /
  `SANDBOX_FAILED` (→ `COMPLETED`/`FAILED`). This is where the check should flip
  `in_progress` → `completed`.
- **Creation** — `internal/server/apiserver/task.go` `CreateTask` writes
  `LIFECYCLE_KIND_CREATED` (status `PENDING`).
- **Cancel / archive** — `task.go` writes `CANCELLED` / `ARCHIVED`.

We add a single hook alongside these writes. Two viable shapes, both grounded in
existing patterns:

- **(chosen) A `pubsub` subscriber.** The server already publishes a
  `model.Notification` after runner transitions
  (`runner.go`, `Type:"change"`, and `Notification.TaskStatus` is populated on
  **terminal** transitions). A new server-side background component —
  `githubserver.CheckReporter` — subscribes via
  `pubsub.Subscriber.Subscribe(ctx, orgID)` and, on a task-status change,
  reconciles that task's checks. This keeps the write off the request path for
  free (it is already a separate goroutine consuming a channel) and reuses the
  bus that `xagent notify` (`internal/command/notify.go`) already consumes.

  Two gaps to close, both small and called out as their own slices:
  - `Notification.TaskStatus` is currently set **only for terminal**
    transitions. To drive `PENDING → in_progress` we either (a) populate
    `TaskStatus` on the non-terminal `RUNNING` transition too, or (b) have the
    reporter re-fetch the task with `store.GetTask` when a notification's
    `Resources` include a `task` action and derive the state from
    `task.Status`. (b) needs no notification change and is preferred.
  - `Subscriber.Subscribe` is **per-org**. The reporter must watch every org
    that has a GitHub installation. It subscribes per-org for orgs with
    `github_installation_id != 0` (enumerated at startup and refreshed when an
    installation is linked). Alternatively a broadcast/all-orgs subscription can
    be added to `pubsub`; scoped as an open question.

- **(alternative) An inline async hook**, exactly like `react`: add an
  `OnLifecycle`-style callback at the `CreateEvent` call sites that spawns a
  detached, timed-out goroutine to reconcile. Simpler wiring, but scatters the
  hook across every transition call site instead of centralizing it behind the
  bus. Preferred only if the pubsub per-org enumeration proves awkward.

Either way the core is one idempotent function:

```go
// reconcileChecks brings every check run for task's linked PRs in line with the
// task's current status. Safe to call repeatedly.
func (r *CheckReporter) reconcileChecks(ctx context.Context, taskID int64) error
```

which: loads the task + its PR links, maps status → check state, and for each PR
creates the run (first time) or PATCHes it (subsequent), persisting the
`check_run_id`.

### Task status → check state mapping

| `model.TaskStatus` | check `status` | `conclusion` |
|---|---|---|
| `PENDING` | `queued` | — |
| `RUNNING`, `RESTARTING` | `in_progress` | — |
| `CANCELLING` | `in_progress` | — |
| `COMPLETED` | `completed` | `success` |
| `FAILED` | `completed` | `failure` |
| `CANCELLED` | `completed` | `cancelled` |

`SANDBOX_FAILED` carries a `Message` (the failure detail); it is surfaced in
`output.summary`. Auto-archive (`LIFECYCLE_KIND_AUTO_ARCHIVED`) does not change
the terminal conclusion — an archived-after-completion task keeps its `success`
check.

### Persisting the check-run id

Check runs are updated by id and are bound to a head SHA, so we need to remember
`(task, PR) → (check_run_id, head_sha)`. A new table:

```sql
-- internal/store/sql/migrations/202607XXXXXXXX_github_check_runs.sql
-- migrate:up
CREATE TABLE github_check_runs (
    id            bigserial PRIMARY KEY,
    task_id       bigint  NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    owner         text    NOT NULL,
    repo          text    NOT NULL,
    pr_number     bigint  NOT NULL,
    head_sha      text    NOT NULL,
    check_run_id  bigint  NOT NULL,   -- GitHub's check run id
    updated_at    timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (task_id, owner, repo, pr_number)
);

-- migrate:down
DROP TABLE github_check_runs;
```

Reconcile logic per (task, PR):

- No row yet → `Checks.CreateCheckRun` on the current head SHA, insert the row.
- Row exists and `head_sha` unchanged → `Checks.UpdateCheckRun(check_run_id)`.
- Row exists and head SHA **advanced** (PR got new commits) → create a **new**
  check run on the new SHA and update the row's `check_run_id` + `head_sha`. (The
  old run stays on the old commit, which is correct — checks are per-commit.)

### Backfill & edge cases

- **Link created after the task is already running/done.** When `CreateLink`
  stores a PR link, publish/enqueue a reconcile for that task so the check
  appears immediately at the task's current state (backfill). Without this, a
  check only appears on the *next* status transition.
- **Multiple tasks on one PR.** Each task is a separate check run (distinct
  `name`, `external_id`). They coexist in the PR's checks list; the `UNIQUE
  (task_id, owner, repo, pr_number)` key keeps them independent.
- **Task cancelled / archived.** `CANCELLED` → `completed`/`cancelled`.
  Archival alone doesn't move a terminal check. If a still-`in_progress` task is
  archived without a terminal transition, the reconcile closes its check as
  `cancelled` so the PR doesn't show a perpetually-running check.
- **No linked PR.** `reconcileChecks` finds no PR links and does nothing. Tasks
  with no GitHub link never touch the Checks API.
- **PR head advances.** Handled by the SHA-changed branch above. Optionally the
  already-handled `pull_request` (`synchronize`) webhook
  (`internal/server/githubserver/webhook.go`) can proactively re-post the
  current state onto the new head; v1 can rely on lazy re-creation at the next
  transition instead.
- **Org without an installation / missing `checks:write`.** `reconcileChecks`
  returns early when `org.GitHubInstallationID == 0` (same guard as `react`). A
  `403` from the Checks API (permission not yet approved) is logged and
  swallowed — the feature no-ops rather than erroring the transition.
- **PR closed / merged.** No special handling; the last check state stands.

## Implementation Plan

Layer cake, ordered so each slice is independently reviewable and the foundation
merges safely before any check-writing behavior exists.

1. **App permission request** — Delivers: `checks: write` added to
   `github-app-manifest.json`. Depends on: nothing. Verifiable by: manifest is
   valid JSON; the installed App's settings show a pending "checks (write)"
   permission request for admins to approve. Ships first so approval can happen
   out of band before code that needs it lands. No runtime behavior.

2. **PR URL parser** — Delivers: `model.ParseGitHubPR(url) (owner, repo string,
   number int, ok bool)` in `internal/model/url.go`, recognizing
   `github.com/<owner>/<repo>/pull/<n>` (and normalized routing keys). Depends
   on: nothing. Verifiable by: table-driven unit tests alongside the existing
   `RoutingKey` tests.

3. **Checks-run store table** — Delivers: the `github_check_runs` migration
   (up/down), regenerated `schema.sql`, sqlc queries, and store methods
   (`UpsertCheckRun`, `GetCheckRun(taskID, owner, repo, prNumber)`,
   `ListCheckRunsByTask`). Depends on: nothing. Verifiable by: migration runs up
   and down cleanly; store CRUD unit tests.

4. **GitHub checks client wrapper** — Delivers: a thin `githubx`/`githubserver`
   helper wrapping go-github's `Checks.CreateCheckRun` / `UpdateCheckRun` and
   `PullRequests.Get` (head SHA), using the existing
   `s.tokens.Client(installationID)`. Depends on: (1) for the permission at
   runtime. Verifiable by: unit test against a stubbed HTTP transport / manual
   call against a scratch repo.

5. **Reconcile core** — Delivers: `CheckReporter.reconcileChecks(ctx, taskID)`
   — load task + `ListLinksByTask`, filter with `ParseGitHubPR`, map
   `TaskStatus` → check state, create/patch via (4), persist via (3), including
   the head-SHA-advanced branch. Depends on: (2),(3),(4). Verifiable by:
   integration test that seeds a task + PR link, calls reconcile at each status,
   and asserts the recorded check calls/rows.

6. **Lifecycle trigger** — Delivers: the `CheckReporter` background component
   subscribing to `pubsub` per installation-org (or the inline `OnLifecycle`
   hook), wired in `server.go` next to the existing webhook/reaction wiring, so
   transitions drive `reconcileChecks`. Depends on: (5). Verifiable by:
   end-to-end — drive a task `PENDING→RUNNING→COMPLETED` and observe
   `queued→in_progress→success` check calls; failure path yields `failure`.

7. **Backfill on link creation** — Delivers: enqueue a reconcile when
   `CreateLink` stores a PR link, so a check appears immediately on an
   already-running/finished task. Depends on: (5),(6). Verifiable by: linking a
   PR to a running task produces an `in_progress` check without waiting for the
   next transition.

8. **(optional) Web UI affordance** — Delivers: surface the posted check /
   `details_url` next to the task's PR link in the task view, and/or an org
   setting to toggle check posting. Depends on: (6). Verifiable by: rendering
   the task view against a task with recorded check runs. `pnpm lint` in
   `webui/` before finishing.

## Trade-offs

- **Check Runs vs. commit statuses** — covered in
  [Decision up front](#decision-up-front-check-runs-api-not-commit-statuses).
  Statuses were rejected because they lack a distinct `in_progress` state and a
  rich updatable body, both central to showing task *progress*. They would be
  the simpler API (idempotent on `(sha, context)`, no id to persist, so slice
  (3) disappears), and are the fallback if `checks: write` approval proves too
  costly to roll out across installations — `statuses: write` is likewise a new
  permission, so it is no cheaper on that axis.

- **pubsub subscriber vs. inline async hook** — the subscriber centralizes the
  trigger behind the existing notification bus (one consumer, already
  off-request-path) at the cost of per-org subscription enumeration and needing
  the task re-fetch for non-terminal states. The inline hook mirrors `react`
  exactly (a goroutine at each `CreateEvent` site) but scatters the trigger. The
  plan picks the subscriber and keeps the inline hook as the documented
  fallback.

- **One check per task vs. one aggregate check per PR** — per-task checks make
  each task's state independently visible and map 1:1 to the task lifecycle. An
  aggregate "xagent" check summarizing all tasks on a PR would be tidier in the
  merge box but hides which task did what and needs cross-task rollup logic.
  Per-task is chosen; an aggregate summary could be added later.

- **Lazy head-SHA re-creation vs. reacting to `synchronize`** — recreating the
  check on the new head only at the next task transition is simplest and needs
  no new webhook wiring; a task that is already terminal won't move its check to
  a newer commit until something changes. Proactively re-posting on the
  `pull_request` `synchronize` webhook keeps checks pinned to the latest head
  but adds webhook-side work. v1 goes lazy.

## Open Questions

- Should reconcile re-post terminal checks onto new head commits via the
  `synchronize` webhook, or is lazy re-creation on the next transition
  acceptable for v1?
- Do we want `pubsub` to grow an all-orgs/broadcast subscription for
  server-side consumers like `CheckReporter`, or is per-installation-org
  subscription enumeration fine?
- Should check posting be opt-in per org (a settings toggle) or on by default
  for any org with a GitHub installation and `checks: write` granted?
- For `CANCELLED`, is `conclusion: cancelled` the right signal, or `neutral`
  (which some merge-protection setups treat differently)?
- Should `output.summary` include the latest `report` line / a short event
  tail, or is the status title + `details_url` enough for v1?
