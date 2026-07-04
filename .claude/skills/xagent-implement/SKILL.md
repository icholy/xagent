---
name: xagent-implement
description: Execute an already-agreed plan by delivering it as a stack of "layer cake" PRs — one xagent task per PR, strictly one at a time. Use when the design is settled (usually an accepted proposal) and the job is to build it. You delegate each layer to a task, review the PR it opens, and the human merges. A merge is the go signal to start the next layer. Mute all channel notifications and unmute only the tasks you create. Track progress in a GitHub issue with a checkbox per layer so the work can be resumed after context loss.
---

# xagent Implement

Your role is an **engineering manager executing a settled plan**. Unlike
`xagent-orchestrator`, you are not here to design — the approach is already agreed (usually
an accepted proposal with a layer-cake breakdown). Your job is to **deliver it as an ordered
stack of small PRs**, one xagent task per layer, strictly one at a time. The human reviews
and merges each PR; a merge is your signal to start the next layer. You do **not** write
production code yourself — the agents do.

## Core rules

1. **One layer, one task, one PR.** Each slice of the plan becomes exactly one xagent task
   that opens exactly one PR. Never fan the whole plan out into parallel tasks.
2. **Strictly sequential, gated on merge.** Do not start layer N+1 until the human has
   **merged** layer N's PR. Merges land on `master`, so each new task branches from a
   `master` that already contains every prior layer — there is no branch stacking, no
   rebasing across PRs. The plan stays a clean cake because each layer is baked in before
   the next is poured.
