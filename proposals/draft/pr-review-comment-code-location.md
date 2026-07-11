# Enrich GitHub PR review-comment events with code location

Issue: https://github.com/icholy/xagent/issues/1306

## Problem

When a GitHub PR review comment wakes a task, the event the agent sees carries the
comment body and URL but not the file path or line number(s) the comment is anchored
to. To find the code under discussion the agent has to fetch the comment via the GitHub
API or guess the location from the body — a wasted round trip on every review-comment
wake, and a wrong guess whenever the body doesn't name the file.

GitHub's `pull_request_review_comment` webhook payload already carries `path`,
`line`/`original_line`, `start_line`, `side`, and `diff_hunk`. Today
`toInputEvent` (`internal/server/githubserver/webhook.go`) extracts only the comment
body (into `Data`), the comment URL, and a short description string — the location
fields are dropped in the webhook-to-event conversion. The web UI timeline is location-blind
for the same reason.

We already have the data at the webhook boundary; we're just discarding it.

## Background: what reaches the agent today

The pipeline from webhook to agent is:

1. `toInputEvent` builds an `eventrouter.InputEvent`. For a review comment it sets
   `Description`, `Data` (the body), `URL` (the comment HTML URL), `Attrs`
   (routing-only), and `Meta` (`GitHubMeta`, source-native identity).
2. `Router.Route` → `Router.attach` (`internal/eventrouter/eventrouter.go`) copies **only**
   `Description`, `URL`, and `Data` into a `model.ExternalPayload` and persists it as an
   `external` event. `Attrs` and `Meta` are routing/identity concerns and are **not
   persisted** — they never reach the agent.
3. The persisted event's payload is the typed `xagentv1.ExternalPayload`
   (`{description, url, data}`), stored as JSONB in the `events.payload` column.
4. `get_my_task` (`internal/agentmcp/xmcp.go`, `taskDetailsToMap`) marshals each event
   with `protojson` and hands the raw JSON to the agent. **Whatever fields exist on the
   `ExternalPayload` proto are surfaced to the agent for free.**
5. The web UI (`webui/src/components/task-timeline.tsx`, `ExternalRow`) renders
   `description`, `data`, and `url` from the same proto.

So the only channel that survives to both the agent and the UI is the persisted
`ExternalPayload`. Location information must live there to be useful — stuffing it into
`Meta` would drop it at step 2.

## Design

