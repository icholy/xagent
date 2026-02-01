package configfile

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
		jf.PrivateKey = string(encodePrivateKey(f.PrivateKey))
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
		key, err := decodePrivateKey([]byte(jf.PrivateKey))
		if err != nil {
			return err
		}
		f.PrivateKey = key
	}
	return nil
}

func encodePrivateKey(key ed25519.PrivateKey) []byte {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(fmt.Sprintf("marshal private key: %v", err))
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
}

func decodePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM data")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an Ed25519 private key")
	}
	return priv, nil
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
