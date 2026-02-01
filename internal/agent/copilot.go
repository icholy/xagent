package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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

	// Write MCP config file if we have MCP servers
	if len(a.mcpServers) > 0 {
		if err := a.writeMcpConfig(); err != nil {
			return fmt.Errorf("failed to write MCP config: %w", err)
		}
	}

	args := []string{
		"--silent",
		"--allow-all",
		"--no-ask-user",
		"--no-auto-update",
	}

	// Add model if specified in options
	if a.options != nil && a.options.Model != "" {
		args = append(args, "--model", a.options.Model)
	}

	// Resume previous session if requested
	if resume {
		args = append(args, "--continue")
	}

	args = append(args, "--prompt", prompt)

	cmd := exec.CommandContext(ctx, "copilot", args...)
	cmd.Dir = a.cwd
	cmd.Stderr = os.Stderr

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

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		a.log.Info("output", "line", line)
	}

	return cmd.Wait()
}

// Close releases any resources held by the agent.
func (a *CopilotAgent) Close() error {
	return nil
}

// writeMcpConfig writes the MCP servers configuration to ~/.copilot/mcp-config.json.
func (a *CopilotAgent) writeMcpConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	copilotDir := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(copilotDir, 0755); err != nil {
		return err
	}

	// Convert our McpServer format to Copilot's expected format
	servers := make(map[string]any)
	for name, srv := range a.mcpServers {
		server := make(map[string]any)
		switch srv.Type {
		case "stdio":
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

	config := map[string]any{
		"servers": servers,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	mcpConfigPath := filepath.Join(copilotDir, "mcp-config.json")
	return os.WriteFile(mcpConfigPath, data, 0644)
}
