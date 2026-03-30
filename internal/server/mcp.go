package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpHandler creates an http.Handler that serves an MCP endpoint with
// list_workspaces and create_task tools. The returned handler must be
// wrapped with auth middleware that sets UserInfo in the request context.
func (s *Server) mcpHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "xagent",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List available workspaces",
	}, s.mcpListWorkspaces)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task",
		Description: "Create a new task",
	}, s.mcpCreateTask)

	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		// Auth middleware has already validated the request and set
		// UserInfo in the context. Reject if not authenticated.
		if apiauth.Caller(r.Context()) == nil {
			return nil
		}
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})
}

type mcpListWorkspacesInput struct{}

func (s *Server) mcpListWorkspaces(ctx context.Context, req *mcp.CallToolRequest, input mcpListWorkspacesInput) (*mcp.CallToolResult, any, error) {
	caller := apiauth.Caller(ctx)
	if caller == nil {
		return mcpErrorResult("authentication required"), nil, nil
	}
	workspaces, err := s.store.ListWorkspaces(ctx, nil, caller.OrgID)
	if err != nil {
		return mcpErrorResult("failed to list workspaces: %v", err), nil, nil
	}
	type workspace struct {
		Name     string `json:"name"`
		RunnerID string `json:"runner_id"`
	}
	result := make([]workspace, len(workspaces))
	for i, ws := range workspaces {
		result[i] = workspace{
			Name:     ws.Name,
			RunnerID: ws.RunnerID,
		}
	}
	return mcpJSONResult(result), nil, nil
}

type mcpCreateTaskInput struct {
	Name        string `json:"name,omitempty" jsonschema:"A short name for the task"`
	Workspace   string `json:"workspace" jsonschema:"The workspace to run the task in"`
	Instruction string `json:"instruction" jsonschema:"The instruction text for the task"`
	Runner      string `json:"runner,omitempty" jsonschema:"Optional runner ID to target"`
}

func (s *Server) mcpCreateTask(ctx context.Context, req *mcp.CallToolRequest, input mcpCreateTaskInput) (*mcp.CallToolResult, any, error) {
	caller := apiauth.Caller(ctx)
	if caller == nil {
		return mcpErrorResult("authentication required"), nil, nil
	}
	task := &model.Task{
		Name:      input.Name,
		Runner:    input.Runner,
		Workspace: input.Workspace,
		Instructions: []model.Instruction{
			{Text: input.Instruction},
		},
		Status:  model.TaskStatusPending,
		Command: model.TaskCommandStart,
		Version: 1,
		OrgID:   caller.OrgID,
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.CreateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  task.ID,
			Type:    "audit",
			Content: fmt.Sprintf("%s created task via MCP", caller.DisplayName()),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return mcpErrorResult("failed to create task: %v", err), nil, nil
	}
	s.log.Info("task created via MCP", "id", task.ID, "workspace", task.Workspace, "org_id", task.OrgID)
	type taskResult struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
		Status    string `json:"status"`
	}
	return mcpJSONResult(taskResult{
		ID:        task.ID,
		Name:      task.Name,
		Workspace: task.Workspace,
		Status:    task.Status.String(),
	}), nil, nil
}

func mcpErrorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}
}

func mcpJSONResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpErrorResult("failed to format response: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}
}
