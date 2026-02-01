package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Path returns the path to the xagent config file.
// It checks XAGENT_CONFIG_DIR first, then falls back to
// os.UserConfigDir()/xagent (e.g. ~/.config/xagent on Linux).
func Path() (string, error) {
	if dir := os.Getenv("XAGENT_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config directory: %w", err)
	}
	return filepath.Join(dir, "xagent", "config.json"), nil
}

// File stores the xagent configuration.
type File struct {
	Token      string `json:"token"`
	PrivateKey string `json:"private_key"`
}

// Load reads the config file.
// Returns a non-nil File even if the file doesn't exist.
func Load() (*File, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{}, nil
		}
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Save writes the config file.
func Save(f *File) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}
