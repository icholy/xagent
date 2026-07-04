# Attribute-Based Event Representation and Rule Matching

## Problem

Every new event type or matchable dimension currently requires touching the
entire stack: a new field on `eventrouter.InputEvent`, a new field on
`model.RoutingRule`, a new proto field, a new clause in `MatchRule`, new
show/hide logic in the webui form, and a proto regen for both `webui` and
`n8n-node`. The history of the routing system is a sequence of exactly this:

- Assignment routing (#741) added `InputEvent.Assignee` +
  `RoutingRule.Assignee` (proto field 6) + `matchAssignee` + the webui
  `isAssignmentType` special case.
- Label routing (#809 and the GitHub follow-up) added `InputEvent.Values` +
  `RoutingRule.Value` (proto field 8) + a membership clause + the webui
  `isLabelType` special case.
- PR close/merge routing shoved `"merged"`/`"closed"` into `InputEvent.Data`
  so the existing `Prefix` matcher could be abused as `Prefix=merged`
  (`internal/server/githubserver/webhook.go`, the `pull_request_closed`
  case — the comment there admits it's a workaround).

Each addition is small, but the shape doesn't scale. Candidate future events —
Jira status transitions, review-requested, check-run results, milestones,
branch filters — each carry one or two new dimensions. Under the current
design that's one or two new proto fields, `MatchRule` clauses, and frontend
special cases *per event type*, with every field silently meaningless for
every other event type.

There are also latent structural problems beyond the churn:

1. **Match semantics are implicit in field names.** `Prefix` means
   "prefix-of-Data", `Value` means "member-of-Values", `Mention` means
   "source-specific mention syntax somewhere in Data". There is no way to say
   "body contains", "label starts with", or to put two constraints on the
   same dimension.
2. **Source-specific knowledge lives in the router.** `matchMention` and
   `matchAssignee` (`internal/eventrouter/rule.go`) switch on
   `InputEvent.Source` and embed GitHub's `@name` word-boundary regex and
   Jira's `[~accountid:…]` syntax. The Jira arm of `matchAssignee` is a
   silent `return false` landmine: a user can configure a rule that can
   never match, with no feedback.
3. **The event-type contract is duplicated by hand.** The
   `EventType*` constants live in `githubserver`/`atlassianserver`, and
   `webui/src/lib/routing-rules.ts` opens with "Mirrors the (source, type)
   pairs the webhook handlers actually emit" — a hand-synced copy, plus
   hand-coded knowledge of which fields apply to which types
   (`isAssignmentType`, `isLabelType`).
4. **`Values` is an anonymous bag.** Today it only ever holds labels, but the
   moment an event carries two multi-valued dimensions (labels *and*
   requested reviewers, say), they collide in one untyped list.

## Current State

### Event representation

`eventrouter.InputEvent` (`internal/eventrouter/eventrouter.go`):

```go
type InputEvent struct {
    Source      string
    Type        string
    Description string
    Data        string   // comment body, or "merged"/"closed" for pr_closed
    URL         string
    UserID      string
    Assignee    string   // only set by assignment events
    Values      []string // only set by label events
    Meta        any      // GitHubMeta / AtlassianMeta, router never looks
}
```

### Rule representation and matching

`model.RoutingRule` (`internal/model/routing_rule.go`, proto fields 1–9):
`Source`, `Type`, `Prefix`, `Mention`, `Assignee`, `URLPrefix`, `Value`,
plus the two action fields `Create` and `Wakeup`. Empty matcher fields are
wildcards; `InputEvent.MatchRule` ANDs one hard-coded clause per field.

Rules are stored as a JSONB array on `orgs.routing_rules`
(`internal/store/org.go` marshals `[]model.RoutingRule` directly), so the
storage format is the model's JSON tags — there is no per-rule table or
schema migration involved in changing the rule shape.

### What consumes what

- `Router.Route` matches rules, then wakes subscribed tasks or creates one.
  It reads `Description`, `Data`, `URL` to build the `model.Event` payload
  and the task preamble. Actions (`Wakeup`, `Create`) and the org/link flow
  are independent of how matching is expressed.
- `RouteOutcome.Input` hands the whole `InputEvent` (including `Meta`) to
  the reaction callback in `githubserver`.
- The webui form edits rules via the proto; `n8n-node` has generated types
  for the same messages.

## Design

The core move: **events carry a typed bag of matchable attributes, and rules
carry a list of `(attr, op, value)` conditions.** Adding a new matchable
dimension becomes: extractor emits a new attr key + one registry entry.
No router, model, proto, or store changes; the UI picks it up from the
registry.

### 1. Event: named multi-valued attributes

```go
// Attrs maps a dimension name to the event's values for that dimension.
// Single-valued dimensions are one-element slices.
type Attrs map[string][]string

type InputEvent struct {
    Source      string
    Type        string
    Description string
    Data        string // agent-visible payload, unchanged role
    URL         string
    UserID      string
    Attrs       Attrs  // replaces Assignee and Values
    Meta        any
}

// Attr returns the event's values for a dimension. The "body" and "url"
// dimensions are views over Data and URL so extractors don't duplicate them.
func (e InputEvent) Attr(key string) []string
```

Well-known attr keys, all lowercase singular: `body`, `url` (derived),
`mention`, `assignee`, `label`, `state`. New dimensions are new keys.

**Extractors normalize source-specific syntax at extraction time.** This is
the piece that moves source knowledge out of the router:

- GitHub comment/review events emit `mention: ["alice", "bob"]` by scanning
  the body with the same word-boundary pattern `matchMention` uses today
  (`(?:^|[\s(])@name(?:$|[\s,.)!?])`, generalized to extract rather than
  test). Jira comments emit the account IDs found in `[~accountid:…]`.
- Assignment events emit `assignee: [login]`.
- Label events emit `label: [...]` (GitHub: the single label per delivery;
  Jira: all added labels — exactly today's `Values`).
- `pull_request_closed` emits `state: ["merged"]` or `["closed"]`. `Data`
  keeps the same string for agent visibility and legacy-rule compatibility.

The router's matcher becomes purely generic — no `switch e.Source` anywhere
in `eventrouter`. The deferred-Jira-assignee landmine disappears
structurally: when the Jira extractor later emits `assignee`, matching just
works.

### 2. Rule: selector + condition list

```go
type RoutingRule struct {
    Source     string      `json:"source,omitempty"` // wildcard when empty
    Type       string      `json:"type,omitempty"`   // wildcard when empty
    Conditions []Condition `json:"conditions,omitempty"`
    Wakeup     bool        `json:"wakeup,omitempty"`
    Create     *CreateTaskAction `json:"create,omitempty"`
}

type Condition struct {
    Attr  string `json:"attr"`
    Op    string `json:"op"` // "equals" | "prefix" | "contains"
    Value string `json:"value"`
}
```

`Source` and `Type` stay dedicated selector fields rather than becoming
conditions: they are the event's identity, every rule sets them, and the UI
presents them as the primary dropdown. Conditions express everything else.

Matching semantics:

- A condition holds when **any** value of `event.Attr(cond.Attr)` satisfies
  `(op, value)` — this generalizes today's `Value ∈ Values` membership and
  degrades to plain comparison for single-valued attrs.
- A condition on an attr the event doesn't carry **fails** — same behavior
  as today's `Assignee` rule against a non-assignment event.
- Conditions AND together. OR is expressed as multiple rules, which
  first-match-wins per org already provides. Negation is deferred (an
  additive `negate bool` on Condition later, if ever needed).
- Comparisons use case-insensitive folding (`strings.EqualFold` and folded
  prefix/contains). Today `Mention`/`Assignee` are already
  case-insensitive; `Prefix`/`Value` are case-sensitive. Uniform folding is
  predictable and matches GitHub's own semantics for logins and labels; the
  delta is called out under Compatibility.

Today's matchers map to:

| Legacy field | Condition |
|---|---|
| `Prefix` | `{body, prefix, v}` |
| `Mention` | `{mention, equals, v}` |
| `Assignee` | `{assignee, equals, v}` |
| `URLPrefix` | `{url, prefix, v}` |
| `Value` | `{label, equals, v}` |

The default rule becomes
`{Conditions: [{body, prefix, "xagent:"}], Wakeup: true}`.

`MatchRule` shrinks to ~15 generic lines; `matchMention`/`matchAssignee` are
deleted (their syntax knowledge moves to the extractors, per §1).

### 3. Event-type registry

A single declarative table, the machine-readable version of the contract
that currently lives half in `EventType*` constants and half in the webui's
hand-mirrored `EVENT_TYPES`:

```go
// internal/eventrouter/schema.go
type EventTypeDef struct {
    Source string
    Type   string
    Label  string   // "GitHub: Issue/PR Comment"
    Attrs  []string // attr keys this event type emits, beyond body/url
}

var EventTypes = []EventTypeDef{
    {Source: "github", Type: "issue_comment", Label: "GitHub: Issue/PR Comment", Attrs: []string{"mention"}},
    {Source: "github", Type: "pull_request_closed", Label: "GitHub: PR Closed", Attrs: []string{"state"}},
    {Source: "github", Type: "label_added", Label: "GitHub: Label Added", Attrs: []string{"label"}},
    {Source: "atlassian", Type: "comment_created", Label: "Jira: Issue Comment", Attrs: []string{"mention"}},
    // ...
}
```

The `EventType*` constants in `githubserver`/`atlassianserver` move here (or
alias entries here) so the producer and the contract can't drift.

Consumed by:

- **Validation.** `SetRoutingRules` rejects unknown attrs/ops, and rejects a
  condition on an attr the selected `Type` never emits (today that
  misconfiguration is accepted and silently never matches). Rules with an
  empty `Type` may use any registered attr.
- **The webui.** A new `GetEventTypes` RPC returns the table; the form
  derives the event-type dropdown and the offered condition attrs from it.
  This deletes the hand-synced `EVENT_TYPES` array and the
  `isAssignmentType`/`isLabelType` special cases — a new event type shipped
  server-side appears in the UI with the right condition fields, no
  frontend release required.

### 4. Proto changes

```proto
message RoutingRule {
  string source = 1;
  string type = 2;
  string prefix = 3 [deprecated = true];
  string mention = 4 [deprecated = true];
  CreateTaskAction create = 5;
  string assignee = 6 [deprecated = true];
  string url_prefix = 7 [deprecated = true];
  string value = 8 [deprecated = true];
  bool wakeup = 9;
  repeated RuleCondition conditions = 10;
}

message RuleCondition {
  string attr = 1;
  string op = 2;
  string value = 3;
}

message EventTypeDef {
  string source = 1;
  string type = 2;
  string label = 3;
  repeated string attrs = 4;
}

rpc GetEventTypes(GetEventTypesRequest) returns (GetEventTypesResponse);
```

Op stays a string (not an enum) to match the JSON storage form and the
model; the server validates against the registry either way.

### 5. Compatibility and migration

Storage is a JSONB blob of model JSON, so this is a decode-shape concern,
not a schema migration:

- `model.RoutingRule` keeps the legacy fields as decode-only JSON fields.
  A `normalize()` step at the store read boundary (`GetOrgRoutingRules`,
  `GetRoutingRulesByOrgs`, `ListRoutingRulesForUser`) folds them into
  `Conditions` per the table in §2 and clears them. `SetOrgRoutingRules`
  persists canonical form only, so rules self-migrate on next save.
- `RoutingRuleFromProto` applies the same fold, so an old webui or n8n
  client sending legacy fields keeps working for as long as the deprecated
  proto fields are kept (at least one release; webui ships with the server,
  n8n-node is published separately and is the reason to keep them).
- The `pull_request_closed` hack stays compatible: `Data` still carries
  `"merged"`/`"closed"`, so a legacy `Prefix=merged` rule folds to
  `{body, prefix, merged}` and still matches, while new rules use the
  honest `{state, equals, merged}`.
- Behavior delta: `body`-prefix and `label`-equals comparisons become
  case-insensitive (§2). No known rule depends on case today; flagged here
  so the change is deliberate.

### 6. What does not change

The `Router.Route` flow — org iteration, first-match-wins per org, link
lookup by routing key, wake vs create, `RouteOutcome`, `OnRouteOutcome`,
notifications — is untouched. This proposal is scoped to representation and
matching; actions (`Wakeup`, `Create`) stay top-level rule fields.

## Trade-offs

- **Full expression language (CEL / expr-lang):** maximum power, but a heavy
  dependency, free-text rules the server can't meaningfully validate, and a
  much harder UI story. The rule volume (a handful per org) doesn't justify
  it. The condition list covers every rule shipped or requested to date.
- **Match against the raw webhook payload (JSON-path, à la Argo Events):**
  avoids extractors entirely, but exposes provider wire formats to users,
  couples stored rules to payload shapes we don't control, and gives up
  cross-source normalization (a "mention" rule would need per-source
  syntax again — in user-authored rules this time).
- **Per-event-type typed rule messages (proto oneof):** models
  applicability in the type system, but recreates the scaling problem as a
  proto message per event type, and makes the storage/UI combinatorial.
- **Status quo plus more fields:** the trajectory this proposal exists to
  stop; every dimension keeps costing a proto field, matcher clause, and
  frontend special case, all silently inapplicable everywhere else.
- **Conditions with OR/negation in v1:** deferred. AND-only keeps the
  matcher trivial and the UI a flat list; OR across rules already exists.

## Implementation Sketch

- [ ] `internal/model/routing_rule.go`: add `Condition`, `Conditions` field;
      keep legacy fields decode-only; `normalize()` fold; proto conversions.
- [ ] `internal/eventrouter/rule.go`: rewrite `MatchRule` over
      `Attr`/conditions; delete `matchMention`/`matchAssignee`; port
      `rule_test.go` cases to conditions (legacy-fold tests keep the old
      cases alive).
- [ ] `internal/eventrouter/eventrouter.go`: `Attrs` type, drop
      `Assignee`/`Values`, add `Attr()` accessor.
- [ ] `internal/eventrouter/schema.go`: registry + `Validate(rule)`.
- [ ] `internal/server/githubserver/webhook.go`,
      `internal/server/atlassianserver/webhook.go`: emit attrs
      (mention extraction, assignee, label, state); move/alias `EventType*`
      constants to the registry.
- [ ] `internal/store/org.go`: normalize on read.
- [ ] `proto/xagent/v1/xagent.proto`: `RuleCondition`, field 10,
      deprecations, `GetEventTypes`; `mise run generate`; regen `webui` and
      `n8n-node` protos.
- [ ] `internal/server/apiserver`: validate on `SetRoutingRules`; implement
      `GetEventTypes`.
- [ ] `webui`: condition-list editor driven by `GetEventTypes`; delete
      `EVENT_TYPES`, `isAssignmentType`, `isLabelType`; keep per-attr
      copy (the existing `mentionCopyForSource` etc. rekeyed by attr).

## Open Questions

- Is uniform case-insensitive matching acceptable, or should sensitivity be
  per-attr in the registry (labels are technically case-sensitive in Jira)?
- Should `GetEventTypes` be an RPC (proposed — stays in sync with the
  deployed server) or a TS artifact generated from the Go table at build
  time (no runtime call, but can drift across deploys)?
- How long do the deprecated proto fields live? The only externally-shipped
  client is `n8n-node`; one minor release after it regenerates seems enough.
- Should the webui keep a "simple mode" that renders common conditions as
  the current friendly single fields (mention / label / assignee) on top of
  the generic condition editor, or is the condition list alone acceptable
  UX for v1?
