# Routing Rules That Create Tasks

Issue: https://github.com/icholy/xagent/issues/717

## Problem

Routing rules today only ever *wake existing tasks*. The webhook flow looks up the commenter's xagent user, calls `Router.Route`, and that function derives candidate orgs implicitly from the set of subscribed links that already match the event URL (`internal/eventrouter/eventrouter.go:42-82`):

```go
linksByOrg, err := r.find(ctx, input)   // links matching the event URL
if err != nil {
    return 0, err
}
if len(linksByOrg) == 0 {
    return 0, nil                        // ← no task exists → give up here
}
```

When no link matches, the function returns early — rules are never consulted, no org is considered, no task can be created. There is no path for "an event arrived, no task exists for it yet, but an org has a rule that says: when this kind of event arrives, spin one up".

This proposal reworks routing so that an org's rules can opt into creating a task on first contact, with subsequent events for the same resource waking the created task via the existing path.

## Design

### Overview

1. Derive candidate orgs from the (linked) commenter via `ListOrgsByMember`, not from matched links. Routing then proceeds per org even when no link covers the URL.
2. Extend `model.RoutingRule` with a URL filter and an embedded create-task action carrying `workspace`, `runner`, and `prompt`.
3. In each org's per-org loop, decide wake-vs-create from that org's own state: if any subscribed link in the org covers the URL, wake (existing `attach` path); otherwise, if a matched rule has the create action, atomically create the task and its subscribed link.
4. Serialize concurrent create-attempts for the same `(org, url)` with a Postgres advisory transaction lock so racing webhooks collapse to a single created task.

The "commenter must be a linked xagent user" constraint stays unchanged. It is the security boundary: only events authored by a linked xagent user may create or wake tasks.

### 1. Org resolution — keep it bound to the commenter

Both webhook handlers already resolve the commenter to an xagent user before invoking `Route`:

- `internal/server/webhookserver/github.go:52` — `GetUserByGitHubUserID(extracted.githubUserID)`; `sql.ErrNoRows` drops the event.
- `internal/server/webhookserver/atlassian.go:77` — `GetUserByAtlassianAccountID(extracted.atlassianAccountID)`; same drop behaviour.

Routing extends from there. Candidate orgs are the orgs the commenter belongs to, via `Store.ListOrgsByMember(user.ID)` (`internal/store/org.go:48`). Each candidate org then evaluates its own routing rules and its own links independently.

**Rejected alternative — resolve orgs by GitHub installation id.** The `orgs` table already stores the installation id (`internal/store/org.go:171`, `SetOrgGitHubInstallation`), so an installation-keyed lookup is technically available. We reject it because it would let *any* commenter in an installed repo trigger task creation. The current invariant — "only linked xagent users can trigger tasks" — is a deliberate security feature, not a limitation. Keying on the installation breaks that invariant.

The Atlassian path inherits the same posture: account id → user → `ListOrgsByMember`. See §6 for Atlassian parity.

### 2. `RoutingRule` schema extension

Today (`internal/model/routing_rule.go`):

```go
type RoutingRule struct {
    Source  string `json:"source,omitempty"`
    Type    string `json:"type,omitempty"`
    Prefix  string `json:"prefix,omitempty"`
    Mention string `json:"mention,omitempty"`
}
```

`Prefix` matches the event **body** (`internal/eventrouter/rule.go:20`, `strings.HasPrefix(e.Data, rule.Prefix)`), not the URL. We need a separate URL-match dimension so a rule can target a specific repo/project, plus a place to carry the create-task config.

Proposed shape:

```go
type RoutingRule struct {
    Source    string             `json:"source,omitempty"`
    Type      string             `json:"type,omitempty"`
    Prefix    string             `json:"prefix,omitempty"`     // matches Data (legacy)
    Mention   string             `json:"mention,omitempty"`
    URLPrefix string             `json:"url_prefix,omitempty"` // NEW: matches event URL
    Create    *CreateTaskAction  `json:"create,omitempty"`     // NEW: if set, rule may create
}

type CreateTaskAction struct {
    Workspace string `json:"workspace"`
    Runner    string `json:"runner"`
    Prompt    string `json:"prompt,omitempty"`
}
```

