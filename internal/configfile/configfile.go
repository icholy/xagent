package configfile

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Dir returns the xagent config directory.
// It checks XAGENT_CONFIG_DIR first, then falls back to
// os.UserConfigDir()/xagent (e.g. ~/.config/xagent on Linux).
func Dir() (string, error) {
	if dir := os.Getenv("XAGENT_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config directory: %w", err)
	}
	return filepath.Join(dir, "xagent"), nil
}

// Path returns the path to the xagent config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Overrides contains values that take priority over the config file.
// If all fields are set, the config file is not read.
type Overrides struct {
	Token      string
	PrivateKey string
}

func (o *Overrides) complete() bool {
	return o != nil && o.Token != "" && o.PrivateKey != ""
}

func (o *Overrides) apply(f *File) error {
	if o == nil {
		return nil
	}
	if o.Token != "" {
		f.Token = o.Token
	}
	if o.PrivateKey != "" {
		key, err := decodePrivateKey(o.PrivateKey)
		if err != nil {
			return fmt.Errorf("decode override private key: %w", err)
		}
		f.PrivateKey = key
	}
	return nil
}

// Load reads the config file and applies any overrides.
// Returns a non-nil File even if the file doesn't exist.
func Load(overrides *Overrides) (*File, error) {
	if overrides.complete() {
		var f File
		if err := overrides.apply(&f); err != nil {
			return nil, err
		}
		return &f, nil
	}
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
	if err := overrides.apply(&f); err != nil {
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

// File stores the xagent configuration.
type File struct {
	Token      string             `json:"token"`
	PrivateKey ed25519.PrivateKey `json:"-"`
}

type jsonFile struct {
	Token      string `json:"token"`
	PrivateKey string `json:"private_key"`
}

func (f *File) MarshalJSON() ([]byte, error) {
	jf := jsonFile{Token: f.Token}
	if f.PrivateKey != nil {
		jf.PrivateKey = encodePrivateKey(f.PrivateKey)
	}
	return json.Marshal(jf)
}

func (f *File) UnmarshalJSON(data []byte) error {
	var jf jsonFile
	if err := json.Unmarshal(data, &jf); err != nil {
		return err
	}
	f.Token = jf.Token
	if jf.PrivateKey != "" {
		key, err := decodePrivateKey(jf.PrivateKey)
		if err != nil {
			return err
		}
		f.PrivateKey = key
	}
	return nil
}

func encodePrivateKey(key ed25519.PrivateKey) string {
	return hex.EncodeToString(key.Seed())
}

// decodePrivateKey parses a hex-encoded Ed25519 seed.
func decodePrivateKey(s string) (ed25519.PrivateKey, error) {
	seed, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode private key hex: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed length: got %d, want %d", len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
