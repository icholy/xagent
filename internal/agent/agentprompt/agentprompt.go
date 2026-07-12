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
)

// RenderEvent marshals a single event to indented JSON. It protojson-marshals the
// event, then re-indents through json.MarshalIndent — the same path get_my_task's
// taskDetailsToMap takes — so an injected event is byte-for-byte the shape it has
// in the get_my_task events array (the agent parses one format, not two) and, as a
// bonus, is deterministic: protojson deliberately varies its inter-token
// whitespace, and routing through encoding/json normalizes it away. Registered as
// the RenderEvent template func; PROMPT.md loops over the events and joins the
// rendered objects into a JSON array.
func RenderEvent(event *xagentv1.Event) (string, error) {
	data, err := protojson.Marshal(event)
	if err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(json.RawMessage(data), "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

//go:embed PROMPT.md
var promptText string

var promptTemplate = template.Must(
	template.New("prompt").Funcs(template.FuncMap{"RenderEvent": RenderEvent}).Parse(promptText),
)

// Options are the inputs to Render.
type Options struct {
	// Started reports whether the task has run before. The first run renders the
	// get_my_task bootstrap; a subsequent run renders the wake branch.
	Started bool
	// Prompt is the workspace prompt appended at the end, if any.
	Prompt string
	// Events is the instruction + external events since the saved cursor. The wake
	// branch of the template loops over them, rendering each via the RenderEvent
	// func. It is empty on the first run and on a wake with nothing pending, in
	// which case nothing is injected.
	Events []*xagentv1.Event
}

// Render builds the bootstrap prompt sent to the agent from opts.
func Render(opts Options) (string, error) {
	var b strings.Builder
	if err := promptTemplate.Execute(&b, opts); err != nil {
		return "", err
	}
	return b.String(), nil
}
