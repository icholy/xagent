# Unified scope-based (capability) permission model

Issue: https://github.com/icholy/xagent/issues/894

## Problem

Authorization in xagent is enforced by several disjoint mechanisms, and the
server must know *which kind of caller* it is talking to in order to apply the
right one. There is no single place that answers "is this caller allowed to do
this?", and no way to grant anything narrower than "everything in the org."

### Today: three credentials, two enforcement styles

**Credential types** are dispatched by token shape in
`internal/auth/apiauth/apiauth.go` (`authenticate`, ~line 204):

1. **Opaque `xat_` API key** — `IsKey(raw)` →
   `KeyValidator.ValidateKey(ctx, HashKey(raw))`. The DB-backed implementation
   is `StoreKeyValidator.ValidateKey` (`internal/server/storeauth.go:23`), which
   returns `&apiauth.UserInfo{OrgID: key.OrgID, Name: key.Name, Type: AuthTypeKey}`.
2. **App JWT** — `VerifyAppToken(appKey, raw)` (`internal/auth/apiauth/jwt.go:71`)
   → `UserInfo{ID, Email, Name, OrgID, Type: AuthTypeApp}`.
3. **Cookie session** (web UI) via zitadel/OIDC — `Auth.User`
   (`apiauth.go:367`) builds `UserInfo{..., Type: AuthTypeCookie}`.

None of these carry permission information. The `UserInfo` struct
(`apiauth.go:29`) has `ID, Email, Name, OrgID, Type, ClientID` — and nothing
else. **Every authenticated caller is implicitly omnipotent within its
`OrgID`.**

**Enforcement style A — org scoping.** Every `apiserver` handler narrows its
store queries to `apiauth.Caller(ctx).OrgID`. For example `ListTasks`
(`internal/server/apiserver/task.go:17`) calls
`s.store.ListTasks(ctx, nil, caller.OrgID)`; `CreateLink`
(`internal/server/apiserver/link.go:14`) verifies
`s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)`. Org is the tenancy
boundary, and within an org it is the *only* check. The lone interceptor,
`RequireUserInterceptor` (`apiauth.go:290`), only checks that *some* caller is
present — never what they may do.

**Enforcement style B — task scoping.** In-container agents talk to the server
through `AgentFilter` (`internal/agentmcp/filter.go`), which implements the same
`XAgentServiceHandler` interface and restricts each RPC to the agent's own task
or its direct children with bespoke per-RPC logic:

- `CreateLink` / `UploadLogs` / `SubmitRunnerEvents`: `req.TaskId == claims.TaskID`
  (events checked per-element, all-or-nothing — `filter.go:62`).
- `GetTask` / `GetTaskDetails` / `ListLogs` / `UpdateTask`: own task always
  allowed; a task whose `Parent == claims.TaskID` allowed **only if** the
  `child_tasks` scope is present.
- `CreateTask`: requires `child_tasks` **and** `req.Parent == claims.TaskID`
  **and** `req.Workspace == claims.Workspace` **and** `req.Runner == claims.Runner`
  (`filter.go:75`).
- `ListChildTasks`: requires `child_tasks` **and** `req.ParentId == claims.TaskID`.
- `CreateGitHubToken`: requires the `github_token` scope (`filter.go:192`).
- `UpdateTask`: additionally rejects the request if the target task is
  `Archived` (`filter.go:166`) — a state check, not an identity check.

The task token already carries an **ad-hoc, partial notion of scopes**:
`TaskClaims.Scopes []string` (`internal/auth/agentauth/token.go:38`) with two
recognized values, `ScopeChildTasks` (`child_tasks`) and `ScopeGitHubToken`
(`github_token`), checked via `claims.HasScope`. These scopes are minted by the
runner from the workspace config (`AgentProxy.TaskToken`,
`internal/runner/proxy.go:82`, fed by `w.Scopes` in
`internal/runner/workspace/workspace.go:310`).

### Why this hurts

