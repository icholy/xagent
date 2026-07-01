package model

import (
	"encoding/json"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestEvent_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		payload EventPayload
	}{
		{
			name:    "instruction",
			payload: &InstructionPayload{Text: "do the thing", URL: "https://example.com/1"},
		},
		{
			name:    "external",
			payload: &ExternalPayload{Description: "PR comment", URL: "https://example.com/2", Data: "xagent: go"},
		},
		{
			name:    "report",
			payload: &ReportPayload{Content: "made progress"},
		},
		{
			name: "lifecycle",
			payload: &LifecyclePayload{
				Kind:       LifecycleKindSandboxExited,
				Actor:      RunnerActor,
				FromStatus: "Running",
				ToStatus:   "Completed",
				Message:    "done",
				Fields:     []string{"status"},
			},
		},
		{
			name:    "link",
			payload: &LinkPayload{LinkID: 9, Relevance: "trigger", URL: "https://example.com/3", Title: "PR", Subscribe: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := &Event{ID: 42, TaskID: 7, OrgID: 3, Wake: true, Payload: tt.payload, CreatedAt: created}

			data, err := json.Marshal(want)
			assert.NilError(t, err)

			// The discriminator is carried inline so the sealed Payload can be
			// reconstructed on decode.
			var envelope struct {
				Type string `json:"type"`
			}
			assert.NilError(t, json.Unmarshal(data, &envelope))
			assert.Equal(t, envelope.Type, tt.payload.Type())

			var got Event
			assert.NilError(t, json.Unmarshal(data, &got))
			assert.Equal(t, got.ID, want.ID)
			assert.Equal(t, got.TaskID, want.TaskID)
			assert.Equal(t, got.OrgID, want.OrgID)
			assert.Equal(t, got.Wake, want.Wake)
			assert.Assert(t, got.CreatedAt.Equal(want.CreatedAt))
			assert.DeepEqual(t, got.Payload, want.Payload)
		})
	}
}

func TestLifecyclePayload_ProtoRoundTrip(t *testing.T) {
	t.Parallel()
	want := &LifecyclePayload{
		Kind:       LifecycleKindSandboxFailed,
		Actor:      RunnerActor,
		FromStatus: "Running",
		ToStatus:   "Failed",
		Message:    "container failed",
	}
	ev := &Event{TaskID: 7, OrgID: 3, Payload: want}

	// model -> proto -> model preserves the arm and its fields.
	got := EventFromProto(ev.Proto())
	payload, ok := got.Payload.(*LifecyclePayload)
	assert.Assert(t, ok)
	assert.DeepEqual(t, payload, want)
	assert.Equal(t, payload.Type(), EventTypeLifecycle)
}

func TestLifecyclePayload_Summary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload LifecyclePayload
		want    string
	}{
		{
			name:    "created by user",
			payload: LifecyclePayload{Kind: LifecycleKindCreated, Actor: UserActor("icholy")},
			want:    "Created by icholy",
		},
		{
			name:    "created by routing rule",
			payload: LifecyclePayload{Kind: LifecycleKindCreated, Actor: RouterActor},
			want:    "Created by routing rule",
		},
		{
			name:    "cancelled no actor name",
			payload: LifecyclePayload{Kind: LifecycleKindCancelled, Actor: RunnerActor},
			want:    "Cancelled",
		},
		{
			name:    "sandbox exited with statuses",
			payload: LifecyclePayload{Kind: LifecycleKindSandboxExited, Actor: RunnerActor, FromStatus: "Running", ToStatus: "Completed"},
			want:    "Sandbox exited (Running -> Completed)",
		},
		{
			name:    "sandbox failed with message",
			payload: LifecyclePayload{Kind: LifecycleKindSandboxFailed, Actor: RunnerActor, Message: "boom"},
			want:    "Sandbox failed: boom",
		},
		{
			name:    "updated with fields by user",
			payload: LifecyclePayload{Kind: LifecycleKindUpdated, Actor: UserActor("icholy"), Fields: []string{"name", "status"}},
			want:    "Updated name, status by icholy",
		},
		{
			name:    "updated without fields",
			payload: LifecyclePayload{Kind: LifecycleKindUpdated, Actor: RunnerActor},
			want:    "Updated",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.payload.Summary(), tt.want)
		})
	}
}
