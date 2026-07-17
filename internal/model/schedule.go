package model

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser is the fixed 5-field parser (minute hour day-of-month month
// day-of-week) plus the @daily/@hourly/... descriptors. Pinning the accepted
// fields keeps the grammar explicit and stable: no seconds field, matching what
// users expect from crontab and GitHub Actions. See
// proposals/accepted/scheduled-tasks.md §"Schedule specification".
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// Instruction is a single to-agent instruction stored in a schedule's template.
// On every occurrence it is seeded onto the new task as an InstructionPayload
// event, so the field names round-trip through the instructions JSONB column as
// [{text, url}].
type Instruction struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Schedule is a stored task template plus a cron spec. A server-side scheduler
// worker materializes a real task run on each occurrence (see
// proposals/accepted/scheduled-tasks.md). A schedule belongs to an org, not a
// user — CreatedBy is attribution only and carries no permission semantics.
type Schedule struct {
	ID        int64  `json:"id"`
	OrgID     int64  `json:"org_id"`
	CreatedBy string `json:"created_by"`
	Name      string `json:"name"`

	// Task template — the same shape CreateTask takes.
	Workspace    string        `json:"workspace"`
	Runner       string        `json:"runner"`
	Namespace    string        `json:"namespace,omitempty"`
	Instructions []Instruction `json:"instructions"`
	AutoArchive  time.Duration `json:"auto_archive,omitempty"`

	// Schedule spec.
	CronExpr string `json:"cron_expr"`
	Timezone string `json:"timezone"`
	Enabled  bool   `json:"enabled"`

	// Scheduler bookkeeping.
	NextRunAt  *time.Time `json:"next_run_at,omitempty"` // UTC; nil when disabled/paused
	LastRunAt  *time.Time `json:"last_run_at,omitempty"` // UTC
	LastTaskID *int64     `json:"last_task_id,omitempty"`
	Version    int64      `json:"version"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Next returns the first occurrence strictly after `after`, evaluated in the
// schedule's timezone, as UTC. The location is only used to interpret the cron
// fields; the returned instant is absolute. Evaluating in the location on every
// call (never by adding a fixed 24h) keeps local-time semantics correct across
// DST transitions. Returns the zero value and a user-facing error if the cron
// expression or timezone is invalid.
func (s *Schedule) Next(after time.Time) (time.Time, error) {
	sched, err := cronParser.Parse(s.CronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", s.CronExpr, err)
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", s.Timezone, err)
	}
	return sched.Next(after.In(loc)).UTC(), nil
}

// Validate parses cron_expr and loads the timezone, returning a user-facing
// error on failure. Callers surface the error as connect.CodeInvalidArgument so
// an invalid schedule can never be stored.
func (s *Schedule) Validate() error {
	if _, err := cronParser.Parse(s.CronExpr); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", s.CronExpr, err)
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", s.Timezone, err)
	}
	return nil
}
