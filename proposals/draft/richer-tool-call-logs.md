# Richer tool-call log output

Issue: https://github.com/icholy/xagent/issues/892

## Problem

The agent implementations in `internal/agent/` parse the streaming JSON output
from the underlying CLI and render a human-readable log that ends up in the Web
UI. For tool-call events the rendered line only contains the **tool name** and
nothing about what the tool did, e.g. `tool name=Bash`, `tool name=Read`,
`tool name=Grep`. A reader cannot tell which command ran, which file was
read/edited, what was searched for, or what arguments an MCP tool received —
even though those inputs are already present in the parsed stream event and are
simply dropped before the log line is built.

## Current data flow

The driver runs inside the task container and streams the CLI's output:

1. `agent.Driver.Run` (`internal/agent/driver.go`) constructs an `Agent` via
   `NewAgent` and calls `Prompt`. The driver's logger is `slog.Default()`
   (`internal/command/driver.go:40`), which writes to the container's
   stderr/stdout.
2. Each agent's `Prompt` launches the CLI with a streaming-JSON output format
   and scans stdout line by line:
   - `ClaudeAgent.Prompt` — `claude --output-format stream-json ... --print`
     (`internal/agent/claude.go:33-98`)
   - `CodexAgent.Prompt` — `codex exec --json ...` (`internal/agent/codex.go`)
   - `CursorAgent.Prompt` — `cursor-agent --output-format stream-json ...`
     (`internal/agent/cursor.go`)
3. For each line, the agent calls `handleStreamEvent(line []byte) bool`, which
   unmarshals the JSON and emits a compact, human-readable record through
   `a.log` (slog). If the line can't be parsed as a known event, the agent
   falls back to logging the raw line (`a.log.Info("output", "line", ...)`).
4. Those slog records become the task logs visible in the Web UI.

When `a.verbose` is set, the parsing is bypassed entirely and every raw line is
logged verbatim. The summarization work described here only applies to the
non-verbose path.

Two of the six agents do **not** parse a structured stream and are out of scope:

- `CopilotAgent` (`internal/agent/copilot.go`) runs `copilot --silent` and logs
  each raw stdout line.
- `SloppyAgent` (`internal/agent/sloppy.go`) logs each raw stdout line.
- `DummyAgent` is a test fixture.

The three agents with a `handleStreamEvent` that renders tool calls — and that
this proposal targets — are **Claude, Codex, and Cursor**.

### Where tool calls are rendered today

`internal/agent/claude.go:108-150`:

```go
func (a *ClaudeAgent) handleStreamEvent(data []byte) bool {
    var event struct {
        Type    string `json:"type"`
        Message struct {
            Content []struct {
                Type      string `json:"type"`
                Text      string `json:"text"`
                Name      string `json:"name"`
                Input     any    `json:"input"`
                ToolUseID string `json:"tool_use_id"`
                Content   string `json:"content"`
                IsError   bool   `json:"is_error"`
            } `json:"content"`
        } `json:"message"`
    }
    ...
    case "assistant":
        for _, block := range event.Message.Content {
            switch block.Type {
            case "text":
                if block.Text != "" {
                    a.log.Info("text", "content", block.Text)
                }
            case "tool_use":
                a.log.Info("tool", "name", block.Name) // <-- input discarded
            }
        }
    ...
}
```

`internal/agent/codex.go:168-208`:

```go
case "function_call":
    if event.Item.Name != "" {
        a.log.Info("tool", "name", event.Item.Name) // <-- Item.Arguments discarded
    }
```

`internal/agent/cursor.go:170-226`:

```go
case "tool_call":
    toolName := "unknown"
    switch {
    case event.ToolCall.ReadToolCall != nil:
        toolName = "read"
    case event.ToolCall.WriteToolCall != nil:
        toolName = "write"
    case event.ToolCall.EditToolCall != nil:
        toolName = "edit"
    case event.ToolCall.BashToolCall != nil:
        toolName = "bash"
    }
    a.log.Info("tool", "name", toolName, ...) // <-- the typed sub-object payload is discarded
}
```

## Tool-call event structure per provider

The three providers expose the tool input differently, which is the main reason
a shared summarizer needs a small per-provider adapter at the call site.

### Claude Code (`stream-json`)

A `tool_use` content block carries `name` (string) and `input` (an arbitrary
JSON object), already decoded into the `Input any` field. For well-known tools
the input keys are stable:

