package model

import "time"

// TaskStatus represents the current state of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusRunning    TaskStatus = "running"
	TaskStatusRestarting TaskStatus = "restarting"
	TaskStatusCancelling TaskStatus = "cancelling"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
	TaskStatusArchived   TaskStatus = "archived"
)

// TaskCommand represents a command to be executed by the runner.
type TaskCommand string

const (
	TaskCommandRestart TaskCommand = "restart"
	TaskCommandStop    TaskCommand = "stop"
)

// Instruction represents a task instruction with text and optional source URL.
type Instruction struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Task represents a task in the system.
type Task struct {
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	Parent       int64         `json:"parent"`
	Workspace    string        `json:"workspace"`
	Instructions []Instruction `json:"instructions"`
	Status       TaskStatus    `json:"status"`
	Command      TaskCommand   `json:"command"`
	Version      int64         `json:"version"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// RunnerEventType represents the type of event reported by the runner.
type RunnerEventType string

const (
	RunnerEventStarted RunnerEventType = "started"
	RunnerEventStopped RunnerEventType = "stopped"
	RunnerEventFailed  RunnerEventType = "failed"
)

// RunnerEvent represents an event from the runner about a task's container.
type RunnerEvent struct {
	TaskID    int64
	Event     RunnerEventType
	Version   int64
	Reconcile bool
}