- **Two enforcement styles, zero shared code.** `AgentFilter` and the
  `apiserver` handlers re-implement permission logic in different shapes. The
  `AgentFilter` runs in the *runner* (in front of the Unix-socket proxy,
  `internal/runner/proxy.go:62`), so the server's own handlers have **no
  task-level authorization at all** — an org-scoped caller (user/key/app JWT)
  can read or mutate *any* task in its org. Task isolation exists only because
  agents are forced through `AgentFilter`; it is not a server-enforced property.
- **Coarse-grained.** An `xat_` key cannot be limited to, say, "create tasks in
  one workspace" or "read-only." It is all-or-nothing per org.
- **Caller-type branching.** Identity type leaks into logic: `AuditName()`
  special-cases `AuthTypeKey` (`apiauth.go:50`); the agent path and the API path
  are entirely separate handler trees.
- **Adding a resource touches everything.** A new RPC must be threaded through
  both org scoping (in the handler) and, if agents can call it, `AgentFilter`.

## Design

One model: an authenticated **caller carries a set of scopes**, and a single
**scope evaluator** decides every request. Org stays a separate axis.

### 1. `Caller.Scopes` — the unifying abstraction

Add a scope set to the authenticated caller:

```go
// internal/auth/apiauth/apiauth.go
type UserInfo struct {
    ID       string
    Email    string
    Name     string
    OrgID    int64
    Type     string
    ClientID string
    Scopes   scope.Set // NEW
}
```

The unifying abstraction is **`Caller.Scopes`**, populated per credential source
— *not* "every JWT has a scopes field." Each authentication path fills it:

| Credential | Source of scopes |
|---|---|
| `xat_` API key | new `scopes` column on the key record (`model.Key`), returned by `StoreKeyValidator.ValidateKey` |
| App JWT (`AppClaims`) | new `scopes` JWT claim, set when the token is minted at `GET /auth/token` (`HandleToken`, `apiauth.go:314`) |
| Cookie session | a granted set computed at `attachUserInfo` / `Auth.User` time (initially the admin wildcard — see Migration) |
| Task token (`TaskClaims`) | the existing `Scopes []string`, reshaped into the new grammar |

Note that **task tokens already work this way** — the agent path is the proof of
concept. This proposal generalizes that one field to all callers and replaces
the bespoke string matching with a real evaluator.

`scope.Set` is a small value type wrapping `[]scope.Scope` (the parsed form,
below) with an `Authorize(target)` method. It lives in a new package,
`internal/auth/scope`, so both `apiauth` (server/API callers) and `agentauth`
(task callers) depend on it without a cycle.

### 2. Scopes are permissions WITHIN an org

Org remains a **separate tenancy axis**. The token/caller still carries
`org_id`; handlers still constrain their store queries to that org exactly as
today (`caller.OrgID`). **A scope never crosses orgs.** Scopes express intra-org
capability only.

Concretely: a request is authorized iff *both* hold:

1. **Tenancy:** the target resource belongs to `caller.OrgID` (enforced by the
   existing `...(ctx, nil, ..., caller.OrgID)` store calls — unchanged), and
2. **Capability:** some held scope authorizes the action on that target (new).

This keeps the org check where it already is and well-tested, and means the
scope grammar never has to encode org IDs. Cross-org access remains impossible
by construction even if a scope is mis-minted.

### 3. Scope grammar — resource/attribute-parameterized capabilities

A scope is an **action** on a **resource type**, optionally constrained by a
conjunction of **attribute predicates**:

```
action:resource[.field:value][,field:value...]
```

- `action` ∈ `{read, write, create, *}` (verb taxonomy — see Open Questions).
- `resource` is a resource type: `task`, `github_token`, … (see taxonomy below).
- Zero or more `field:value` predicates constrain *which* instances of the
  resource the scope covers. `*` is a wildcard in **any** position (action,
  resource, field-value).

Worked vocabulary:

| Scope | Meaning |
|---|---|
| `read:task.id:123` | read the task with id 123 |
| `read:task.parent:123` | read any task whose `parent` is 123 (child access, resolved against the loaded row) |
| `write:task.id:123` | write the task with id 123 |
| `create:github_token` | issue a GitHub token (a plain capability, no resource instance) |
| `create:task.parent:123,workspace:X,runner:Y` | create a task **iff** parent=123 **and** workspace=X **and** runner=Y |
| `*:task.id:*` | any action on any task (task-domain admin) |
| `*:*:*` | global admin within the org |

The multi-predicate form (`create:task.parent:123,workspace:X,runner:Y`) is a
**single scope carrying a conjunction**. This is the crux of correct CREATE
handling — see §4.

#### String form and parsed form

Wire form is the string above (compact, fits a JWT claim or a DB column).
Parsed form:

```go
// internal/auth/scope
type Scope struct {
    Action   string            // "read", "write", "create", or "*"
    Resource string            // "task", "github_token", or "*"
    Preds    map[string]string // field -> required value; value may be "*"
}

type Target struct {
    Action   string
    Resource string
    Attrs    map[string]string // concrete attributes of the request/row
}

func (s Scope) Matches(t Target) bool
func (set Set) Authorize(t Target) bool
```

`TaskClaims.Scopes` migrates from `["child_tasks","github_token"]` to the
grammar (see §5 for the exact mapping). `scope.Set` parses the strings once at
token-verification time.

### 4. Evaluation semantics: AND within a scope, OR across scopes

