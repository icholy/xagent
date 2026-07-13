# Event-native GetTaskDetails

Issue: https://github.com/icholy/xagent/issues/1406

## Problem

`GetTaskDetails` is a leftover adapter from before tasks became an event stream.
During that re-architecture the RPC kept presenting the old task-object shape over
the new model:

- `taskDetailsToMap` (`internal/agentmcp/xmcp.go:157`) flattens the response into
  the pre-event-stream object and **reconstructs an `instructions` array** by
  projecting `GetInstruction()` out of the instruction events. Its own comment:
  *"Instructions are no longer a task field — they are instruction events … they
  are projected out of the event stream here."*
- The same projection is **copied** into two more consumers: `RenderBrief`
  (`internal/agent/agentprompt/agentprompt.go:61`) and the CLI `task list`
  (`internal/command/task_list.go:56`).

The result is a synthesized `instructions` field that does not exist on the wire,
duplicated across three renderers, presenting instructions as a task field when
they are actually events in the stream.

**Settled constraint:** no backward compatibility. This is early dev with no
external users. We reshape in place (breaking), migrate every consumer in the same
effort, and add **no** parallel RPC, deprecation path, or compat shim.

## Survey: what each consumer actually needs

The heart of this proposal is a survey of every `GetTaskDetails` consumer, because
the survey — not an assumption — decides how far the proto can move.

| Consumer | File | Reads from `Task` | Reads `events`? | Projects `instructions`? |
|---|---|---|---|---|
| `get_my_task` | `internal/agentmcp/xmcp.go:157` | `id, name, status, workspace, namespace, url` (thin subset) | yes (raw) | **yes** |
| First-run brief | `internal/agent/agentprompt/agentprompt.go:61` | same thin subset | yes (raw) | **yes** (duplicate) |
| CLI `task list` | `internal/command/task_list.go:56` | `id, name, status` | yes (raw) | **yes** (third copy) |
| `mcpserver.getTask` | `internal/server/mcpserver/mcpserver.go:184` | `id, name, workspace, runner, status, url` | instruction arm only, + separate `ListEventsByTask` for `logs` | **yes** |
| webui task detail | `webui/src/routes/tasks.$id.tsx:82` | **the full fat `Task`** (see below) + `links` | **no** | no |

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
infinite query (`useTaskTimeline` → `ListEventsByTask`, `tasks.$id.tsx:92`). So
the webui's need is the opposite of the agent's: it wants the *full header* and
*ignores the stream*.

`Task` is also shared by four other RPCs — `ListTasks`, `GetTask`,
`CreateTaskResponse`, `ListRunnerTasks` — and the runner state machine reads
`Command`, `Version`, `ShellSession`, `Workspace`, `Runner`
(`internal/runner/runner.go:191,213,437,464`).

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
in Go, three times. There is therefore **no `instructions` proto field to drop**;
the adapter lives entirely in the consumers.

So the reshape is a **Go-side** reshape, not a proto reshape. This proposal makes
that explicit rather than inventing a proto change to look like progress.

### How far to slim `Task`: not at all

Per the survey, `Task` stays exactly as it is (`proto/xagent/v1/xagent.proto:94`).
Every field is required by at least one of the webui detail page, the webui list,
or the runner. "Thin header over the stream" is achieved at the **rendering
layer** — the agent-facing renderers already project only the 6-field header the
model needs — not by amputating a message that five RPCs share.

The `Task` message's `reserved 6 // Previously: instructions` already records that
instructions left the message during the event-stream migration. Nothing further
to reserve or remove.

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

### One shared renderer, resolving the deliberate duplication

The first-run-brief work deliberately duplicated the field set between
`taskDetailsToMap` and `RenderBrief`, guarded by an import-cycle concern:
`agentprompt` depends only on the proto package so it can be imported by the
driver without pulling in `internal/agent`
(`internal/agent/agentprompt/agentprompt.go:1-4,49-56`). That duplication was
introduced *because* the two renderings were expected to diverge — a
readable-for-a-cold-model brief vs. a caller-shaped tool result.

Once the projected `instructions` is dropped, **both renderings collapse to the
same object** (thin header + `links` + raw `events`), so the divergence rationale
evaporates. This is the natural point to reconcile them.

Introduce a proto-only package `internal/taskbrief` with a single renderer:

```go
// internal/taskbrief/taskbrief.go
package taskbrief

// Render builds the event-native task brief: a thin header, links, and the raw
// event stream. Instructions are NOT projected — instruction-arm events are the
// instructions. Depends only on the proto package (no import cycle), so every
// consumer — the in-container MCP server, the first-run brief, and the CLI — can
// share it.
func Render(resp *xagentv1.GetTaskDetailsResponse) map[string]any
```

