package agentprompt

import (
	"testing"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

// TestRenderEvent covers renderEvent across all five payload arms, asserting the
// exact markdown block each produces. The external arm is exercised both with
// source/type/data/details (the worked example in the proposal) and without
// (a pre-#1410 event, where the label line is omitted).
func TestRenderEvent(t *testing.T) {
	t.Parallel()
	// Fixed timestamps: 1_700_000_000 == 2023-11-14 22:13:20 UTC. The formatter
	// drops seconds, so each renders to the minute.
	at := func(offset int64) *timestamppb.Timestamp {
		return timestamppb.New(time.Unix(1_700_000_000+offset, 0).UTC())
	}
	tests := []struct {
		name  string
		event *xagentv1.Event
		want  string
	}{
		{
			name: "instruction",
			event: &xagentv1.Event{
				Id:        43,
				CreatedAt: at(100),
				Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{
					Text: "keep going",
					Url:  "https://github.com/icholy/xagent/issues/2",
				}},
			},
			want: "### Instruction — 2023-11-14 22:15 UTC\n" +
				"keep going\n" +
				"Source: https://github.com/icholy/xagent/issues/2",
		},
		{
			name: "external without source or type",
			event: &xagentv1.Event{
				Id:        42,
				CreatedAt: at(0),
				Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
					Description: "PR review requested",
					Url:         "https://github.com/icholy/xagent/pull/1394",
				}},
			},
			want: "### PR review requested — 2023-11-14 22:13 UTC\n" +
				"Source: https://github.com/icholy/xagent/pull/1394",
		},
		{
			name: "external with source, type, content, and details",
			event: &xagentv1.Event{
				Id:        51,
				CreatedAt: at(400),
				Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
					Description: "icholy commented on driver.go",
					Source:      "github",
					Type:        "pull_request_review_comment",
					Url:         "https://github.com/icholy/xagent/pull/1394#discussion_r512",
					Data:        "This nil check needs a test before we merge — can you add one that covers the wake path?",
					Details: map[string]string{
						"path":      "internal/agent/driver.go",
						"line":      "218",
						"side":      "RIGHT",
						"diff_hunk": "@@ -215,7 +215,7 @@ func Render(opts Options) {\n-\tTaskDetails: brief,\n+\tTaskDetails: details, // nil on wake",
					},
				}},
			},
			want: "### icholy commented on driver.go — 2023-11-14 22:20 UTC\n" +
				"github · pull_request_review_comment\n" +
				"Source: https://github.com/icholy/xagent/pull/1394#discussion_r512\n" +
				"\n" +
				"This nil check needs a test before we merge — can you add one that covers the wake path?\n" +
				"\n" +
				"```json\n" +
				"{\n" +
				"  \"diff_hunk\": \"@@ -215,7 +215,7 @@ func Render(opts Options) {\\n-\\tTaskDetails: brief,\\n+\\tTaskDetails: details, // nil on wake\",\n" +
				"  \"line\": \"218\",\n" +
				"  \"path\": \"internal/agent/driver.go\",\n" +
				"  \"side\": \"RIGHT\"\n" +
				"}\n" +
				"```",
		},
		{
			name: "lifecycle",
			event: &xagentv1.Event{
				Id:        60,
				CreatedAt: at(0),
				Payload: &xagentv1.Event_Lifecycle{Lifecycle: &xagentv1.LifecyclePayload{
					Kind:       xagentv1.LifecycleKind_LIFECYCLE_KIND_SANDBOX_EXITED,
					FromStatus: "Running",
					ToStatus:   "Completed",
				}},
			},
			want: "### Sandbox exited (Running -> Completed) — 2023-11-14 22:13 UTC",
		},
		{
			name: "link",
			event: &xagentv1.Event{
				Id:        7,
				CreatedAt: at(50),
				Payload: &xagentv1.Event_Link{Link: &xagentv1.LinkPayload{
					LinkId:    7,
					Title:     "feat(agent): first-run brief",
					Relevance: "the PR this task opened",
					Url:       "https://github.com/icholy/xagent/pull/1394",
					Subscribe: true,
				}},
			},
			want: "### Link: feat(agent): first-run brief — 2023-11-14 22:14 UTC\n" +
				"the PR this task opened\n" +
				"https://github.com/icholy/xagent/pull/1394 · (subscribed)",
		},
		{
			name: "report",
			event: &xagentv1.Event{
				Id:        70,
				CreatedAt: at(100),
				Payload: &xagentv1.Event_Report{Report: &xagentv1.ReportPayload{
					Content: "Looked into the failing test; root cause is a nil cursor.",
				}},
			},
			want: "### Report — 2023-11-14 22:15 UTC\n" +
				"Looked into the failing test; root cause is a nil cursor.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, renderEvent(tt.event), tt.want)
		})
	}
}
