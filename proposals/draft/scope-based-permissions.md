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

The design has two cleanly separated layers, and keeping them separate is the
single most important property:

- A **generic matching engine** (`internal/auth/scope`) that assigns *no
  meaning* to operation segments or predicate keys. It is a pure pattern matcher
  over `(operation-path, attributes)`.
- An **application layer** (the per-RPC mapping in the handlers/interceptor) that
  owns the entire operation taxonomy — what the segments mean, which attributes
  exist, how a request becomes a `Target`. This is the *only* place domain
  knowledge lives.

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
today (`caller.OrgID`). **A scope never crosses orgs, and an org id is never
encoded in a scope.** Scopes express intra-org capability only.

Concretely: a request is authorized iff *both* hold:

1. **Tenancy:** the target resource belongs to `caller.OrgID` (enforced by the
   existing `...(ctx, nil, ..., caller.OrgID)` store calls — unchanged), and
2. **Capability:** some held scope authorizes the operation on that target (new).

This keeps the org check where it already is and well-tested, and means the
scope grammar never has to encode org IDs. Cross-org access remains impossible
by construction even if a scope is mis-minted.

### 3. The engine is semantically agnostic (design principle)

The matching engine assigns **no meaning** to operation segments or predicate
keys. There is no `resource`/`verb`/`action` concept *inside the engine* —
those are an application convention, not an engine feature.

- **Engine** = a generic pattern-matcher over `(operation-path, attributes)`:
  segment-wise glob on the path, plus attribute-set membership on the
  predicates. It does not know that a segment is a "resource" or that an
  attribute is named `parent`.
- **Application** = owns the operation taxonomy and the per-RPC mapping that
  builds a `Target`. This is the *only* place that knows what the segments mean
  (e.g. that one segment names a resource, that an attribute is named `parent`,
  that creating a task involves `workspace`/`runner`).

Engine form (fully generic, no domain types):

```go
// internal/auth/scope
type Scope struct {
    Op    [][]string          // segment i -> allowed alternatives ("*" is a member for wildcard)
    Preds map[string][]string // attribute key -> set of allowed values ("*" = unconstrained)
}

type Target struct {
    Op    []string          // concrete operation-path segments
    Attrs map[string]string // concrete attributes of the request or loaded row
}

func (s Scope) Matches(t Target) bool
func (set Set) Authorize(t Target) bool
```

Both the path and the predicates support **"one of a set"** at every position —
path segments via `|` alternation (§4), predicate attributes via JSON arrays
(§5). They are two encodings of the same idea only because the path is a flat
string and the predicates are JSON. Each segment of `Op` parses into a set of
allowed alternatives, exactly as a predicate scalar normalizes to a singleton
set; `"*"` is simply a member of that set.

Because the engine is agnostic, the operation taxonomy below
(`task.read`, `task.create`, `github_token.create`, …) is an **illustrative
application convention** chosen for this codebase, not part of the engine. The
proposal uses a `resource.action` segment order to match the examples; the
engine would work identically with any segmentation.

### 4. Wire syntax: dot-path, optional JSON predicate object

A scope string is a **dot-delimited operation path**, then a `:`, then a **JSON
object** of predicates:

```
seg1.seg2.…:{json-predicates}
```

- **Parse by splitting on the first colon.** This is unambiguous even though the
  JSON contains colons: the operation path is colon-free, so everything left of
  the first colon is the path (split on `.` into `Op` segments) and everything
  right of it is the predicate object (`json.Unmarshal`).
- The `:{…}` suffix is **optional**; absent ⇒ empty predicates (`{}`). A
  capability with no instance is just its path, e.g. `github_token.create`.
- **A segment may list `|`-separated alternatives**, e.g. `task.create|update`
  matches when the second segment is `create` **or** `update`. Alternation is
  split into a set at parse time (`Op[i] = ["create","update"]`), so the matcher
  does plain membership and never splits strings in the hot path. `*` is just a
  member of that set (`a|b|*` ≡ `*`).

