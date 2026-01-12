package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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

	// Write MCP config file if we have MCP servers
	if len(a.mcpServers) > 0 {
		if err := a.writeMcpConfig(); err != nil {
			return fmt.Errorf("failed to write MCP config: %w", err)
		}
	}

	// Get or create chat ID for session management
	chatID, err := a.getOrCreateChatID(ctx, resume)
	if err != nil {
		return fmt.Errorf("failed to get chat ID: %w", err)
	}

	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--force",
		"--approve-mcps",
	}

	// Add model if specified in options
	if a.options != nil && a.options.Model != "" {
		args = append(args, "--model", a.options.Model)
	}

	// Set workspace directory
	if a.cwd != "" && a.cwd != "." {
		args = append(args, "--workspace", a.cwd)
	}

	// Resume with chat ID
	if chatID != "" {
		args = append(args, "--resume", chatID)
	}

	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "cursor-agent", args...)
	cmd.Dir = a.cwd
	cmd.Env = os.Environ()
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

// writeMcpConfig writes the MCP servers configuration to the workspace .cursor/mcp.json file.
func (a *CursorAgent) writeMcpConfig() error {
	// Cursor expects MCP config in .cursor/mcp.json in the workspace directory
	cursorDir := filepath.Join(a.cwd, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return err
	}

	// Convert our McpServer format to Cursor's expected format
	mcpServers := make(map[string]any)
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
			server["transport"] = srv.Type
			server["url"] = srv.URL
			if len(srv.Headers) > 0 {
				server["headers"] = srv.Headers
			}
		}
		mcpServers[name] = server
	}

	config := map[string]any{
		"mcpServers": mcpServers,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	mcpConfigPath := filepath.Join(cursorDir, "mcp.json")
	return os.WriteFile(mcpConfigPath, data, 0644)
}

// chatIDPath returns the path to the chat ID file for session persistence.
func (a *CursorAgent) chatIDPath() string {
	return filepath.Join(a.cwd, ".cursor", "chat_id")
}

// getOrCreateChatID gets an existing chat ID or creates a new one.
func (a *CursorAgent) getOrCreateChatID(ctx context.Context, resume bool) (string, error) {
	chatIDPath := a.chatIDPath()

	// If resuming, try to load existing chat ID
	if resume {
		data, err := os.ReadFile(chatIDPath)
		if err == nil {
			chatID := strings.TrimSpace(string(data))
			if chatID != "" {
				a.log.Info("resuming chat", "chatID", chatID)
				return chatID, nil
			}
		}
	}

	// Create a new chat ID
	cmd := exec.CommandContext(ctx, "cursor-agent", "create-chat")
	cmd.Dir = a.cwd
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create chat: %w", err)
	}

	chatID := strings.TrimSpace(out.String())
	if chatID == "" {
		return "", fmt.Errorf("cursor-agent create-chat returned empty chat ID")
	}

	a.log.Info("created new chat", "chatID", chatID)

	// Save the chat ID for future resume
	cursorDir := filepath.Dir(chatIDPath)
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(chatIDPath, []byte(chatID), 0644); err != nil {
		return "", err
	}

	return chatID, nil
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
