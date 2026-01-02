package agent

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/coder/acp-go-sdk"
)

type ClientOptions struct {
	Log      *slog.Logger
	OnUpdate func(acp.SessionUpdate)
}

// Client implements the acp.Client interface to handle agent callbacks.
type Client struct {
	log       *slog.Logger
	onUpdate  func(acp.SessionUpdate)
	terminals map[string]*Terminal
}

var _ acp.Client = (*Client)(nil)

func NewClient(opts ClientOptions) *Client {
	return &Client{
		log:       cmp.Or(opts.Log, slog.Default()),
		onUpdate:  opts.OnUpdate,
		terminals: map[string]*Terminal{},
	}
}

func getAllowOptionId(options []acp.PermissionOption) acp.PermissionOptionId {
	if len(options) == 0 {
		return ""
	}
	for _, opt := range options {
		if opt.Kind == acp.PermissionOptionKindAllowAlways || opt.Kind == acp.PermissionOptionKindAllowOnce {
			return opt.OptionId
		}
	}
	return options[0].OptionId
}

func (c *Client) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	if len(params.Options) == 0 {
		return acp.RequestPermissionResponse{}, nil
	}
	title := ""
	if params.ToolCall.Title != nil {
		title = *params.ToolCall.Title
	}
	if title == "" && params.ToolCall.Kind != nil {
		title = string(*params.ToolCall.Kind)
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{
			Selected: &acp.RequestPermissionOutcomeSelected{
				OptionId: getAllowOptionId(params.Options),
			},
		},
	}, nil
}

func (c *Client) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	if c.onUpdate != nil {
		c.onUpdate(params.Update)
	}
	return nil
}

func (c *Client) ReadTextFile(ctx context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	c.log.Info("read", "path", params.Path)
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	return acp.ReadTextFileResponse{Content: string(content)}, nil
}

func (c *Client) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	c.log.Info("write", "path", params.Path, "bytes", len(params.Content))
	dir := filepath.Dir(params.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	if err := os.WriteFile(params.Path, []byte(params.Content), 0o644); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

func (c *Client) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	// Build environment variables
	var env []string
	for _, e := range params.Env {
		env = append(env, e.Name+"="+e.Value)
	}

	// Determine working directory
	cwd := ""
	if params.Cwd != nil {
		cwd = *params.Cwd
	}

	// If no args provided, wrap in sh -c to handle full command strings
	command := params.Command
	args := params.Args
	if len(args) == 0 {
		args = []string{"-c", command}
		command = "sh"
	}

	term, err := NewTerminal(command, args, cwd, env)
	if err != nil {
		return acp.CreateTerminalResponse{}, err
	}

	id := fmt.Sprintf("term-%d", len(c.terminals)+1)
	c.terminals[id] = term

	c.log.Info("terminal", "action", "create", "id", id, "command", params.Command)
	return acp.CreateTerminalResponse{TerminalId: id}, nil
}

func (c *Client) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	term := c.terminals[params.TerminalId]
	if term == nil {
		return acp.TerminalOutputResponse{}, fmt.Errorf("terminal not found: %s", params.TerminalId)
	}

	output, truncated, exitStatus := term.Output()
	return acp.TerminalOutputResponse{
		Output:     output,
		Truncated:  truncated,
		ExitStatus: exitStatus,
	}, nil
}

func (c *Client) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	c.log.Info("terminal", "action", "release", "id", params.TerminalId)
	delete(c.terminals, params.TerminalId)
	return acp.ReleaseTerminalResponse{}, nil
}

func (c *Client) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	term := c.terminals[params.TerminalId]
	if term == nil {
		return acp.WaitForTerminalExitResponse{}, fmt.Errorf("terminal not found: %s", params.TerminalId)
	}

	exitCode, signal := term.Wait()
	return acp.WaitForTerminalExitResponse{
		ExitCode: exitCode,
		Signal:   signal,
	}, nil
}

func (c *Client) KillTerminalCommand(ctx context.Context, params acp.KillTerminalCommandRequest) (acp.KillTerminalCommandResponse, error) {
	term := c.terminals[params.TerminalId]
	if term == nil {
		return acp.KillTerminalCommandResponse{}, fmt.Errorf("terminal not found: %s", params.TerminalId)
	}

	c.log.Info("terminal", "action", "kill", "id", params.TerminalId)
	if err := term.Kill(); err != nil {
		return acp.KillTerminalCommandResponse{}, err
	}
	return acp.KillTerminalCommandResponse{}, nil
}