Backward-compatible: rules without `url_prefix` or `create` keep behaving exactly as today (`url_prefix == ""` is wildcard, `create == nil` means wake-only). Adding fields to the JSON-stored rule list does not require a migration — `GetOrgRoutingRules` (`internal/store/org.go:191`) `json.Unmarshal`s into the current struct and skips unknown fields anyway. Existing rows continue to parse.

Proto mirror (`proto/xagent/v1/xagent.proto:532-537`):

```protobuf
message RoutingRule {
    string source = 1;
    string type = 2;
    string prefix = 3;
    string mention = 4;
    string url_prefix = 5;          // NEW
    CreateTaskAction create = 6;    // NEW
}

message CreateTaskAction {
    string workspace = 1;
    string runner = 2;
    string prompt = 3;
}
```

`model.RoutingRule.Proto` / `RoutingRuleFromProto` (`internal/model/routing_rule.go:17-34`) gain the two extra fields. The set/get RPCs (`GetRoutingRules` / `SetRoutingRules`) don't change shape — they ship the new fields automatically.

### 3. `MatchRule` gains the URL check

`internal/eventrouter/rule.go:13` becomes:

```go
func (e InputEvent) MatchRule(rule model.RoutingRule) bool {
    if rule.Source != "" && rule.Source != e.Source {
        return false
    }
    if rule.Type != "" && rule.Type != e.Type {
        return false
    }
    if rule.URLPrefix != "" && !strings.HasPrefix(e.URL, rule.URLPrefix) {
        return false
    }
    if rule.Prefix != "" && !strings.HasPrefix(e.Data, rule.Prefix) {
        return false
    }
    if rule.Mention != "" && !e.matchMention(rule.Mention) {
        return false
    }
    return true
}
```

The URL filter is a plain prefix match against `InputEvent.URL`, consistent with the existing body `Prefix`. Recommend prefix over substring or regex: it covers the obvious repo/project-scoping case (`https://github.com/icholy/xagent/`) without giving the user a sharp tool.

### 4. Reworked `Route` flow

The replacement for `internal/eventrouter/eventrouter.go:42-82`:

```go
func (r *Router) Route(ctx context.Context, input InputEvent) (int, error) {
    if input.URL == "" || input.UserID == "" {
        return 0, nil
    }

    // Candidate orgs come from the (linked) commenter, not from matched links.
    orgs, err := r.Store.ListOrgsByMember(ctx, nil, input.UserID)
    if err != nil {
        return 0, err
    }
    if len(orgs) == 0 {
        return 0, nil
    }
    orgIDs := make([]int64, len(orgs))
    for i, o := range orgs {
        orgIDs[i] = o.ID
    }

    // Per-org links (subscribed, matching URL) and per-org rules, in one pass each.
    linksByOrg, err := r.find(ctx, input) // still scoped to user, still groups by org
    if err != nil {
        return 0, err
    }
    orgRules, err := r.Store.GetRoutingRulesByOrgs(ctx, nil, orgIDs)
    if err != nil {
        return 0, err
    }

    var n int
    for _, orgID := range orgIDs {
        rules := orgRules[orgID]
        if len(rules) == 0 {
            rules = defaultRules
        }
        // Collect the matching rules; first matched create-rule (if any) wins.
        var matched bool
        var createRule *model.RoutingRule
        for i := range rules {
            if !input.MatchRule(rules[i]) {
                continue
            }
            matched = true
            if createRule == nil && rules[i].Create != nil {
                createRule = &rules[i]
            }
        }
        if !matched {
            continue
        }
        if links := linksByOrg[orgID]; len(links) > 0 {
            // Wake-path: unchanged.
            event := &model.Event{Description: input.Description, Data: input.Data, URL: input.URL, OrgID: orgID}
            if err := r.Store.CreateEvent(ctx, nil, event); err != nil {
                r.Log.Error("failed to create event", "org_id", orgID, "error", err)
                continue
            }
            for _, link := range links {
                if err := r.attach(ctx, link.TaskID, event); err != nil {
                    r.Log.Error("failed to attach event to task", "event_id", event.ID, "task_id", link.TaskID, "error", err)
                    continue
                }
                n++
            }
            continue
        }
        if createRule == nil {
            continue
        }
        created, err := r.create(ctx, input, orgID, createRule)
        if err != nil {
            r.Log.Error("failed to create task from rule", "org_id", orgID, "error", err)
            continue
        }
        if created {
            n++
        }
    }
    return n, nil
}
```

