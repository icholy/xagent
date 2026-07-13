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

// firstRunFraming frames the injected brief: it tells the model this is the
// first run and its full context follows inline, so it need not call
// get_my_task to begin.
const firstRunFraming = "This is your first run on this task. Its full context is below — you already have everything you need and do not need to call get_my_task to begin."

// RenderBrief renders a task's full brief for injection into the first-run
// prompt. It reads the event-native GetTaskDetailsResponse directly — the thin
// task header, the raw event stream, and the links — and renders a header, the
// first-run framing sentence, the events as a flat renderEvent list, and the
// links. It no longer emits the flattened taskDetailsToMap JSON blob or the
// duplicated instructions projection; the instruction appears once, as an
// instruction event through renderEvent.
//
// It is nil-safe: a nil resp (or nil task) renders zero values through the
// proto getters. Events with no set arm and empty link/event slices contribute
// nothing.
func RenderBrief(resp *xagentv1.GetTaskDetailsResponse) string {
	blocks := []string{
		renderHeader(resp.GetTask()),
		firstRunFraming,
	}
	for _, event := range resp.GetEvents() {
		if block := renderEvent(event); block != "" {
			blocks = append(blocks, block)
		}
	}
	if links := renderLinks(resp.GetLinks()); links != "" {
		blocks = append(blocks, links)
	}
	return strings.Join(blocks, "\n\n")
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

// renderLinks renders the task's links, one block per link joined by blank
// lines, in the same shape as the renderEvent link arm (`### Link: {title} —
// {time}` / relevance / url · (subscribed)). It returns "" for an empty slice
// so the caller can drop the section entirely.
func renderLinks(links []*xagentv1.TaskLink) string {
	blocks := make([]string, 0, len(links))
	for _, link := range links {
		var b strings.Builder
		b.WriteString("### Link: " + link.GetTitle() + " — " + formatEventTime(link.GetCreatedAt()) + "\n")
		b.WriteString(link.GetRelevance() + "\n")
		b.WriteString(link.GetUrl())
		if link.GetSubscribe() {
			b.WriteString(" · (subscribed)")
		}
		blocks = append(blocks, b.String())
	}
	return strings.Join(blocks, "\n\n")
}

//go:embed PROMPT.md
var promptText string

var promptTemplate = template.Must(
	template.New("prompt").Funcs(template.FuncMap{
		"RenderEvent": RenderEvent,
		"renderEvent": renderEvent,
		"RenderBrief": RenderBrief,
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
