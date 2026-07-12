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
// branches: the first-run get_my_task bootstrap, a wake that injects the pending
// events as a JSON array, the bare fallback when a wake has nothing pending, and
// a wake with a workspace prompt appended.
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
			name:   "wake injects pending events",
			opts:   Options{Started: true, Events: events},
			golden: "prompt-wake-events.golden",
		},
		{
			name:   "wake with nothing pending falls back",
			opts:   Options{Started: true},
			golden: "prompt-wake-empty.golden",
		},
		{
			name:   "wake injects events with a workspace prompt appended",
			opts:   Options{Started: true, Prompt: "Custom workspace instructions.", Events: events},
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