Notes on this flow:

- `find` (`internal/eventrouter/eventrouter.go:86`) is unchanged. Its result is now consumed as an org-keyed map looked up inside the per-org loop; orgs absent from the map simply have no matching link.
- Per-org isolation falls out naturally: a matching link in org A has no effect on org B's wake-vs-create decision, because `linksByOrg[orgB]` is empty by construction.
- The wake branch reuses the existing `attach` helper (`internal/eventrouter/eventrouter.go:117-159`), so log lines, notifications, and the "webhook started task" audit log are unchanged for the existing case.
- "First matched create-rule wins" is a deterministic, easy-to-explain tie-breaker. Multiple matching create-rules are pathological config; we don't try to merge or score them.

### 5. Atomic create + dedup

When a create-rule fires, the new task and its subscribed `Link` to the event URL must be created in the **same transaction**. The link is the dedup key: once it exists, the next event for this URL takes the wake path via `find` → `attach` and no duplicate task is created.

`internal/server/apiserver/task.go:103-115` already shows the pattern for creating a task + audit log in one tx via `Store.WithTx`. The router-side helper extends that pattern with the link create and an advisory lock:

```go
func (r *Router) create(ctx context.Context, input InputEvent, orgID int64, rule *model.RoutingRule) (bool, error) {
    var created bool
    err := r.Store.WithTx(ctx, nil, func(tx *sql.Tx) error {
        // Serialize concurrent create attempts for the same (org, url).
        if err := r.Store.LockOrgURL(ctx, tx, orgID, input.URL); err != nil {
            return err
        }
        // Re-check inside the lock: another tx may have just created the task+link.
        existing, err := r.Store.FindLinksByURL(ctx, tx, input.URL, orgID)
        if err != nil {
            return err
        }
        for _, l := range existing {
            if l.Subscribe {
                return nil // dedup hit — leave the existing task; the wake path on the next event handles it
            }
        }

        task := &model.Task{
            Name:         routedTaskName(input),
            Runner:       rule.Create.Runner,
            Workspace:    rule.Create.Workspace,
            Instructions: buildInstructions(input, rule),
            Status:       model.TaskStatusPending,
            Command:      model.TaskCommandStart,
            Version:      1,
            OrgID:        orgID,
        }
        if err := r.Store.CreateTask(ctx, tx, task); err != nil {
            return err
        }
        if err := r.Store.CreateLink(ctx, tx, &model.Link{
            TaskID:    task.ID,
            URL:       input.URL,
            Relevance: "trigger",
            Subscribe: true,
            CreatedAt: time.Now().UTC(),
        }); err != nil {
            return err
        }
        if err := r.Store.CreateLog(ctx, tx, &model.Log{
            TaskID: task.ID, Type: "audit",
            Content: "webhook created task",
        }); err != nil {
            return err
        }
        created = true
        return tx.Commit()
    })
    if err != nil {
        return false, err
    }
    if created {
        r.publish(ctx, /* notification: task created + log appended */)
    }
    return created, nil
}
```

**Why an advisory lock instead of a unique constraint.** A `UNIQUE (org_id, url) WHERE subscribe = TRUE` partial index would prevent racing tasks at the DB level, but `task_links` (`internal/store/sql/migrations/20240101000001_initial.sql:77-88`) intentionally allows multiple tasks in the same org to subscribe to the same URL (e.g., a parent task and a child task both watching the same PR). A unique constraint would break that pattern and require backfilling/deduplicating any current rows that share `(org, url)` with `subscribe = TRUE`. A per-`(org, url)` `pg_advisory_xact_lock` serializes concurrent create-rule firings without imposing a global uniqueness rule.

