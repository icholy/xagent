package agent

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

var ConfigDir = "/tmp/xagent"

type Config struct {
	// Runner-provided
	McpServers map[string]McpServer `json:"mcp_servers,omitempty"`
	Commands   []string             `json:"commands,omitempty"`

	// Agent-managed state
	SessionID    string `json:"session_id,omitempty"`
	PromptIndex  int    `json:"prompt_index,omitempty"`
	CommandIndex int    `json:"command_index,omitempty"`
}

type McpServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func ConfigPath(taskID string) string {
	return filepath.Join(ConfigDir, taskID+".json")
}

func LoadConfig(taskID string) (*Config, error) {
	path := ConfigPath(taskID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(taskID string, cfg *Config) error {
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