Worked vocabulary (illustrative `resource.action` convention):

| Scope | Meaning |
|---|---|
| `task.read:{"id":123}` | read the task with id 123 |
| `task.read:{"parent":123}` | read any task whose `parent` is 123 (child access, resolved against the loaded row) |
| `task.write:{"id":123}` | write the task with id 123 |
| `github_token.create` | issue a GitHub token (no instance; predicates `{}`) |
| `task.create:{"parent":123,"workspace":"X","runner":"Y"}` | create a task **iff** parent=123 **and** workspace=X **and** runner=Y |
| `task.create:{"parent":42,"workspace":["X","Y"],"runner":"rn"}` | create a child of 42 in workspace X **or** Y on runner rn |
| `task.read\|write:{"id":123}` | read **or** write task 123 (path alternation) |
| `task.*` | any action on a task instance (task-domain admin) |
| `*.*` | global admin within the org (any 2-segment operation, any instance) |

Predicate values are normalized to **sets of strings** at parse time: a JSON
scalar (`42`, `"X"`, `true`) becomes a singleton set (`["42"]`, `["X"]`,
`["true"]`); a JSON array becomes the set of its stringified elements. The
target's `Attrs` are likewise stringified by the handler when it builds the
`Target`. Keeping everything as strings makes the matcher uniform and total.

Earlier drafts of this proposal used a different grammar
(`action:resource.field:value` with comma-separated conjunctions, and
field/value baked into the path); that form is **dropped entirely** in favor of
the dot-path + JSON-object form above.

### 5. Predicates are set-valued; AND across keys, OR (membership) within a key

Each predicate key constrains one attribute; its value is a **set** of allowed
values (scalars normalized to singletons). A scope matches a target only if, for
**every** key in the scope's object, the target's attribute is a **member** of
that key's allowed set:

- **AND across keys** — every key in the scope must match.
- **Set membership within a single key** — a disjunction over that one
  attribute's allowed values.
- `"*"` (or `["*"]`) as a value, **or an absent key**, ⇒ unconstrained for that
  attribute. An empty object `{}` ⇒ matches any instance of the operation.

It is important that within-key disjunction is **not** OR-across-keys.
OR-across-keys would be *attribute smuggling*: a token could satisfy a
multi-attribute operation by matching only one of its attributes (the CREATE
escalation in §6a). What we have is standard per-attribute set membership, which
stays sound: every constrained attribute must independently hold.

Set-valued predicates add **no expressiveness** over holding several
fully-constrained scopes — both encode the same DNF. They are purely a
**compactness optimization** to avoid combinatorial blow-up in the token: "this
agent may create children in 2 workspaces on 3 runners" is one scope with set
values, not 6 separate scopes (and not a token that bloats as the cross-product
grows).

```go
// alts is the parsed set of alternatives for one segment; "*" is a member for wildcard.
func segMatch(alts []string, seg string) bool {
    return slices.Contains(alts, "*") || slices.Contains(alts, seg)
}

func (s Scope) Matches(t Target) bool {
    if len(s.Op) != len(t.Op) { // each segment matches exactly one; lengths must agree
        return false
    }
    for i := range s.Op {
        if !segMatch(s.Op[i], t.Op[i]) {
            return false
        }
    }
    for key, allowed := range s.Preds { // AND across keys
        if slices.Contains(allowed, "*") {
            continue // unconstrained attribute
        }
        got, ok := t.Attrs[key]
        if !ok || !slices.Contains(allowed, got) { // membership within the key (a disjunction)
            return false
        }
    }
    return true
}

func (set Set) Authorize(t Target) bool {
    for _, s := range set { // OR across the caller's held scopes
        if s.Matches(t) {
            return true
        }
    }
    return false
}
```

#### Wildcards on the operation path

- `*` matches **exactly one** segment — no greedy tail. So `task.*` matches
  `task.read` and `task.write` but not `task` or `task.a.b`, and `*.*` matches
  exactly the 2-segment operations.
