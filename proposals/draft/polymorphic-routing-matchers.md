# Polymorphic Routing Matchers

Issue: https://github.com/icholy/xagent/issues/843

## Problem

The routing model is a flat bag of scalar fields. `model.RoutingRule`
(`internal/model/routing_rule.go`) holds `Source` / `Type` / `Prefix` /
`Mention` / `Assignee` / `URLPrefix`, all ANDed together, and
`eventrouter.InputEvent.MatchRule` (`internal/eventrouter/rule.go`) wires each
field to an event field by hand, with source-specific special cases (a
`switch e.Source` for mention/assignee). Every new thing to route on requires a
parallel field on **both** structs (and the proto, the webui form, and the n8n
node).

This is rigid in three ways: matching is **AND-only**, each field targets a
**single value**, and each field has a **fixed operator** (`Prefix` is always a
prefix, `Type`/`Source` always exact). PR #840 exposed it directly — there was
no label match available, so it overloaded `Data` (one event per label, the
label name stuffed into `Data`) purely to reuse the `Prefix` match. See also
#809 and `proposals/draft/atlassian-label-routing-rule.md`.

## Design

### Core idea

Make the **matcher** polymorphic — not the whole rule. A `RoutingRule` becomes a
thin outer wrapper holding two orthogonal things:

```go
type RoutingRule struct {
    Matcher Matcher           // discriminated union — how the rule decides a match
    Create  *CreateTaskAction // shared across all matcher kinds — what to do on a match
}
```

`CreateTaskAction` (the `create` action) is the same regardless of how the rule
matches, so it stays on the outer struct. Only the *match* part becomes a
discriminated union of variants, each implementing a single-method interface:

```go
// Matcher reports whether a routing rule matches an event. Implementations are
// a closed set of variants, discriminated by a "kind" tag in their JSON form.
type Matcher interface {
    Match(InputEvent) bool
}
```

The first and (initially) only variant is `LegacyMatcher`: a faithful port of
today's flat fields with identical semantics. Crucially, **a matcher with no
`kind` discriminator deserializes as `LegacyMatcher`**, so every existing
`orgs.routing_rules` JSONB row keeps working untouched — zero data migration,
including #840's label-via-`Data` routing, which `LegacyMatcher` preserves
exactly.

Follow-up variants — designed here, built later — are a dedicated
`LabelMatcher` (retiring the `Data` hack by reading a new `InputEvent.Labels`)
and `And` / `Or` / `Not` composites for boolean expressiveness.

This proposal describes the full target design first, then lands it in slices,
mirroring `proposals/draft/link-routing-url.md`. **PR 1 is a pure refactor**:
introduce the union with `LegacyMatcher` as the only variant and the
no-discriminator default. Zero behavior change, zero migration, no
`InputEvent.Labels`.

### 1. The matcher variants

All variants and the `Matcher` interface live in `internal/model` alongside
`RoutingRule`, because the rule is what the store serializes and the variants
are part of that serialized shape. "How a rule matches" is now a property the
rule owns, so the `Match` behavior lives with the data rather than in a
hand-wired `switch` in `eventrouter`.

```go
// internal/model/routing_rule.go (or a new internal/model/matcher.go)

// LegacyMatcher is a faithful port of the original flat RoutingRule fields with
// identical semantics. Empty fields are wildcards. It is the default when a
// matcher's JSON carries no "kind" discriminator, so pre-union rows keep
// matching exactly as before.
type LegacyMatcher struct {
    Source    string `json:"source,omitempty"`
    Type      string `json:"type,omitempty"`
    Prefix    string `json:"prefix,omitempty"`
    Mention   string `json:"mention,omitempty"`
    Assignee  string `json:"assignee,omitempty"`
    URLPrefix string `json:"url_prefix,omitempty"`
}

func (m LegacyMatcher) Match(e InputEvent) bool { /* today's MatchRule body */ }
```

Follow-up variants (designed, not built in PR 1):

```go
// LabelMatcher matches when the event carries a given label. It reads the new
// InputEvent.Labels slice (§5), retiring #840's "one event per label, label in
// Data" overload.
type LabelMatcher struct {
    Label string `json:"label"` // kind: "label"
}
func (m LabelMatcher) Match(e InputEvent) bool { return slices.Contains(e.Labels, m.Label) }

// And / Or / Not compose sub-matchers for boolean expressiveness.
type AndMatcher struct{ Matchers []Matcher } // kind: "and"  — all must match (empty ⇒ true)
type OrMatcher  struct{ Matchers []Matcher } // kind: "or"   — any must match (empty ⇒ false)
type NotMatcher struct{ Matcher  Matcher }   // kind: "not"  — negation

func (m AndMatcher) Match(e InputEvent) bool { for _, s := range m.Matchers { if !s.Match(e) { return false } }; return true }
func (m OrMatcher)  Match(e InputEvent) bool { for _, s := range m.Matchers { if  s.Match(e) { return true  } }; return false }
func (m NotMatcher) Match(e InputEvent) bool { return !m.Matcher.Match(e) }
```

