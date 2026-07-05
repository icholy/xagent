package agent

import (
	"testing"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

func TestTaskBriefRender(t *testing.T) {
	created := timestamppb.New(time.Date(2026, 7, 5, 4, 47, 28, 0, time.UTC))
	brief := &taskBrief{
		task: &xagentv1.Task{
			Id:        7,
			Name:      "Fix bug",
			Status:    xagentv1.TaskStatus_RUNNING,
			Workspace: "ws",
			Url:       "https://example.com/ui/tasks/7?org=1",
		},
		events: []*xagentv1.Event{
			{Id: 1, Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{
				Text: "Do the thing",
				Url:  "https://example.com/issue/1",
			}}},
			{Id: 2, Payload: &xagentv1.Event_Instruction{Instruction: &xagentv1.InstructionPayload{
				Text: "Line one\nLine two",
			}}},
			{Id: 3, CreatedAt: created, Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
				Description: "PR comment on example/repo#1",
				Url:         "https://example.com/pr/1#comment",
				Data:        `{"body": "looks good"}`,
			}}},
		},
		links: []*xagentv1.TaskLink{
			{Url: "https://example.com/pr/1", Title: "My PR", Subscribe: true, Relevance: "PR opened for this task"},
		},
	}
	assert.Equal(t, brief.render(), `# Task 7: Fix bug

Status: Running
Workspace: ws
URL: https://example.com/ui/tasks/7?org=1

## Instructions

1. Do the thing
   Source: https://example.com/issue/1

2. Line one
   Line two

## Events

### 2026-07-05T04:47:28Z — PR comment on example/repo#1

URL: https://example.com/pr/1#comment

{"body": "looks good"}

## Links

- https://example.com/pr/1 — My PR (subscribed)
  Relevance: PR opened for this task`)
}

func TestTaskBriefRender_Resume(t *testing.T) {
	created := timestamppb.New(time.Date(2026, 7, 5, 4, 47, 28, 0, time.UTC))
	brief := &taskBrief{
		task:   &xagentv1.Task{Id: 7, Name: "Fix bug", Status: xagentv1.TaskStatus_RUNNING, Workspace: "ws"},
		resume: true,
		events: []*xagentv1.Event{
			{Id: 4, CreatedAt: created, Payload: &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
				Description: "PR review on example/repo#1",
				Url:         "https://example.com/pr/1#review",
			}}},
		},
		// Links are session context the resumed agent already has; a resume
		// brief carries only the new activity.
		links: []*xagentv1.TaskLink{
			{Url: "https://example.com/pr/1", Title: "My PR", Subscribe: true},
		},
	}
	assert.Equal(t, brief.render(), `# Task 7: Fix bug — new activity

The following arrived since your last run:

## Events

### 2026-07-05T04:47:28Z — PR review on example/repo#1

URL: https://example.com/pr/1#review`)
}

func TestTaskBriefRender_ResumeEmpty(t *testing.T) {
	brief := &taskBrief{
		task:   &xagentv1.Task{Id: 7, Name: "Fix bug"},
		resume: true,
	}
	assert.Equal(t, brief.render(), "")
}

func TestTaskBriefRender_Unnamed(t *testing.T) {
	brief := &taskBrief{
		task: &xagentv1.Task{Id: 7, Status: xagentv1.TaskStatus_PENDING, Workspace: "ws"},
	}
	assert.Equal(t, brief.render(), `# Task 7

Status: Pending
Workspace: ws`)
}
