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

Add an optional, structured **code location** to `ExternalPayload`, populate it in the
GitHub webhook extractor for review comments, and render it in the timeline. Because the
agent and UI both read the `ExternalPayload` proto directly, a single structured field
reaches both with no per-consumer plumbing.

### Which fields to keep

The minimum useful anchor is `path` + `line`. We also keep `side`, `start_line`, and
`diff_hunk`, and record whether the anchor is outdated:

| Field        | Source (go-github `PullRequestComment`) | Why keep it |
|--------------|------------------------------------------|-------------|
| `path`       | `Path`                                   | The file — required to locate the code at all. |
| `line`       | `Line`, falling back to `OriginalLine`   | The line the comment anchors to. GitHub sends `line == nil` when the comment is on an outdated diff; `original_line` still points at the line in the commit that was reviewed. |
| `start_line` | `StartLine` (`0` when single-line)       | Multi-line comments anchor to a range; without the start the agent only sees the last line. |
| `side`       | `Side` (`LEFT`/`RIGHT`)                   | Disambiguates a deletion (old side) from an addition/context (new side) on the same displayed line. |
| `diff_hunk`  | `DiffHunk`                               | The actual diff context the comment is attached to. This is the single biggest token-saver: with it the agent sees the exact code without fetching anything. |
| `outdated`   | derived: `Line == nil && OriginalLine != nil` | Tells the agent (and UI) that `line` came from the original diff and may not match the current file, so it should locate by content rather than trusting the number blindly. |

`diff_hunk` can be large for a wide multi-line range; see Open Questions on capping.

We deliberately do **not** carry `commit_id`/`original_commit_id`, `position`,
`start_side`, or `subject_type` in the first cut — they add surface without clearly
paying for themselves. `commit_id` is a candidate for a follow-up (see Open Questions).

### Where it lives on the event

A new `CodeLocation` message, referenced as an optional field on `ExternalPayload`.

`proto/xagent/v1/xagent.proto`:

```protobuf
message ExternalPayload {
  string description = 1;
  string url = 2;
  string data = 3;
  CodeLocation location = 4; // optional; set only for code-anchored events
}

// CodeLocation anchors an external event to a place in a diff/file. It is set
// today only for GitHub PR review comments; it is source-agnostic so a future
// code-review source (e.g. Bitbucket) can populate the same shape.
message CodeLocation {
  string path = 1;       // file path within the repo
  int32 line = 2;        // anchored line (resolved line, else original_line)
  int32 start_line = 3;  // start of a multi-line range; 0 when single-line
  string side = 4;       // "LEFT" or "RIGHT"; empty when unknown
  string diff_hunk = 5;  // the diff context the comment is attached to
  bool outdated = 6;     // true when line was resolved from original_line
}
```

The Go mirror in `internal/model/event.go` gains a matching struct and wires it through
`SetPayloadProto` / `EventPayloadFromProto`. It is `omitempty` so existing JSONB rows
(which have no `location` key) round-trip unchanged:

```go
type CodeLocation struct {
    Path      string `json:"path"`
    Line      int32  `json:"line"`
    StartLine int32  `json:"start_line,omitempty"`
    Side      string `json:"side,omitempty"`
    DiffHunk  string `json:"diff_hunk,omitempty"`
    Outdated  bool   `json:"outdated,omitempty"`
}

type ExternalPayload struct {
    Description string        `json:"description"`
    URL         string        `json:"url"`
    Data        string        `json:"data"`
    Location    *CodeLocation `json:"location,omitempty"`
}
```

**Why a structured field and not the string channels:**

- **Not `Data`** — `Data` is the comment body verbatim; the agent and the UI treat it as
  the human's message. Prepending `path:line` pollutes the body, is lossy (no room for
  `diff_hunk` without swamping the message), and forces every consumer to parse it back
  out.
- **Not (only) `Description`** — `Description` is a one-line human summary. We *do* fold
  the path and line into it as a cheap readability win (below), but it can't carry the
  diff hunk or a machine-readable side/outdated flag. Structured data belongs in a field.
- **`ExternalPayload.location`, not a new event arm** — external events are already a
  single self-contained payload type; the location is inherently optional and only ever
  set for a subset. A whole new oneof arm would be far heavier for an optional attribute.
  The field simply stays unset (nil) for issue comments, labels, assignments, etc.

### Carrying it through the router

`ExternalPayload` is built inside `Router.attach`, which today reads only three fields
off `InputEvent`. `InputEvent` therefore needs to carry the location too — `Meta` won't
do, because `attach` drops it. Add a field alongside `Data`:

```go
// eventrouter.InputEvent
Location *CodeLocation // optional; copied into the persisted ExternalPayload
```

and in `attach`:

```go
Payload: &model.ExternalPayload{
    Description: input.Description,
    URL:         input.URL,
    Data:        input.Data,
    Location:    input.Location,
},
```

`eventrouter` already imports `model` for `model.Event`, so `CodeLocation` lives in
`model` and `InputEvent.Location` is `*model.CodeLocation`. The router does not interpret
it — it only forwards it, the same way it forwards `Data`.

### GitHub extractor

In the `*github.PullRequestReviewCommentEvent` arm of `toInputEvent`, build the location
from the comment and fold `path:line` into the description:

```go
c := event.Comment
loc := &model.CodeLocation{
    Path:      c.GetPath(),
    StartLine: int32(c.GetStartLine()),
    Side:      c.GetSide(),
    DiffHunk:  c.GetDiffHunk(),
}
if l := c.GetLine(); l != 0 {
    loc.Line = int32(l)
} else {
    loc.Line = int32(c.GetOriginalLine())
    loc.Outdated = true
}

description := fmt.Sprintf("%s reviewed PR #%d", login, number)
if loc.Path != "" {
    description = fmt.Sprintf("%s reviewed PR #%d (%s:%d)", login, number, loc.Path, loc.Line)
}
// ... InputEvent{ ..., Description: description, Location: loc }
```