`MatchRule` (today a method on `InputEvent`) is **removed**; matching is now
`rule.Matcher.Match(event)`. The source-specific helpers `matchMention` /
`matchAssignee` move from `eventrouter/rule.go` into `LegacyMatcher` (they are
the legacy semantics and belong with it). A convenience
`func (r RoutingRule) Match(e InputEvent) bool { return r.Matcher.Match(e) }`
keeps call sites terse.

### 2. Package boundary: where `InputEvent` lives

Per-variant `Match(InputEvent)` methods on `model` types need to reference
`InputEvent`, which lives in `eventrouter` today. Since `eventrouter` already
imports `model`, defining the methods in `model` against an `eventrouter` type
would create an import cycle.

**Resolution: move the `InputEvent` struct definition into `internal/model`,
and leave a type alias in `eventrouter` for zero call-site churn:**

```go
// internal/eventrouter/eventrouter.go
type InputEvent = model.InputEvent
```

Every existing reference to `eventrouter.InputEvent` (the webhook handlers in
`githubserver`/`atlassianserver`, the router, the tests) keeps compiling
unchanged through the alias. `InputEvent` is the natural input to matching, and
matching is now a `model` concern, so the type sits comfortably there; its
opaque `Meta any` field stays opaque. This is the minimal-churn way to satisfy
the "per-variant `Match` methods" requirement without a cycle.

An alternative — keep `InputEvent` in `eventrouter` and have `Match` take a
narrow read-only interface (`EventSource()`, `EventType()`, …) defined in
`model` — was rejected: it adds getter boilerplate, and every new field a future
matcher reads (e.g. `Labels`) must be threaded through the interface. See
Trade-offs.

### 3. JSON: the union (de)serialization

Storage is unchanged: the `orgs.routing_rules` JSONB column, scanned as
`json.RawMessage` (`internal/store/sqlc/org.sql.go`) and unmarshaled into
`[]model.RoutingRule` by `internal/store/org.go`. The whole design rides on
custom `MarshalJSON`/`UnmarshalJSON` so the on-disk shape is unchanged for
legacy rows.

**Matcher dispatch** peeks at a `kind` discriminator and defaults to legacy when
absent:

```go
func unmarshalMatcher(data []byte) (Matcher, error) {
    var disc struct{ Kind string `json:"kind"` }
    if err := json.Unmarshal(data, &disc); err != nil {
        return nil, err
    }
    switch disc.Kind {
    case "", "legacy":          // ← no discriminator ⇒ legacy (zero migration)
        var m LegacyMatcher
        if err := json.Unmarshal(data, &m); err != nil { return nil, err }
        return m, nil
    case "label":               // follow-up
        var m LabelMatcher
        ...
    case "and", "or":           // follow-up — recurse on sub-matchers
        ...
    case "not":                 // follow-up
        ...
    default:
        return nil, fmt.Errorf("unknown matcher kind %q", disc.Kind)
    }
}
```

Each non-legacy variant marshals **with** its `kind`
(`{"kind":"label","label":"xagent"}`); `LegacyMatcher` marshals **without** a
`kind`, emitting exactly today's flat fields. The composites recurse through
`unmarshalMatcher` for their sub-matchers.

**The outer `RoutingRule`** splits `create` from the matcher. To keep legacy
rows byte-identical, the matcher's fields are *inlined* at the top level
alongside `create` (rather than nested under a `match` key), so a pre-union row
like `{"source":"github","prefix":"xagent:","create":{…}}` round-trips exactly:

```go
func (r *RoutingRule) UnmarshalJSON(data []byte) error {
    var aux struct{ Create *CreateTaskAction `json:"create"` }
    if err := json.Unmarshal(data, &aux); err != nil { return err }
    m, err := unmarshalMatcher(data) // whole object; LegacyMatcher ignores "create"
    if err != nil { return err }
    r.Create, r.Matcher = aux.Create, m
    return nil
}

func (r RoutingRule) MarshalJSON() ([]byte, error) {
    obj := map[string]json.RawMessage{}
    mb, err := json.Marshal(r.Matcher)          // legacy ⇒ flat fields, no "kind"
    if err != nil { return nil, err }
    if err := json.Unmarshal(mb, &obj); err != nil { return nil, err }
    if r.Create != nil {
        cb, err := json.Marshal(r.Create)
        if err != nil { return nil, err }
        obj["create"] = cb
    }
    return json.Marshal(obj)
}
```

