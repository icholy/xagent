# Event-native GetTaskDetails

Issue: https://github.com/icholy/xagent/issues/1406

## Problem

`GetTaskDetails` is a leftover adapter from before tasks became an event stream.
During that re-architecture the RPC kept presenting the old task-object shape over
the new model: a synthesized `instructions` array, projected out of the
instruction-arm events in Go rather than carried on the wire.

That projection is copied across the RPC's JSON consumers:

- `taskDetailsToMap` (`internal/agentmcp/xmcp.go:157`) flattens the response into
  the pre-event-stream object and **reconstructs an `instructions` array** by
  projecting `GetInstruction()` out of the instruction events. Its own comment:
  *"Instructions are no longer a task field — they are instruction events … they
  are projected out of the event stream here."*
- The CLI `task list` (`internal/command/task_list.go:57`) inlines the **same**
  projection loop into its per-task flattening.
- `mcpserver.getTask` (`internal/server/mcpserver/mcpserver.go:226`) projects
  instruction events into a typed `Instructions []instruction` field on its
  user-facing debug view.

The result is a synthesized `instructions` field that does not exist on the wire,
duplicated across three renderers, presenting instructions as a task field when
they are actually events in the stream.

> **Already shipped — the first-run brief is no longer a consumer.** When this
> proposal was first written the first-run brief (`agentprompt.RenderBrief`) was a
> fourth copy of the projection. The hybrid-prompt effort (#1408) and its
> follow-up (#1421) have since **migrated the brief off the adapter entirely**:
> `RenderBrief` is gone, and the first-run prompt now renders the task header,
> event stream, and links as **markdown via the PROMPT.md template**
> (`renderHeader`/`renderEvent`/`renderLink` in
> `internal/agent/agentprompt/agentprompt.go`), reading `Task`/`Events`/`Links`
> directly with **no** `instructions` projection and **no** `map[string]any`. See
> `proposals/implemented/hybrid-prompt-rendering.md`. That migration also **deleted
> `renderMessage` and `RenderEvent`** from `agentprompt` — the protojson →
> `json.MarshalIndent` normalization this proposal originally planned to reuse no
> longer exists. The scope below is trimmed accordingly: the brief is done, and a
> shared renderer must carry its own normalization.

**Settled constraint:** no backward compatibility. This is early dev with no
external users. We reshape in place (breaking), migrate every consumer in the same
effort, and add **no** parallel RPC, deprecation path, or compat shim.

## Survey: what each consumer actually needs

The heart of this proposal is a survey of every `GetTaskDetails` consumer, because
the survey — not an assumption — decides how far the proto can move.

| Consumer | File | Reads from `Task` | Reads `events`? | Projects `instructions`? |
|---|---|---|---|---|
| `get_my_task` | `internal/agentmcp/xmcp.go:157` | `id, name, status, workspace, namespace, url` (thin subset) | yes (raw) | **yes** |
| CLI `task list` | `internal/command/task_list.go:57` | `id, name, status` | yes (raw) | **yes** (duplicate) |
| `mcpserver.getTask` | `internal/server/mcpserver/mcpserver.go:184` | `id, name, workspace, runner, status, url` | instruction arm only, + separate `ListEventsByTask` for `logs` | **yes** (typed) |
| webui task detail | `webui/src/routes/tasks.$id.tsx:83` | **the full fat `Task`** (see below) + `links` | **no** | no |
| ~~First-run brief~~ | ~~`internal/agent/agentprompt`~~ | — | — | **migrated off the adapter (#1408/#1421)** — now markdown, not a JSON consumer |

### The decisive finding: the webui needs the fat `Task`

The webui was the "verify" item in the issue, and the verification flips the
naive conclusion. `webui/src/routes/tasks.$id.tsx` binds `data.task` straight into
components that read nearly the whole message:

- `<StatusBadge task={task} />` — `status`
- `<CommandBadge task={task} />` — `command`, `version`
- `<ArchivedBadge task={task} />` — `archived`
- `<TaskActionsMenu … />` and `canOpenShell(task)` — `actions`, `shellSession`, `status`
- header strip — `name`, `namespace`, `runner`, `workspace`, `createdAt`, `updatedAt`

It does **not** read `data.events` — the timeline is a separate bidirectional
infinite query (`useTaskTimeline` → `ListEventsByTask`, `tasks.$id.tsx:98`). So
the webui's need is the opposite of the agent's: it wants the *full header* and
*ignores the stream*.

`Task` is also shared by four other RPCs — `ListTasks`, `GetTask`,
`CreateTaskResponse`, `ListRunnerTasks` — and the runner state machine reads
`Command`, `Version`, `Workspace`, `Runner`
(`internal/runner/runner.go:191,213,437,464`), with the driver reading
`shell_session` at startup to fork its run branch.

**Conclusion:** the fat `Task` message is a legitimate, shared, materialized task
*header/state* object. It cannot be slimmed for `GetTaskDetails`' sake without
breaking the webui and the runner, or forcing a second parallel message — exactly
the surface the "reshape in place" constraint tells us to avoid.

## Design

### Key realization: the wire is already event-native

`GetTaskDetailsResponse` (`proto/xagent/v1/xagent.proto:196`) is already:

```proto
message GetTaskDetailsResponse {
  Task task = 1;
  reserved 2;                  // Previously: children (child tasks removed)
  repeated Event events = 3;
  repeated TaskLink links = 4;
}
```

That is *already* "thin header + raw event stream + links." The `instructions`
array the issue calls an adapter **is not on the wire at all** — it is synthesized
in Go, once per JSON consumer. There is therefore **no `instructions` proto field
to drop**; the adapter lives entirely in the consumers.

So the reshape is a **Go-side** reshape, not a proto reshape. This proposal makes
that explicit rather than inventing a proto change to look like progress.

### How far to slim `Task`: not at all

Per the survey, `Task` stays exactly as it is (`proto/xagent/v1/xagent.proto:94`).
Every field is required by at least one of the webui detail page, the webui list,
or the runner. "Thin header over the stream" is achieved at the **rendering
layer** — the agent-facing renderers already project only the 6-field header the
model needs — not by amputating a message that five RPCs share.

The `Task` message's `reserved 6 // Previously: instructions`
(`proto/xagent/v1/xagent.proto:100`) already records that instructions left the
message during the event-stream migration. Nothing further to reserve or remove.

### The event-native shape presented to the agent

Drop the projected `instructions` key everywhere. The agent-facing JSON becomes a
thin header + `links` + the raw `events` array (instruction-arm events *are* the
instructions — the model reads them from the stream):

```json
{
  "id": 1304,
  "name": "…",
  "status": "RUNNING",
  "workspace": "xagent",
  "namespace": "",
  "url": "https://…",
  "links": [ … ],
  "events": [ { "instruction": { "text": "…", "url": "…" } }, … ]
}
```

This is the same object today minus the `instructions` key. The `get_my_task` tool
description is updated from *"instructions, links, and events"* to reflect that
instructions are the instruction-arm events.

### One shared renderer for the JSON consumers

The first-run brief already resolved its half of the duplication, but in the
*opposite* direction from a shared map: #1408/#1421 gave it a genuinely different
format — markdown blocks rendered for a model reading the task cold — rather than
the caller-shaped JSON the tool results emit. That validates the original
divergence rationale (a readable brief vs. a machine-shaped tool result are not
the same object) and takes the brief permanently out of scope here.

What remains is the duplication *among the JSON consumers themselves*.
`get_my_task`, CLI `task list`, and `mcpserver.getTask` each independently
protojson-marshal the same header + `links` + `events`, and each independently
projects `instructions` out of the stream. Once the projection is dropped, the
three flattenings collapse to the same event-native object — so this is the point
to reconcile them into one renderer.

Introduce a proto-only package `internal/taskbrief` with a single renderer:

```go
// internal/taskbrief/taskbrief.go
package taskbrief

// Render builds the event-native task brief: a thin header, links, and the raw
// event stream. Instructions are NOT projected — instruction-arm events are the
// instructions. Depends only on the proto package (no import cycle), so every
// JSON consumer — the in-container MCP server, the CLI, and the server-side debug
// view — can share it.
func Render(resp *xagentv1.GetTaskDetailsResponse) map[string]any
```

`renderMessage`/`RenderEvent` — the old protojson → `json.MarshalIndent`
normalization — were **deleted from `agentprompt`** by the brief migration, so
there is no existing normalization to reuse. `taskbrief.Render` **owns its own**:
it protojson-marshals each event/link, then re-indents through
`json.MarshalIndent`, because protojson deliberately varies its inter-token
whitespace per process and routing through `encoding/json` makes the output
deterministic. Owning it in one place keeps `get_my_task` and `task list`
byte-for-byte identical and gives the agent one format to parse, not two.

This package serves the JSON consumers only. The first-run brief does **not** use
it — it stays on its markdown PROMPT.md path, which is the correct format for its
audience.

### Per-consumer migration

1. **`get_my_task`** — `getMyTask` (`xmcp.go:108`) calls `taskbrief.Render(resp)`;
   delete `taskDetailsToMap`. Update the tool description.
2. **CLI `task list`** — replace the inline projection (`task_list.go:56-85`) with
   `taskbrief.Render(details)`; delete the duplicated loop.
3. **`mcpserver.getTask`** — this is a user-facing *debug* view, not an agent brief.
   Migrate it fully event-native: present the raw event stream instead of the
   synthesized `instructions` **and** the separately-fetched `logs`. Today it makes
   two event reads — `GetTaskDetails` (instruction+external, for `instructions`)
   plus `ListEventsByTask` (report+lifecycle, for `logs`) (`mcpserver.go:226-252`).
   The event-native form is a single `ListEventsByTask` (all arms) presented as a
   raw `events` array beside the header + `links`, collapsing both projections into
   the stream — the same shape the webui timeline already shows. `Task` header
   fields it displays (`id, name, workspace, runner, status, url`) are unchanged.
4. **First-run brief** — **no change.** Already migrated off the adapter by
   #1408/#1421; it renders markdown via PROMPT.md and no longer projects
   `instructions`. Listed here only to record that it is intentionally out of scope.
5. **webui** — **no change.** It already binds the fat `Task` + `links` and ignores
   `events`. Since neither the proto nor those fields change, the generated
   bindings and the route are untouched. (Confirmed: the only webui references to
   `getTaskDetails` are the query in `tasks.$id.tsx` and the SSE cache
   invalidation in `use-org-sse.ts` — no field is added or removed.)

### Proto change

**None.** `Task` (`:94`) and `GetTaskDetailsResponse` (`:196`) are unchanged; no
regeneration required. The reshape is entirely in the Go rendering layer. This is
the honest result of the survey and worth stating plainly, since the issue
anticipated a proto reshape: the "old task shape" being removed was a Go-side
projection, not a wire field.

## Implementation Plan

Layer cake, ordered so each slice is independently reviewable and mergeable. The
foundation adds the shared renderer; each consumer then migrates onto it
independently, so the migration slices can land in any order (or in parallel) once
slice 1 is in.

1. **Shared `taskbrief` renderer** — Delivers: new proto-only package
   `internal/taskbrief` with `Render(resp) map[string]any` producing the
   event-native shape (thin header + `links` + raw `events`, **no** projected
   `instructions`), carrying its own protojson → `json.MarshalIndent`
   normalization (the old `agentprompt` helper is gone). Depends on: nothing.
   Verifiable by: unit test with a fixture response — asserts no `instructions`
   key, `events` present in stream order, links present, header subset correct,
   whitespace deterministic across runs.

2. **Migrate `get_my_task` onto the renderer** — Delivers: `getMyTask` calls
   `taskbrief.Render`; `taskDetailsToMap` is deleted; the `get_my_task` tool
   description is updated. Depends on: (1). Verifiable by: `agentmcp` test —
   `get_my_task` output contains no `instructions` key and carries the raw event
   stream.

3. **Migrate CLI `task list`** — Delivers: `task_list.go` uses `taskbrief.Render`;
   the inline projection is deleted. Depends on: (1). Verifiable by: running
   `xagent task list` — output has raw `events`, no `instructions` key.

4. **Migrate `mcpserver.getTask` to raw events** — Delivers: `getTask` presents a
   single raw `events` array (all arms via `ListEventsByTask`) beside the header +
   `links`, dropping both the `instructions` projection and the separate `logs`
   projection. Depends on: (1) for the shared shape convention (may present its own
   struct; the point is dropping the projections). Verifiable by: `mcpserver` test
   — `get_task` output exposes the event stream and no synthesized `instructions`.

5. **(Verification-only) Confirm webui + proto untouched** — Delivers: a note /
   test-run confirming the webui detail page and the generated bindings still
   compile and render against an unchanged `GetTaskDetailsResponse`, and that
   `mise run generate` produces no diff. Depends on: nothing (can run first or
   last). Verifiable by: `pnpm lint` + `pnpm build` in `webui/`, `mise run
   generate` clean.

## Trade-offs

- **Keep `Task` fat vs. slim it / add a `TaskHeader`.** Considered introducing a
  thin `TaskHeader` message for the agent-facing path. Rejected: the webui needs
  the fat `Task` in the *same* `GetTaskDetailsResponse`, so the response must carry
  it regardless; a second message would only add surface. Slimming the shared
  `Task` would break the webui detail page and the runner. Thinness belongs in the
  renderer, and it is already there.
- **No proto change may read as "nothing to do."** The value is real but Go-side:
  removing a synthesized field and collapsing the remaining duplicate projections
  into one shared renderer. Stating "no proto change" up front prevents a reviewer
  from expecting a wire diff that the survey shows is unwarranted.
- **Drop `instructions` vs. keep it as a convenience.** Keeping a synthesized
  `instructions` array is friendlier to a model skimming the result, but it is the
  exact adapter the issue removes and it desyncs from the stream (ordering,
  external events interleaved with instructions). Event-native wins:
  instruction-arm events carry the same `text`/`url` and sit in true stream order.
- **Shared JSON renderer vs. the brief's separate markdown path.** The brief
  deliberately diverged onto markdown (#1408/#1421) because a model reading a task
  cold wants prose blocks, not a tool-shaped JSON map. That divergence is kept.
  The unification here is narrower and safe: it collapses only the three JSON
  consumers, which genuinely do emit the same object, without dragging the brief
  back into a shared shape it has outgrown.

## Open Questions

- **CLI `task list` N+1.** `task list` calls `GetTaskDetails` once per task purely
  to render `events`/`links`. Out of scope here, but the event-native cleanup
  invites a follow-up: either drop the per-task detail fetch (list only needs the
  header) or add a batch endpoint. Flagging, not solving.
- **`mcpserver.getTask` shape.** Should the user-facing debug `get_task` reuse
  `taskbrief.Render` verbatim (map) or keep a typed struct with an `events` field?
  Leaning toward presenting the raw stream to match the webui timeline, but the
  exact struct-vs-map form is a reviewer preference to settle in slice 4.
