package apiauth

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// DecodeEncryptionKey decodes a hex-encoded 32-byte key.
func DecodeEncryptionKey(hexKey string) ([]byte, error) {
	if hexKey == "" {
		return nil, errors.New("encryption key is required")
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	return key, nil
}
