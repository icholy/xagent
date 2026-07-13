// Package agentprompt renders the bootstrap prompt sent to the agent. It takes
// its inputs as parameters so it depends only on the proto package and not on
// internal/agent (avoiding an import cycle).
package agentprompt

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
	"text/template"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// renderMessage marshals a single proto message to normalized, indented JSON. It
// protojson-marshals the message, then re-indents through json.MarshalIndent:
// protojson deliberately varies its inter-token whitespace per process, and
// routing through encoding/json normalizes it away so the output is
// deterministic. This is the shared normalization RenderEvent and RenderBrief
// both build on.
func renderMessage(m proto.Message) (json.RawMessage, error) {
	data, err := protojson.Marshal(m)
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(json.RawMessage(data), "", "  ")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

// RenderEvent marshals a single event to indented JSON. It reuses renderMessage —
// the same normalization path get_my_task's taskDetailsToMap takes — so an
// injected event is byte-for-byte the shape it has in the get_my_task events
// array (the agent parses one format, not two) and is deterministic. Registered
// as the RenderEvent template func; PROMPT.md loops over the events and joins the
// rendered objects into a JSON array.
func RenderEvent(event *xagentv1.Event) (string, error) {
	out, err := renderMessage(event)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// RenderBrief renders the body of a task's first-run brief: the event stream
// partitioned into a `## Context` section (external/lifecycle/report events plus
// the task's standing links) and a `## Instructions` section, so instructions
// render last. It reads the event-native GetTaskDetailsResponse directly and no
// longer emits the flattened taskDetailsToMap JSON blob or the duplicated
// instructions projection; the instruction appears once, as an instruction event
// through renderEvent. The header, how-to-work guidance, and framing sentence are
// rendered by PROMPT.md so the prose lives in the template.
//
// It shares renderSections with the wake path (renderWake) — the only difference
// is that the brief carries links, which the wake path omits. It is nil-safe: a
// nil resp renders an empty string.
func RenderBrief(resp *xagentv1.GetTaskDetailsResponse) string {
	return renderSections(resp.GetEvents(), resp.GetLinks())
}

// renderWake renders the wake update's event stream through the same
// context/instructions partition as RenderBrief, but without links: a wake
// resumes the same session, which already saw the links on its first turn.
func renderWake(events []*xagentv1.Event) string {
	return renderSections(events, nil)
}

// partitionEvents splits an event stream by arm into the context group
// (external, lifecycle, report) and the instruction group, preserving stream
// order within each group. It is what lets both the first-run brief and the wake
// update render `## Context` then `## Instructions` from one code path, so a new
// instruction always lands last regardless of which events accompany it.
func partitionEvents(events []*xagentv1.Event) (context, instructions []*xagentv1.Event) {
	for _, event := range events {
		if _, ok := event.GetPayload().(*xagentv1.Event_Instruction); ok {
			instructions = append(instructions, event)
		} else {
			context = append(context, event)
		}
	}
	return context, instructions
}

// renderSections renders an event stream as a `## Context` section followed by a
// `## Instructions` section, each event through renderEvent in stream order.
// Links (init only) render under `## Context` after the context events, since
// links are context. A section whose group is empty is omitted entirely. Both
// the first-run brief and the wake update render through this one function so
// instructions always render last.
func renderSections(events []*xagentv1.Event, links []*xagentv1.TaskLink) string {
	context, instructions := partitionEvents(events)
	var contextBlocks []string
	for _, event := range context {
		if block := renderEvent(event); block != "" {
			contextBlocks = append(contextBlocks, block)
		}
	}
	for _, link := range links {
		contextBlocks = append(contextBlocks, linkBlock(
			link.GetTitle(), formatEventTime(link.GetCreatedAt()),
			link.GetRelevance(), link.GetUrl(), link.GetSubscribe()))
	}
	var instructionBlocks []string
	for _, event := range instructions {
		if block := renderEvent(event); block != "" {
			instructionBlocks = append(instructionBlocks, block)
		}
	}
	var sections []string
	if len(contextBlocks) > 0 {
		sections = append(sections, "## Context\n\n"+strings.Join(contextBlocks, "\n\n"))
	}
	if len(instructionBlocks) > 0 {
		sections = append(sections, "## Instructions\n\n"+strings.Join(instructionBlocks, "\n\n"))
	}
	return strings.Join(sections, "\n\n")
}

// renderHeader renders the task header block: the `# Task {id} · {name}` title
// plus the workspace/namespace and task url lines. It deliberately omits status
// — a task reading this prompt is by definition running, so status is noise (see
// proposals/draft/hybrid-prompt-rendering.md). The returned block has no
// trailing newline; callers join blocks with blank lines. Nil-safe via the
// proto getters.
func renderHeader(task *xagentv1.Task) string {
	var b strings.Builder
	b.WriteString("# Task " + strconv.FormatInt(task.GetId(), 10) + " · " + task.GetName() + "\n\n")
	b.WriteString("- Workspace: " + task.GetWorkspace() + " · Namespace: " + task.GetNamespace() + "\n")
	b.WriteString("- Task: " + task.GetUrl())
	return b.String()
}

//go:embed PROMPT.md
var promptText string

var promptTemplate = template.Must(
	template.New("prompt").Funcs(template.FuncMap{
		"RenderEvent":  RenderEvent,
		"renderEvent":  renderEvent,
		"renderHeader": renderHeader,
		"RenderBrief":  RenderBrief,
		"renderWake":   renderWake,
	}).Parse(promptText),
)

// Options are the inputs to Render.
type Options struct {
	// Started reports whether the task has run before. The first run renders the
	// get_my_task bootstrap; a subsequent run renders the wake branch.
	Started bool
	// Prompt is the workspace prompt appended at the end, if any.
	Prompt string
	// Events is the instruction + external events since the saved cursor. The wake
	// branch of the template loops over them, rendering each as a markdown block
	// via the renderEvent func. It is empty on the first run and on a wake with
	// nothing pending, in which case nothing is injected.
	Events []*xagentv1.Event

	// Task carries the id and name rendered into the wake header line
	// (`# Task {id} · {name}`). It is the task the driver already fetched at the
	// top of the run, so the wake path needs no extra fetch. The proto getters are
	// nil-safe, so a nil Task renders zero values rather than panicking.
	Task *xagentv1.Task

	// TaskDetails is the full task brief rendered into the first-run prompt in
	// place of the get_my_task bootstrap instruction. It is nil on wake runs
	// (Started == true), where the wake branch renders Events instead. RenderBrief
	// is nil-safe, so a nil value renders an empty brief rather than panicking.
	TaskDetails *xagentv1.GetTaskDetailsResponse
}

// Render builds the bootstrap prompt sent to the agent from opts.
func Render(opts Options) (string, error) {
	var b strings.Builder
	if err := promptTemplate.Execute(&b, opts); err != nil {
		return "", err
	}
	return b.String(), nil
}