- `**` is **reserved and unspecified** for now. It is the future escape hatch if
  we ever need subtree (depth-independent) matching; this proposal does not spec
  its semantics.
- Because attribute values live in the predicate object (not in the path),
  operation paths stay **shallow**. Admin and migration grants are therefore
  expressed at the application's chosen operation arity (here, `*.*` for the
  2-segment taxonomy). Genuinely depth-independent admin is the `**` future-work
  case, not something we need today.

#### Alternation on the operation path (`|`)

A path segment may list `|`-separated alternatives. It is the path-side mirror of
array-valued predicates: **every position supports "one of a set"** — path
segments via `|`, predicate attributes via JSON arrays. The two encodings exist
only because the path is a flat string and predicates are JSON.

- **Split at parse time, not during evaluation.** `task.create|update` parses to
  `Op = [["task"], ["create","update"]]`; the matcher does plain membership
  (`segMatch` above) and never calls `strings.Split` in the hot path. `*` is
  simply a member of the alternative set, identical to how `"*"` works in a
  predicate value (`a|b|*` ≡ `*`).
- **Sound for the same reason set-valued predicates are.** `|` is
  **within-position** alternation (one segment, one of N) — never cross-position
  — so the positional AND across segments is preserved. There is no
  segment/attribute smuggling, exactly as within-key membership is not
  OR-across-keys (§5 intro).
