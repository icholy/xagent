package agent

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var ConfigDir = "/tmp/xagent"

type Config struct {
	// Runner-provided
	Type       string               `json:"type,omitempty"`
	Cwd        string               `json:"cwd,omitempty"`
	Prompt     string               `json:"prompt,omitempty"`
	McpServers map[string]McpServer `json:"mcp_servers,omitempty"`
	Commands   []string             `json:"commands,omitempty"`

	// Agent-managed state
	Setup   bool `json:"setup,omitempty"`
	Started bool `json:"started,omitempty"`
}

type McpServer struct {
	Type    string            `json:"type" yaml:"type"`
	URL     string            `json:"url,omitempty" yaml:"url"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers"`
	Command string            `json:"command,omitempty" yaml:"command"`
	Args    []string          `json:"args,omitempty" yaml:"args"`
	Env     map[string]string `json:"env,omitempty" yaml:"env"`
}

func (m *McpServer) Validate() error {
	if m.Type == "" {
		return fmt.Errorf("type is required")
	}
	switch m.Type {
	case "stdio":
		if m.Command == "" {
			return fmt.Errorf("command is required for stdio")
		}
	case "http", "sse":
		if m.URL == "" {
			return fmt.Errorf("url is required for %s", m.Type)
		}
	default:
		return fmt.Errorf("unknown type: %s", m.Type)
	}
	return nil
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

	// Write to a temporary file first, then rename atomically.
	// This ensures that if the container is killed mid-write,
	// the original config file remains intact.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o666); err != nil {
		return err
	}

	// Rename is atomic on POSIX systems
	return os.Rename(tmpPath, path)
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
