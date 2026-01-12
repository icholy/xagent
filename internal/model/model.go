// Package model defines shared types used across the xagent codebase.
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

// RunnerEventType represents the type of event reported by the runner.
type RunnerEventType string

const (
	RunnerEventStarted RunnerEventType = "started"
	RunnerEventStopped RunnerEventType = "stopped"
	RunnerEventFailed  RunnerEventType = "failed"
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

// RunnerEvent represents an event from the runner about a task's container.
type RunnerEvent struct {
	TaskID    int64
	Event     RunnerEventType
	Version   int64
	Reconcile bool
}

// Event represents an external event that can trigger task actions.
type Event struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Data        string    `json:"data"`
	URL         string    `json:"url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Link represents a link between a task and an external resource.
type Link struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Relevance string    `json:"relevance"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Notify    bool      `json:"notify"`
	CreatedAt time.Time `json:"created_at"`
}

// Log represents a log entry for a task.
type Log struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}
