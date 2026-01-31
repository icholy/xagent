package apiauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const apiKeyPrefix = "xat_"

// GenerateKey generates a new API key with the xat_ prefix.
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return apiKeyPrefix + hex.EncodeToString(b), nil
}

// HashKey returns the SHA-256 hex digest of the given raw key.
func HashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// IsKey returns true if the token has the xat_ prefix.
func IsKey(token string) bool {
	return strings.HasPrefix(token, apiKeyPrefix)
}
