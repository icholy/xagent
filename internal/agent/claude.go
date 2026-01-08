package agent

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
)

// ClaudeOptions contains configuration for creating a ClaudeDriver.
type ClaudeOptions struct {
	Cwd        string
	Log        *slog.Logger
	McpServers map[string]McpServer
}

// ClaudeDriver implements Driver using Claude Code CLI.
type ClaudeDriver struct {
	log        *slog.Logger
	cwd        string
	mcpServers map[string]McpServer
}

// NewClaudeDriver creates a new ClaudeDriver.
func NewClaudeDriver(opts ClaudeOptions) *ClaudeDriver {
	return &ClaudeDriver{
		log:        cmp.Or(opts.Log, slog.Default()),
		cwd:        cmp.Or(opts.Cwd, "."),
		mcpServers: opts.McpServers,
	}
}

// Prompt sends a prompt to Claude and waits for completion.
func (d *ClaudeDriver) Prompt(ctx context.Context, prompt string, resume bool) error {
	d.log.Info("sending prompt", "text", prompt)

	args := []string{
		"@anthropic-ai/claude-code",
		"--dangerously-skip-permissions",
		"--verbose",
		"--output-format", "stream-json",
		"--strict-mcp-config",
		"--model", "opus",
	}

	// Add MCP config if present
	if len(d.mcpServers) > 0 {
		mcpConfig := map[string]any{"mcpServers": d.mcpServers}
		mcpJSON, err := json.Marshal(mcpConfig)
		if err != nil {
			return err
		}
		args = append(args, "--mcp-config", string(mcpJSON))
	}

	// Resume previous session if requested
	if resume {
		args = append(args, "--continue")
	}

	args = append(args, "--print", prompt)

	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = d.cwd
	cmd.Env = append(os.Environ(), "DISABLE_AUTOUPDATER=1")
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !d.handleStreamEvent(line) {
			d.log.Info("output", "line", string(line))
		}
	}

	return cmd.Wait()
}

// Close releases any resources held by the driver.
func (d *ClaudeDriver) Close() error {
	return nil
}

func (d *ClaudeDriver) handleStreamEvent(data []byte) bool {
	var event struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Name  string `json:"name"`
				Input any    `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return false
	}
	switch event.Type {
	case "assistant":
		for _, block := range event.Message.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					d.log.Info("text", "content", block.Text)
				}
			case "tool_use":
				d.log.Info("tool", "name", block.Name)
			}
		}
	}
	return true
}
