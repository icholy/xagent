package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
)

// CopilotAgent implements Agent using GitHub Copilot CLI.
type CopilotAgent struct {
	log        *slog.Logger
	cwd        string
	mcpServers map[string]McpServer
	options    *CopilotOptions
}

// Prompt sends a prompt to Copilot and waits for completion.
func (a *CopilotAgent) Prompt(ctx context.Context, prompt string, resume bool) error {
	a.log.Info("sending prompt", "text", prompt)

	args := []string{
		"--silent",
	}

	// Add model if specified in options
	if a.options != nil && a.options.Model != "" {
		args = append(args, "--model", a.options.Model)
	}

	// Add MCP config if present
	if len(a.mcpServers) > 0 {
		mcpConfig := map[string]any{"mcpServers": a.mcpServers}
		mcpJSON, err := json.Marshal(mcpConfig)
		if err != nil {
			return err
		}
		args = append(args, "--additional-mcp-config", string(mcpJSON))
	}

	// Resume previous session if requested
	if resume {
		args = append(args, "--resume")
	}

	args = append(args, "-p", prompt)

	cmd := exec.CommandContext(ctx, "copilot", args...)
	cmd.Dir = a.cwd
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
		line := scanner.Text()
		a.log.Info("output", "line", line)
	}

	err = cmd.Wait()
	if context.Cause(ctx) == ErrStop {
		return ErrStop
	}
	return err
}

// Close releases any resources held by the agent.
func (a *CopilotAgent) Close() error {
	return nil
}