> **Revised after review.** An earlier draft added a structured, per-source
> `CodeLocation` message to `ExternalPayload`. Review feedback (icholy on #1307) was that
> this couples the shared payload to specific event types — the payload schema should not
> grow a GitHub-shaped message. This version replaces it with a **generic, source-defined
> attribute bag**: `ExternalPayload` learns nothing about "code location"; it just carries
> an opaque `map<string, string>` of extra context, and the GitHub extractor is the only
> code that knows the `path`/`line`/… keys.

Add an optional, generic **attributes** map to `ExternalPayload`, populate it in the
GitHub webhook extractor for review comments, and render it in the timeline. Because the
agent and UI both read the `ExternalPayload` proto directly, one map reaches both with no
per-consumer plumbing, and the payload stays source-agnostic.

### Which attributes to keep

The map is opaque to the schema, but the GitHub extractor and the consumers agree on a
key convention. The minimum useful anchor is `path` + `line`; we also set `side`,
`start_line`, and `diff_hunk`, and mark whether the anchor is outdated:

| Key          | Source (go-github `PullRequestComment`) | Why keep it |
|--------------|------------------------------------------|-------------|
| `path`       | `Path`                                   | The file — required to locate the code at all. |
| `line`       | `Line`, falling back to `OriginalLine`   | The line the comment anchors to. GitHub sends `line == nil` when the comment is on an outdated diff; `original_line` still points at the line in the reviewed commit. |
| `start_line` | `StartLine` (omitted when single-line)   | Multi-line comments anchor to a range; without the start the agent only sees the last line. |
| `side`       | `Side` (`LEFT`/`RIGHT`)                   | Disambiguates a deletion (old side) from an addition/context (new side) on the same displayed line. |
| `diff_hunk`  | `DiffHunk`                               | The actual diff context the comment is attached to — the single biggest token-saver: the agent sees the exact code without fetching anything. |
| `outdated`   | derived: `Line == nil && OriginalLine != nil` | Tells the agent (and UI) that `line` came from the original diff and may not match the current file, so it should locate by content rather than trusting the number blindly. |

All values are strings (`line` → `"42"`, `outdated` → `"true"`; unset keys are simply
absent). `diff_hunk` can be large for a wide multi-line range; see Open Questions on
capping.

We deliberately do **not** set `commit_id`/`original_commit_id`, `position`,
`start_side`, or `subject_type` in the first cut — they add weight without clearly paying
for themselves. `commit_id` is a candidate for a follow-up (see Open Questions). Because
the mechanism is a generic map, adding a key later is a one-line change with no schema
churn.

### Where it lives on the event

A generic string map on `ExternalPayload`.

`proto/xagent/v1/xagent.proto`:

```protobuf
message ExternalPayload {
  string description = 1;
  string url = 2;
  string data = 3;
  // attributes carries optional, source-defined key/value context for consumers
  // (the agent via get_my_task, the web UI timeline). The payload does not
  // interpret it: GitHub review comments populate path/line/side/diff_hunk; other
  // sources may set their own keys or none. Distinct from the router's matchable
  // Attrs, which are not persisted.
  map<string, string> attributes = 4;
}
```

The Go mirror in `internal/model/event.go`:

```go
type ExternalPayload struct {
    Description string            `json:"description"`
    URL         string            `json:"url"`
    Data        string            `json:"data"`
    Attributes  map[string]string `json:"attributes,omitempty"`
}
```

`omitempty` means existing JSONB rows (which have no `attributes` key) round-trip
unchanged. `SetPayloadProto` / `EventPayloadFromProto` copy the map through.

**Why a generic map, and not the alternatives:**

- **Not a structured `CodeLocation` message** (the earlier draft) — that bakes a
  GitHub-shaped, code-review-specific type into the payload every external event shares.
  The generic map keeps `ExternalPayload` free of per-source knowledge: the meaning of
  `path`/`line` lives only in the GitHub extractor (producer) and a light UI convention,
  not in the wire schema.
- **Not "another URL"** — the review-comment `url` we already store *is* a deep link: it
  anchors to the exact diff line for a human who clicks it. A second URL would duplicate
  that and still couldn't carry a machine-readable line, side, or the diff hunk. The gap
  is *consumable* location data, which the attribute map fills while the existing `url`
  keeps serving the human deep-link.
- **Not `Data`** — `Data` is the comment body verbatim; agent and UI treat it as the
  human's message. Prepending `path:line` pollutes the body, is lossy (no room for a diff
  hunk), and forces every consumer to parse it back out.
- **Not (only) `Description`** — `Description` is a one-line human summary. We *do* fold
  the path and line into it as a cheap readability win (below), but it can't carry a diff
  hunk or machine-readable keys. Structured-ish context belongs in the map.