| Tool         | Key input fields                              |
|--------------|-----------------------------------------------|
| `Bash`       | `command`, `description`                      |
| `Read`       | `file_path`, `offset`, `limit`                |
| `Edit`       | `file_path`, `old_string`, `new_string`       |
| `Write`      | `file_path`, `content`                        |
| `Grep`       | `pattern`, `path`, `glob`, `output_mode`      |
| `Glob`       | `pattern`, `path`                             |
| `LS`         | `path`                                        |
| `WebFetch`   | `url`, `prompt`                               |
| `WebSearch`  | `query`                                       |
| `Task`       | `description`, `subagent_type`                |
| `TodoWrite`  | `todos` (array)                               |

MCP tools arrive with a name of the form `mcp__<server>__<tool>` and an
arbitrary input object whose shape is defined by that server.

### Codex (`--json`)

A `function_call` item carries `name` (string) and `arguments` — a **JSON
string** (not a decoded object), per the existing `Arguments string` field in
`codex.go`. Summarization must `json.Unmarshal([]byte(arguments), &m)` first,
then reuse the same generic object summarizer. Codex's built-in tools include a
`shell` function whose arguments contain a `command` array.

### Cursor (`stream-json`)

A `tool_call` event carries a `tool_call` object with exactly one of
`readToolCall` / `writeToolCall` / `editToolCall` / `bashToolCall` set, each a
`*json.RawMessage`. Cursor nests the actual arguments under an `args` object
inside each sub-object. The set sub-object can be unmarshalled to a small map
and the relevant field extracted (e.g. `args.command`, `args.path`).

## Design

### A shared, provider-agnostic summarizer

Add a new file `internal/agent/toolsummary.go` containing one pure, dependency-
free function plus helpers:

```go
// summarizeToolInput returns a short, human-readable, single-line summary of a
// tool call's input. name is the tool name (may be an MCP name of the form
// mcp__server__tool). input is the decoded argument object (map[string]any),
// or nil if unavailable. The returned string never contains newlines and is
// length-limited.
func summarizeToolInput(name string, input map[string]any) string
```

Each agent keeps its provider-specific decoding at the call site and funnels
into this one function:

- **Claude**: `block.Input` is already `any`; assert to `map[string]any` and
  pass through.
- **Codex**: `json.Unmarshal([]byte(event.Item.Arguments), &m)` into a
  `map[string]any`, then pass `m`. The Codex tool name (`shell`) maps onto the
  generic path; `command` (array) is handled by the array renderer below.
- **Cursor**: unmarshal the single set `*json.RawMessage` into a small struct
  `{ Args map[string]any }` and pass `Args` with the derived name (`read`,
  `write`, `edit`, `bash`).

The log call then becomes:

```go
summary := summarizeToolInput(name, input)
a.log.Info("tool", "name", name, "summary", summary)
```

Keeping `name` as its own attribute preserves the existing structured field;
`summary` is the new human-readable detail. (Alternatively the summary can be
folded into a single message — see Trade-offs.)

### Special-cased well-known tools

`summarizeToolInput` switches on a normalized (case-insensitive) tool name for
the common tools and pulls out the single most informative field:

| Tool                     | Summary                                    | Example output                         |
|--------------------------|--------------------------------------------|----------------------------------------|
| `Bash` / `shell`         | the command                                | `npm test -- -run=TestFoo`             |
| `Read` / `LS`            | the path                                   | `internal/agent/claude.go`             |
| `Edit`                   | the path                                   | `internal/agent/claude.go`             |
| `Write`                  | the path (never the content)               | `proposals/draft/foo.md`               |
| `Grep`                   | pattern, with path/glob if present         | `handleStreamEvent in *.go`            |
| `Glob`                   | the pattern                                | `**/*.go`                              |
| `WebFetch`               | the url                                    | `https://example.com/x`                |
| `WebSearch`              | the query                                  | `connect rpc streaming`                |
| `Task`                   | subagent type + description                | `[explore] find the parser`            |
| `TodoWrite`              | count of todos                             | `5 todos`                              |

`Edit`/`Write` deliberately ignore `old_string`/`new_string`/`content` because
those are large and unhelpful in a one-line log.

### MCP tools

Names matching `mcp__<server>__<tool>` are detected by prefix. The summary
renders the friendly `server/tool` plus a generic object summary of the
arguments:

```
xagent/create_link  url=https://… title="My PR"
```

The server/tool split makes MCP calls readable without hardcoding any server's
schema, and the generic object summary (below) covers the arguments.

### Generic fallback for arbitrary input

For any tool not special-cased (including all MCP tools and unknown built-ins),
render the argument object compactly:

1. Iterate keys in a **stable order** (sort alphabetically; this avoids relying
   on Go map iteration order and keeps output deterministic for tests).
2. For each key, render `key=value` where the value is formatted by type:
   - **string**: quoted only if it contains spaces; truncated (see limits).
   - **number/bool**: as-is.
   - **array**: `command`-style string arrays are joined with spaces; other
     arrays render as `[n items]`.
   - **object**: `{…}` (not expanded) to keep one line.
   - **null**: skipped.
3. Join pairs with a single space, then apply the overall length limit.

Skipping objects/large arrays from expansion keeps the line bounded regardless
of input depth.

### Length, redaction, and multi-line handling

Centralize these rules in the summarizer so every provider behaves identically:

- **Single line**: replace any `\r?\n` (and runs of whitespace) in a value with
  a single space, so a multi-line `command` or `pattern` collapses to one line.
- **Per-value truncation**: cap each rendered value at ~120 runes, appending an
  ellipsis (`…`) when truncated. Truncate on rune boundaries, not bytes, to
  avoid splitting UTF-8.
- **Overall cap**: cap the whole summary at ~200 runes with a trailing `…`.
- **Redact bulky fields by omission**: large content-bearing fields
  (`content`, `old_string`, `new_string`, `todos`, base64-looking blobs) are
  never rendered as values — they are either replaced by the special-case path
  (path only) or rendered as `[n items]` / `{…}` by the generic renderer.
- **Empty summary**: if there's nothing useful to show, return `""` and the log
  line falls back to just the name (current behavior), so we never regress.

Suggested constants (tunable):

```go
const (
    maxValueLen   = 120
    maxSummaryLen = 200
)
```

### Resulting log examples

Before:

```
tool name=Bash
tool name=Read
tool name=Grep
tool name=mcp__xagent__create_link
```

After:

```
tool name=Bash summary="go test ./internal/agent/ -run TestSummarize"
tool name=Read summary=internal/agent/claude.go
tool name=Grep summary="handleStreamEvent in *.go"
tool name=mcp__xagent__create_link summary="xagent/create_link url=https://github.com/… title=\"Add summaries\""
```

### Testing

`summarizeToolInput` is a pure function over `(name, map[string]any)` and is
trivially table-testable per the `testing` skill — one case per well-known
tool, MCP detection, the generic fallback, array/object handling, truncation,
and multi-line collapsing. No container or DB is required. The deterministic
key ordering makes generic-fallback output stable to assert on.

## Trade-offs

- **Shared summarizer vs. per-agent logic.** A single `summarizeToolInput`
  keeps formatting, truncation, and redaction consistent across providers and
  is independently testable. The cost is a thin per-agent decode adapter
  (Claude already has a decoded object; Codex needs a JSON-string unmarshal;
  Cursor needs to reach into its typed sub-object). This is preferred over
  duplicating formatting in three `handleStreamEvent`s.

- **Separate `summary` attribute vs. folded message.** Keeping `name` and
  `summary` as distinct slog attributes preserves the existing structured
  `name` field (anything downstream that greps for `name=` keeps working) and
  is the recommended approach. Folding into a single message
  (`a.log.Info("tool", "summary", "Bash: go test …")`) reads marginally nicer
  in a raw text log but loses the structured name. Recommendation: keep both
  attributes.

- **Special-casing vs. pure generic.** A pure generic renderer would need zero
  per-tool knowledge, but produces noisier lines (e.g. `Bash` would show
  `command=… description=…` instead of just the command). Special-casing the
  ~10 common tools yields the cleanest output where it matters most; the
  generic fallback guarantees every other tool (and every MCP tool) is still
  summarized. Recommendation: special-case the common tools, generic-fallback
  everything else.

- **Truncation length.** Longer caps show more context but make the log harder
  to scan and bloat stored log rows. The ~120/200 rune caps are a starting
  point and can be tuned; they are centralized so a single change adjusts all
  providers.

## Open questions

1. Should the summary be a separate slog attribute (`summary=…`) or folded into
   the message? (Proposal recommends separate.)
2. Should `verbose` mode also emit summaries, or remain fully raw? (Proposal
   leaves verbose untouched — raw lines only.)
3. Are the ~120/200 rune caps the right defaults for the Web UI's log column
   width, or should they be larger?
4. Cursor's `args` nesting and exact field names should be confirmed against a
   live `stream-json` capture before implementation, since the current parser
   only inspects which sub-object is set, not its contents.
