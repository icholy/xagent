package model_test

import (
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

// TestScheduleNext exercises Next across a DST boundary in America/Toronto. The
// point is that Next re-evaluates the cron fields in the schedule's location on
// every call (never by adding a fixed 24h), so local-time semantics hold across
// the spring-forward gap and the fall-back overlap, and the returned instant is
// always correct UTC.
func TestScheduleNext(t *testing.T) {
	t.Parallel()

	toronto, err := time.LoadLocation("America/Toronto")
	assert.NilError(t, err)

	tests := []struct {
		name     string
		cronExpr string
		timezone string
		after    time.Time
		want     time.Time // UTC
	}{
		{
			// 02:30 does not exist on the spring-forward day (2026-03-08, clocks
			// jump 02:00 -> 03:00). robfig skips it rather than firing at a
			// non-existent instant, landing on the next day's 02:30 EDT.
			name:     "spring forward gap",
			cronExpr: "30 2 * * *",
			timezone: "America/Toronto",
			after:    time.Date(2026, 3, 8, 0, 0, 0, 0, toronto),
			want:     time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC),
		},
		{
			// 01:30 occurs twice on the fall-back day (2026-11-01, clocks fall
			// 02:00 -> 01:00). Next returns the first (earliest) occurrence:
			// 01:30 EDT (UTC-4), not the later 01:30 EST (UTC-5 = 06:30 UTC).
			name:     "fall back overlap",
			cronExpr: "30 1 * * *",
			timezone: "America/Toronto",
			after:    time.Date(2026, 11, 1, 0, 0, 0, 0, toronto),
			want:     time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC),
		},
		{
			// A 09:00 local job in winter (EST, UTC-5) lands at 14:00 UTC...
			name:     "before DST is EST offset",
			cronExpr: "0 9 * * *",
			timezone: "America/Toronto",
			after:    time.Date(2026, 1, 15, 12, 0, 0, 0, toronto),
			want:     time.Date(2026, 1, 16, 14, 0, 0, 0, time.UTC),
		},
		{
			// ...and the same expression in summer (EDT, UTC-4) lands at 13:00
			// UTC. The one-hour UTC difference proves the offset is re-derived
			// from the zone each time, not carried forward.
			name:     "after DST is EDT offset",
			cronExpr: "0 9 * * *",
			timezone: "America/Toronto",
			after:    time.Date(2026, 7, 17, 12, 0, 0, 0, toronto),
			want:     time.Date(2026, 7, 18, 13, 0, 0, 0, time.UTC),
		},
		{
			// The @daily descriptor is accepted and evaluated in UTC.
			name:     "daily descriptor in UTC",
			cronExpr: "@daily",
			timezone: "UTC",
			after:    time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
			want:     time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &model.Schedule{CronExpr: tt.cronExpr, Timezone: tt.timezone}

			got, err := s.Next(tt.after)

			assert.NilError(t, err)
			assert.Assert(t, got.After(tt.after), "next %v must be strictly after %v", got, tt.after)
			assert.Equal(t, got.UTC(), tt.want)
		})
	}
}

func TestScheduleNext_Invalid(t *testing.T) {
	t.Parallel()

	badCron := &model.Schedule{CronExpr: "not a cron", Timezone: "UTC"}
	_, err := badCron.Next(time.Now())
	assert.ErrorContains(t, err, "invalid cron expression")

	badTZ := &model.Schedule{CronExpr: "0 9 * * *", Timezone: "Mars/Phobos"}
	_, err = badTZ.Next(time.Now())
	assert.ErrorContains(t, err, "invalid timezone")
}

// TestScheduleTaskAndEvents pins the schedule -> task/events mapping a fire
// materializes: a pending/start task carrying the template fields, then a
// created-by-ScheduleActor event followed by one wake instruction event per
// template instruction, in that order.
func TestScheduleTaskAndEvents(t *testing.T) {
	t.Parallel()

	sched := &model.Schedule{
		OrgID:       7,
		Name:        "nightly",
		Runner:      "r",
		Workspace:   "w",
		Namespace:   "ns",
		AutoArchive: time.Hour,
		Instructions: []model.ScheduleInstruction{
			{Text: "bump deps", URL: "https://example.com/deps"},
			{Text: "groom changelog"},
		},
	}

	task := sched.Task()
	assert.Equal(t, task.Name, "nightly")
	assert.Equal(t, task.Runner, "r")
	assert.Equal(t, task.Workspace, "w")
	assert.Equal(t, task.Namespace, "ns")
	assert.Equal(t, task.OrgID, int64(7))
	assert.Equal(t, task.AutoArchive, time.Hour)
	assert.Equal(t, task.Status, model.TaskStatusPending)
	assert.Equal(t, task.Command, model.TaskCommandStart)
	assert.Equal(t, task.Version, int64(1))

	// Events reference the inserted task; simulate the id the store would assign.
	task.ID = 42
	events := sched.Events(task)
	assert.Equal(t, len(events), 3)

	created, ok := events[0].Payload.(*model.LifecyclePayload)
	assert.Assert(t, ok, "first event must be a lifecycle event")
	assert.Equal(t, created.Kind, model.LifecycleKindCreated)
	assert.Equal(t, created.Actor, model.ScheduleActor)
	assert.Equal(t, created.ToStatus, task.Status.Label())
	assert.Equal(t, events[0].TaskID, int64(42))
	assert.Equal(t, events[0].OrgID, int64(7))
	assert.Assert(t, !events[0].Wake, "the created event does not wake")

	inst1, ok := events[1].Payload.(*model.InstructionPayload)
	assert.Assert(t, ok, "second event must be an instruction event")
	assert.Equal(t, inst1.Text, "bump deps")
	assert.Equal(t, inst1.URL, "https://example.com/deps")
	assert.Assert(t, events[1].Wake, "instruction events must wake")
	assert.Equal(t, events[1].TaskID, int64(42))
	assert.Equal(t, events[1].OrgID, int64(7))

	inst2, ok := events[2].Payload.(*model.InstructionPayload)
	assert.Assert(t, ok, "third event must be an instruction event")
	assert.Equal(t, inst2.Text, "groom changelog")
	assert.Equal(t, inst2.URL, "")
}

func TestScheduleValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cronExpr string
		timezone string
		err      string
	}{
		{"valid", "0 9 * * *", "America/Toronto", ""},
		{"valid descriptor", "@weekly", "UTC", ""},
		{"bad cron", "0 9 * *", "UTC", "invalid cron expression"},
		{"seconds field rejected", "0 0 9 * * *", "UTC", "invalid cron expression"},
		{"bad timezone", "0 9 * * *", "Nowhere/Land", "invalid timezone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &model.Schedule{CronExpr: tt.cronExpr, Timezone: tt.timezone}

			err := s.Validate()

			if tt.err == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.err)
			}
		})
	}
}
