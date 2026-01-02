package agent

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	acp "github.com/coder/acp-go-sdk"
)

var ConfigDir = "/tmp/xagent"

type Config struct {
	// Runner-provided
	McpServers map[string]McpServer `json:"mcp_servers,omitempty"`
	Commands   []string             `json:"commands,omitempty"`
	ACP        ACP                  `json:"acp,omitempty"`

	// Agent-managed state
	SessionID    string `json:"session_id,omitempty"`
	PromptIndex  int    `json:"prompt_index,omitempty"`
	CommandIndex int    `json:"command_index,omitempty"`
}

func (c *Config) Validate() error {
	if err := c.ACP.Validate(); err != nil {
		return fmt.Errorf("acp: %w", err)
	}
	return nil
}

type ACP struct {
	Command          []string `json:"command,omitempty"`
	Cwd              string   `json:"cwd,omitempty"`
	ClaudeResumeHack bool     `json:"claude_resume_hack,omitempty"` // use _meta.claudeCode.options.resume
}

func (a *ACP) Validate() error {
	if len(a.Command) == 0 {
		return fmt.Errorf("command is required")
	}
	return nil
}

type McpServer struct {
	// HTTP transport
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// Stdio transport
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ACP converts the McpServer to the ACP SDK format.
func (s McpServer) ACP(name string) acp.McpServer {
	switch s.Type {
	case "http":
		return acp.McpServer{
			Http: &acp.McpServerHttp{
				Name:    name,
				Type:    "http",
				Url:     s.URL,
				Headers: s.acpHeaders(),
			},
		}
	case "sse":
		return acp.McpServer{
			Sse: &acp.McpServerSse{
				Name:    name,
				Type:    "sse",
				Url:     s.URL,
				Headers: s.acpHeaders(),
			},
		}
	default:
		env := make([]acp.EnvVariable, 0, len(s.Env))
		for k, v := range s.Env {
			env = append(env, acp.EnvVariable{Name: k, Value: v})
		}
		return acp.McpServer{
			Stdio: &acp.McpServerStdio{
				Name:    name,
				Command: s.Command,
				Args:    s.Args,
				Env:     env,
			},
		}
	}
}

func (s McpServer) acpHeaders() []acp.HttpHeader {
	headers := make([]acp.HttpHeader, 0, len(s.Headers))
	for k, v := range s.Headers {
		headers = append(headers, acp.HttpHeader{Name: k, Value: v})
	}
	return headers
}

func ConfigPath(taskID string) string {
	return filepath.Join(ConfigDir, taskID+".json")
}

func LoadConfig(taskID string) (*Config, error) {
	path := ConfigPath(taskID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("config not found: %s", path)
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

func SaveConfig(taskID string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	path := ConfigPath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o666)
}

// Tar returns a tar archive containing the config file for the given task ID.
func (c *Config) Tar(taskID string) ([]byte, error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Create directory entry
	if err := tw.WriteHeader(&tar.Header{
		Name:     ConfigDir + "/",
		Mode:     0777,
		Typeflag: tar.TypeDir,
	}); err != nil {
		return nil, err
	}

	// Create file entry
	if err := tw.WriteHeader(&tar.Header{
		Name: ConfigPath(taskID),
		Mode: 0666,
		Size: int64(len(data)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(data); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
