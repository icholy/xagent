# Hybrid Prompt Rendering: Converge Init/Wake, Render Events as Markdown

Issue: https://github.com/icholy/xagent/issues/1408

Data-side companion: https://github.com/icholy/xagent/issues/1406 (reshapes the
`GetTaskDetails` *data* to be event-native). This proposal is the **rendering**
side: given the event-native data #1406 lands, how the prompt composes and
renders it. It does **not** redesign the RPC.

## Problem

The bootstrap prompt the driver hands the agent (`internal/agent/agentprompt`)
has two structurally different branches and both lean on JSON blobs:

- **First run** renders the full brief. `RenderBrief` builds the flattened
  `taskDetailsToMap` shape (`id, name, status, workspace, namespace, url,
  instructions, links, events`) and emits it as one `json.MarshalIndent` object.
  Note `instructions` is a *projection of the instruction events* — the same
  data appears twice, once re-bucketed into `instructions` and once raw inside
  `events`. This is exactly the legacy adapter shape #1406 sets out to delete.
- **Wake** renders `Events` as a **JSON array** via `RenderEvent` (protojson →
  re-indented through `encoding/json`), under the line `The task received new
  events:`.
- A third **bootstrap** branch (no brief yet) just tells the agent to go call
  `get_my_task` itself.

Three problems:

1. **The init brief is the old-task shape.** It renders the flattened
   `taskDetailsToMap` JSON — the adapter #1406 is removing — including the
   duplicated `instructions` projection. It should be event-native.
2. **JSON-only is a poor cold read.** A model opening the task for the first
   time gets a wall of protojson: nested single-key objects, RFC3339
   timestamps, `subscribe: true` booleans, quoted field names. It's parseable
   but it doesn't *frame* anything — there's no "here is what you were asked,
   here is what happened, here is what changed."
3. **Init and wake are divergent code paths.** They render the same underlying
   primitive — a task header plus a list of typed events — through two different
   templates and two different Go functions (`RenderBrief` vs `RenderEvent`),
   with duplicated standing-instruction trailers. Every change to how an event
   reads has to be made twice.

### Where this sits in the stack

This continues the arc of #946 (`wake-prompt-event-injection.md`, inject events
into the wake prompt) and #1398 (`first-run-brief-injection.md`, inject the full
brief into the first run). Those two shipped the *plumbing* — the driver now
hands the agent its context instead of making it pull it. Both deliberately
rendered **raw JSON** ("the same shape `get_my_task` returns") as the
expedient first cut. This proposal is the *rendering* follow-up they left open:
now that the data is handed over, make it read well and make the two paths one.

## Design

### Recommendation in one line

**One renderer, one structure, hybrid markdown.** Collapse `RenderBrief` and
`RenderEvent` into a single event renderer that emits a prose-framed markdown
block per event, and render both the init brief and the wake update through the
same header + event-list + trailer structure. Init and wake differ only in
*which slice of the stream* they show and *one framing sentence* — not in
format, and not in code path.

### The assumed input shape (from #1406)

This proposal assumes #1406's event-native `GetTaskDetails`: a **thin task
header** plus the **raw event stream** plus links, with the flattened
`instructions` projection gone. The event-native MCP projection
(`proposals/draft/event-native-mcp-tools.md`) already specifies a flat,
`type`-tagged event object:

```jsonc
{ "id": 43, "type": "instruction", "wake": true,
  "created_at": "2023-11-14T22:15:00Z",
  "text": "Implement the first-run brief.",
  "url": "https://github.com/icholy/xagent/issues/1398" }
```

The renderer keys off the arm discriminator (`Payload.Type()` —
`instruction`/`external`/`report`/`lifecycle`/`link`, the constants in
`internal/model/event.go`). Critically, **the renderer already reads this way
today**: it walks `*xagentv1.Event` and switches on the set oneof arm. So the
renderer can be built against the *current* proto and needs no wire change —
its only coupling to #1406 is that RenderBrief stops reading the flattened
`instructions`/task-object fields and reads the thin header + stream instead.
That makes this proposal buildable in parallel with #1406 and a clean consumer
of it.

### The event renderer: `renderEvent(*Event) string`

A single Go function, replacing `RenderEvent` and the per-message JSON
normalization in `renderMessage`. It switches on the arm and emits a markdown
block. The mapping:

