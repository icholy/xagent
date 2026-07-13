package agentprompt

import (
	"encoding/json"
	"strings"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// renderEvent renders a single event as a prose-framed markdown block. It
// switches on the set payload arm and emits a header line (`### … — {time}`)
// plus an arm-specific body/footer, per the mapping in
// proposals/draft/hybrid-prompt-rendering.md. It is the hybrid renderer: the
// envelope (header, external label, Source: line) and the external content body
// are prose markdown, while the opaque external details map is emitted verbatim
// as an indented-JSON block. The returned block has no trailing newline; callers
// join blocks with blank lines. An event with no set arm renders empty.
func renderEvent(event *xagentv1.Event) string {
	ts := formatEventTime(event.GetCreatedAt())
	var lines []string
	switch arm := event.GetPayload().(type) {
	case *xagentv1.Event_Instruction:
		p := arm.Instruction
		lines = append(lines, "### Instruction — "+ts, p.GetText())
		if p.GetUrl() != "" {
			lines = append(lines, "Source: "+p.GetUrl())
		}
	case *xagentv1.Event_External:
		p := arm.External
		lines = append(lines, "### "+p.GetDescription()+" — "+ts)
		if label := externalLabel(p.GetSource(), p.GetType()); label != "" {
			lines = append(lines, label)
		}
		if p.GetUrl() != "" {
			lines = append(lines, "Source: "+p.GetUrl())
		}
		if p.GetData() != "" {
			lines = append(lines, "", p.GetData())
		}
		if len(p.GetDetails()) > 0 {
			lines = append(lines, "", "```json", renderDetails(p.GetDetails()), "```")
		}
	case *xagentv1.Event_Lifecycle:
		lines = append(lines, "### "+lifecycleSummary(event)+" — "+ts)
	case *xagentv1.Event_Link:
		p := arm.Link
		lines = append(lines, "### Link: "+p.GetTitle()+" — "+ts, p.GetRelevance())
		footer := p.GetUrl()
		if p.GetSubscribe() {
			footer += " · (subscribed)"
		}
		lines = append(lines, footer)
	case *xagentv1.Event_Report:
		lines = append(lines, "### Report — "+ts, arm.Report.GetContent())
	default:
		return ""
	}
	return strings.Join(lines, "\n")
}

// formatEventTime renders an event timestamp as a deterministic, human-readable
// UTC line — e.g. "2023-11-14 22:15 UTC" — rather than RFC3339. It formats in
// UTC ourselves so the output is stable across processes, and the web UI
// timeline already speaks this format. It is nil-safe (a nil timestamp renders
// the Unix epoch).
func formatEventTime(ts *timestamppb.Timestamp) string {
	return ts.AsTime().UTC().Format("2006-01-02 15:04") + " UTC"
}

// externalLabel builds the "{source} · {type}" label line for an external event
// (from #1410). The source is display-name-cased (github → GitHub) with a raw
// fallback; either field is omitted when empty, so a pre-#1410 event with both
// empty yields "" and the caller drops the label line entirely.
func externalLabel(source, eventType string) string {
	var parts []string
	if source != "" {
		parts = append(parts, sourceDisplayName(source))
	}
	if eventType != "" {
		parts = append(parts, eventType)
	}
	return strings.Join(parts, " · ")
}

// sourceDisplayName maps an external source string onto its display name,
// mirroring the web UI's externalSourceStyle. Unknown sources fall back to the
// raw string.
func sourceDisplayName(source string) string {
	switch strings.ToLower(source) {
	case "github":
		return "GitHub"
	case "jira":
		return "Jira"
	default:
		return source
	}
}

// renderDetails marshals an external event's opaque details map as one
// indented-JSON block. The map is source-defined and opaque, so it is rendered
// untouched — json.MarshalIndent sorts the keys and the renderer promotes none
// of them. map[string]string never fails to marshal.
func renderDetails(details map[string]string) string {
	out, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(out)
}

// lifecycleSummary renders a lifecycle event's human-readable summary line
// (e.g. "Sandbox exited (Running -> Completed)"). It reuses
// model.LifecyclePayload.Summary — the same line the timeline shows — so the
// model needn't decode the lifecycle enum.
func lifecycleSummary(event *xagentv1.Event) string {
	if p, ok := model.EventPayloadFromProto(event).(*model.LifecyclePayload); ok {
		return p.Summary()
	}
	return ""
}
