package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// Workspace represents a registered workspace from a runner.
type Workspace struct {
	ID        int64     `json:"id"`
	RunnerID  string    `json:"runner_id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
}

// Proto converts a Workspace to its protobuf representation.
func (w *Workspace) Proto() *xagentv1.RegisteredWorkspace {
	return &xagentv1.RegisteredWorkspace{
		Name: w.Name,
	}
}

// WorkspaceFromProto converts a protobuf RegisteredWorkspace to a model Workspace.
func WorkspaceFromProto(pb *xagentv1.RegisteredWorkspace) *Workspace {
	return &Workspace{
		Name: pb.Name,
	}
}
