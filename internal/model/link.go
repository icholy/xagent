package model

import "time"

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
