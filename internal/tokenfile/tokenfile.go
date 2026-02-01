package tokenfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Dir returns the xagent config directory.
// It checks XAGENT_CONFIG_DIR first, then falls back to
// os.UserConfigDir()/xagent (e.g. ~/.config/xagent on Linux).
func Dir() string {
	if dir := os.Getenv("XAGENT_CONFIG_DIR"); dir != "" {
		return dir
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "data"
	}
	return filepath.Join(dir, "xagent")
}

// File stores the API key.
type File struct {
	APIKey string `json:"api_key"`
}

// Valid reports whether the token has a non-empty API key.
func (t *File) Valid() bool {
	return t != nil && t.APIKey != ""
}

// Load reads the token file.
// Returns a non-nil File even if the file doesn't exist (with an empty API key).
func Load() (*File, error) {
	data, err := os.ReadFile(filepath.Join(Dir(), "token.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &File{}, nil
		}
		return nil, err
	}
	var token File
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// Save writes the token file.
func Save(token *File) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "token.json"), data, 0600)
}
