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

### Engine as built (PR #902) — reconciling §3–§6 with what shipped

§3–§6 above describe the *target* design. The engine that actually landed
(`internal/auth/authscope`, PR #902) is a deliberately smaller subset, and the
rest of this proposal is written against it, not against the earlier prose:

- The package is `internal/auth/authscope` (not `internal/auth/scope`), the
  caller's held set is `authscope.Scopes` (not `scope.Set`), and
  `apiauth.UserInfo.Scopes authscope.Scopes` **already exists** (added in #902).
- The evaluator is `func (scopes authscope.Scopes) Allow(op []string, attrs ...authscope.Attr) bool`.
  There is **no** `Target`/`Authorize`/`Set` type — the handler *is* the target
  builder: it passes the operation path and the request/row attributes straight
  to `Allow`.
- Operation paths are exported `[]string` vars (`authscope.OpTaskRead`,
  `OpTaskWrite`, `OpTaskCreate`, `OpGitHubTokenCreate` in `task.go`). A `Scope`'s
  `Op` is `[]string`; `"*"` matches any one segment. `AdminScope = "*.*"` and
  `authscope.Admin()` return the two-segment wildcard.
- Attributes are namespaced typed constructors (`authscope.WithTaskID`,
  `WithTaskParent`, `WithTaskWorkspace`, `WithTaskRunner`), built on
  `Int64Attr`/`StringAttr`. Keys are `"task.id"`, `"task.parent"`, etc.
- Scopes are built with `authscope.New(op, attrs...)` and **self-serialize**:
  `authscope.Scopes` marshals to/from a JSON `[]string` of wire-grammar strings,
  so a token's `scopes` claim is unchanged on the wire.
- **Predicates are single-valued** (`map[string]string`); `Parse` rejects JSON
  numbers, booleans, and arrays. The **set-valued predicates and `|` path
  alternation** discussed in §4–§6 are *not implemented* (a `parse.go` comment
  flags set values as "can be added later"). Where §6a says "allow N workspaces
  uses a set value," the shipped engine instead needs **N fully-constrained
  scopes** (one per workspace) — a token-size cost, not a correctness one. The
  mapping below uses only single-valued predicates.
- **Workspace capability flags** (`agentauth.CapabilityChildTasks`,
  `CapabilityGitHubToken`) are *not* grammar scopes. They are the workspace-level
  inputs the runner's minter, `agentauth.Scopes(ScopeOptions{...})`, turns into
  the task token's scopes. Keep this distinction: capabilities are config; scopes
  are the evaluated grant.

The agent-caller half of this proposal is **already enforced**:
`internal/agentmcp.AgentFilter` checks every agent RPC with
`scopes.Allow(...)`, loading the row first when an attribute (the task's
`parent`) comes from the stored entity (`GetTask`, `UpdateTask`, the child-leg
of `ListLogs`). The remainder of this document specifies the *API-caller* half —
the full `XAgentService` as called by users, `xat_` keys, app JWTs, and cookie
sessions — which today has **no** capability check at all (only org scoping).

### 7. RPC → required-permission mapping (full `XAgentService`)

This per-RPC mapping **is the application taxonomy** — the only place operation
segments and attribute names carry meaning. Authorization for an API caller is
two independent gates, both of which must pass:

1. **Tenancy (unchanged):** the target belongs to `caller.OrgID`, enforced by the
   existing `s.store.…(ctx, nil, …, caller.OrgID)` calls. Cross-org access is
   impossible regardless of scope.
2. **Capability (new):** `caller.Scopes.Allow(op, attrs…)` returns true.

Each RPC is one of two shapes, exactly as on the agent side:

- **Request-only** — every attribute is in the request (or the op is a bare
  capability). Checkable *before* the handler runs.
- **Row-dependent** — a predicate references a stored attribute (a task's
  `parent`, an org's `owner`). The row must be loaded org-scoped *first*, then
  `Allow` is called — mirroring `AgentFilter.GetTask`/`UpdateTask`.

#### New `Op*` paths and attribute keys the taxonomy needs

Beyond the task-caller set already in `task.go`, the full service needs (names
illustrative; same `resource.action` convention):

```go
// operation paths (add alongside OpTask*/OpGitHubTokenCreate)
OpEventRead      = []string{"event", "read"}
OpEventWrite     = []string{"event", "write"}   // delete + add/remove task fold into write
OpEventCreate    = []string{"event", "create"}
OpWorkspaceRead  = []string{"workspace", "read"}
OpWorkspaceWrite = []string{"workspace", "write"} // register + clear
OpKeyRead        = []string{"key", "read"}
OpKeyCreate      = []string{"key", "create"}
OpKeyWrite       = []string{"key", "write"}     // delete folds into write
OpOrgRead        = []string{"org", "read"}      // settings, members, routing-rule reads
OpOrgWrite       = []string{"org", "write"}     // members, settings, routing-rules, GH-installation link
OpOrgCreate      = []string{"org", "create"}
OpOrgDelete      = []string{"org", "delete"}
OpAccountWrite   = []string{"account", "write"} // unlink GitHub/Atlassian — user-identity axis

// attribute keys (add alongside AttrTask*)
AttrEventID         = "event.id"
AttrWorkspaceRunner = "workspace.runner"
AttrKeyID           = "key.id"
```

Lifecycle and sub-resource verbs are folded coarsely (Open Questions #2/#3):
`Archive`/`Unarchive`/`Cancel`/`Restart` are `task.write`; links and logs inherit
their task's op; `routing_rule`/settings reads and writes are `org.read`/
`org.write`. Splitting any of these out later needs no engine change — just new
`Op*` vars.

#### The full table

| RPC | Operation | Attributes | Shape | Notes |
|---|---|---|---|---|
| `Ping` | — | — | exempt | no caller data; interceptor allowlists it |
| `GetProfile` | — | — | exempt | the caller's own identity/orgs; gated by *authenticated user*, not an org scope (see "identity axis" below) |
| `ListTasks` | `task.read` | — | request-only | full-org list ⇒ needs an unconstrained `task.read` (`*.*` qualifies); a `{task.id:N}`-only scope does **not** authorize a list |
| `ListRunnerTasks` | `task.read` | — | request-only | filtered by `req.Runner` but returns tasks; treat as org read |
| `ListChildTasks` | `task.read` | `task.parent=req.ParentId` | request-only | identical to `AgentFilter.ListChildTasks` |
| `CreateTask` | `task.create` | `task.parent=req.Parent`, `task.workspace=req.Workspace`, `task.runner=req.Runner` | request-only | `req.Parent==0` for a top-level task; minter must fully constrain (§6a) |
| `GetTask` | `task.read` | `task.id=row.Id`, `task.parent=row.Parent` | **row-load** | own (`{id}`) OR child (`{parent}`) |
| `GetTaskDetails` | `task.read` | `task.id=row.Id`, `task.parent=row.Parent` | **row-load** | as `GetTask` |
| `UpdateTask` | `task.write` | `task.id=row.Id`, `task.parent=row.Parent` | **row-load** | + archived state guard (below) |
| `ArchiveTask` | `task.write` | `task.id=row.Id`, `task.parent=row.Parent` | **row-load** | lifecycle ⇒ write |
| `UnarchiveTask` | `task.write` | `task.id=row.Id`, `task.parent=row.Parent` | **row-load** | |
| `CancelTask` | `task.write` | `task.id=row.Id`, `task.parent=row.Parent` | **row-load** | |
| `RestartTask` | `task.write` | `task.id=row.Id`, `task.parent=row.Parent` | **row-load** | |
| `UploadLogs` | `task.write` | `task.id=req.TaskId` | request-only | as `AgentFilter` |
| `ListLogs` | `task.read` | `task.id=req.TaskId`, then `{id,parent}` of row | **hybrid** | own-task fast path by id, child leg loads row — copy `AgentFilter.ListLogs` |
| `CreateLink` | `task.write` | `task.id=req.TaskId` | request-only | as `AgentFilter` |
| `ListLinks` | `task.read` | `task.id=req.TaskId` | request-only | id-only; child-scoped read would need a row-load (note) |
| `ListEvents` | `event.read` | — | request-only | full-org list |
| `CreateEvent` | `event.create` | — | request-only | |
| `GetEvent` | `event.read` | `event.id=req.Id` | request-only | |
| `DeleteEvent` | `event.write` | `event.id=req.Id` | request-only | |
| `AddEventTask` | `event.write` **and** `task.write` | `event.id=req.EventId`; `task.id=req.TaskId` | request-only | **two** `Allow` calls, both must pass (mirrors the dual `HasEvent`/`HasTask`) |
| `RemoveEventTask` | `event.write` **and** `task.write` | `event.id=req.EventId`; `task.id=req.TaskId` | request-only | as above |
| `ListEventTasks` | `event.read` | `event.id=req.EventId` | request-only | |
| `ListEventsByTask` | `task.read` | `task.id=req.TaskId` | request-only | keyed on the task |
| `SubmitRunnerEvents` | `task.write` | per-event `task.id=ev.TaskId` | request-only | all-or-nothing batch, exactly as `AgentFilter` |
| `RegisterWorkspaces` | `workspace.write` | `workspace.runner=req.RunnerId` | request-only | runner-facing |
| `ListWorkspaces` | `workspace.read` | — | request-only | |
| `ClearWorkspaces` | `workspace.write` | `workspace.runner=req.RunnerId` (omit when empty) | request-only | empty `req.RunnerId` ⇒ org-wide clear ⇒ no instance attr ⇒ needs unconstrained `workspace.write` |
| `CreateKey` | `key.create` | — | request-only | |
| `ListKeys` | `key.read` | — | request-only | |
| `DeleteKey` | `key.write` | `key.id=req.Id` | request-only | `key.id` is a string UUID |
| `UnlinkGitHubAccount` | `account.write` | — | request-only | identity axis (caller's own user) |
| `UnlinkAtlassianAccount` | `account.write` | — | request-only | identity axis |
| `LinkGitHubInstallation` | `org.write` | — | **row-load** | mutates org settings; the existing "same GitHub user started the install" check stays as an identity guard |
| `CreateOrg` | `org.create` | — | request-only | org axis (no current-org instance — see below) |
| `ListOrgs` | `org.read` | — | request-only | cross-org by membership — org axis |
| `DeleteOrg` | `org.delete` | — | **row-load** | + owner role guard; `req.Id` is an org id (tenancy axis), never a scope predicate |
| `AddOrgMember` | `org.write` | — | **row-load** | + owner role guard |
| `RemoveOrgMember` | `org.write` | — | **row-load** | + owner role guard |
| `ListOrgMembers` | `org.read` | — | request-only | |
| `GetOrgSettings` | `org.read` | — | request-only | |
| `GenerateAtlassianWebhookSecret` | `org.write` | — | request-only | |
| `GetRoutingRules` | `org.read` | — | request-only | folded into `org` (coarse) |
| `SetRoutingRules` | `org.write` | — | request-only | folded into `org` (coarse) |
| `CreateGitHubToken` | `github_token.create` | — | request-only | server returns `Unimplemented`; only the runner proxy / `AgentFilter` serves it |

#### The identity / org axis is not "within-org"

A handful of RPCs are **not** scoped within a single org and so sit awkwardly
against the "scopes are intra-org capability" rule (§2):

- **Identity-axis** (`GetProfile`, `UnlinkGitHubAccount`,
  `UnlinkAtlassianAccount`) act on the *caller's own user*, not on org-owned
  rows. These are governed by *being an authenticated user* (the existing
  `RequireUserInterceptor`); the `account.write` op is a thin capability for the
  unlinks, with no instance and no org predicate. `GetProfile` carries no scope.
- **Org-axis** (`CreateOrg`, `ListOrgs`, `DeleteOrg`, member add/remove) act on
  the org-tenancy axis itself. `CreateOrg` has no current-org context; `ListOrgs`
  is intentionally cross-org (by membership); `DeleteOrg` takes an *org id* in the
  request, and since an org id is the tenancy axis it is **never** encoded as a
  scope predicate. The real gate on these is the **org `role`** owner-check
  already in the handlers (`org.Owner != caller.ID`), which we keep as an
  orthogonal guard (below). The `org.*` ops exist mainly so an admin grant covers
  them and so Phase 3 can later narrow non-owners.

Because these escape the within-org scope model, the proposal treats them as
explicitly-tagged exemptions/coarse-capabilities rather than forcing an org id
into a predicate. Reconciling org `role` with scopes is Open Question #5.

#### Role guards and state guards coexist with scope checks

Two existing kinds of check are **not** capability questions and stay as
request-time guards evaluated *after* the scope check passes — exactly as
`AgentFilter.UpdateTask` keeps the archived guard after its `Allow`:

- **State guards** — `UpdateTask`'s `if row.Archived { … }`: mutable runtime
  state, not a token-scopable property.
- **Role guards** — the org-management owner check (`org.Owner != caller.ID` in
  `DeleteOrg`/`AddOrgMember`/`RemoveOrgMember`) and `LinkGitHubInstallation`'s
  "same GitHub user" check: these are identity/role facts resolved against loaded
  rows. They remain where they are; scopes do not subsume them in this proposal.

Keeping state and role out of the grammar keeps `authscope` a pure function of
(scopes, op, attributes) and exhaustively testable.

### 8. Enforcement mechanism: interceptor vs. per-handler

Two mechanisms are available, and the right answer is to use **both**, split by
RPC shape:

**A central Connect interceptor** holds a static table from RPC procedure
(`req.Spec().Procedure`) to `(op []string, extractor func(req) []authscope.Attr)`
and, before the handler runs, calls `apiauth.Caller(ctx).Scopes.Allow(op, attrs…)`,
returning `connect.CodePermissionDenied` on failure. This is ideal for the ~30
**request-only** RPCs: the check is declarative, lives in one auditable place
next to the taxonomy, and a new request-only RPC is one table row. It composes
with `RequireUserInterceptor` (which still runs first and guarantees a caller).

**Per-handler checks** (the `AgentFilter` pattern) are *required* for the ~12
**row-dependent** RPCs: the interceptor cannot see a task's `parent` or an org's
`owner` because those come from a row the handler loads. These RPCs call
`Allow` in the handler, right after the org-scoped load — copying
`AgentFilter.GetTask`/`UpdateTask` almost verbatim.

To avoid a silent hole, the interceptor classifies **every** procedure into
exactly one of three buckets and fails closed on anything unlisted:

| Bucket | Interceptor behavior |
|---|---|
| request-only (has an entry) | build attrs, call `Allow`, deny on failure |
| handler-enforced (row-dependent) | pass through; the handler **must** call `Allow` itself |
| exempt (`Ping`, `GetProfile`) | pass through, no check |
| *anything else* | **deny** (`CodePermissionDenied`) — default-deny on unknown procedures |

Recommendation: **hybrid, interceptor-first.** A pure interceptor cannot
authorize row-dependent RPCs; a pure per-handler approach would duplicate the
trivial request-only check across ~30 handlers and scatter the taxonomy. The
hybrid keeps the bulk declarative and confines hand-written checks to the dozen
RPCs that genuinely need a loaded row — which is also exactly the set
`AgentFilter` already demonstrates.

**Interaction with org scoping.** Scopes are *within-org*; org is a separate
axis and its enforcement does **not** move. The store calls keep taking
`caller.OrgID`, so:

- For request-only RPCs the interceptor's `Allow` only refines capability; the
  org boundary is still applied by the handler's store call.
- For row-dependent RPCs the org-scoped load runs first and returns `NotFound`
  for a row in another org **before** `Allow` is ever consulted — so a scope can
  only narrow access *inside* the caller's org and can never widen it across
  orgs, by construction. This is why no `Op`/attribute ever encodes an org id.

### State-based checks coexist as request-time guards

Some rules are about *resource state*, not identity/capability, and cannot live
in a scope. The clearest is `UpdateTask`'s "cannot update archived task"
(`AgentFilter.UpdateTask`, and the analogous API handler): whether a task is
archived is mutable runtime state. These remain ordinary request-time guards,
evaluated **after** scope authorization passes:

```go
// 1. capability: scopes.Allow(authscope.OpTaskWrite, WithTaskID(row.Id), WithTaskParent(row.Parent))
// 2. state guard: if row.Archived { return errPermissionDenied("cannot update archived task") }
```

Scope evaluation answers "may this caller, in principle, write this task?"; the
state guard answers "is the task currently writable?" Both must pass.

## Migration / phasing — behavior-preserving first

The crux is unchanged from the original draft: **get every API caller onto the
model without changing anyone's effective access, then narrow.** What *has*
changed is the starting line — PR #902 already shipped the engine and the
agent-caller half — so this section is re-anchored to today's state.

### Where we already are (post-#902)

| Caller | Carries scopes today? | Value today |
|---|---|---|
| Task token | **yes** — `TaskClaims.Scopes`, minted by `agentauth.Scopes` | real, fully-constrained per workspace capabilities |
| Cookie session | **yes** — `Auth.User` sets `Scopes` | `authscope.Admin()` (`*.*`) |
| App JWT | **yes** — `NewAppClaims` sets `Scopes` | `authscope.Admin()` (`*.*`) |
| `xat_` API key | **no** — `StoreKeyValidator.ValidateKey` returns `UserInfo` with `Scopes == nil` | empty ⇒ would be denied everything |
| API handlers | **no checks** — only org scoping | n/a |

So two facts shape Phase 1: cookie/app callers *already* hold the wildcard, but
**`xat_` keys hold nothing**, and the server performs **no** capability check
yet. Turning on enforcement naively would lock out every `xat_` key.

### Phase 1 — land API-caller enforcement as a no-op

Behavior-preservation rests on a single invariant: **every API caller holds
`authscope.Admin()`**, and `AdminScope` (`*.*`) matches any two-segment op on any
instance — so every `Allow` in §7/§8 returns true and nothing is denied.

Three changes, none of which denies any existing request:

1. **Close the `xat_` key gap.** Add a nullable `scopes` column to the `keys`
   table (new dbmate migration under `internal/store/sql/migrations/`,
   `TEXT[]`/`JSONB` of wire-grammar strings); backfill **all existing rows to
   `["*.*"]`**; `model.Key` gains a `Scopes authscope.Scopes`; `CreateKey`
   defaults new keys to `*.*`; `StoreKeyValidator.ValidateKey` parses the column
   into `UserInfo.Scopes`. As a belt-and-suspenders transitional default,
   `ValidateKey` treats a `NULL`/empty column as `authscope.Admin()` so an
   un-backfilled row is never locked out.
2. **Install the authorization interceptor + per-handler checks** for the whole
   service per §7/§8. Because the *entire* surface is mapped here, the
   interceptor can be **default-deny on unknown procedures from day one** — there
   is no "unmapped RPC" gap and therefore no need for the original draft's
   RPC-side shim.
3. **Confirm** cookie/app minting of `Admin()` (already in place from #902) and
   that the `Ping`/`GetProfile` exemptions are listed.

**Exactly what changes:** a control-flow check is added to every handler/RPC; it
always passes. One schema column is added and backfilled to admin. **No 403
appears, no caller loses access, task isolation is untouched** (`AgentFilter`
unchanged). The only *new* behavior is structural — the server can now enforce
capabilities itself rather than relying solely on the runner-side filter.

### Phase 2 — mint real `xat_` key scopes (first opt-in behavior change)

Only now can a *new* credential be narrower than its org. `CreateKeyRequest`
grows a `repeated string scopes` field and a UI affordance at key creation; the
`scopes` column stores the chosen grant. A read-only key is `*.read` (the engine
already supports a `*` first segment, so `*.read` matches `task.read`,
`event.read`, `key.read`, …); a single-workspace key is several
`task.create:{…workspace…}` scopes (no set values — §"Engine as built").

**What changes:** *only* keys explicitly created with narrow scopes are now
limited. Every pre-existing key still holds the backfilled `*.*`; sessions and
app JWTs are untouched. This phase is purely additive/opt-in.

### Phase 3 — derive app/cookie session scopes from org role

Today `Auth.User` and `NewAppClaims` both hard-code `Admin()` (with `// TODO:
revisit permissions for cookie auth` already in `Auth.User`). Phase 3 maps the
org `role` (`OrgMember.Role`, `owner`/member) to a scope set — `owner ⇒ *.*`,
member ⇒ a narrower set TBD — and mints that instead.

**What changes:** non-owner members' effective access narrows for the first
time. This is the one phase that can break an existing human user, so it is
gated behind deciding the member scope set (Open Question #5) and ships only
after that decision; until then both paths keep minting `Admin()`.

### Phase 4 — remove the transitional admin defaults

Once all keys carry explicit scopes (backfilled or chosen) and sessions/JWTs
derive from role, drop the transitional safety nets: the `NULL`-column ⇒
`Admin()` default in `StoreKeyValidator`, and any "absent scopes ⇒ admin"
fallback. After this, a scopeless caller is denied — real default-deny for
everyone. `AdminScope` remains, but only as an *explicit* grant (owners, the
backfill), never as an implicit fallback.

### What is explicitly behavior-preserving vs. behavior-changing

| Step | Changes effective access? |
|---|---|
| Phase 1: key `scopes` column + backfill `*.*` | **No** — every key still admin |
| Phase 1: interceptor + per-handler checks | **No** — every caller holds `*.*`; every `Allow` passes |
| Phase 2: narrow scopes on *new* keys | **Yes, opt-in** — only keys created narrow |
| Phase 3: role-derived session/app scopes | **Yes** — non-owner members narrow |
| Phase 4: remove admin fallbacks | **Yes** — scopeless callers now denied |

### Where scopes are stored & minted, per credential

| Credential | Storage | Minted by | Status |
|---|---|---|---|
| `xat_` key | new `scopes` column on key row | `CreateKey` (`apiserver/key.go`); backfilled `*.*` in migration | **Phase 1** |
| App JWT | `scopes` claim in `AppClaims` | `NewAppClaims` (`jwt.go`) | `Admin()` shipped (#902); role-derived in Phase 3 |
| Cookie session | not persisted; computed per request | `Auth.User` (`apiauth.go`) | `Admin()` shipped (#902); role-derived in Phase 3 |
| Task token | `Scopes` claim in `TaskClaims` (exists) | `agentauth.Scopes` from workspace capabilities | shipped (#902) |

## Trade-offs

**Uniformity & flexibility vs. security-critical string logic.** A generic
matching engine is more flexible (per-resource, per-instance, set-valued grants)
and uniform (one evaluator, no caller-type branching) than today's two ad-hoc
styles. The cost is that authorization becomes **pattern logic in the trust
path** — exactly the kind of code where an off-by-one in wildcard or membership
handling is a privilege escalation. Mitigations:

- **Exhaustive table tests** for `authscope` are mandatory and already shipped
  (`scope_test.go`, `parse_test`-equivalents): segment-count mismatch; `*`
  single-segment matching (and that it does *not* span segments); empty/nil
  `Preds` matching any instance; a constrained key whose attr is missing must
  **deny**, not match (the `ok` check in `allow`); first-colon split with colons
  inside the JSON; `Scopes` JSON round-tripping; and the `filter_test.go` cases
  expressed against `Allow`. Any new `Op*`/attribute added for the API surface
  extends these tables.
- **`AgentFilter` tests are a frozen behavioral spec** (`filter_test.go`) — they
  passed unchanged through the #902 conversion and must keep passing as the API
  side lands.
- **Minting discipline.** The grammar has sharp edges. `task.write` with empty
  predicates is *org-wide admin over tasks*, not "one task" — a forgotten
  predicate key silently widens a grant. Because an absent key is unconstrained,
  an **incomplete create scope is a hole**, not a deny (which is why
  `agentauth.Scopes` fully constrains the create scope with `parent`, `workspace`,
  *and* `runner`). Defenses: the `authscope.ValidScope` syntactic validator;
  typed minting helpers (`authscope.New` + `WithTask*`, the per-credential minters
  `agentauth.Scopes` / the future `CreateKey` path) so call-sites never
  hand-assemble wire strings; and a test that the only places `*.*` is produced
  are the admin/migration paths (`authscope.Admin()` call-sites).

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

**Set-valued predicates (deferred) vs. multiple scopes.** The shipped engine has
single-valued predicates only (`Parse` rejects arrays — see "Engine as built").
Until set values land, "this key may use 2 workspaces" is **two**
fully-constrained `task.create` scopes rather than one with an array value — same
DNF, just a larger token. Set values add no expressiveness; they would only stop
tokens bloating with the workspace × runner cross-product, at the cost of one
more thing the matcher and minters must get right. We accept the bloat for now
and revisit if real tokens grow large.

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
   `DeleteOrg`)? This proposal leaves role as-is — Phase 1 grants every
   session/JWT caller `authscope.Admin()` and keeps the existing owner-only role
   guards — and defers reconciling role with scopes to **Phase 3** (role-derived
   session/app scopes), the one phase that narrows a human user's access.