- **No new expressiveness.** `task.create|update:{}` is equivalent to holding
  `task.create:{}` plus `task.update:{}` — the same DNF. Like array predicates,
  it is purely a compactness optimization, here for the path (e.g. "read or
  write this task" is one scope, not two).
- **`|` is path-only.** Predicate values already express "one of a set" via JSON
  arrays; adding `|` there would be a redundant second encoding *and* would
  collide with user-controlled strings (a workspace or runner literally named
  `a|b`). Path segments are app-defined operation constants, so `|` is
  delimiter-safe there but not in predicate values.

### 6. Evaluation semantics: AND within a scope, OR across scopes

Framed as DNF: **each scope is one conjunctive clause** (segment matches AND
per-key memberships), and **the held set is the disjunction** of those clauses.
`Authorize` is true iff some clause is satisfied. This single rule has to
satisfy four requirements simultaneously; each is worked through below with the
failure mode that motivates the AND-within / OR-across split.

#### (a) CREATE needs a conjunction — kept *inside one scope*, minter fully constrains

`CreateTask` today requires **four** things at once (`filter.go:83-91`):
`child_tasks` present, `parent == claims.TaskID`, `workspace == claims.Workspace`,
`runner == claims.Runner`. In the new model the agent token holds a single,
**fully-constrained** scope:

```
task.create:{"parent":42,"workspace":"ws","runner":"rn"}
```

and the `CreateTask` handler builds the target from the *request*:

```
Target{Op:["task","create"],
       Attrs:{"parent":"<req.Parent>", "workspace":"<req.Workspace>", "runner":"<req.Runner>"}}
```

`Matches` returns true only if **all three** keys hold — exactly the current
behavior. The "allow N workspaces" case uses a set value rather than multiple
scopes:

```
task.create:{"parent":42,"workspace":["X","Y"],"runner":"rn"}
```

**The minter is responsible for emitting a fully-constrained predicate object.**
It must enumerate every access-relevant attribute of the create operation. An
absent key is *unconstrained* — i.e. a hole — so completeness is mandatory at
mint time. We explicitly do **not** have the server derive missing attributes
(e.g. defaulting `workspace`/`runner` from the caller's task): the server-derive
approach would put "which attributes must be constrained" knowledge back into
the handler, which is exactly what §6d forbids. Completeness lives in the minter.

**Failure mode if we instead split the conjunction into separate scopes** —
`["task.create:{\"parent\":42}", "task.create:{\"workspace\":\"ws\"}", "task.create:{\"runner\":\"rn\"}"]`
— and OR across them: a `CreateTask` target matches
`task.create:{"parent":42}` on its own, because that scope leaves `workspace`
and `runner` unconstrained (absent keys). A caller could then create a task with
`parent=42` but an **arbitrary workspace/runner**, escalating out of its
sandbox. The conjunction *must* live inside a single scope's object so that
ORing across the caller's scopes can never relax it. (This is the same hazard as
OR-across-keys in §5, viewed from the scope-set level.)

#### (b) Relationship / child access is naturally disjunctive — *across scopes*

`GetTask` today allows own task **or** a direct child (with `child_tasks`)
(`filter.go:118-127`). That is a genuine OR, and it maps to two scopes:

```
task.read:{"id":42}        # own task
task.read:{"parent":42}    # any direct child of 42
```

`GetTask` loads the row (it already does — `p.client.GetTask`), then builds:

```
Target{Op:["task","read"], Attrs:{"id":"<row.Id>", "parent":"<row.Parent>"}}
```

- Own task (`row.Id==42`): matches `task.read:{"id":42}` (the `parent` key is
  absent there ⇒ unconstrained). ✅
- Child (`row.Parent==42`): matches `task.read:{"parent":42}` (the `id` key
  absent ⇒ unconstrained). ✅
- Unrelated task: matches neither → denied. ✅

OR-across-scopes is exactly right here. The `parent` predicate is **resolved
against the loaded row at request time**, which is how relationship access works
without the engine needing a graph: the handler fetches the row (scoped to the
caller's org), then asks the engine about the row's attributes.

**Failure mode if we instead require AND across all held scopes:** a caller
holding both `task.read:{"id":42}` and `task.read:{"parent":42}` could read
*nothing* — its own task fails the `parent:42` scope, and a child fails the
`id:42` scope, so no single target satisfies both. Pure-AND-across-scopes breaks
the moment a caller holds more than one grant. Scopes must be **additive** (OR),
which is the standard capability-model semantics.

#### (c) Wildcard admin subsumes everything narrower

`task.*` must cover own task **and** children, because a child is still a task
instance:

- Child target `Attrs:{"id":"99","parent":"42"}` vs scope `task.*` (predicates
  `{}`): op `task.*` matches `task.read`/`task.write`✓, empty predicates match
  any instance✓ → allowed.

And `*.*` matches any 2-segment operation with any instance. Because matching is
OR-across-scopes, holding a broad scope **plus** narrow ones is always ≥ the
narrow ones alone — admin can never be *less* than a sub-grant. This is the
property that lets Migration grant `*.*` to existing callers and preserve
today's omnipotence exactly.

#### (d) Handlers must not need per-RPC "which attributes are required" knowledge

The danger is that the engine pushes model knowledge back into handlers. The
agnostic-engine split (§3) prevents it:

- The **handler** only describes *what the request is*: it emits a `Target` with
  the operation path and the concrete attributes of the request (for create) or
  the loaded row (for read/write). It does **not** know which predicates a scope
  "should" constrain.
- The **scope** declares which keys *it* constrains; the engine ANDs exactly
  those and treats absent keys as unconstrained.

So `CreateTask` always emits `Attrs:{parent, workspace, runner}` regardless of
what scopes exist. A token scoped `task.create:{"parent":42,"workspace":"ws","runner":"rn"}`
constrains all three; a hypothetical `task.create:{"parent":42}` would constrain
only `parent` — **the handler code is identical either way.** The "which
attributes matter" decision lives entirely in the minted scope object, not in
the handler. Combined with §6a's rule that the minter must fully constrain, this
means correctness is the minter's responsibility and the handler stays
domain-light.

The failure mode we are avoiding is the "separate scopes" / "server-derive"
design: there, to be safe, the *handler* would have to know "for create, the
caller must hold a parent-scope AND a workspace-scope AND a runner-scope" (or
derive the missing ones) and check three things — leaking the constraint model
into every create handler. The conjunctive single-scope object keeps that
knowledge in the token.

### 7. Default-deny + RPC → required-permission mapping

Authorization becomes a **single evaluator call** plus, for relationship/state
checks, a thin per-handler `Target` builder. This per-RPC mapping **is the
application taxonomy** — the only place where operation segments and attribute
names have meaning. Default is **deny**: an RPC with no mapping, or a target no
held scope matches, is rejected with `connect.CodePermissionDenied`. (This
default-deny on *unmapped* RPCs is what the migration's RPC-side shim
temporarily relaxes for non-task callers — see Migration; the end state is
default-deny for everyone.)

The table below maps only the 7 RPCs that `AgentFilter` covers today. The full
service (`proto/xagent/v1/xagent.proto`) has ~50 RPCs — org admin, key
management, routing rules, workspace registration, events, and so on. Mapping
*all* of them is the bulk of the Phase 2 work; until an RPC has a `Target`
builder it is "unmapped," which the migration handles explicitly rather than
silently denying.

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
| `CreateLink`, `UploadLogs` | `Op:[task,write] Attrs:{id:req.TaskId}` | `req.TaskId == claims.TaskID` via `task.write:{"id":42}` |
| `SubmitRunnerEvents` | per-event `Op:[task,write] Attrs:{id:ev.TaskId}`, **all** must pass | all-or-nothing batch (`filter.go:67`) |
| `GetTask`, `GetTaskDetails`, `ListLogs` | `Op:[task,read] Attrs:{id:row.id, parent:row.parent}` | own (`task.read:{"id":42}`) OR child (`task.read:{"parent":42}`); child scope absent ⇒ child denied |
| `UpdateTask` | `Op:[task,write] Attrs:{id:row.id, parent:row.parent}` | own/child as above + archived guard (§ below) |
| `CreateTask` | `Op:[task,create] Attrs:{parent:req.Parent, workspace:req.Workspace, runner:req.Runner}` | four-way conjunction via one fully-constrained scope |
| `ListChildTasks` | `Op:[task,read] Attrs:{parent:req.ParentId}` | parent match + `child_tasks` |
| `CreateGitHubToken` | `Op:[github_token,create] Attrs:{}` | `github_token` scope |

The old task scopes therefore map to:

- `child_tasks` ⇒ the token gains the child-relationship scopes
  `task.read:{"parent":<self>}`, `task.write:{"parent":<self>}`, and
  `task.create:{"parent":<self>,"workspace":<ws>,"runner":<rn>}`.
- `github_token` ⇒ `github_token.create`.
- The always-present own-task access ⇒ `task.read:{"id":<self>}`,
  `task.write:{"id":<self>}`.

Re-expressing the **current org-scoping** (the API callers): users, sessions,
and `xat_` keys are omnipotent within their org today. In the model that is
`*.*` (see Migration). The org tenancy check is untouched; `*.*` only satisfies
the *capability* half, so behavior is identical. Narrowing those grants later
(e.g. a read-only key = `*.read`) is future work, not this proposal.

Because both the agent path and the API path now compute a `Target` and call the
same `scope.Set.Authorize`, the two enforcement styles collapse into one.
`AgentFilter`'s hand-written per-RPC checks are replaced by target builders that
feed the shared engine; the server can additionally run the same evaluator, so
task isolation is no longer dependent on the runner-side filter being in the
path.

### State-based checks coexist as request-time guards

Some rules are not about *identity/capability* but about *resource state* and
cannot live in a scope. The clearest is `UpdateTask`'s "cannot update archived
task" (`filter.go:166`): whether a task is archived is mutable runtime state,
not a property a token can be scoped to. These remain ordinary request-time
guards, evaluated **after** scope authorization passes:

```go
// 1. capability: scope evaluator authorizes Op:[task,write] for the row (own or parent)
// 2. state guard: if row.Archived { return PermissionDenied("cannot update archived task") }
```

Scope evaluation answers "may this caller, in principle, write this task?"; the
state guard answers "is the task currently in a writable state?" They are
orthogonal and both must pass. Keeping state guards out of the grammar keeps the
engine pure (a function of the token and the target's *identity* attributes, not
its lifecycle), which is what makes it exhaustively testable.

## Migration

This is the crux: **migrate everything we have right now onto the model without
changing anyone's effective access.** Existing user sessions and `xat_` keys
carry no scopes and are omnipotent within their org; the moment a handler
*requires* a scope, they break unless they already hold an admin scope. So the
order is strict: introduce the machinery, grant admin to everyone, *then*
convert checks. Narrowing real permissions is a separate, later effort.

### Phase 0 — Define the model, no enforcement

- Add `internal/auth/scope` (`Scope`, `Set`, `Target`, the first-colon parser,
  `Matches`/`Authorize`) with exhaustive table tests. No caller consults it yet.
- Add `Scopes scope.Set` to `apiauth.UserInfo` (unparsed/empty for now).
- Define the admin wildcard constant `*.*`.

No behavioral change; pure addition.

### Phase 1 — Grant admin to every existing caller

Populate `Caller.Scopes` so that *every* current caller holds `*.*` **before**
anything requires scopes:

- **Cookie sessions:** `Auth.User` / `attachUserInfo` set `Scopes = {*.*}`.
- **App JWTs:** `NewAppClaims` (`jwt.go:26`) adds `Scopes: ["*.*"]`; mint at
  `HandleToken`. Old already-issued JWTs are short-lived (`AppTokenTTL = 5m`,
  `jwt.go:23`), so they age out within minutes — but the evaluator must also
  treat **absence of a scopes claim on a user/app/key caller as full access**
  during the transition (a compatibility shim), so even an in-flight tokenless
  caller is unaffected.
- **`xat_` keys:** add a nullable `scopes` column to the key table; backfill all
  existing rows to `*.*`; `StoreKeyValidator.ValidateKey` returns it. New keys
  default to `*.*` until the UI grows scope selection.

This **caller-side shim** ("absent scopes ⇒ full access for user/app/key
callers") is one of two safety nets that keep Phase 2 a true no-op. Task callers
are *not* covered by it — they already carry explicit scopes and always have. On
its own, though, the caller-side shim is **not sufficient**: it relaxes the
*scope* check, but a caller holding `*.*` still gets denied by an RPC that has no
`Target` builder yet (default-deny on unmapped RPCs, §7). That gap is closed by
the RPC-side shim in Phase 2.

### Phase 2 — Convert enforcement to the evaluator (dual-running)

The interceptor is installed with a **symmetric RPC-side shim** that mirrors the
caller-side one, so that Phase 2 is a true no-op while RPCs are mapped
incrementally rather than in a single flag-day:

> During Phase 2, an **unmapped** RPC (no `Target` builder) ⇒ **allow for
> non-task callers**. Task callers stay default-deny for unmapped RPCs — they
> were always an explicit allowlist (`AgentFilter` only ever implemented a fixed
> set), so "unmapped ⇒ deny" is already their status quo and must not be
> loosened.

With both shims in place, every existing call still passes: a user/key/app
caller that hits a not-yet-mapped RPC is allowed by the RPC-side shim, and one
that hits a mapped RPC is allowed by the caller-side shim (it holds `*.*`, or no
scopes ⇒ full access). Convert one surface at a time underneath:

1. **`AgentFilter` → evaluator.** Reshape `TaskClaims.Scopes` minting
   (`runner/proxy.go:82`) into the grammar (§7 mapping), with the minter fully
   constraining create scopes. Replace each hand-written check in `filter.go`
   with a `Target` builder + `Authorize`. `filter_test.go` must pass unchanged
   (it is the behavioral spec). Keep the archived guard as a post-scope state
   check. (The agent path was already a closed allowlist, so it is mapped in one
   go — the RPC-side shim never applies to task callers.)
2. **API handlers → evaluator.** Map the full service: add the authorization
   interceptor (request-only targets) and per-handler `Target` builders
   (row-dependent ones), alongside the untouched org-scoping store calls. Each
   RPC that gains a mapping stops relying on the RPC-side shim and starts being
   evaluated for real — still a no-op because callers hold `*.*`. The shim is
   what lets this happen RPC-by-RPC across multiple PRs instead of all at once.

Both paths now call the same `scope.Set.Authorize`. The two enforcement styles
are unified; the server can enforce task-level scopes itself rather than relying
solely on the runner-side filter.

### Phase 3 — Remove both compatibility shims

Once all callers provably carry explicit scopes (keys backfilled, JWTs/sessions
minting `*.*`, task tokens on the grammar) **and** the whole service is mapped
(no RPC lacks a `Target` builder), delete **both** shims together:

- the **caller-side** shim ("absent scopes ⇒ full access"), and
- the **RPC-side** shim ("unmapped ⇒ allow non-task callers").

They must come out together: removing only one re-introduces the gap (an
unmapped RPC would deny a `*.*` holder, or a scopeless caller would be denied on
a mapped RPC). After this, default-deny is real for everyone — a caller with no
scopes, or a request to an RPC with no mapping, can do nothing.

**Alternative considered — flag-day mapping.** Instead of the RPC-side shim,
Phase 2 could map the *entire* service before enabling the interceptor at all.
That removes one transient shim but couples "turn on enforcement" to "every one
of ~50 RPCs is mapped and reviewed" in a single change — a large, risky PR with
no incremental fallback. The symmetric shim trades a short-lived, clearly-scoped
allowance (unmapped ⇒ allow non-task callers, removed in Phase 3) for the
ability to land mappings PR-by-PR. We take the shim.

### Phase 4 (future, out of scope) — Narrow grants

Only now does anyone's *effective* access change: read-only keys (`*.read`),
workspace-scoped keys, per-resource grants, UI for choosing scopes at key
creation. This proposal deliberately stops at "everything is on the model and
nobody's access changed."

### Where scopes are stored & minted, per credential

| Credential | Storage | Minted by |
|---|---|---|
| `xat_` key | new `scopes` column on key row | `CreateKey` (`apiserver/key.go:14`); backfilled `*.*` in migration |
| App JWT | `scopes` claim in `AppClaims` | `HandleToken` → `NewAppClaims` |
| Cookie session | not persisted; computed per request | `Auth.User` / `attachUserInfo` |
| Task token | `Scopes` claim in `TaskClaims` (exists) | `AgentProxy.TaskToken` from workspace `Scopes` (minter fully constrains) |

## Trade-offs

**Uniformity & flexibility vs. security-critical string logic.** A generic
matching engine is more flexible (per-resource, per-instance, set-valued grants)
and uniform (one evaluator, no caller-type branching) than today's two ad-hoc
styles. The cost is that authorization becomes **pattern logic in the trust
path** — exactly the kind of code where an off-by-one in wildcard or membership
handling is a privilege escalation. Mitigations:

- **Exhaustive table tests** for `scope` are mandatory, covering: segment-count
  mismatch; `*` single-segment matching (and that it does *not* span segments);
  `|` alternation (membership, `a|b|*` ≡ `*`, rejection of empty alternatives);
  empty `Preds` matching any instance; absent key vs `"*"` value (both
  unconstrained); scalar→singleton and array normalization; missing target
  attribute (must deny, not match — the `ok` check in `Matches`); first-colon
  split with colons inside the JSON; the four §6 worked scenarios as regression
  tests; and the `filter_test.go` cases re-expressed.
- **`AgentFilter` tests are a frozen behavioral spec** — they must pass through
  Phases 2–3 unchanged.
- **Minting discipline.** The grammar has sharp edges. `task.write:{"id":"*"}`
  (or `task.write` with empty predicates) is *org-wide admin over tasks*, not
  "one task" — a wildcard value, or a forgotten predicate key, silently widens a
  grant. Because an absent key is unconstrained, an **incomplete create object
  is a hole**, not a deny. Defenses: a `ValidScope` validator (the existing one,
  `agentauth/token.go:23`, generalizes) that rejects unknown operation paths and
  malformed alternation (empty alternatives like `create|` or `|update`);
  minting helpers (`scope.OwnTask(id)`, `scope.ChildTasks(id, ws, rn)`) so
  call-sites never hand-assemble scope strings or JSON; and a lint/test that the
  only place `*.*` (or any empty-predicate wildcard) is produced is the
  migration/admin path.

**Generic engine + application mapping vs. a domain-typed evaluator.** Making the
engine semantically agnostic (no resource/verb concept) keeps all domain
knowledge in one auditable place (the per-RPC mapping) and lets the engine be
tested as pure string logic. The cost is a small indirection: reading a scope
string in isolation doesn't tell you what `task.read` *means* without the
mapping. We accept this; the mapping is the spec.

**One evaluator vs. keeping two styles.** Unifying means the server can finally
enforce task isolation itself instead of depending on the runner-side
`AgentFilter` being in the request path. The downside is a single evaluator is a
single point of failure — countered by the test burden above.

**Set-valued predicates vs. multiple scopes.** Set values add no expressiveness
(same DNF as multiple fully-constrained scopes); they exist only to stop tokens
bloating with the cross-product of allowed workspaces × runners. The cost is one
more thing the matcher and minter must get right (membership, normalization),
justified by keeping JWTs small.

**ABAC-in-token vs. policy engine (e.g. OPA/Cedar).** A full external policy
engine is more powerful but is a large dependency and a new operational surface
for a system whose entire authz need is "own task, children, org admin, a couple
capabilities." A small generic engine with exhaustive tests is proportionate;
revisit if grants get genuinely policy-shaped.

## Open Questions

1. **Operation-path convention.** The engine is agnostic, so the segment order
   and arity are the application's choice. This proposal uses `resource.action`
   (`task.read`, `github_token.create`) at arity 2, which makes `*.*` the admin
   grant. `action.resource` would work identically. Worth fixing the convention
   (and documenting it next to the per-RPC mapping) before implementation so all
   minters agree.

2. **Resource/verb taxonomy granularity.** Does `task.write:{"id":123}` grant
   *all* write-ish ops on/under that task — links, logs, events, update, cancel,
   archive — or do we need finer resource segments (`link`, `log`, `event`)?
   Today `AgentFilter` treats `CreateLink`/`UploadLogs` as "write the task"
   (`req.TaskId == claims.TaskID`), which argues for the coarse reading
   (sub-resources inherit the task's write op). But a read-only key that may
   read tasks yet not append logs would need the finer split. Proposed default:
   start coarse (`task` covers its sub-resources), introduce `link`/`log`/`event`
   resources only when a use case needs the distinction — the agnostic engine
   already supports adding operation segments without any engine change.

3. **Verb set.** Is `{read, write, create}` enough, or do destructive/lifecycle
   ops (`delete`, `cancel`, `archive`, `restart`) deserve their own action
   segments rather than folding into `write`? Finer verbs enable "can restart but
   not archive" but multiply the scopes every admin must hold. Leaning toward
   folding lifecycle into `write` initially.

4. **Depth-independent admin (`**`).** With a fixed 2-segment taxonomy, `*.*`
   covers everything and no subtree wildcard is needed. If the taxonomy ever
   grows variable-depth operation paths, admin grants would have to enumerate
   each arity (`*.*`, `*.*.*`, …) unless we specify `**`. `**` is reserved now;
   specifying it is deferred until a variable-depth taxonomy actually exists.

5. **Org-membership role vs. scopes.** Org membership has a `role` column
   (`owner`/member, `OrgMember.Role`, set in `StoreUserResolver.Provision`).
   Does role map to a scope set (owner ⇒ `*.*`, member ⇒ something narrower), or
   stay an orthogonal concept consulted by org-management RPCs (`AddOrgMember`,
   `DeleteOrg`)? This proposal leaves role as-is (Phase 1 grants all members
   `*.*`); reconciling role with scopes is part of Phase 4.