- **Distinct from routing `Attrs`** — the router already has
  `Attrs map[string][]string`, but those are matchable dimensions consumed by the matcher
  and **dropped after routing** (see the task's "keep routing separate" constraint). The
  new `attributes` map is persisted payload the router does not interpret; it is not a
  routing dimension, so nothing here adds `path`/`diff_hunk` to the rule surface.

### Carrying it through the router

`ExternalPayload` is built inside `Router.attach`, which today reads only three fields
off `InputEvent`. `InputEvent` therefore needs to carry the map too — `Meta` won't do,
because `attach` drops it. Add a field:

```go
// eventrouter.InputEvent

// Attributes is source-defined key/value context copied verbatim into the
// persisted ExternalPayload for consumers (agent, UI). It is distinct from
// Attrs: Attrs are matchable routing dimensions the matcher reads and the
// router drops after routing; Attributes are persisted payload the router does
// not interpret.
Attributes map[string]string
```

and in `attach`:

```go
Payload: &model.ExternalPayload{
    Description: input.Description,
    URL:         input.URL,
    Data:        input.Data,
    Attributes:  input.Attributes,
},
```

The router forwards it the same way it forwards `Data`, without interpreting it. (The
near-identical names `Attrs` vs `Attributes` are a readability smell — see Open Questions.)

### GitHub extractor

In the `*github.PullRequestReviewCommentEvent` arm of `toInputEvent`, build the map from
the comment and fold `path:line` into the description:

```go
c := event.Comment
line := c.GetLine()
outdated := false
if line == 0 { // GitHub sends null line for comments on an outdated diff
    line = c.GetOriginalLine()
    outdated = true
}
attrs := map[string]string{
    "path": c.GetPath(),
    "line": strconv.Itoa(line),
}
if s := c.GetStartLine(); s != 0 {
    attrs["start_line"] = strconv.Itoa(s)
}
if s := c.GetSide(); s != "" {
    attrs["side"] = s
}
if h := c.GetDiffHunk(); h != "" {
    attrs["diff_hunk"] = h
}
if outdated {
    attrs["outdated"] = "true"
}

description := fmt.Sprintf("%s reviewed PR #%d", login, number)
if path := c.GetPath(); path != "" {
    description = fmt.Sprintf("%s reviewed PR #%d (%s:%d)", login, number, path, line)
}
// ... InputEvent{ ..., Description: description, Attributes: attrs }
```

The description enrichment means even the existing timeline row and the existing `data`
channel immediately read better, before any UI change lands.

### Web UI

`ExternalPayload.attributes` flows to the generated TS client automatically. The timeline
needs three small changes:

1. `webui/src/lib/timeline.ts` — add an optional `attributes` map to the `external`
   `TimelineItem` variant and populate it in `eventsToTimeline` from the proto.
2. `webui/src/components/task-timeline.tsx` (`ExternalRow`) — when `attributes.path` is
   present, render a monospace `path:line` chip (linking to `item.url`, which already
   deep-links to the comment) between the description and the body, with a subtle
   "outdated" marker when `attributes.outdated === "true"`. Optionally render `diff_hunk`
   in a collapsed `<pre>`. The UI applies a light convention over well-known keys; the
   schema stays generic.
3. Run `pnpm lint` in `webui/`.

### PR review events (`pull_request_review`) and inline comments

No change needed. When a reviewer submits a review with inline comments, GitHub delivers
one `pull_request_review` (`submitted`) event whose body is the **summary** — it carries
no `path`/`line` — plus a separate `pull_request_review_comment` (`created`) event for
**each** inline comment, each carrying its own location. Enriching the review-comment arm
therefore already covers the inline-comment case end to end; the review-summary event has
no location to enrich.

### Atlassian / Jira

Not applicable today, and the generic map means no schema decision is forced. The
Atlassian integration (`internal/server/atlassianserver/webhook.go`) produces
`comment_created` and `label_added` events. Jira is an issue tracker: its comment payload
(`internal/x/atlassian/webhook.go`, `Comment{ID, Body, Author}`) has no file/line anchor,
and there is no Bitbucket integration in the tree. If a code-review source (Bitbucket PR
comments) is added later, its extractor simply sets the same `path`/`line`/… keys on the
same `attributes` map — no proto or model change.

### Backward compatibility

- **Stored events**: `events.payload` is schemaless JSONB; no migration. Existing rows
  have no `attributes` key, so the `omitempty` Go field and the unset proto field leave
  them reading exactly as before.
- **Proto**: adding a `map` field 4 to `ExternalPayload` is additive; older clients ignore
  the unknown field.
- **Agent / UI**: both already tolerate an absent field — `get_my_task` omits an empty map
  from the JSON, and `ExternalRow` guards on `attributes?.path` being present.

### Out of scope: routing attrs

This proposal is about the payload the agent consumes, not new rule conditions. Adding a
`path` routing dimension (so a rule could match comments on a given file) is a separate
concern and is deferred. It would be a cheap addition to the routing `Attrs` if desired
later, but it changes the routing surface and should be proposed on its own. The persisted
`attributes` map is intentionally kept distinct from routing `Attrs` for exactly this
reason.

## Implementation Plan

1. **Proto + model: `attributes` map** — Delivers: the `ExternalPayload.attributes`
   map field, regenerated proto, and the Go `map[string]string` mirror wired through
   `SetPayloadProto` / `EventPayloadFromProto` with the `omitempty` JSON tag. Depends on:
   nothing. Verifiable by: `ExternalPayload` proto↔model round-trip unit tests, including
   an old-shape payload (no `attributes`) unmarshalling cleanly.
2. **Router plumbing** — Delivers: `InputEvent.Attributes` and its copy into the persisted
   `ExternalPayload` in `Router.attach`. Depends on: (1). Verifiable by: an `eventrouter`
   test that routes an `InputEvent` with `Attributes` and asserts the persisted event
   carries them.
3. **GitHub extractor** — Delivers: `toInputEvent` populating `Attributes` for
   `pull_request_review_comment` (path, resolved line with outdated fallback, start_line,
   side, diff_hunk) and folding `path:line` into the description. Depends on: (2).
   Verifiable by: a webhook table test asserting the built `InputEvent` for a sample
   review-comment payload (both current-diff and outdated-diff cases).
4. **Web UI** — Delivers: the `attributes` map in `timeline.ts` and the `path:line` chip
   (plus outdated marker / optional diff-hunk) in `ExternalRow`. Depends on: (1).
   Verifiable by: rendering the timeline against an event with `attributes`, and
   `pnpm lint`.

## Trade-offs

- **Generic attribute map vs. a structured per-source message.** Chosen (after review): a
  generic `map<string, string>`. It keeps `ExternalPayload` free of GitHub/code-review
  specifics — the shared payload is what every external event uses, so it shouldn't grow a
  type only review comments populate. Cost: values are stringly-typed and the key set is a
  convention rather than a schema, so producer and consumers must agree on names; the
  consumer does a little more work than reading a typed field.
- **Attribute map vs. "another URL".** Chosen: the map. The existing `url` already
  deep-links a human to the exact line; a second URL can't carry a machine-readable
  line/side or the diff hunk, which is precisely the consumable data the agent needs.
- **One generic map vs. the string channels (`Data`/`Description`).** Chosen: the map,
  with a `path:line` fold into `Description` as a hedge. The map keeps the comment body
  clean and carries the diff hunk without swamping `data`, while the description fold
  improves the human line even for consumers that never read the map.
- **Include `diff_hunk` vs. not.** Chosen: include it — the biggest single saver, giving
  the agent the anchored code with zero API calls. Cost: it can be sizeable; it is optional
  and only present on review comments, and can be capped (Open Questions).

## Open Questions

- **`Attrs` vs `Attributes` naming.** The persisted `InputEvent.Attributes` sits right next
  to the routing `InputEvent.Attrs`, and the near-identical names invite confusion. Options:
  live with it plus clear doc comments (current plan), or rename the routing field to
  `RoutingAttrs` in a separate cleanup. Leaning on doc comments to keep this proposal
  scoped.
- **Cap `diff_hunk`?** A comment on a wide multi-line range can produce a long hunk. Do we
  store it whole, or truncate to the last N lines around the anchor? Leaning whole in the
  first cut, revisit if payloads get large.
- **Carry the commit SHA?** A `commit_id` key would let the agent `git show` the exact
  reviewed revision, which matters when the anchor is outdated. Cheap to add later given the
  generic map; deferred until the `outdated` flag proves insufficient.
