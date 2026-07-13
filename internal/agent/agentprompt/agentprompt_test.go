package agentprompt

import (
	"testing"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

// TestRenderGolden snapshots the whole rendered bootstrap prompt across its
// branches: the first-run get_my_task bootstrap (nil TaskDetails fallback), the
// first-run brief injected in place of that bootstrap, a wake that renders the
// pending events as markdown blocks, the bare fallback when a wake has nothing
// pending, and a wake with a workspace prompt appended. Both the brief and the
// wake render through the same flat renderEvent stream (no section headers),
// with links appended at the end on init only; the wake header stays thin
// (id · name only).
// Regenerate the goldens with: go test ./internal/agent/agentprompt/ -run TestRenderGolden -update
func TestRenderGolden(t *testing.T) {
	t.Parallel()
	// Fixed timestamps keep the rendered createdAt fields stable across runs.
	events := []*xagentv1.Event{
		{
			Id:        42,
			CreatedAt: timestamppb.New(time.Unix(1_700_000_000, 0).UTC()),
			Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
				Description: "PR review requested",
				Url:         "https://github.com/icholy/xagent/pull/1394",
			}},
		},
		{
			Id:        43,
			CreatedAt: timestamppb.New(time.Unix(1_700_000_100, 0).UTC()),
			Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{
				Text: "keep going",
				Url:  "https://github.com/icholy/xagent/issues/2",
			}},
		},
	}
	// The task the wake header renders (`# Task {id} · {name}`) — the same task the
	// driver already fetched at the top of the run.
	task := &xagentv1.Task{
		Id:        1302,
		Name:      "first-run-brief L2",
		Status:    xagentv1.TaskStatus_RUNNING,
		Workspace: "xagent",
		Namespace: "team-core",
		Url:       "https://xagent.choly.ca/ui/tasks/1302",
	}
	// A field-complete brief: named task with url/namespace, an instruction event,
	// an external event, and a link. Exercises every field the first-run brief
	// renders (Task, Events, Links).
	brief := &xagentv1.GetTaskDetailsResponse{
		Task: &xagentv1.Task{
			Id:        1302,
			Name:      "first-run-brief L2",
			Status:    xagentv1.TaskStatus_RUNNING,
			Workspace: "xagent",
			Namespace: "team-core",
			Url:       "https://xagent.choly.ca/ui/tasks/1302",
		},
		Events: []*xagentv1.Event{
			{
				Id:        43,
				CreatedAt: timestamppb.New(time.Unix(1_700_000_100, 0).UTC()),
				Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{
					Text: "Implement the first-run brief.",
					Url:  "https://github.com/icholy/xagent/issues/1398",
				}},
			},
			{
				Id:        42,
				CreatedAt: timestamppb.New(time.Unix(1_700_000_000, 0).UTC()),
				Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
					Description: "PR review requested",
					Url:         "https://github.com/icholy/xagent/pull/1394",
				}},
			},
		},
		Links: []*xagentv1.TaskLink{
			{
				Id:        7,
				TaskId:    1302,
				Relevance: "the PR this task opened",
				Url:       "https://github.com/icholy/xagent/pull/1394",
				Title:     "feat(agent): first-run brief",
				Subscribe: true,
				CreatedAt: timestamppb.New(time.Unix(1_700_000_050, 0).UTC()),
			},
		},
	}
	tests := []struct {
		name   string
		opts   Options
		golden string
	}{
		{
			name:   "first run bootstraps via get_my_task",
			opts:   Options{},
			golden: "prompt-first-run.golden",
		},
		{
			name:   "first run renders the task brief",
			opts:   Options{Task: brief.Task, Events: brief.Events, Links: brief.Links},
			golden: "prompt-first-run-brief.golden",
		},
		{
			name:   "wake injects pending events",
			opts:   Options{Started: true, Events: events, Task: task},
			golden: "prompt-wake-events.golden",
		},
		{
			name:   "wake with nothing pending falls back",
			opts:   Options{Started: true},
			golden: "prompt-wake-empty.golden",
		},
		{
			// A wake carrying only an instruction event: a flat single-block stream.
			name:   "wake with only an instruction",
			opts:   Options{Started: true, Task: task, Events: events[1:]},
			golden: "prompt-wake-instruction-only.golden",
		},
		{
			// A wake carrying only a context (external) event: a flat single-block stream.
			name:   "wake with only a context event",
			opts:   Options{Started: true, Task: task, Events: events[:1]},
			golden: "prompt-wake-context-only.golden",
		},
		{
			name:   "wake injects events with a workspace prompt appended",
			opts:   Options{Started: true, Prompt: "Custom workspace instructions.", Events: events, Task: task},
			golden: "prompt-wake-events-workspace.golden",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.opts)
			assert.NilError(t, err)
			golden.Assert(t, got, tt.golden)
		})
	}
}
