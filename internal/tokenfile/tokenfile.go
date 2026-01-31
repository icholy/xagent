package tokenfile

import (
	"encoding/json"
	"os"
)

// File stores the API key.
type File struct {
	APIKey string `json:"api_key"`
}

// Valid reports whether the token has a non-empty API key.
func (t *File) Valid() bool {
	return t != nil && t.APIKey != ""
}

// Load reads a token from a JSON file.
// Returns a non-nil File even if the file doesn't exist (with an empty API key).
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
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

// Save writes a token to a JSON file.
func Save(path string, token *File) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
