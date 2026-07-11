package model

import (
	"encoding/json"
	"testing"

	"gotest.tools/v3/assert"
)

func TestFilterPayloads(t *testing.T) {
	t.Parallel()
	events := []*Event{
		{Payload: &InstructionPayload{Text: "first"}},
		{Payload: &LinkPayload{URL: "https://example.com"}},
		{Payload: &InstructionPayload{Text: "second"}},
		{Payload: &ExternalPayload{Description: "webhook"}},
	}

	// Only the *InstructionPayload events come back, in order.
	insts := FilterPayloads[*InstructionPayload](events)
	assert.DeepEqual(t, insts, []*InstructionPayload{
		{Text: "first"},
		{Text: "second"},
	})
}

func TestExternalPayload_ProtoRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ExternalPayload{
		Description: "icholy reviewed PR #42 (main.go:10)",
		URL:         "https://github.com/icholy/xagent/pull/42#discussion_r1",
		Data:        "please fix this",
		Details: map[string]string{
			"path":      "main.go",
			"line":      "10",
			"side":      "RIGHT",
			"diff_hunk": "@@ -1,3 +1,4 @@",
		},
	}
	ev := &Event{TaskID: 7, OrgID: 3, Payload: want}

	// model -> proto -> model preserves the arm and its fields, details included.
	got := EventFromProto(ev.Proto())
	payload, ok := got.Payload.(*ExternalPayload)
	assert.Assert(t, ok)
	assert.DeepEqual(t, payload, want)
	assert.Equal(t, payload.Type(), EventTypeExternal)
}

func TestExternalPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	want := &ExternalPayload{
		Description: "icholy reviewed PR #42 (main.go:10)",
		URL:         "https://github.com/icholy/xagent/pull/42#discussion_r1",
		Data:        "please fix this",
		Details: map[string]string{
			"path": "main.go",
			"line": "10",
		},
	}

	// model -> jsonb -> model preserves the details map.
	b, err := json.Marshal(want)
	assert.NilError(t, err)

	var got ExternalPayload
	assert.NilError(t, json.Unmarshal(b, &got))
	assert.DeepEqual(t, &got, want)
}

func TestExternalPayload_JSONOmitsEmptyDetails(t *testing.T) {
	t.Parallel()
	// A payload without details serializes without a details key, so existing
	// JSONB rows are byte-for-byte unchanged.
	b, err := json.Marshal(&ExternalPayload{Description: "d", URL: "u", Data: "body"})
	assert.NilError(t, err)
	assert.Equal(t, string(b), `{"description":"d","url":"u","data":"body"}`)
}

func TestExternalPayload_JSONOldShapeUnmarshalsClean(t *testing.T) {
	t.Parallel()
	// An old-shape row written before the details field existed (no details key)
	// unmarshals cleanly, leaving Details nil.
	const oldShape = `{"description":"d","url":"u","data":"body"}`
	var got ExternalPayload
	assert.NilError(t, json.Unmarshal([]byte(oldShape), &got))
	assert.DeepEqual(t, &got, &ExternalPayload{Description: "d", URL: "u", Data: "body"})
	assert.Assert(t, got.Details == nil)
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
