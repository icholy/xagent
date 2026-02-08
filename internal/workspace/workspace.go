package workspace

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/configfile"
	"github.com/icholy/xagent/internal/expandvar"
	"gopkg.in/yaml.v3"
)

var defaultYAML = `workspaces:
  pets-workshop:
    container:
      image: node:20
      working_dir: /root
      environment:
        CLAUDE_CODE_OAUTH_TOKEN: ${env:CLAUDE_CODE_OAUTH_TOKEN}
    commands:
      - npm install -g @anthropic-ai/claude-code
      - git clone https://github.com/github-samples/pets-workshop
    agent:
      type: claude
      cwd: /root/pets-workshop
      mcp_servers: {}
      prompt: |
        This is an example github repository.
        Don't try opening PRs or issues.
`

// DefaultPath returns the default workspaces.yaml path inside the config directory.
func DefaultPath() (string, error) {
	dir, err := configfile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workspaces.yaml"), nil
}

// CreateDefault creates a workspaces.yaml with example config if one doesn't exist.
// Returns the path to the file and whether it was created.
func CreateDefault() (string, bool, error) {
	path, err := DefaultPath()
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(defaultYAML), 0644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

type Config struct {
	Workspaces map[string]Workspace `yaml:"workspaces"`
}

func (c *Config) Validate() error {
	if len(c.Workspaces) == 0 {
		return fmt.Errorf("no workspaces defined")
	}
	for name, ws := range c.Workspaces {
		if err := ws.Validate(); err != nil {
			return fmt.Errorf("workspace %q: %w", name, err)
		}
	}
	return nil
}

type Workspace struct {
	Container Container `yaml:"container"`
	Agent     Agent     `yaml:"agent"`
	Commands  []string  `yaml:"commands"`
}

type Agent struct {
	Type       string                     `yaml:"type"`
	Cwd        string                     `yaml:"cwd"`
	Prompt     string                     `yaml:"prompt"`
	McpServers map[string]agent.McpServer `yaml:"mcp_servers"`
	Claude     *ClaudeConfig              `yaml:"claude,omitempty"`
	Copilot    *CopilotConfig             `yaml:"copilot,omitempty"`
	Cursor     *CursorConfig              `yaml:"cursor,omitempty"`
	Dummy      *DummyConfig               `yaml:"dummy,omitempty"`
}

// ClaudeConfig contains Claude-specific agent configuration.
type ClaudeConfig struct {
	Model string `yaml:"model"`
	Bin   string `yaml:"bin"`
}

// CopilotConfig contains Copilot-specific agent configuration.
type CopilotConfig struct {
	Model string `yaml:"model"`
	Bin   string `yaml:"bin"`
}

// CursorConfig contains Cursor-specific agent configuration.
type CursorConfig struct {
	Model string `yaml:"model"`
	Bin   string `yaml:"bin"`
}

// DummyConfig contains Dummy-specific agent configuration.
type DummyConfig struct {
	// Sleep duration in seconds. If -1, sleeps forever.
	Sleep int `yaml:"sleep"`
	// ToolCalls specifies MCP tool calls to make.
	ToolCalls []agent.DummyToolCall `yaml:"tool_calls"`
}

func (w *Workspace) Validate() error {
	if err := w.Container.Validate(); err != nil {
		return fmt.Errorf("container: %w", err)
	}
	if err := w.Agent.Validate(); err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	return nil
}

func (a *Agent) Validate() error {
	for name, srv := range a.McpServers {
		if err := srv.Validate(); err != nil {
			return fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
	}
	return nil
}

type Container struct {
	Image       string            `yaml:"image"`
	Runtime     string            `yaml:"runtime"`
	WorkingDir  string            `yaml:"working_dir"`
	User        string            `yaml:"user"`
	Volumes     []string          `yaml:"volumes"`
	Networks    []string          `yaml:"networks"`
	GroupAdd    []string          `yaml:"group_add"`
	Environment map[string]string `yaml:"environment"`
}

// Environ returns the environment variables as a slice of "key=value" strings.
func (c *Container) Environ() []string {
	env := make([]string, 0, len(c.Environment))
	for k, v := range c.Environment {
		env = append(env, k+"="+v)
	}
	return env
}

func (c *Container) Validate() error {
	if c.Image == "" {
		return fmt.Errorf("image is required")
	}
	return nil
}

// NetworkingConfig returns the Docker networking configuration for this container.
func (c *Container) NetworkingConfig() *network.NetworkingConfig {
	if len(c.Networks) == 0 {
		return nil
	}
	endpoints := make(map[string]*network.EndpointSettings, len(c.Networks))
	for _, net := range c.Networks {
		endpoints[net] = &network.EndpointSettings{}
	}
	return &network.NetworkingConfig{
		EndpointsConfig: endpoints,
	}
}

// ExpandFunc is called for each ${namespace:value} found in the config.
type ExpandFunc func(namespace, value string) (string, error)

// ExpandVar is the default ExpandFunc that supports:
//   - ${env:VAR} - environment variables
//   - ${sh:command} - shell command output
func ExpandVar(namespace, value string) (string, error) {
	switch namespace {
	case "env":
		return os.Getenv(value), nil
	case "sh":
		out, err := exec.Command("sh", "-c", value).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	default:
		return "", fmt.Errorf("unknown namespace: %s", namespace)
	}
}

// LoadConfig loads the workspace config from a file.
// Variables in the format ${namespace:value} are expanded using the provided function.
// If expand is nil, ExpandVar is used.
func LoadConfig(path string, expand ExpandFunc) (*Config, error) {
	if expand == nil {
		expand = ExpandVar
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := expandNode(&node, expand); err != nil {
		return nil, fmt.Errorf("failed to expand variables: %w", err)
	}

	var cfg Config
	if err := node.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func expandNode(node *yaml.Node, expand ExpandFunc) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!str" || node.Tag == "" {
			expanded, err := expandvar.Expand(node.Value, expand)
			if err != nil {
				return err
			}
			node.Value = expanded
		}
	case yaml.SequenceNode, yaml.MappingNode:
		for _, child := range node.Content {
			if err := expandNode(child, expand); err != nil {
				return err
			}
		}
	case yaml.DocumentNode:
		for _, child := range node.Content {
			if err := expandNode(child, expand); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Config) Get(name string) (*Workspace, error) {
	ws, ok := c.Workspaces[name]
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", name)
	}
	return &ws, nil
}

// AgentConfig converts the workspace agent configuration into an agent.Config.
func (w *Workspace) AgentConfig() agent.Config {
	cfg := agent.Config{
		Type:       w.Agent.Type,
		Cwd:        w.Agent.Cwd,
		Prompt:     w.Agent.Prompt,
		McpServers: make(map[string]agent.McpServer),
		Commands:   w.Commands,
	}
	if w.Agent.Claude != nil {
		cfg.Claude = &agent.ClaudeOptions{
			Model: w.Agent.Claude.Model,
			Bin:   w.Agent.Claude.Bin,
		}
	}
	if w.Agent.Copilot != nil {
		cfg.Copilot = &agent.CopilotOptions{
			Model: w.Agent.Copilot.Model,
			Bin:   w.Agent.Copilot.Bin,
		}
	}
	if w.Agent.Cursor != nil {
		cfg.Cursor = &agent.CursorOptions{
			Model: w.Agent.Cursor.Model,
			Bin:   w.Agent.Cursor.Bin,
		}
	}
	if w.Agent.Dummy != nil {
		cfg.Dummy = &agent.DummyOptions{
			Sleep:     w.Agent.Dummy.Sleep,
			ToolCalls: w.Agent.Dummy.ToolCalls,
		}
	}
	maps.Copy(cfg.McpServers, w.Agent.McpServers)
	return cfg
}