The description enrichment means even the existing timeline row and the existing `data`
channel immediately read better, before any UI change lands.

### Web UI

`ExternalPayload.location` flows to the generated TS client automatically. The timeline
needs three small changes:

1. `webui/src/lib/timeline.ts` — add an optional `location` shape to the `external`
   `TimelineItem` variant and populate it in `eventsToTimeline` from the proto.
2. `webui/src/components/task-timeline.tsx` (`ExternalRow`) — render a monospace
   `path:line` chip (linking to `item.url`, which already deep-links to the comment) between
   the description and the body, with a subtle "outdated" marker when `location.outdated`.
   Optionally render `diff_hunk` in a collapsed `<pre>`.
3. Run `pnpm lint` in `webui/`.

### PR review events (`pull_request_review`) and inline comments

No change needed. When a reviewer submits a review with inline comments, GitHub delivers
one `pull_request_review` (`submitted`) event whose body is the **summary** — it carries
no `path`/`line` — plus a separate `pull_request_review_comment` (`created`) event for
**each** inline comment, each carrying its own location. Enriching the review-comment arm
therefore already covers the inline-comment case end to end; the review-summary event has
no location to enrich.

### Atlassian / Jira

Not applicable. The Atlassian integration
(`internal/server/atlassianserver/webhook.go`) produces `comment_created` and
`label_added` events. Jira is an issue tracker: its comment payload
(`internal/x/atlassian/webhook.go`, `Comment{ID, Body, Author}`) has no file/line anchor,
and there is no Bitbucket integration in the tree. The `CodeLocation` message is
deliberately source-agnostic so that if a code-review source (Bitbucket PR comments) is
added later, it can populate the same field with no schema change.

### Backward compatibility

- **Stored events**: `events.payload` is schemaless JSONB; no migration. Existing rows
  have no `location` key, so the `omitempty` Go field and the unset proto field leave them
  reading exactly as before.
- **Proto**: adding field 4 to `ExternalPayload` and a new message is additive; older
  clients ignore the unknown field.
- **Agent / UI**: both already tolerate an absent field — `get_my_task` simply omits it
  from the JSON, and `ExternalRow` guards on `item.location` being present.

### Out of scope: routing attrs

This proposal is about the payload the agent consumes, not new rule conditions. Adding a
`path` routing dimension (so a rule could match comments on a given file) is a separate
concern and is deferred. It would be a cheap one-liner (`Attrs["path"] = {loc.Path}`) if
desired later, but it changes the routing surface and should be proposed on its own.

## Implementation Plan

1. **Proto + model: `CodeLocation`** — Delivers: the `CodeLocation` message, the
   `ExternalPayload.location` field, regenerated proto, and the Go `CodeLocation` struct
   wired through `SetPayloadProto` / `EventPayloadFromProto` with the `omitempty` JSON tag.
   Depends on: nothing. Verifiable by: `ExternalPayload` proto↔model round-trip unit
   tests, including an old-shape payload (no `location`) unmarshalling cleanly.
2. **Router plumbing** — Delivers: `InputEvent.Location` and its copy into the persisted
   `ExternalPayload` in `Router.attach`. Depends on: (1). Verifiable by: an `eventrouter`
   test that routes an `InputEvent` with a `Location` and asserts the persisted event
   carries it.
3. **GitHub extractor** — Delivers: `toInputEvent` populating `Location` for
   `pull_request_review_comment` (path, resolved line with outdated fallback, start_line,
   side, diff_hunk) and folding `path:line` into the description. Depends on: (2).
   Verifiable by: a webhook table test asserting the built `InputEvent` for a sample
   review-comment payload (both current-diff and outdated-diff cases).
4. **Web UI** — Delivers: the `location` shape in `timeline.ts` and the `path:line` chip
   (plus outdated marker / optional diff-hunk) in `ExternalRow`. Depends on: (1).
   Verifiable by: rendering the timeline against an event with a `location`, and
   `pnpm lint`.

## Trade-offs

- **Structured field vs. stuffing the string channels.** Chosen: a typed `location`.
  It keeps the comment body clean, lets the UI render a real chip/link and the agent read
  discrete fields, and carries the diff hunk without swamping `data`. Cost: proto + model +
  router + UI churn across four thin layers. The description enrichment is a hedge — it
  improves the human line even for consumers that never read the structured field.
- **One generic field vs. a GitHub-specific type or a new event arm.** Chosen: a generic
  `CodeLocation` on the existing `ExternalPayload`. Location is an optional attribute of a
  code-anchored event, not a new kind of event; a new oneof arm would force every consumer
  to branch. Cost: the field is nil for the majority of external events — acceptable, since
  `omitempty`/unset keeps it invisible.
- **Include `diff_hunk` vs. not.** Chosen: include it. It is the biggest single saver — the
  agent sees the anchored code with zero API calls. Cost: it can be sizeable; it is optional
  and only present on review comments, and can be capped (Open Questions).

## Open Questions

- **Cap `diff_hunk`?** A comment on a wide multi-line range can produce a long hunk. Do we
  store it whole, or truncate to the last N lines around the anchor? Leaning whole in the
  first cut, revisit if payloads get large.
- **Carry the commit SHA?** `commit_id`/`original_commit_id` would let the agent
  `git show` the exact reviewed revision, which matters when the anchor is outdated. Worth a
  follow-up field if the `outdated` flag proves insufficient in practice.
- **`start_side` for multi-line ranges that cross sides?** Rare; omitted for now. Add if a
  real case shows up.
