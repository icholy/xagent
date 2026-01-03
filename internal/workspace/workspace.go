package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/expandvar"
	"gopkg.in/yaml.v3"
)

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
	Cwd        string                    `yaml:"cwd"`
	McpServers map[string]agent.McpServer `yaml:"mcp_servers"`
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
	WorkingDir  string            `yaml:"working_dir"`
	Volumes     []string          `yaml:"volumes"`
	Networks    []string          `yaml:"networks"`
	GroupAdd    []string          `yaml:"group_add"`
	Environment map[string]string `yaml:"environment"`
}

func (c *Container) Validate() error {
	if c.Image == "" {
		return fmt.Errorf("image is required")
	}
	return nil
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
