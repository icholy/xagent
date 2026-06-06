# Unified scope-based (capability) permission model

Issue: https://github.com/icholy/xagent/issues/894

## Problem

Authorization in xagent is split across two enforcement styles, and the server
must know *which kind of caller* it is talking to before it can decide what that
caller may do. There is still no single place that answers "is this caller
allowed to do this?" for **API callers**, and no way to grant a user or API key
anything narrower than "everything in the org."

### The scope engine exists; only the agent path uses it

A generic scope-matching engine, `internal/auth/authscope`, is in the tree (it
landed in PR #902). A caller holds an `authscope.Scopes` — a set of capability
patterns — and `Scopes.Allow(op, attrs...)` decides a request. The **agent path
already runs on it**: `internal/agentmcp.AgentFilter` implements
`XAgentServiceHandler` and gates every agent RPC with `scopes.Allow(...)`,
restricting an agent to its own task or, when the workspace enables it, its
direct children. The task token carries those scopes in `TaskClaims.Scopes`
(`internal/auth/agentauth/token.go`), minted by the runner from the workspace's
capability flags via `agentauth.Scopes(ScopeOptions{...})`.

The **API path has not been converted.** Every `apiserver` handler still does
exactly one authorization check — org scoping — and no capability check. Each
handler narrows its store queries to `apiauth.Caller(ctx).OrgID`: `ListTasks`
(`apiserver/task.go`) calls `s.store.ListTasks(ctx, nil, caller.OrgID)`;
`CreateLink` verifies `s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)`. Org
is the tenancy boundary, and within an org it is the *only* check. The lone
interceptor, `apiauth.RequireUserInterceptor`, checks only that *some* caller is
present — never what they may do.

### Credentials and the scopes they carry today

Three API credential types are dispatched by token shape in
`apiauth.authenticate`:

| Credential | Built by | Scopes today |
|---|---|---|
| `xat_` API key | `StoreKeyValidator.ValidateKey` (`server/storeauth.go`) | **none** — `UserInfo.Scopes` is left nil |
| App JWT | `NewAppClaims` (`apiauth/jwt.go`) | `authscope.Admin()` (`*.*`) |
| Cookie session | `Auth.User` (`apiauth/apiauth.go`) | `authscope.Admin()` (`*.*`) |

`apiauth.UserInfo` already has a `Scopes authscope.Scopes` field. Cookie and app
callers are minted the admin wildcard so that turning on enforcement won't change
their behavior; `xat_` keys carry nothing yet — they would be denied everything
the moment a handler required a scope (see Migration).

### Why this hurts

- **The server cannot enforce task isolation itself.** `AgentFilter` runs in the
  *runner*, in front of the Unix-socket proxy. The server's own handlers have
  **no task-level authorization** — an org-scoped API caller (user/key/app JWT)
  can read or mutate *any* task in its org. Task isolation exists only because
  agents are forced through `AgentFilter`; it is not a server-enforced property.
- **Coarse-grained.** An `xat_` key cannot be limited to "create tasks in one
  workspace" or "read-only." It is all-or-nothing per org.
- **Caller-type branching.** Identity type leaks into logic (`AuditName()`
  special-cases `AuthTypeKey`); the agent path and the API path are separate
  handler trees with separate authorization shapes.

This proposal finishes what the engine started: put **API callers** on the same
`authscope` evaluator the agent path already uses, so one model answers every
request.

## Design

One model: an authenticated **caller carries `authscope.Scopes`**, and the single
evaluator `Scopes.Allow` decides every request. Org stays a separate axis.

The design has two cleanly separated layers, and keeping them separate is the
single most important property:

- A **generic matching engine** (`internal/auth/authscope`) that assigns *no
  meaning* to operation segments or predicate keys. It is a pure pattern matcher
  over `(operation-path, attributes)`.
- An **application layer** (the operation taxonomy in `authscope/task.go` plus the
  per-RPC mapping in the handlers/interceptor) that owns what the segments mean,
  which attributes exist, and how a request becomes an `(op, attrs)` pair. This is
  the *only* place domain knowledge lives.

### 1. `Caller.Scopes` — the unifying abstraction

Every authenticated caller carries `authscope.Scopes`:

```go
// internal/auth/apiauth/apiauth.go
type UserInfo struct {
    ID       string
    Email    string
    Name     string
    OrgID    int64
    Type     string
    ClientID string
    Scopes   authscope.Scopes // capabilities held within OrgID
}
```

The unifying abstraction is **`Caller.Scopes`**, populated per credential source —
*not* "every JWT has a scopes field." Each path fills it:

| Credential | Source of scopes |
|---|---|
| Task token (`TaskClaims.Scopes`) | minted by `agentauth.Scopes(ScopeOptions{...})` from the workspace's capability flags — **already live** |
| Cookie session | computed in `Auth.User` — `authscope.Admin()` today |
| App JWT (`AppClaims.Scopes`) | set in `NewAppClaims` — `authscope.Admin()` today |
| `xat_` API key | a `scopes` column on the key record, returned by `StoreKeyValidator.ValidateKey` — **to be added** (Migration) |

The task path is the proof of concept; this proposal generalizes the same field to
every API caller and routes every handler through the same evaluator.

`authscope.Scopes` is `[]authscope.Scope` with an `Allow` method. It lives in
`internal/auth/authscope`, which both `apiauth` (API callers) and `agentauth`
(task callers) import without a cycle. It **self-serializes**: `Scopes` marshals
to and from a JSON array of wire-grammar strings, so a token's `scopes` claim is a
plain `[]string` on the wire.

### 2. Scopes are permissions WITHIN an org

Org remains a **separate tenancy axis**. The caller still carries `OrgID`; handlers
still constrain their store queries to that org exactly as today (`caller.OrgID`).
**A scope never crosses orgs, and an org id is never encoded in a scope.** Scopes
express intra-org capability only.

Concretely, a request is authorized iff *both* hold:

1. **Tenancy:** the target resource belongs to `caller.OrgID` (enforced by the
   existing `…(ctx, nil, …, caller.OrgID)` store calls — unchanged), and
2. **Capability:** `caller.Scopes.Allow(op, attrs…)` returns true (new for API
   callers; already true for the agent path).

This keeps the org check where it already is and well-tested, and means the scope
grammar never has to encode org IDs. Cross-org access remains impossible by
construction even if a scope is mis-minted.

### 3. The engine is semantically agnostic

The matching engine assigns **no meaning** to operation segments or predicate
keys. There is no `resource`/`verb`/`action` concept *inside the engine* — those
are an application convention.

- **Engine** (`authscope`) = a generic matcher over `(operation-path,
  attributes)`: segment-wise match on the path (with `"*"` matching one segment),
  plus per-key equality on the predicates. It does not know that a segment is a
  "resource" or that an attribute is named `task.parent`.
- **Application** (`authscope/task.go` + the per-RPC mapping) owns the taxonomy:
  the `Op*` operation paths, the attribute keys, and the typed `With*`
  constructors that turn a request into `(op, attrs)`.

The engine types as they exist in `scope.go`:

```go
type Scope struct {
    Op    []string          // operation-path segments; "*" matches any one segment
    Preds map[string]string // attribute key -> required value; an absent key is unconstrained
}

type Attr struct { // one concrete request attribute, already stringified
    Name  string
    Value string
}

func New(op []string, attrs ...Attr) Scope                 // build a scope
func (scopes Scopes) Allow(op []string, attrs ...Attr) bool // evaluate a request
```

A handler authorizes a request by handing the operation path and the request's (or
a loaded row's) attributes straight to `Allow` — there is no intermediate target
object. Because the engine is agnostic, the taxonomy below (`task.read`,
`task.create`, `github_token.create`, …) is an application convention; this
proposal uses `resource.action` segments at arity 2.

### 4. Wire syntax: dot-path, optional JSON predicate object

A scope's wire form (what `authscope.Parse` reads and `Scope.String` writes) is a
**dot-delimited operation path**, optionally followed by `:` and a **JSON object**
of predicates:

```
seg1.seg2.…:{json-predicates}
```

- **Parse splits on the first colon.** The operation path is colon-free, so
  everything left of the first colon is the path (split on `.` into `Op` segments)
  and everything right of it is the predicate object. The JSON may contain colons;
  only the first one matters.
- The `:{…}` suffix is **optional**; absent ⇒ no predicates. A bare capability is
  just its path, e.g. `github_token.create`.
- Each path segment is a **single token**. `"*"` matches any one segment; there is
  no `|` alternation and no multi-segment wildcard.
- **Predicate values are JSON strings.** `parsePreds` unmarshals into
  `map[string]string`, so numbers, booleans, arrays, and nested objects are
  rejected — a predicate pins one attribute to one string value. Attribute keys are
  namespaced by resource (`task.id`, not `id`) so they stay globally unambiguous as
  the taxonomy grows.

Worked vocabulary (`resource.action` convention; predicate values are strings):

| Scope | Meaning |
|---|---|
| `task.read:{"task.id":"123"}` | read the task with id 123 |
| `task.read:{"task.parent":"123"}` | read any task whose `task.parent` is 123 (child access, resolved against the loaded row) |
| `task.write:{"task.id":"123"}` | write the task with id 123 |
| `github_token.create` | issue a GitHub token (no predicates) |
| `task.create:{"task.parent":"123","task.workspace":"X","task.runner":"Y"}` | create a task **iff** parent=123 **and** workspace=X **and** runner=Y |
| `task.*` | any action on a task instance (task-domain admin) |
| `*.*` | global admin within the org (any 2-segment operation, any instance) — `authscope.AdminScope` |

Two requests that differ only in an allowed value need two scopes: "create in
workspace X or Y" is `task.create:{…"task.workspace":"X"…}` **plus**
`task.create:{…"task.workspace":"Y"…}`, relying on OR-across-the-held-set (§6). The
engine has no array/set predicate value, so breadth costs token size, not
correctness.

### 5. Predicates: AND across keys, equality within a key

`authscope.Scope` documents the rule on its `Preds` field: each key pins an
attribute to a single required value, and predicates only ever **narrow** a grant.
A scope matches only if, for **every** key it lists, the request supplies that
attribute with exactly that value:

- **AND across keys** — every predicate key in the scope must match.
- **Equality within a key** — the request's attribute must equal the scope's value.
  There is **no predicate wildcard**: a `"*"` *value* is matched literally, not
  treated as "any."
- **An absent key is unconstrained** — the scope ignores attributes it does not
  mention. A scope with no predicates matches any instance of its operation.
- The test is **scope-predicates against request-attributes, never the reverse**: a
  key the scope constrains but the request omits **denies** (the `ok` check below);
  attributes the request carries but the scope doesn't mention are irrelevant.

The evaluator, verbatim from `scope.go`:

```go
func (scopes Scopes) Allow(op []string, attrs ...Attr) bool {
    for _, s := range scopes { // OR across the caller's held scopes
        if s.allow(op, attrs) {
            return true
        }
    }
    return false
}

func (s Scope) allow(op []string, attrs []Attr) bool {
    if len(s.Op) != len(op) { // same number of segments
        return false
    }
    for i, seg := range s.Op {
        if seg != "*" && seg != op[i] { // "*" matches any one segment
            return false
        }
    }
    for key, want := range s.Preds { // AND across keys
        got, ok := attrValue(attrs, key)
        if !ok || got != want { // missing or different -> deny
            return false
        }
    }
    return true
}
```

#### Wildcards on the operation path

- `*` matches **exactly one** segment. `task.*` matches `task.read` and
  `task.write` but not `task` or `task.a.b`; `*.*` matches exactly the 2-segment
  operations, which is why `authscope.AdminScope` is `"*.*"`.
- There is **no multi-segment wildcard**. Because the taxonomy is a fixed 2-segment
  `resource.action`, `*.*` already covers everything, so no subtree wildcard is
  needed. (A variable-depth taxonomy would need one — Open Questions.)

### 6. Evaluation semantics: AND within a scope, OR across scopes

Framed as DNF: **each scope is one conjunctive clause** (every segment matches AND
every predicate matches), and **the held set is the disjunction** of those clauses
— `Allow` is true iff some scope matches. This single rule satisfies four
requirements at once; each is worked through below with the failure mode that
motivates the AND-within / OR-across split. `AgentFilter` is the live reference
implementation for all four.

#### (a) CREATE needs a conjunction — kept inside one scope; the minter fully constrains

`AgentFilter.CreateTask` requires **four** things at once: the `child_tasks`
capability, plus `parent`, `workspace`, and `runner` all matching the agent's
task. The task token holds a single, **fully-constrained** scope (built by
`agentauth.Scopes`):

```go
authscope.New(authscope.OpTaskCreate,
    authscope.WithTaskParent(taskID),
    authscope.WithTaskWorkspace(ws),
    authscope.WithTaskRunner(rn),
) // wire: task.create:{"task.parent":"…","task.workspace":"…","task.runner":"…"}
```

and the handler authorizes the request directly from its fields:

```go
scopes.Allow(authscope.OpTaskCreate,
    authscope.WithTaskParent(req.Parent),
    authscope.WithTaskWorkspace(req.Workspace),
    authscope.WithTaskRunner(req.Runner),
)
```

`Allow` returns true only if **all three** keys match — exactly the current
behavior. Allowing N workspaces is N such scopes (one per workspace), since
predicates are single-valued.

**The minter must emit a fully-constrained scope.** An absent predicate key is
unconstrained — a hole — so completeness is mandatory at mint time; that is why
`agentauth.Scopes` always sets `parent`, `workspace`, **and** `runner` on the
create scope. We do **not** have the server fill in missing attributes (§6d).

**Failure mode if the conjunction were split into separate scopes** —
`task.create:{"task.parent":"42"}`, `task.create:{"task.workspace":"ws"}`,
`task.create:{"task.runner":"rn"}` — and ORed: a request matches
`task.create:{"task.parent":"42"}` alone, because that scope leaves `workspace` and
`runner` unconstrained. The caller could then create a task under parent 42 with an
**arbitrary workspace/runner**, escaping its sandbox. The conjunction must live
inside one scope so that ORing across the held set can never relax it.

#### (b) Relationship / child access is naturally disjunctive — across scopes

`AgentFilter.GetTask` allows the agent's own task **or** a direct child. That is a
genuine OR, mapped to two scopes (the second present only with `child_tasks`):

```
task.read:{"task.id":"42"}        # own task
task.read:{"task.parent":"42"}    # any direct child of 42
```

The handler **loads the row first** (it already calls `GetTask` on the upstream
client) and authorizes against the row's attributes:

```go
scopes.Allow(authscope.OpTaskRead,
    authscope.WithTaskID(resp.Task.Id),
    authscope.WithTaskParent(resp.Task.Parent),
)
```

- Own task (`row.Id==42`): matches `task.read:{"task.id":"42"}` (the `task.parent`
  key is absent there ⇒ unconstrained). ✅
- Child (`row.Parent==42`): matches `task.read:{"task.parent":"42"}` (the `task.id`
  key absent ⇒ unconstrained). ✅
- Unrelated task: matches neither → denied. ✅

OR-across-scopes is exactly right. The `task.parent` predicate is **resolved
against the loaded row at request time** — that is how relationship access works
without the engine needing a graph: load the row (org-scoped), then ask `Allow`
about the row's attributes. **Loading the row before the check is required whenever
an attribute comes from the stored entity**, and is the pattern every row-dependent
RPC follows (§7).

**Failure mode if we required AND across all held scopes:** a caller holding both
`task.read:{"task.id":"42"}` and `task.read:{"task.parent":"42"}` could read
*nothing* — its own task fails the parent scope, a child fails the id scope. Scopes
must be **additive** (OR), the standard capability-model semantics.

#### (c) Wildcard admin subsumes everything narrower

`task.*` covers own task **and** children, because a child is still a task
instance: op `task.*` matches `task.read`/`task.write`, and with no predicates it
matches any instance. `*.*` matches any 2-segment operation on any instance.
Because evaluation is OR-across-scopes, holding a broad scope **plus** narrow ones
is always ≥ the narrow ones alone — admin can never be *less* than a sub-grant.
This is the property that lets Migration mint `authscope.Admin()` for existing
callers and preserve today's omnipotence exactly.

#### (d) Handlers must not need per-RPC "which attributes are required" knowledge

- The **handler** only describes *what the request is*: it passes the operation
  path and the concrete attributes of the request (for create) or the loaded row
  (for read/write) to `Allow`. It does **not** know which predicates a scope
  "should" constrain.
- The **scope** declares which keys *it* constrains; `Allow` ANDs exactly those and
  ignores attributes the scope omits.

So `CreateTask` always passes `parent, workspace, runner` regardless of what scopes
exist. A token scoped on all three constrains all three; a hypothetical token
scoping only `parent` would constrain only `parent` — **the handler code is
identical either way.** "Which attributes matter" lives in the minted scope, not
the handler. Combined with §6a (the minter fully constrains), correctness is the
minter's responsibility and the handler stays domain-light.

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

The crux is sequencing: **get every API caller onto the model without changing
anyone's effective access, then narrow.** The engine and the agent-caller half
already exist, so phasing starts from a partly-built system — the table below is
the actual starting state, not a plan.

### Where we already are

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
   interceptor is **default-deny on unknown procedures from day one** — there is
   no "unmapped RPC" gap to paper over.
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
`task.create:{…"task.workspace":…}` scopes, one per workspace (predicates are
single-valued, §4).

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
matching engine is more flexible (per-resource, per-instance grants) and uniform
(one evaluator, no caller-type branching) than today's two ad-hoc styles. The
cost is that authorization becomes **pattern logic in the trust path** — exactly
the kind of code where an off-by-one in wildcard or predicate handling is a
privilege escalation. Mitigations:

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

**Single-valued predicates vs. set values.** The engine pins each predicate key to
one string value (`Parse` rejects arrays), so "this key may use 2 workspaces" is
**two** fully-constrained `task.create` scopes rather than one with an array value
— same DNF, just a larger token. This keeps the matcher and the wire grammar
trivial. A future array-valued predicate would add no expressiveness (it is the
same DNF) and would only keep tokens from bloating with the workspace × runner
cross-product, at the cost of more matcher surface; we keep predicates
single-valued until a real token grows large enough to justify it.

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
