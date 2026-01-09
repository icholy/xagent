package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
)

// CursorAgent implements Agent using the Cursor Agent CLI.
type CursorAgent struct {
	log        *slog.Logger
	cwd        string
	mcpServers map[string]McpServer
	options    *CursorOptions
}

// Prompt sends a prompt to Cursor and waits for completion.
func (a *CursorAgent) Prompt(ctx context.Context, prompt string, resume bool) error {
	a.log.Info("sending prompt", "text", prompt)

	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--force",
	}

	// Add model if specified in options
	if a.options != nil && a.options.Model != "" {
		args = append(args, "--model", a.options.Model)
	}

	// Add MCP config if present
	// Note: cursor-agent uses .cursor/mcp.json by default, but we pass additional config
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
		args = append(args, "--resume")
	}

	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "cursor-agent", args...)
	cmd.Dir = a.cwd
	cmd.Env = os.Environ()
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
		if !a.handleStreamEvent(line) {
			a.log.Info("output", "line", string(line))
		}
	}

	return cmd.Wait()
}

// Close releases any resources held by the agent.
func (a *CursorAgent) Close() error {
	return nil
}

func (a *CursorAgent) handleStreamEvent(data []byte) bool {
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
