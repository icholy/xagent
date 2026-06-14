package agentmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Server struct {
	client       xagentclient.Client
	task         *model.Task
	capabilities []string
}

func NewServer(client xagentclient.Client, task *model.Task, capabilities []string) *Server {
	return &Server{
		client:       client,
		task:         task,
		capabilities: capabilities,
	}
}

func (s *Server) hasCapability(capability string) bool {
	return slices.Contains(s.capabilities, capability)
}

func (s *Server) AddTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_link",
		Description: "Associate an external resource (PR, Jira ticket, etc.) with the current task",
	}, s.createLink)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report",
		Description: "Report a problem or log message for the current task",
	}, s.report)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_my_task",
		Description: "Get the current task instructions, links, and events",
	}, s.getMyTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_my_task",
		Description: "Update the current task's name",
	}, s.updateMyTask)

	if s.hasCapability(agentauth.CapabilityGitHubToken) {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "get_github_token",
			Description: "Get a short-lived GitHub App installation token for the current org. Fallback for shell-outs (e.g. a one-off `gh` invocation) that need a raw GITHUB_TOKEN — primary GitHub access goes through git (credential helper) and the github MCP server.",
		}, s.getGitHubToken)
	}
}

type createLinkInput struct {
	Relevance string `json:"relevance" jsonschema:"Describe how this link is relevant to the task"`
	URL       string `json:"url" jsonschema:"URL of the external resource"`
	Title     string `json:"title,omitempty" jsonschema:"Optional display title for the link"`
	Subscribe bool   `json:"subscribe,omitempty" jsonschema:"True to receive events for this link"`
}

func (s *Server) createLink(ctx context.Context, req *mcp.CallToolRequest, input createLinkInput) (*mcp.CallToolResult, any, error) {
	_, err := s.client.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    s.task.ID,
		Relevance: input.Relevance,
		Url:       input.URL,
		Title:     input.Title,
		Subscribe: input.Subscribe,
	})
	if err != nil {
		return errorResult("failed to create link: %v", err), nil, nil
	}

	return textResult("Link created: %s", input.URL), nil, nil
}

type reportInput struct {
	Message string `json:"message" jsonschema:"The message to report"`
}

func (s *Server) report(ctx context.Context, req *mcp.CallToolRequest, input reportInput) (*mcp.CallToolResult, any, error) {
	// The wire is unchanged (UploadLogs) until the agent surface lands, but the
	// server now re-points the `llm` channel onto the event stream: this upload
	// appends a from-agent `report` event rather than a logs row.
	_, err := s.client.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: s.task.ID,
		Entries: []*xagentv1.LogEntry{
			{Type: "llm", Content: input.Message},
		},
	})
	if err != nil {
		return errorResult("failed to upload log: %v", err), nil, nil
	}

	return textResult("Report submitted"), nil, nil
}

func (s *Server) getMyTask(ctx context.Context, req *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: s.task.ID})
	if err != nil {
		return errorResult("failed to get task: %v", err), nil, nil
	}

	return jsonResult(taskDetailsToMap(resp)), nil, nil
}

type updateMyTaskInput struct {
	Name        string `json:"name,omitempty" jsonschema:"The new name for the task"`
	AutoArchive *int64 `json:"auto_archive_seconds,omitempty" jsonschema:"Set the auto-archive timeout in seconds. Omit to leave the existing value untouched. 0 = never; negative = archive immediately; positive = delay."`
}

func (s *Server) updateMyTask(ctx context.Context, _ *mcp.CallToolRequest, input updateMyTaskInput) (*mcp.CallToolResult, any, error) {
	var autoArchive *durationpb.Duration
	if input.AutoArchive != nil {
		autoArchive = durationpb.New(time.Duration(*input.AutoArchive) * time.Second)
	}
	if _, err := s.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:          s.task.ID,
		Name:        input.Name,
		AutoArchive: autoArchive,
	}); err != nil {
		return errorResult("failed to update task: %v", err), nil, nil
	}

	return textResult("Task updated"), nil, nil
}

type getGitHubTokenInput struct{}

func (s *Server) getGitHubToken(ctx context.Context, req *mcp.CallToolRequest, input getGitHubTokenInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.client.CreateGitHubToken(ctx, &xagentv1.CreateGitHubTokenRequest{})
	if err != nil {
		return errorResult("failed to create github token: %v", err), nil, nil
	}

	return protojsonResult(resp), nil, nil
}

func textResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
	}
}

func errorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}
}

func protojsonResult(m proto.Message) *mcp.CallToolResult {
	data, err := protojson.MarshalOptions{Indent: "  "}.Marshal(m)
	if err != nil {
		return errorResult("failed to format response: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult("failed to format response: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}
}

// taskDetailsToMap converts a GetTaskDetailsResponse to a map for JSON output.
// Instructions are no longer a task field — they are instruction events in the
// brief, so they are projected out of the event stream here.
func taskDetailsToMap(resp *xagentv1.GetTaskDetailsResponse) map[string]any {
	marshalOpts := protojson.MarshalOptions{Indent: "  "}

	var instructions []json.RawMessage
	for _, event := range resp.GetEvents() {
		inst := event.GetInstruction()
		if inst == nil {
			continue
		}
		data, _ := marshalOpts.Marshal(inst)
		instructions = append(instructions, data)
	}

	links := make([]json.RawMessage, len(resp.GetLinks()))
	for i, link := range resp.GetLinks() {
		links[i], _ = marshalOpts.Marshal(link)
	}

	events := make([]json.RawMessage, len(resp.GetEvents()))
	for i, event := range resp.GetEvents() {
		events[i], _ = marshalOpts.Marshal(event)
	}

	return map[string]any{
		"id":           resp.Task.Id,
		"name":         resp.Task.Name,
		"status":       resp.Task.Status.String(),
		"workspace":    resp.Task.Workspace,
		"url":          resp.Task.Url,
		"instructions": instructions,
		"links":        links,
		"events":       events,
	}
}