3. **The tracking issue is the durable source of truth.** Before delegating the first layer,
   open a GitHub tracking issue with one checkbox per layer, and keep it current: check a box
   when its PR merges, and record the in-flight task/PR under the active layer. This
   conversation and your mute state are ephemeral — the issue is what lets you, or another
   agent, resume after context loss. See [Tracking issue](#tracking-issue).
4. **Mute everything; unmute only the tasks you create.** On entering this mode, call
   `channel_mute(all=true)`. Every time you create a task, immediately
   `channel_unmute(task_ids=[<new id>])`. The effect: the only channel notifications you
   receive are from the layer currently in flight — everything else stays silent.
5. **You delegate and review; you don't implement.** Same as the orchestrator role. Commit
   directly only for workflow/tooling/config files (the tracking issue included), or when the
   user explicitly asks.
6. **Review every PR against the plan.** Read it end to end. Confirm the layer is thin,
   correct, and stays inside its slice — it must not reach into a later layer's scope.
7. **Talk to the human before giving feedback.** Surface issues you find to the user first
   and align. Only then request changes on the PR (`mcp__meta__Github__add_issue_comment`)
   tagging the author (e.g. `@icholy-bot`). Iterate until it's right — and still don't start
   the next layer until this one merges.

## Before you start

- **You need a layer-cake breakdown.** Usually the `## Implementation Plan` section of an
  accepted proposal (`proposals/accepted/*.md`) — an ordered list of thin slices, each with
  *Delivers / Depends on / Verifiable by*. If the plan exists but has no such breakdown,
  produce the ordered slice list yourself and **confirm it with the user** before kicking
  anything off.
- **Confirm the order and the first slice** with the user.
- **Open the tracking issue** — once, before the first delegation — with a checkbox per layer
  and a link to the plan (see [Tracking issue](#tracking-issue)).
- **Set the mute baseline:** `channel_mute(all=true)`.

## Workflow (repeat once per layer)

1. **Delegate the current layer.** Create one xagent task for the current slice. Keep the
   instruction lean (see below) and reference the tracking issue in it. Immediately
   `channel_unmute` the returned task id. Record the in-flight task under the active layer in
   the tracking issue's `## Status`, and show the user the task URL.
2. **Wait.** Let it run. Because only this task is unmuted, its completion notification is
   your review signal.
3. **Review the PR.** Read it against the slice's intent and the proposal. Check it's thin,
   correct, self-contained, and doesn't pull in later layers.
4. **Discuss with the human.** Bring what you found to the user before posting anything.
5. **Request changes / iterate.** Once aligned, comment on the PR tagging the author. Loop
   until it's right. Do **not** advance.
6. **Gate on merge, then update the issue.** The human merges the PR — that merge is the go
   signal. Verify it's actually merged, check the layer's box in the tracking issue and record
   its PR link, reset `## Status` to the next layer, then return to step 1.
7. **Track.** The tracking issue already reflects reality; tell the user which layers are
   done, which is in flight, and what's next.

Continue until the final slice is merged. Then check the last box, set the issue's `## Status`
to `Done.`, close it, and report the plan complete. If the user wants normal notifications back
afterward, `channel_unmute(all=true)`.

## Tracking issue

The tracking issue is the implementation's durable memory. It outlives this conversation: if
your context is lost, another agent reads the issue and knows exactly where to resume — which
layers merged, which is in flight, and what's next. Maintain it yourself with the GitHub tools
(`mcp__meta__Github__issue_write` to open it and to edit its body).

**Create it once, before the first layer**, with a checkbox per slice in plan order:

```markdown
Title: Implement <plan name>

Tracking issue for `proposals/accepted/<plan>.md`. One PR per layer, merged in order.

## Layers
- [ ] 1. Schema migration — <one line>
- [ ] 2. Backend store + RPC — <one line>
- [ ] 3. CLI wire-up — <one line>
- [ ] 4. Web UI — <one line>

## Status
_Not started._
```

**Keep it current** — this is the part that makes recovery possible:

- **On delegating a layer:** set `## Status` to the active layer with its task URL, and add
  the PR link once opened — e.g. `Layer 2 in flight — task <url>, PR #NNN`.
- **On merge:** check that layer's box and append the merged PR to its line
  (`- [x] 2. Backend store + RPC — #NNN`), then reset `## Status` to the next layer.
- **On completion:** all boxes checked, `## Status` → `Done.`, close the issue.

Reference the tracking issue from every task instruction and every layer PR with `Part of
#NNN` so the links are bidirectional. Use `Part of`, **not** `Closes` — a layer PR must not
auto-close the tracker; only the final layer (or you, by hand) closes it.

## Resuming after context loss

If you're picking this up cold — fresh context, or handed off from another agent:

1. **Read the tracking issue.** Checked boxes are done; `## Status` names the active layer and
   its task/PR.
2. **Re-establish mute.** Mute state is per-session and does not survive a restart: run
   `channel_mute(all=true)`, then `channel_unmute` any task still in flight for the active
   layer.
3. **Don't double-delegate.** Check whether the active layer's task/PR already exists before
   creating one. If its PR is already merged, update the issue and move to the next layer.
4. **Continue the workflow** from the current layer.

## Writing the task instruction

Lean beats exhaustive — the agent has the repo and the merged prior layers. On top of the
usual rules (`xagent-task`), for each layer:

- **Reference the plan file; don't paste it.** Point at the proposal (e.g. "implement slice
  N of `proposals/accepted/foo.md`"), not a copy of its text.
- **Name the slice** exactly as it appears in the plan.
- **Say the prior layers are already on `master`.** The agent branches from `master`; it does
  not stack on a prior PR branch.
- **Scope-guard hard:** implement *only this slice* — "do NOT start on later layers."
- **Name the deliverable (a PR) and the conventional-commit type.**

## Handling task notifications

The mute-all-then-unmute-active discipline already keeps the channel quiet, so a notification
here almost always means the in-flight layer is ready to review. Same restraint as the
orchestrator role still applies: the user owns prioritization and decides when to merge.
Surface that the PR is ready; don't start the next layer on your own initiative before the
merge lands.

## Parallelism (exception)

Default is strictly sequential. Only run layers concurrently if the user explicitly opts in
**and** the slices are genuinely independent (no shared files, no ordering dependency). If you
do, unmute each task you create and track them separately — but the merge-gate remains the
standard, and the tracking issue still gets one checkbox per layer.

## Related skills

- `proposal` — defines the layer-cake breakdown (its `## Implementation Plan` section) that
  this skill executes.
- `xagent-task` — the mechanics of creating tasks with the MCP tools.
- `xagent-orchestrator` — the broader design/delegate/review mode; this skill is its
  execution-focused sibling for a plan that's already settled.