Worked examples:

| Stored JSON (unchanged) | Deserializes to |
|---|---|
| `{"prefix":"xagent:"}` | `RoutingRule{Matcher: LegacyMatcher{Prefix:"xagent:"}}` |
| `{"source":"github","type":"issue_comment","create":{…}}` | `RoutingRule{Matcher: LegacyMatcher{Source:"github",Type:"issue_comment"}, Create:&{…}}` |
| `{"kind":"label","label":"xagent","create":{…}}` *(follow-up)* | `RoutingRule{Matcher: LabelMatcher{Label:"xagent"}, Create:&{…}}` |

The all-wildcard legacy rule still marshals to `{}` (everything `omitempty`),
identical to today.

`defaultRules` in `eventrouter.go` changes from
`{{Prefix: "xagent:"}}` to `{{Matcher: model.LegacyMatcher{Prefix: "xagent:"}}}`.

### 4. Proto representation

`message RoutingRule` (`proto/xagent/v1/xagent.proto:532`) keeps
`CreateTaskAction create = 5` on the outer message and replaces the flat match
fields with a matcher union. Because the composites (`And`/`Or`/`Not`) are
recursive, the union is modeled as a **standalone, recursive `RoutingMatcher`
message** referenced by `RoutingRule`, rather than an inline `oneof` on
`RoutingRule` (an inline oneof works for `legacy`/`label` but cannot express
`repeated RoutingMatcher` children, so starting recursive avoids a later
breaking restructure):

```proto
message RoutingRule {
  reserved 1, 2, 3, 4, 6, 7;        // were source/type/prefix/mention/assignee/url_prefix
  CreateTaskAction create = 5;       // unchanged field number
  RoutingMatcher matcher = 8;
}

message RoutingMatcher {
  oneof matcher {
    LegacyMatcher legacy = 1;
    // follow-up variants:
    LabelMatcher  label  = 2;
    AndMatcher    and    = 3;
    OrMatcher     or     = 4;
    NotMatcher    not    = 5;
  }
}

message LegacyMatcher {
  string source = 1;
  string type = 2;
  string prefix = 3;
  string mention = 4;
  string assignee = 5;
  string url_prefix = 6;
}

// follow-up
message LabelMatcher { string label = 1; }
message AndMatcher  { repeated RoutingMatcher matchers = 1; }
message OrMatcher   { repeated RoutingMatcher matchers = 1; }
message NotMatcher  { RoutingMatcher matcher = 1; }
```

In PR 1 `RoutingMatcher` contains only `legacy`; the other variants are added
in their slices. The old field numbers 1–4/6–7 on `RoutingRule` are `reserved`
so they can never be silently reused. `RoutingRule.Proto` /
`RoutingRuleFromProto` (`internal/model/routing_rule.go`) and the
`SetRoutingRules` handler (`internal/server/apiserver/org.go:226`) translate
between the union and the oneof — a type switch on the model side, a
`switch pb.Matcher.(type)` on the proto side.

This proto change crosses `GetRoutingRules` / `SetRoutingRules` to the webui rule
editor and the n8n node, both of which regenerate from the proto.

### 5. Webui rule editor

The FE consumes the proto and currently reads/writes flat fields
(`rule.source`, `rule.prefix`, …) in `webui/src/lib/routing-rules.ts` and
`webui/src/components/routing-rule-form.tsx`. After the change it reads/writes
through the oneof — for the legacy variant, `rule.matcher.value` (case
`'legacy'`) carries the same `source`/`type`/`prefix`/`mention`/`assignee`/
`urlPrefix` fields the form already renders.

**PR 1 keeps the FE behavior identical**: the form still shows exactly the
legacy fields; the only change is the mapping layer
(`formValuesFromRoutingRule` reads `rule.matcher` case `'legacy'`;
`buildRoutingRule` emits `matcher: { case: 'legacy', value: {…} }`). The
existing `legacyEventTypeOption` / `eventTypeLabel` machinery in
`routing-rules.ts` is unaffected — it keys off `source`/`type`, which now live
on the legacy matcher value.

The FE only needs to expose **flat, single-kind matchers + legacy** initially:
the user picks a matcher kind and the form renders that kind's fields. Composite
(`And`/`Or`/`Not`) authoring in the UI is explicitly out of scope for the early
slices — they can be stored/edited via the API or n8n first, with a tree editor
added later. This is called out as future work, not a silent gap.

## Implementation sequencing

Full target design above; the work lands in slices, each independently
shippable.

### PR 1 — pure refactor (this proposal's deliverable to build first)