Events/links are normalized through the existing `renderMessage` path (protojson
→ `json.MarshalIndent`) so whitespace is deterministic and `get_my_task` and the
brief are byte-for-byte identical — the agent parses one format, not two, which
was already the stated goal of `RenderEvent` (`agentprompt.go:35-47`).

`renderMessage`/`RenderEvent` stay in `agentprompt` (they serve the PROMPT.md
template); `taskbrief.Render` reuses the same normalization. `agentprompt` imports
`taskbrief`, so there is still exactly one implementation of the normalization.

### Per-consumer migration

1. **`get_my_task`** — `getMyTask` (`xmcp.go:108`) calls `taskbrief.Render(resp)`;
   delete `taskDetailsToMap`. Update the tool description.
2. **First-run brief** — `RenderBrief` (`agentprompt.go:61`) becomes a thin wrapper
   that calls `taskbrief.Render` and `json.MarshalIndent`s the result; delete the
   duplicated projection body. The `RenderBrief` template func and PROMPT.md are
   unchanged.
3. **CLI `task list`** — replace the inline projection (`task_list.go:56-85`) with
   `taskbrief.Render(details)`; delete the third copy of the loop.
4. **`mcpserver.getTask`** — this is a user-facing *debug* view, not an agent brief.
   Migrate it fully event-native: present the raw event stream instead of the
   synthesized `instructions` **and** the separately-fetched `logs`. Today it makes
   two event reads — `GetTaskDetails` (instruction+external, for `instructions`)
   plus `ListEventsByTask` (report+lifecycle, for `logs`) (`mcpserver.go:227-252`).
   The event-native form is a single `ListEventsByTask` (all arms) presented as a
   raw `events` array beside the header + `links`, collapsing both projections into
   the stream — the same shape the webui timeline already shows. `Task` header
   fields it displays (`id, name, workspace, runner, status, url`) are unchanged.
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
independently, so the four migration slices can land in any order (or in parallel)
once slice 1 is in.

1. **Shared `taskbrief` renderer** — Delivers: new proto-only package
   `internal/taskbrief` with `Render(resp) map[string]any` producing the
   event-native shape (thin header + `links` + raw `events`, **no** projected
   `instructions`), reusing the deterministic normalization. Depends on: nothing.
   Verifiable by: unit test with a fixture response — asserts no `instructions`
   key, `events` present in stream order, links present, header subset correct.

2. **Migrate `get_my_task` + first-run brief onto the renderer** — Delivers:
   `getMyTask` and `RenderBrief` both call `taskbrief.Render`; `taskDetailsToMap`
   and the duplicated `RenderBrief` body are deleted; `get_my_task` description
   updated. Depends on: (1). Verifiable by: `agentmcp` and `agentprompt` tests —
   `get_my_task` output and the brief are byte-identical and contain no
   `instructions` key.

3. **Migrate CLI `task list`** — Delivers: `task_list.go` uses `taskbrief.Render`;
   the third inline projection is deleted. Depends on: (1). Verifiable by: running
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
  removing a synthesized field and collapsing three duplicate projections into one
  shared renderer. Stating "no proto change" up front prevents a reviewer from
  expecting a wire diff that the survey shows is unwarranted.
- **Drop `instructions` vs. keep it as a convenience.** Keeping a synthesized
  `instructions` array is friendlier to a model skimming the result, but it is the
  exact adapter the issue removes and it desyncs from the stream (ordering,
  external events interleaved with instructions). Event-native wins:
  instruction-arm events carry the same `text`/`url` and sit in true stream order.
- **`get_my_task` and brief byte-identical vs. free to diverge.** The first-run
  work argued for divergence. Post-reshape they are identical, so unifying removes
  duplication now; if a genuinely more readable brief form is wanted later, it can
  fork `taskbrief.Render` deliberately rather than by accident.

## Open Questions

- **CLI `task list` N+1.** `task list` calls `GetTaskDetails` once per task purely
  to render `events`/`links`. Out of scope here, but the event-native cleanup
  invites a follow-up: either drop the per-task detail fetch (list only needs the
  header) or add a batch endpoint. Flagging, not solving.
- **`mcpserver.getTask` shape.** Should the user-facing debug `get_task` reuse
  `taskbrief.Render` verbatim (map) or keep a typed struct with an `events` field?
  Leaning toward presenting the raw stream to match the webui timeline, but the
  exact struct-vs-map form is a reviewer preference to settle in slice 4.