Concretely, `LockOrgURL` is a new store method that issues:

```sql
SELECT pg_advisory_xact_lock(hashtextextended($1 || '|' || $2, 0));
```

with `$1 = orgID::text` and `$2 = url`. The lock is automatically released at transaction end. The loser of a race waits, then sees the subscribed link the winner created, and returns "dedup hit" without creating a duplicate.

**No migration is required** for the dedup mechanism. The advisory lock is a runtime primitive; we don't add columns, indexes, or constraints to `task_links`.

The other route through `Route` (the wake path) is unaffected by this lock — only the create path acquires it.

### 6. Prompt sourcing

The created task gets a two-instruction prompt:

1. **A boilerplate preamble** generated from event context. Recommend a single short line that names the source, type, description, and URL — enough for the agent to know who triggered it and where to look. Sketch:

   ```
   You were created by a routing rule in response to a {source} {type} event.

   Description: {description}
   URL: {url}

   Data:
   {data}
   ```

2. **The rule's `Prompt`** appended verbatim as a second instruction, with the trigger context above. If `Prompt` is empty, only the preamble is sent (acceptable degenerate case).

Two instructions, not one concatenated string, matches the existing system: `model.Task.Instructions` is `[]Instruction` (`internal/model/task.go:67`) and the runner already iterates over multiple instructions. It also keeps the rule author's prompt distinguishable from the auto-generated context.

Templating the rule prompt with `{description}` / `{url}` / etc. is **not** proposed. The preamble already carries the event context; mixing templating into the user-supplied prompt invites a second design problem (escaping, missing-field errors, validation). If we later need it, it's an additive change to a single function.

### 7. Atlassian parity

Both `webhookserver/atlassian.go` and `webhookserver/github.go` already produce `eventrouter.InputEvent` with a resolved `UserID`. The org resolution change in §1 is implemented inside `Route` (not in the handlers), so Atlassian inherits the same behaviour with no handler-side changes: account id → user → `ListOrgsByMember` → per-org rule evaluation → create or wake.

The only Atlassian-specific consideration is that `extractAtlassianWebhookEvent` (`internal/server/webhookserver/atlassian.go:117`) uses the issue browse URL as the routing URL. A create-rule with `url_prefix: "https://your-domain.atlassian.net/browse/PROJ-"` targets a single project, the obvious parallel to the GitHub repo-prefix case.

We **do not** scope create-rules to GitHub-only; Atlassian gets the same capability in the same change.

### 8. UI impact

The routing-rule editor lives in a shared form (`webui/src/components/routing-rule-form.tsx`) used by `/routing/new` and `/routing/$index` after PR #730. PR #730 is the structural prep; a friendlier event-type dropdown (source-aware fields replacing the raw `source`/`type` inputs) is mid-redesign.

This proposal adds four fields the friendlier form will need to surface:

- `url_prefix` — text input, alongside the existing match fields. Suggest the autocomplete prefix per source (e.g. `https://github.com/`).
- `create` — a toggle that reveals the action sub-form. Off: today's wake-only rule.
- `create.workspace` and `create.runner` — selects backed by the existing workspace listing (the Create Task screen already does this; reuse the pattern).
- `create.prompt` — multi-line text area.

The proposal does **not** design that form; it flags that the friendlier event-type rework should layer these on, not the old raw-field form. Also, comment #4584625992 on the issue plans to move Routing Rules out of Settings into a top-level Events section; the new fields land in whichever home that refactor settles on (it does not block this work).

### 9. Default rules

`defaultRules` (`internal/eventrouter/eventrouter.go:35-37`) stays a single prefix-only wake rule. The default behaviour for an org with no configured rules is unchanged: comments starting with `xagent:` wake matching tasks, no creation. To opt into creation an org configures a rule with `create` set.

### 10. Test plan sketch

`internal/eventrouter` already has table-driven tests in `eventrouter_test.go`; the new cases extend it:

