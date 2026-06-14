package model

import (
	"testing"

	"gotest.tools/v3/assert"
)

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

func TestNewLifecycleEvent(t *testing.T) {
	t.Parallel()
	task := &Task{ID: 1, OrgID: 2, Status: TaskStatusCancelled}
	ev := NewLifecycleEvent(task, LifecycleKindCancelled, UserActor("icholy"), TaskStatusPending, "")

	assert.Equal(t, ev.TaskID, int64(1))
	assert.Equal(t, ev.OrgID, int64(2))
	payload := ev.Payload.(*LifecyclePayload)
	assert.Equal(t, payload.Kind, LifecycleKindCancelled)
	assert.Equal(t, payload.Actor, Actor{Kind: ActorKindUser, Name: "icholy"})
	assert.Equal(t, payload.FromStatus, "Pending")
	assert.Equal(t, payload.ToStatus, "Cancelled")
}

func TestNewLifecycleEvent_UnspecifiedFromStatus(t *testing.T) {
	t.Parallel()
	// A freshly created task has no prior status, so from renders empty.
	task := &Task{ID: 1, OrgID: 2, Status: TaskStatusPending}
	ev := NewLifecycleEvent(task, LifecycleKindCreated, RouterActor, TaskStatusUnspecified, "")
	payload := ev.Payload.(*LifecyclePayload)
	assert.Equal(t, payload.FromStatus, "")
	assert.Equal(t, payload.ToStatus, "Pending")
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.payload.Summary(), tt.want)
		})
	}
}
