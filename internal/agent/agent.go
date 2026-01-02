package agent

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	acp "github.com/coder/acp-go-sdk"
)

// Options contains configuration for creating an Agent.
type Options struct {
	Cwd        string
	Log        *slog.Logger
	SessionID  string
	McpServers map[string]McpServer
	ACP        ACP
	OnUpdate   func(acp.SessionUpdate)
}

// Agent manages a Claude Code ACP connection.
type Agent struct {
	log       *slog.Logger
	cwd       string
	cmd       *exec.Cmd
	client    *Client
	conn      *acp.ClientSideConnection
	sessionID string
}

// Start creates and starts a new Agent.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	log := cmp.Or(opts.Log, slog.Default())
	cwd := cmp.Or(opts.ACP.Cwd, opts.Cwd, ".")

	a := &Agent{
		log:       log,
		cwd:       cwd,
		sessionID: opts.SessionID,
	}

	// Convert MCP servers to ACP format
	mcpServers := make([]acp.McpServer, 0, len(opts.McpServers))
	for name, srv := range opts.McpServers {
		mcpServers = append(mcpServers, srv.ACP(name))
	}

	// Start Claude Code ACP adapter
	if len(opts.ACP.Command) == 0 {
		return nil, fmt.Errorf("ACP command not configured")
	}
	a.cmd = exec.CommandContext(ctx, opts.ACP.Command[0], opts.ACP.Command[1:]...)
	a.cmd.Dir = cwd
	a.cmd.Stderr = os.Stderr
	stdin, err := a.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	stdout, err := a.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := a.cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Claude Code: %w", err)
	}

	// Create client and establish connection
	a.client = NewClient(ClientOptions{
		Log:      log,
		OnUpdate: opts.OnUpdate,
	})
	a.conn = acp.NewClientSideConnection(a.client, stdin, stdout)

	// Initialize connection
	initResp, err := a.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapability{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	})
	if err != nil {
		a.cmd.Process.Kill()
		return nil, fmt.Errorf("initialize failed: %w", err)
	}
	log.Info("connected", "protocol", initResp.ProtocolVersion)

	// Load existing session or create new one
	// See: https://github.com/zed-industries/claude-code-acp/pull/196#issuecomment-3634887033
	if a.sessionID != "" && opts.ACP.ClaudeResumeHack {
		log.Info("resuming session via meta hack", "id", a.sessionID)
		_, err := a.conn.NewSession(ctx, acp.NewSessionRequest{
			Cwd:        cwd,
			McpServers: mcpServers,
			Meta: map[string]any{
				"claudeCode": map[string]any{
					"options": map[string]any{
						"resume": a.sessionID,
					},
				},
			},
		})
		if err != nil {
			a.cmd.Process.Kill()
			return nil, fmt.Errorf("resume session failed: %w", err)
		}
		return a, nil
	}

	if a.sessionID != "" {
		log.Info("loading session", "id", a.sessionID)
		_, err := a.conn.LoadSession(ctx, acp.LoadSessionRequest{
			SessionId: acp.SessionId(a.sessionID),
		})
		if err != nil {
			a.cmd.Process.Kill()
			return nil, fmt.Errorf("load session failed: %w", err)
		}
		return a, nil
	}

	session, err := a.conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: mcpServers,
	})
	if err != nil {
		a.cmd.Process.Kill()
		return nil, fmt.Errorf("new session failed: %w", err)
	}
	a.sessionID = string(session.SessionId)
	log.Info("session created", "id", a.sessionID)

	return a, nil
}

// SessionID returns the ACP session ID.
func (a *Agent) SessionID() string {
	return a.sessionID
}

// Close shuts down the agent.
func (a *Agent) Close() error {
	if a.cmd != nil && a.cmd.Process != nil {
		return a.cmd.Process.Kill()
	}
	return nil
}

// Prompt sends a prompt to the agent and waits for completion.
func (a *Agent) Prompt(ctx context.Context, prompt string) error {
	a.log.Info("sending prompt", "text", prompt)
	_, err := a.conn.Prompt(ctx, acp.PromptRequest{
		SessionId: acp.SessionId(a.sessionID),
		Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
	})
	if err != nil {
		return fmt.Errorf("prompt failed: %w", err)
	}
	return nil
}