- **`MatchRule` URL filter** — table test in `rule_test.go` covering `url_prefix` empty (wildcard), match, mismatch, prefix overlap with body.
- **Route — wake unchanged** — `TestRouteCreatesEventAndStartsTask` (current) and `TestRouteMultipleOrgs` (current) keep passing without modification.
- **Route — create on first event** — set up an org with a create-rule, no link, fire an event whose URL matches `url_prefix`; assert one task created with the expected workspace/runner/instructions and one subscribed link pointing at the event URL.
- **Route — second event wakes the created task** — replay the same event; assert no second task created, and the existing task transitions through `attach`.
- **Per-org isolation** — user belongs to org A (matching link) and org B (matching create-rule, no link). Assert A wakes its task and B creates a new one; the link in A does not suppress creation in B.
- **Concurrent dedup** — fire two `Route` calls for the same `(org, url)` in parallel goroutines, both matching a create-rule. Assert exactly one task and one subscribed link exist after both return. This is the test that exercises the advisory lock.
- **Create-rule without matching URL prefix** — assert the rule is skipped and no task is created.
- **Multiple matched create-rules** — assert the first-in-list rule wins (deterministic tie-break).

The advisory-lock test belongs in `internal/store` or `internal/eventrouter` with `teststore.New(t)` — `teststore` already spins up real Postgres, so the lock is exercised end-to-end.

## Trade-offs

### Org resolution: linked-commenter vs installation id

**Chosen: linked-commenter (`ListOrgsByMember`).** The invariant "only linked xagent users can trigger tasks" is a security feature. Installation-id keying would let any commenter in an installed repo trigger creation — strictly broader and a strict regression in posture. See §1.

### Dedup: advisory lock vs unique constraint

**Chosen: per-`(org, url)` advisory transaction lock.** A `UNIQUE (org_id, url) WHERE subscribe = TRUE` partial index would also work and is arguably simpler, but it forbids multiple tasks in the same org subscribing to the same URL — a pattern the current schema allows and that parent/child task setups can legitimately produce. The advisory lock keeps the constraint scoped to "only one *router-created* task per `(org, url)`" rather than "only one *anything* subscribed per `(org, url)`". See §5.

### Rule shape: embedded `create` block vs flat fields

**Chosen: an embedded `CreateTaskAction` struct (`create` in JSON, optional sub-message in proto).** Two reasons:

1. Presence-as-discriminator: `create != nil` cleanly says "this rule may create a task". A flat `create_task bool` + flat `workspace`/`runner`/`prompt` would be functionally equivalent but invites invalid combinations (`create_task = false` but `workspace = "x"`).
2. The action fields are conceptually a unit. Grouping them helps the UI render the action as one collapsible block.

### URL match: prefix vs substring vs regex

**Chosen: prefix.** Substring is too permissive (matches inside fragments/query strings); regex is overkill and a sharp tool for the common case (scoping to a repo or project). If a use case for regex emerges, it can be added as a separate field without breaking the prefix semantics.

### Prompt templating

**Chosen: no templating in the rule's `prompt`.** The auto-generated preamble already carries `{source, type, description, url, data}`; the rule's prompt is appended verbatim. Templating is an additive change later if a real need surfaces.

## Open Questions

1. **Exact preamble text.** §6 sketches it. The wording is easy to iterate on later; the structural decision is "preamble is its own instruction, separate from the rule's prompt".
2. **Notification message for created tasks.** `attach` publishes `"Task N woken by event M: …"`. The create path should publish an analogous `"Task N created by routing rule for event …"`. Wording belongs in implementation review.
3. **Rule limits or validation.** Should the API reject create-rules whose `workspace`/`runner` don't exist on the org at save time? `apiserver.CreateTask` validates this at task-create time (`internal/server/apiserver/task.go:78-84`); doing it in `SetRoutingRules` is friendlier but adds a foreign-key-like check across the rules JSON column. Defer to implementation.
4. **Auto-archive defaults for created tasks.** `Task.ArchiveAfter` (`internal/model/task.go:77`) defaults to "never". For long-lived event-driven tasks this may be fine; for one-shot triggers a non-zero default could be useful. Could be added later as `create.archive_after` without breaking the v1 shape.