- `internal/model`: add the `Matcher` interface and `LegacyMatcher`; move the
  `InputEvent` struct into `model`; add `RoutingRule{Matcher, Create}` with
  custom `Marshal`/`Unmarshal` and the no-discriminator-⇒-legacy default; move
  `matchMention`/`matchAssignee` into `LegacyMatcher`.
- `internal/eventrouter`: `type InputEvent = model.InputEvent`; delete
  `rule.go`'s `MatchRule`; change `Route` to call `rule.Match(input)`; update
  `defaultRules`.
- proto: `RoutingMatcher` with only `legacy`, `reserved` the old numbers,
  rewire `Proto`/`FromProto` and the `apiserver` handlers; regen.
- webui + n8n: remap to the `legacy` oneof; **no UI behavior change**.
- Tests: a JSON round-trip table proving pre-union rows (including #840's
  label-via-`Data` shape) deserialize to `LegacyMatcher` and re-serialize
  byte-identically; port the existing `rule_test.go` cases onto
  `LegacyMatcher.Match`.

**Outcome:** zero behavior change, zero data migration, identical UI. Only the
shape of the code changes.

### PR 2 — `LabelMatcher` + `InputEvent.Labels`

- Add `Labels []string` to `InputEvent`; populate it in the Atlassian webhook
  handler (the `jira:issue_updated` path from
  `proposals/draft/atlassian-label-routing-rule.md`) and any GitHub label paths.
- Add the `LabelMatcher` variant (model + proto + dispatch) and a label form in
  the webui.
- Retire #840's `Data` overload: emit a single event carrying `Labels`, and
  route on `LabelMatcher` instead of `LegacyMatcher{Prefix:…}` against `Data`.
  (Legacy rules that still use `Prefix` against `Data` keep working — this is
  additive.)

### PR 3 — `And` / `Or` / `Not` composites

- Add the three composite variants (model + recursive JSON + proto + dispatch),
  with the empty-`And`⇒true / empty-`Or`⇒false semantics noted above.
- API/n8n can author trees immediately; a webui tree editor is a follow-up.

## Trade-offs

- **Polymorphic matcher vs. polymorphic rule.** Only the match part varies; the
  `create` action is identical across kinds, so it stays on the outer
  `RoutingRule`. Making the whole rule a union would force every variant to
  re-declare `create`.
- **Inline matcher fields vs. nested `match` key.** Inlining the matcher at the
  top level (next to `create`) makes legacy rows byte-identical and is the most
  literal reading of "default to `LegacyMatcher` when the discriminator is
  absent." A nested `{"match":{…},"create":{…}}` shape is arguably cleaner but
  would require the outer unmarshaler to handle both legacy-flat and new-nested
  forms — more code for no migration benefit. Inlining chosen.
- **`InputEvent` in `model` vs. a narrow interface.** Moving `InputEvent` into
  `model` (with an `eventrouter` alias) lets variants have real
  `Match(InputEvent)` methods with zero call-site churn. A read-only interface
  keeps `InputEvent` in `eventrouter` but adds getters and must grow whenever a
  new matcher reads a new field. The alias approach is less boilerplate and
  keeps matching co-located with the data it matches.
- **Recursive `RoutingMatcher` message vs. inline `oneof`.** A standalone
  recursive message is needed for `And`/`Or`/`Not` (`repeated RoutingMatcher`).
  Starting with it — even when only `legacy` exists — avoids a breaking proto
  restructure when composites land.
- **Backward compatibility.** No migration: the no-`kind`-⇒-legacy default plus
  inlined fields mean existing JSONB rows deserialize and re-serialize
  unchanged, preserving every current match including #840's `Data` routing.

## Open Questions

1. **`InputEvent` home.** Moving a transient, runtime event type into the
   `model` package (which otherwise holds persisted domain types) is a slight
   smell. The alias keeps churn at zero, but is `model` the right home, or
   should matching live in a small dedicated package both `model` and
   `eventrouter` import? (Recommendation: move to `model`; revisit only if the
   package grows awkward.)
2. **Unknown `kind` on read.** `unmarshalMatcher` errors on an unrecognized
   `kind`. If a newer server writes a `kind` an older server later reads (during
   a rollback), that org's rules fail to load. Acceptable given coordinated
   deploys, or should unknown kinds degrade to "never match" + a warning?
3. **Composite UI.** PR 3 ships composites without a webui tree editor (API/n8n
   only). Confirm that's an acceptable interim, or should the editor land in the
   same slice?
4. **`reserved` vs. reuse of proto field numbers.** The proposal `reserved`s the
   old flat-field numbers on `RoutingRule`. Confirm no external/un-regenerated
   consumer still reads them (the webui and n8n node both regenerate).
