package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// ClaudeAgent implements Agent using Claude Code CLI.
type ClaudeAgent struct {
	log        *slog.Logger
	cwd        string
	mcpServers map[string]McpServer
	options    *ClaudeOptions
}

// Prompt sends a prompt to Claude and waits for completion.
func (a *ClaudeAgent) Prompt(ctx context.Context, prompt string, resume bool) error {
	a.log.Info("sending prompt", "text", prompt)

	// Use model from options, defaulting to "opus"
	model := "opus"
	if a.options != nil && a.options.Model != "" {
		model = a.options.Model
	}

	args := []string{
		"@anthropic-ai/claude-code",
		"--dangerously-skip-permissions",
		"--verbose",
		"--output-format", "stream-json",
		"--strict-mcp-config",
		"--model", model,
	}

	// Add MCP config if present
	if len(a.mcpServers) > 0 {
		mcpConfig := map[string]any{"mcpServers": a.mcpServers}
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
	cmd.Dir = a.cwd
	cmd.Env = append(os.Environ(), "DISABLE_AUTOUPDATER=1")
	cmd.Stderr = os.Stderr

	// Create a new process group so we can kill all child processes.
	// When npx spawns node, we need to kill the entire process tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Send SIGTERM to the entire process group (negative PID)
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

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
		if !a.handleStreamEvent(line) {
			a.log.Info("output", "line", string(line))
		}
	}

	return cmd.Wait()
}

// Close releases any resources held by the agent.
func (a *ClaudeAgent) Close() error {
	return nil
}

func (a *ClaudeAgent) handleStreamEvent(data []byte) bool {
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
					a.log.Info("text", "content", block.Text)
				}
			case "tool_use":
				a.log.Info("tool", "name", block.Name)
			}
		}
	}
	return true
}
