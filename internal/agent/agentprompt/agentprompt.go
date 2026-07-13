// Package agentprompt renders the bootstrap prompt sent to the agent. It takes
// its inputs as parameters so it depends only on the proto package and not on
// internal/agent (avoiding an import cycle).
package agentprompt

import (
	_ "embed"
	"encoding/json"
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

// RenderBrief renders a task's full brief for injection into the first-run
// prompt. It deliberately DUPLICATES the field set agentmcp.taskDetailsToMap
// exposes (id, name, status, workspace, namespace, url, instructions, links,
// events) rather than sharing it: agentprompt depends only on the proto package
// to avoid an import cycle (see the package doc), and the two renderings are
// meant to diverge — this one is free to grow a more readable form for a model
// reading it cold, while get_my_task is free to be reshaped for its own callers.
//
// Nested messages (instructions, links, events) are rendered through
// renderMessage so their whitespace is deterministic, and instructions are
// projected out of the instruction events via GetInstruction(). It is nil-safe:
// a nil resp (or nil task) renders zero values.
func RenderBrief(resp *xagentv1.GetTaskDetailsResponse) (string, error) {
	var instructions []json.RawMessage
	for _, event := range resp.GetEvents() {
		inst := event.GetInstruction()
		if inst == nil {
			continue
		}
		data, err := renderMessage(inst)
		if err != nil {
			return "", err
		}
		instructions = append(instructions, data)
	}

	links := make([]json.RawMessage, len(resp.GetLinks()))
	for i, link := range resp.GetLinks() {
		data, err := renderMessage(link)
		if err != nil {
			return "", err
		}
		links[i] = data
	}

	events := make([]json.RawMessage, len(resp.GetEvents()))
	for i, event := range resp.GetEvents() {
		data, err := renderMessage(event)
		if err != nil {
			return "", err
		}
		events[i] = data
	}

	task := resp.GetTask()
	brief := map[string]any{
		"id":           task.GetId(),
		"name":         task.GetName(),
		"status":       task.GetStatus().String(),
		"workspace":    task.GetWorkspace(),
		"namespace":    task.GetNamespace(),
		"url":          task.GetUrl(),
		"instructions": instructions,
		"links":        links,
		"events":       events,
	}
	out, err := json.MarshalIndent(brief, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
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
