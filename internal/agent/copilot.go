package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"syscall"
	"time"
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
		"--allow-all-tools",
		"--allow-all-paths",
		"--allow-all-urls",
		"--no-auto-update",
		"--log-level", "debug",
	}

	// Add model if specified in options
	if a.options != nil && a.options.Model != "" {
		args = append(args, "--model", a.options.Model)
	}

	// Add MCP config if present
	if len(a.mcpServers) > 0 {
		mcpJSON, err := a.mcpConfigJSON()
		if err != nil {
			return err
		}
		a.log.Info("mcp config", "json", string(mcpJSON))
		args = append(args, "--additional-mcp-config", string(mcpJSON))
	} else {
		a.log.Warn("no mcp servers configured")
	}

	// Resume previous session if requested
	if resume {
		args = append(args, "--continue")
	}

	args = append(args, "--prompt", prompt)

	cmd := exec.CommandContext(ctx, "copilot", args...)
	cmd.Dir = a.cwd

	// Create a new process group so we can kill all child processes
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
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Read stdout and stderr concurrently
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			a.handleOutput(line, true)
		}
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		a.handleOutput(line, false)
	}

	return cmd.Wait()
}

// handleOutput processes a line of output from Copilot and logs it with an appropriate type.
func (a *CopilotAgent) handleOutput(line string, isStderr bool) {
	// Try to parse as JSON to extract structured information.
	// Copilot may emit JSON log events or debug info.
	var data map[string]any
	if err := json.Unmarshal([]byte(line), &data); err == nil {
		// It's valid JSON - try to extract useful information
		if msgType, ok := data["type"].(string); ok {
			// Handle structured event types if present
			switch msgType {
			case "tool_call", "tool":
				if name, ok := data["name"].(string); ok {
					a.log.Info("tool", "name", name)
					return
				}
			case "tool_result":
				a.log.Debug("tool_result")
				return
			case "text", "assistant":
				if content, ok := data["content"].(string); ok {
					a.log.Info("text", "content", content)
					return
				}
				if text, ok := data["text"].(string); ok {
					a.log.Info("text", "content", text)
					return
				}
			}
		}
		// Log as generic JSON event
		if isStderr {
			a.log.Debug("json_event", "data", data)
		} else {
			a.log.Info("json_event", "data", data)
		}
		return
	}

	// Not JSON - log as plain output
	if isStderr {
		a.log.Debug("stderr", "line", line)
	} else {
		a.log.Info("output", "line", line)
	}
}

// Close releases any resources held by the agent.
func (a *CopilotAgent) Close() error {
	return nil
}

// mcpConfigJSON returns the MCP servers configuration as JSON for --additional-mcp-config.
func (a *CopilotAgent) mcpConfigJSON() ([]byte, error) {
	// Convert our McpServer format to Copilot's expected format
	servers := make(map[string]any)
	for name, srv := range a.mcpServers {
		server := map[string]any{
			"tools": []string{"*"},
		}
		switch srv.Type {
		case "stdio":
			server["type"] = "local"
			server["command"] = srv.Command
			if len(srv.Args) > 0 {
				server["args"] = srv.Args
			}
			if len(srv.Env) > 0 {
				server["env"] = srv.Env
			}
		case "http", "sse":
			server["type"] = srv.Type
			server["url"] = srv.URL
			if len(srv.Headers) > 0 {
				server["headers"] = srv.Headers
			}
		}
		servers[name] = server
	}
	return json.Marshal(map[string]any{"mcpServers": servers})
}