> A request is authorized if **some** held scope's predicate fully matches the
> target (**OR across the caller's held scopes**); a scope matches only if
> **all** of its internal predicates match the target (**AND within the
> scope**).

```go
func (s Scope) Matches(t Target) bool {
    if !wild(s.Action, t.Action) || !wild(s.Resource, t.Resource) {
        return false
    }
    for field, want := range s.Preds { // AND within the scope
        got, ok := t.Attrs[field]
        if !ok || !wild(want, got) {
            return false
        }
    }
    return true
}

func (set Set) Authorize(t Target) bool {
    for _, s := range set { // OR across scopes
        if s.Matches(t) {
            return true
        }
    }
    return false
}

func wild(pattern, value string) bool { return pattern == "*" || pattern == value }
```

This single rule has to satisfy four requirements simultaneously. Each is worked
through below, with the failure modes that motivate the AND-within / OR-across
split.

#### (a) CREATE needs a conjunction — kept *inside one scope*

`CreateTask` today requires **four** things at once (`filter.go:83-91`):
`child_tasks` present, `parent == claims.TaskID`, `workspace == claims.Workspace`,
`runner == claims.Runner`. In the new model the agent token holds:

```
create:task.parent:42,workspace:ws,runner:rn
```

and the `CreateTask` handler builds the target from the *request*:

```
Target{Action:"create", Resource:"task",
       Attrs:{parent:"<req.Parent>", workspace:"<req.Workspace>", runner:"<req.Runner>"}}
```

`Matches` returns true only if **all three** predicates hold — exactly the
current behavior.

**Failure mode if we instead split the conjunction into separate scopes** —
`["create:task.parent:42", "create:task.workspace:ws", "create:task.runner:rn"]`
— and OR across them: a `CreateTask` target would match
`create:task.parent:42` on its own (the other scopes don't have to match; OR
only needs one). A caller could then create a task with `parent=42` but an
**arbitrary workspace/runner**, escalating out of its sandbox. The conjunction
*must* live inside a single scope so that ORing across the caller's scopes can
never relax it.

This is why the grammar has a multi-predicate form at all: it lets the AND that
CREATE needs be expressed without the evaluator ever having to AND *across*
scope strings.

#### (b) Relationship / child access is naturally disjunctive — *across scopes*

`GetTask` today allows own task **or** a direct child (with `child_tasks`)
(`filter.go:118-127`). That is a genuine OR, and it maps to two scopes:

```
read:task.id:42          # own task
read:task.parent:42      # any direct child of 42
```

`GetTask` loads the row (it already does — `p.client.GetTask`), then builds:

```
Target{Action:"read", Resource:"task", Attrs:{id:"<row.Id>", parent:"<row.Parent>"}}
```

- Own task (`row.Id==42`): matches `read:task.id:42`. ✅
- Child (`row.Parent==42`): matches `read:task.parent:42`. ✅
- Unrelated task: matches neither → denied. ✅

OR-across-scopes is exactly right here. The `parent` predicate is **resolved
against the loaded row at request time**, which is how relationship access works
without the evaluator needing a graph: the handler fetches the row (scoped to
the caller's org), then asks the evaluator about the row's attributes.

**Failure mode if we instead require AND across all held scopes:** a caller
holding both `read:task.id:42` and `read:task.parent:42` could read *nothing* —
its own task fails the `parent:42` scope, and a child fails the `id:42` scope, so
no single target satisfies both. Pure-AND-across-scopes breaks the moment a
caller holds more than one grant. Scopes must be **additive** (OR), which is the
standard capability-model semantics.

#### (c) Wildcard admin subsumes everything narrower

`*:task.id:*` must cover own task **and** children, because a child is still a
task matched by `id:*`:

- Child target `Attrs:{id:99, parent:42}` vs scope `*:task.id:*`: action `*`✓,
  resource `task`✓, predicate `id:*` matches `99`✓ → allowed.

And `*:*:*` matches any target (`Preds` empty, action/resource wild). Because
matching is OR-across-scopes, holding a broad scope **plus** narrow ones is
always ≥ the narrow ones alone — admin can never be *less* than a sub-grant.
This is the property that lets Migration grant `*:*:*` to existing callers and
preserve today's omnipotence exactly.

#### (d) Handlers must not need per-RPC "which attributes are required" knowledge

The danger is that the evaluator pushes model knowledge back into handlers. We
avoid it by splitting responsibilities cleanly:

- The **handler** only describes *what the request is*: it emits a `Target`
  with the action, resource, and the concrete attributes of the request (for
  create) or the loaded row (for read/write). It does **not** know which
  predicates a scope "should" constrain.
- The **scope** declares which predicates *it* constrains; the evaluator ANDs
  exactly those.

So `CreateTask` always emits `Attrs:{parent, workspace, runner}` regardless of
what scopes exist. A token scoped `create:task.parent:42,workspace:ws,runner:rn`
constrains all three; a hypothetical `create:task.parent:42` would constrain
only `parent` — **the handler code is identical either way.** The "which
attributes matter" decision lives entirely in the minted scope string, not in
the handler.

The failure mode we are avoiding is the "separate ANDed scope strings" design
from (a): there, to be safe, the *handler* would have to know "for create, the
caller must hold a parent-scope AND a workspace-scope AND a runner-scope" and
check three memberships — leaking the constraint model into every create
handler. The conjunctive-single-scope grammar keeps that knowledge in the token.

### 5. Default-deny + RPC → required-permission mapping

Authorization becomes a **single interceptor** plus, for relationship/state
checks, a thin per-handler `Target` builder. Default is **deny**: an RPC with no
mapping, or a target no held scope matches, is rejected with
`connect.CodePermissionDenied`.

Each RPC declares the permission it requires. Two shapes:

1. **Request-only targets** — everything needed is in the request (or is a plain
   capability). Checked in an interceptor before the handler runs.
2. **Row-dependent targets** — the predicate references attributes of a stored
   row (e.g. `task.parent`). The row must be loaded (org-scoped) first, then the
   target is evaluated. These stay close to the handler (the handler already
   loads the row today), but call the shared `scope.Set.Authorize`.

Re-expressing the **current `AgentFilter`** rules (the existing tests in
`internal/agentmcp/filter_test.go` are the behavioral spec to preserve):

| RPC | Target | Today's rule reproduced |
|---|---|---|
| `CreateLink`, `UploadLogs` | `write:task.id:<req.TaskId>` | `req.TaskId == claims.TaskID` via `write:task.id:42` |
| `SubmitRunnerEvents` | per-event `write:task.id:<ev.TaskId>`, **all** must pass | all-or-nothing batch (`filter.go:67`) |
| `GetTask`, `GetTaskDetails`, `ListLogs` | `read:task.id:<row.id>` OR `read:task.parent:<row.parent>` | own-or-child; child requires the `read:task.parent:42` scope, absent ⇒ child denied |
| `UpdateTask` | `write:task.id:<row.id>` OR `write:task.parent:<row.parent>` | own-or-child; plus archived guard (§ below) |
| `CreateTask` | `create:task.parent:<req.Parent>,workspace:<req.Workspace>,runner:<req.Runner>` | four-way conjunction |
| `ListChildTasks` | `read:task.parent:<req.ParentId>` | parent match + `child_tasks` |
| `CreateGitHubToken` | `create:github_token` | `github_token` scope |

The old task scopes therefore map to:

- `child_tasks` ⇒ the token gains the child-relationship scopes
  `read:task.parent:<self>`, `write:task.parent:<self>`,
  `create:task.parent:<self>,workspace:<ws>,runner:<rn>`, and
  `read:task.parent:<self>` for `ListChildTasks`.
- `github_token` ⇒ `create:github_token`.
- The always-present own-task access ⇒ `read:task.id:<self>`,
  `write:task.id:<self>`.

Re-expressing the **current org-scoping** (the API callers): users, sessions,
and `xat_` keys are omnipotent within their org today. In the model that is
`*:*:*` (see Migration). The org tenancy check is untouched; `*:*:*` only
satisfies the *capability* half, so behavior is identical. Narrowing those
grants later (e.g. a read-only key = `read:*:*`) is future work, not this
proposal.

Because both the agent path and the API path now compute a `Target` and call
the same `scope.Set.Authorize`, the two enforcement styles collapse into one.
`AgentFilter`'s hand-written per-RPC checks are replaced by target builders that
feed the shared evaluator; the server can additionally run the same evaluator,
so task isolation is no longer dependent on the runner-side filter being in the
path.

### State-based checks coexist as request-time guards

Some rules are not about *identity/capability* but about *resource state* and
cannot live in a scope. The clearest is `UpdateTask`'s "cannot update archived
task" (`filter.go:166`): whether a task is archived is mutable runtime state,
not a property a token can be scoped to. These remain ordinary request-time
guards, evaluated **after** scope authorization passes:

```go
// 1. capability: scope evaluator authorizes write:task.id:<row.id> (or parent)
// 2. state guard: if row.Archived { return PermissionDenied("cannot update archived task") }
```

Scope evaluation answers "may this caller, in principle, write this task?"; the
state guard answers "is the task currently in a writable state?" They are
orthogonal and both must pass. Keeping state guards out of the grammar keeps the
grammar pure (a function of the token and the target's *identity* attributes,
not its lifecycle), which is what makes it exhaustively testable.

## Migration

This is the crux: **migrate everything we have right now onto the model without
changing anyone's effective access.** Existing user sessions and `xat_` keys
carry no scopes and are omnipotent within their org; the moment a handler
*requires* a scope, they break unless they already hold an admin scope. So the
order is strict: introduce the machinery, grant admin to everyone, *then*
convert checks. Narrowing real permissions is a separate, later effort.

### Phase 0 — Define the model, no enforcement

- Add `internal/auth/scope` (`Scope`, `Set`, `Target`, parser, `Authorize`)
  with exhaustive table tests. No caller consults it yet.
- Add `Scopes scope.Set` to `apiauth.UserInfo` (unparsed/empty for now).
- Define the admin wildcard constant `*:*:*`.

No behavioral change; pure addition.

### Phase 1 — Grant admin to every existing caller

Populate `Caller.Scopes` so that *every* current caller holds `*:*:*` **before**
anything requires scopes:

- **Cookie sessions:** `Auth.User` / `attachUserInfo` set `Scopes = {*:*:*}`.
- **App JWTs:** `NewAppClaims` (`jwt.go:26`) adds `Scopes: ["*:*:*"]`; mint at
  `HandleToken`. Old already-issued JWTs are short-lived (`AppTokenTTL = 5m`,
  `jwt.go:23`), so they age out within minutes — but the evaluator must also
  treat **absence of a scopes claim on a user/app/key caller as full access**
  during the transition (a compatibility shim), so even an in-flight tokenless
  caller is unaffected.
- **`xat_` keys:** add a nullable `scopes` column to the key table; backfill all
  existing rows to `*:*:*`; `StoreKeyValidator.ValidateKey` returns it. New keys
  default to `*:*:*` until the UI grows scope selection.

The "absent scopes ⇒ full access for user/app/key callers" shim is the safety
net that makes Phase 1 and Phase 2 independently shippable. Task callers are
*not* covered by the shim — they already carry explicit scopes and always have.

### Phase 2 — Convert enforcement to the evaluator (dual-running)

With everyone holding `*:*:*`, convert checks one surface at a time; each
conversion is a no-op because `*:*:*` authorizes everything:

1. **`AgentFilter` → evaluator.** Reshape `TaskClaims.Scopes` minting
   (`runner/proxy.go:82`) into the grammar (§5 mapping). Replace each
   hand-written check in `filter.go` with a `Target` builder + `Authorize`.
   `filter_test.go` must pass unchanged (it is the behavioral spec). Keep the
   archived guard as a post-scope state check.
2. **API handlers → evaluator.** Add the authorization interceptor for
   request-only targets and per-handler `Target` builders for row-dependent
   ones, alongside the untouched org-scoping store calls. With `*:*:*` granted,
   every existing call still passes.

Both paths now call the same `scope.Set.Authorize`. The two enforcement styles
are unified; the server can enforce task-level scopes itself rather than relying
solely on the runner-side filter.

### Phase 3 — Remove the compatibility shim

Once all callers provably carry explicit scopes (keys backfilled, JWTs/sessions
minting `*:*:*`, task tokens on the grammar), delete the "absent ⇒ full access"
shim. From here, default-deny is real: a caller with no scopes can do nothing.

### Phase 4 (future, out of scope) — Narrow grants

Only now does anyone's *effective* access change: read-only keys (`read:*:*`),
workspace-scoped keys, per-resource grants, UI for choosing scopes at key
creation. This proposal deliberately stops at "everything is on the model and
nobody's access changed."

### Where scopes are stored & minted, per credential

| Credential | Storage | Minted by |
|---|---|---|
| `xat_` key | new `scopes` column on key row | `CreateKey` (`apiserver/key.go:14`); backfilled `*:*:*` in migration |
| App JWT | `scopes` claim in `AppClaims` | `HandleToken` → `NewAppClaims` |
| Cookie session | not persisted; computed per request | `Auth.User` / `attachUserInfo` |
| Task token | `Scopes` claim in `TaskClaims` (exists) | `AgentProxy.TaskToken` from workspace `Scopes` |

## Trade-offs

**Uniformity & flexibility vs. security-critical string logic.** A scope/ABAC
matching engine is more flexible (per-resource, per-instance grants) and uniform
(one evaluator, no caller-type branching) than today's two ad-hoc styles. The
cost is that authorization becomes **string-pattern logic in the trust path** —
exactly the kind of code where an off-by-one in wildcard handling is a privilege
escalation. Mitigations:

- **Exhaustive table tests** for `scope` are mandatory, covering: every
  action/resource/wildcard combination; empty `Preds`; missing target attribute
  (must deny, not match — note the `ok` check in `Matches`); the four §4 worked
  scenarios as regression tests; and the `filter_test.go` cases re-expressed.
- **`AgentFilter` tests are a frozen behavioral spec** — they must pass through
  Phases 2–3 unchanged.
- **Minting discipline.** The grammar has sharp edges. `write:task.id:*` is
  *org-wide admin over tasks*, not "one task" — a wildcard in the wrong position
  silently widens a grant. Defenses: a `ValidScope` validator (the existing one,
  `agentauth/token.go:23`, generalizes) that rejects unknown actions/resources;
  minting helpers (`scope.OwnTask(id)`, `scope.ChildTasks(id, ws, rn)`) so
  call-sites never hand-assemble wildcard strings; and a lint/test that the only
  place `*:*:*` is produced is the migration/admin path.

**One evaluator vs. keeping two styles.** Unifying means the server can finally
enforce task isolation itself instead of depending on the runner-side
`AgentFilter` being in the request path. The downside is a single evaluator is a
single point of failure — countered by the test burden above.

**Grammar richness vs. simplicity.** A flat `action:resource` capability set
(no predicates) would be far simpler but cannot express "own task or child" or
the create conjunction without either (a) per-RPC handler knowledge or (b)
unsafe OR-splitting. The attribute predicates are the minimum needed to move
*all* current rules into the token; we are not adding generality beyond what the
existing rules require.

**ABAC-in-token vs. policy engine (e.g. OPA/Cedar).** A full external policy
engine is more powerful but is a large dependency and a new operational surface
for a system whose entire authz need is "own task, children, org admin, a couple
capabilities." A ~100-line evaluator with exhaustive tests is proportionate;
revisit if grants get genuinely policy-shaped.

## Open Questions

1. **Exact string form of conjunctive scopes.** The draft uses
   `create:task.parent:42,workspace:ws,runner:rn`. Alternatives:
   `create:task{parent=42,workspace=ws,runner=rn}` (clearer grouping, needs a
   real parser) or a structured JSON claim for JWTs with the string form only
   for the DB column. Comma-separated is the simplest to parse but makes
   literal commas in values illegal (not currently an issue — values are ids and
   identifiers).

2. **Resource/verb taxonomy granularity.** Does `write:task.id:123` grant *all*
   write-ish ops on/under that task — links, logs, events, update, cancel,
   archive — or do we need finer resource types (`link`, `log`, `event`)? Today
   `AgentFilter` treats `CreateLink`/`UploadLogs` as "write the task"
   (`req.TaskId == claims.TaskID`), which argues for the coarse reading
   (sub-resources inherit the task's write scope). But a read-only key that may
   read tasks yet not append logs would need the finer split. Proposed default:
   start coarse (`task` covers its sub-resources), introduce `link`/`log`/`event`
   resources only when a use case needs the distinction — the grammar already
   supports adding resource types without changing the evaluator.

3. **Verb set.** Is `{read, write, create}` enough, or do destructive/lifecycle
   ops (`delete`, `cancel`, `archive`, `restart`) deserve their own verbs rather
   than folding into `write`? Finer verbs enable "can restart but not archive"
   but multiply the scopes every admin must hold. Leaning toward folding
   lifecycle into `write` initially.

4. **How admin breadth is expressed across all resources.** `*:*:*` is global
   admin; `*:task.id:*` is task-domain admin. Is a middle tier needed (e.g.
   "admin over tasks and their sub-resources but not keys/orgs")? If sub-resources
   are folded under `task` (Q2), `*:task.id:*` already covers it; if not, admin
   needs an enumerated set, which is the argument for keeping the taxonomy coarse.

5. **Org-membership role vs. scopes.** Org membership has a `role` column
   (`owner`/member, `OrgMember.Role`, set in `StoreUserResolver.Provision`).
   Does role map to a scope set (owner ⇒ `*:*:*`, member ⇒ something narrower),
   or stay an orthogonal concept consulted by org-management RPCs
   (`AddOrgMember`, `DeleteOrg`)? This proposal leaves role as-is (Phase 1 grants
   all members `*:*:*`); reconciling role with scopes is part of Phase 4.