| Arm | Header | Body | Footer |
| --- | --- | --- | --- |
| `instruction` | `### Instruction — {time}` | `text` | `Source: {url}` (if set) |
| `external` | `### {description} — {time}` | opaque `data` in a fenced block (if set); `details` as sub-bullets (if set) | `Source: {url}` (if set) |
| `lifecycle` | `### {summary} — {time}` | — | — (actor already folded into `summary`) |
| `link` | `### Link: {title} — {time}` | `relevance` | `{url}` · `(subscribed)` if `subscribe` |
| `report` | `### Report — {time}` | `content` | — |

Notes:

- **Timestamps** render human-readable UTC (`2023-11-14 22:15 UTC`) rather than
  RFC3339. Still fully deterministic — we format it ourselves — but easier to
  read cold. (This is the format the web UI timeline already speaks.)
- **`lifecycle`** reuses `LifecyclePayload.Summary()` (e.g. *"Sandbox exited
  (Running -> Completed)"*, *"Updated name, status by alice"*) — the same
  human line the timeline shows — so the model needn't decode an enum. This
  mirrors the `summary`-beside-structure decision in
  `event-native-mcp-tools.md`.
- **`external.details`** (GitHub review comments populate `path`/`line`/`side`/
  `diff_hunk`) render as a small sub-bullet list, and opaque `data` as a fenced
  block. This is the **hybrid** point: structure that is genuinely structured
  stays structured (a fenced diff hunk is more useful as a fenced block than as
  a JSON string), but the *envelope* around it is prose, not a JSON object.
- **`report`** never enters the brief today (reports are from-agent, not
  to-agent) but the renderer handles the arm so the same function can render a
  full timeline elsewhere without a second code path.

### The converged structure

Both prompts render through the same skeleton:

```
{header}          # Task {id} · {name}  + status/workspace/namespace/url lines
{framing}         # one sentence: "first run, full history" vs "new since last run"
{event section}   # a list of renderEvent() blocks
{links}           # init only: the task's links
{trailer}         # standing instructions (init only — see below)
{workspace prompt}
```

The **only** structural differences between init and wake:

1. **Framing sentence** — init: *"This is your first run… the full history
   follows."* wake: *"Since your last run, the task received new events."*
2. **Which events** — init shows the full to-agent slice #1406 hands the brief;
   wake shows just the events newer than the driver's saved cursor (#946).
3. **Links + trailer** — rendered on init only. The wake resumes the *same*
   session, which already read the header, the links, and the standing
   instructions on its first turn; re-injecting them every wake is noise. This
   is a concrete quality win from converging: today the standing-instruction
   block is duplicated across all three template branches.

Everything else — the header, the per-event markdown, the timestamp format — is
identical, produced by identical code.

### Before / after

#### Init (first-run brief)

**Before** (`prompt-first-run-brief.golden`, abridged):

```
Here is your task brief:

{
  "events": [
    {
      "id": "43",
      "createdAt": "2023-11-14T22:15:00Z",
      "instruction": {
        "text": "Implement the first-run brief.",
        "url": "https://github.com/icholy/xagent/issues/1398"
      }
    },
    {
      "id": "42",
      "createdAt": "2023-11-14T22:13:20Z",
      "external": {
        "description": "PR review requested",
        "url": "https://github.com/icholy/xagent/pull/1394"
      }
    }
  ],
  "id": 1302,
  "instructions": [
    {
      "text": "Implement the first-run brief.",
      "url": "https://github.com/icholy/xagent/issues/1398"
    }
  ],
  "links": [ { "id": "7", "taskId": "1302", "relevance": "the PR this task opened",
      "url": "https://github.com/icholy/xagent/pull/1394", "title": "feat(agent): first-run brief",
      "subscribe": true, "createdAt": "2023-11-14T22:14:10Z" } ],
  "name": "first-run-brief L2",
  "namespace": "team-core",
  "status": "RUNNING",
  "url": "https://xagent.choly.ca/ui/tasks/1302",
  "workspace": "xagent"
}

If the task does not have a name, use xagent:update_my_task to set one.
... (standing instructions) ...
```

**After:**

```
# Task 1302 · first-run-brief L2

- Status: Running
- Workspace: xagent · Namespace: team-core
- Task: https://xagent.choly.ca/ui/tasks/1302

This is your first run on this task. Its full history is below — you already
have everything you need and do not need to call get_my_task to begin.

## History

### Implement the first-run brief. — 2023-11-14 22:15 UTC
Instruction. Source: https://github.com/icholy/xagent/issues/1398

### PR review requested — 2023-11-14 22:13 UTC
External event. Source: https://github.com/icholy/xagent/pull/1394

## Links
- [feat(agent): first-run brief](https://github.com/icholy/xagent/pull/1394) — the PR this task opened (subscribed)

## How to work this task
If the task does not have a name, use xagent:update_my_task to set one.
If you have questions, problems, or take no action, respond on the platform from the most recent instruction or event url, suffixing your message with (task 1302).
When you create a resource (PR, issue, comment), record it with xagent:create_link and subscribe=true so you receive replies. Use subscribe=false only for reference links you didn't create.
Prefer web URLs a user can visit over API URLs.
Use xagent:report to log important observations. Your text responses are not visible to users — only tool calls are.
If you need to re-check for updates mid-run, call xagent:get_my_task.
```

(The instruction header shows the instruction text as the title with the type on
the body line; an equally reasonable variant is `### Instruction — {time}` with
the text in the body. Either is fine and is an [open question](#open-questions);
the point is prose framing over a JSON object.)

#### Wake (new events)

**Before** (`prompt-wake-events.golden`):

```
The task received new events:

[
{
  "id": "42",
  "createdAt": "2023-11-14T22:13:20Z",
  "external": {
    "description": "PR review requested",
    "url": "https://github.com/icholy/xagent/pull/1394"
  }
},
{
  "id": "43",
  "createdAt": "2023-11-14T22:15:00Z",
  "instruction": {
    "text": "keep going",
    "url": "https://github.com/icholy/xagent/issues/2"
  }
}
]

Continue working on the task.
```

**After:**

```
# Task 1302 · first-run-brief L2

Since your last run, the task received new events:

### PR review requested — 2023-11-14 22:13 UTC
External event. Source: https://github.com/icholy/xagent/pull/1394

### keep going — 2023-11-14 22:15 UTC
Instruction. Source: https://github.com/icholy/xagent/issues/2

Continue working on the task.
```

The two `### …` event blocks are **byte-for-byte the same rendering** as the
init history above — that's the convergence. A wake with nothing pending keeps
today's terse fallback (`The task was updated. Continue.`).

### Code shape

`internal/agent/agentprompt` after:

- `renderEvent(*xagentv1.Event) string` — the single arm-switch renderer above.
  Replaces `RenderEvent`, `renderMessage`, and the JSON-normalization comment
  block. No more protojson-through-`encoding/json` round trip.
- `renderHeader(*xagentv1.Task) string` and `renderLinks([]*TaskLink) string` —
  small helpers.
- `RenderBrief(resp)` becomes: header + framing + `renderEvent` over the event
  slice + links. Reads the thin header + stream from #1406, no longer builds the
  `map[string]any` with the duplicated `instructions` key.
- `PROMPT.md` collapses to one structure with a shared trailer partial. The
  `{{- if .Started -}}` split shrinks to: pick the framing sentence and the
  event slice; render links + trailer only when `!Started`.

`Options` is unchanged (`Started`, `Prompt`, `Events`, `TaskDetails`), so
`internal/agent/driver.go:218` needs no change. The rendering funcs registered
on the template change; the driver contract does not.

## Implementation Plan

A layer cake of rendering-only slices. No schema, no proto, no RPC changes here
— every layer is a pure change to `internal/agent/agentprompt` verifiable by its
golden and unit tests (`go test ./internal/agent/agentprompt/ -run
TestRenderGolden [-update]`). Layers land one at a time; the driver contract
(`Options`) is stable throughout.

1. **Hybrid event renderer.** Delivers: `renderEvent(*Event) string` covering
   all five arms (table above), plus per-arm unit tests, plus the
   human-timestamp formatter. Not yet wired into the template. Depends on:
   nothing (works against the current `Event` proto). Verifiable by: table unit
   tests, one per arm, asserting the markdown block for each.

2. **Wake path renders via `renderEvent`.** Delivers: the wake branch of
   `PROMPT.md` loops `renderEvent` instead of emitting a JSON array; add the
   header line. Depends on: (1). Verifiable by: updated `prompt-wake-events*`
   goldens; the JSON array is gone.

3. **Brief renders via `renderEvent` + header/links helpers.** Delivers:
   `RenderBrief` rewritten to header + framing + `renderEvent` list + links,
   reading the event-native thin header/stream (drops the flattened
   `instructions`/`map[string]any`). Depends on: (1); coordinates with #1406 for
   the input shape. Verifiable by: updated `prompt-first-run-brief.golden`; no
   duplicated instructions.

4. **Converge `PROMPT.md`.** Delivers: init and wake share one skeleton (header
   + framing + event list + trailer); the standing-instruction trailer is
   defined once and rendered init-only; the three-way branch shrinks to
   framing + slice selection. Depends on: (2), (3). Verifiable by: all goldens;
   the wake goldens no longer duplicate the standing-instruction block.

5. **Prompt-quality polish.** Delivers: tighten the standing-instruction wording
   (single deduplicated block, task id interpolated inline), drop the stale
   `get_my_task`-bootstrap branch if the brief is now always injected on first
   run, consistent section headers. Depends on: (4). Verifiable by: goldens +
   a read-through.

Ordering rationale: (1) is a self-contained, well-tested unit with no callers,
so it merges risk-free. (2) and (3) each independently convert one path and can
land in either order (both depend only on (1)). (4) is the convergence and needs
both paths converted first. (5) is cosmetic cleanup on top.

## Trade-offs

**Hybrid markdown vs. keeping JSON.** The current `RenderEvent` was deliberately
built to be *byte-for-byte identical* to the `get_my_task` events array so "the
agent parses one format, not two." Moving to markdown drops that parity. We
judge the parity not worth its cost: the prompt is **read by a model**, not
parsed by code — nothing does `JSON.parse` on the bootstrap prompt — whereas
`get_my_task`'s output *is* consumed structurally. The two audiences differ, so
the two renderings should differ. The existing `RenderBrief` doc comment already
anticipates exactly this ("the two renderings are meant to diverge — this one is
free to grow a more readable form for a model reading it cold"); this proposal
cashes that in.

**Full prose vs. hybrid.** A pure-prose narrative reads smoothly for one event
but gets ambiguous across many, and it discards fields a model wants verbatim
(URLs, diff hunks, status transitions). The hybrid keeps prose *framing* while
leaving genuinely structured payloads (external `details`, opaque `data`,
lifecycle transitions) as markdown structure. Rejected pure prose for fidelity;
rejected pure JSON for readability.

**One template vs. two.** Converging means one template must branch internally
(full-vs-new slice, trailer-or-not). That's a little more conditional logic in
`PROMPT.md` than two flat branches. But the alternative is what we have — every
event-rendering change made twice, and a standing-instruction block copy-pasted
three times that has already drifted between branches. One structure with a
minimal branch is the lesser evil.

**Human timestamps vs. RFC3339.** RFC3339 is machine-canonical; a model reading
cold parses `2023-11-14 22:15 UTC` faster and the web UI already speaks it. Both
are deterministic. Minor, but it's a free readability win while we're in here.

**Render client-side (driver) vs. server-side.** We keep rendering in
`agentprompt`, driven by the driver, rather than having the server hand back a
finished prompt string. Server-side rendering would couple the C2 to agent
prompt wording; keeping it driver-side keeps prompt iteration cheap and is
consistent with where #946/#1398 put it. #1406 reshapes the *data*, not the
rendering location.

## Open Questions

- **Instruction header layout.** `### {text} — {time}` (text as title, shown
  above) vs. `### Instruction — {time}` with the text in the body. Title-as-text
  scans faster in a long history; type-as-title is more uniform across arms.
  Pick one in L1.
- **Report/lifecycle in the init brief.** Today the brief carries only the
  to-agent slice (instruction + external). Should a first run also see recent
  `lifecycle`/`report` events for context (e.g. "sandbox failed last run"), or
  does that belong only to the full timeline? This is really a #1406
  *which-slice* question; the renderer handles all arms regardless.
- **Trailer on every wake?** This proposal renders the standing-instruction
  trailer init-only, assuming the wake resumes a session that already saw it. If
  a wake can ever start a *fresh* harness session (no prior context), the
  trailer must render there too — confirm against the driver's session-resume
  behavior.
