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

// Render builds the bootstrap prompt sent to the agent. started reports whether
// the task has run before; prompt is the workspace prompt appended at the end;
// events is the instruction + external events since the saved cursor. The wake
// branch of the template loops over the events, rendering each via the
// RenderEvent func. events is empty on the first run and on a wake with nothing
// pending, in which case nothing is injected.
func Render(started bool, prompt string, events []*xagentv1.Event) (string, error) {
	var b strings.Builder
	err := promptTemplate.Execute(&b, struct {
		Started bool
		Prompt  string
		Events  []*xagentv1.Event
	}{
		Started: started,
		Prompt:  prompt,
		Events:  events,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
