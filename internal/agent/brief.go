package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// taskBrief is the start-up context rendered into an agent's bootstrap prompt
// (#946): everything the driver already knows about why the task is being run
// — instructions, external events, links — so the agent starts informed
// instead of pulling that context via get_my_task. It renders the same
// GetTaskDetails response the get_my_task tool exposes; the driver tracks
// which events it has already injected (Config.LastEventID), so no server
// state is involved.
type taskBrief struct {
	task *xagentv1.Task
	// events are the to-agent events (instruction/external) to render, in
	// stream order. For a fresh session this is the full stream; for a
	// resumed session only the events above the config's LastEventID.
	events []*xagentv1.Event
	links  []*xagentv1.TaskLink
	// resume renders the brief as a "new activity" update for an agent that
	// already holds the previously injected context in its session, instead
	// of the full task context.
	resume bool
}

// render returns the brief as markdown. A resume brief with no events renders
// as "" — nothing new arrived, so there is nothing to say.
func (b *taskBrief) render() string {
	if b.resume && len(b.events) == 0 {
		return ""
	}
	var sb strings.Builder
	title := fmt.Sprintf("Task %d", b.task.GetId())
	if b.task.GetName() != "" {
		title += ": " + b.task.GetName()
	}
	if b.resume {
		fmt.Fprintf(&sb, "# %s — new activity\n\n", title)
		fmt.Fprintf(&sb, "The following arrived since your last run:\n")
	} else {
		fmt.Fprintf(&sb, "# %s\n\n", title)
		fmt.Fprintf(&sb, "Status: %s\n", model.TaskStatus(b.task.GetStatus()).Label())
		fmt.Fprintf(&sb, "Workspace: %s\n", b.task.GetWorkspace())
		if b.task.GetUrl() != "" {
			fmt.Fprintf(&sb, "URL: %s\n", b.task.GetUrl())
		}
	}
	var instructions []*xagentv1.InstructionPayload
	for _, e := range b.events {
		if inst := e.GetInstruction(); inst != nil {
			instructions = append(instructions, inst)
		}
	}
	if len(instructions) > 0 {
		fmt.Fprintf(&sb, "\n## Instructions\n")
		for i, inst := range instructions {
			fmt.Fprintf(&sb, "\n%d. %s\n", i+1, indentContinuation(inst.GetText(), "   "))
			if inst.GetUrl() != "" {
				fmt.Fprintf(&sb, "   Source: %s\n", inst.GetUrl())
			}
		}
	}
	if externals := b.externalEvents(); len(externals) > 0 {
		fmt.Fprintf(&sb, "\n## Events\n")
		for _, e := range externals {
			ext := e.GetExternal()
			fmt.Fprintf(&sb, "\n### %s — %s\n", e.GetCreatedAt().AsTime().UTC().Format(time.RFC3339), ext.GetDescription())
			if ext.GetUrl() != "" {
				fmt.Fprintf(&sb, "\nURL: %s\n", ext.GetUrl())
			}
			if ext.GetData() != "" {
				fmt.Fprintf(&sb, "\n%s\n", ext.GetData())
			}
		}
	}
	if !b.resume && len(b.links) > 0 {
		fmt.Fprintf(&sb, "\n## Links\n\n")
		for _, l := range b.links {
			line := l.GetUrl()
			if l.GetTitle() != "" {
				line += " — " + l.GetTitle()
			}
			if l.GetSubscribe() {
				line += " (subscribed)"
			}
			fmt.Fprintf(&sb, "- %s\n", line)
			if l.GetRelevance() != "" {
				fmt.Fprintf(&sb, "  Relevance: %s\n", l.GetRelevance())
			}
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func (b *taskBrief) externalEvents() []*xagentv1.Event {
	var events []*xagentv1.Event
	for _, e := range b.events {
		if e.GetExternal() != nil {
			events = append(events, e)
		}
	}
	return events
}

// indentContinuation indents every line after the first so multi-line text
// stays inside the list item it opens.
func indentContinuation(text, indent string) string {
	return strings.ReplaceAll(text, "\n", "\n"+indent)
}
