package agentauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-jwt/jwt/v4"
)

// TaskClaims contains the JWT claims for a task's identity.
type TaskClaims struct {
	jwt.RegisteredClaims
	TaskID    int64  `json:"task_id"`
	Workspace string `json:"workspace"`
	Runner    string `json:"runner"`
}

// CreatePrivateKey generates a new Ed25519 private key.
func CreatePrivateKey() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return priv, nil
}

// DecodePrivateKey decodes a PEM-encoded PKCS#8 Ed25519 private key.
func DecodePrivateKey(data []byte) (ed25519.PrivateKey, error) {
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

// LoadOrCreatePrivateKey loads an Ed25519 private key from a file,
// or generates and saves one if it doesn't exist.
func LoadOrCreatePrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return DecodePrivateKey(data)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	// Generate new key
	priv, err := CreatePrivateKey()
	if err != nil {
		return nil, err
	}
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create key directory: %w", err)
	}
	// Marshal to PKCS#8 format
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		return nil, fmt.Errorf("write key file: %w", err)
	}
	return priv, nil
}

// SignToken creates a JWT signed with the private key.
func SignToken(key ed25519.PrivateKey, claims *TaskClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(key)
}

// VerifyToken verifies and parses a JWT, returning the claims.
func VerifyToken(key ed25519.PrivateKey, tokenStr string) (*TaskClaims, error) {
	// Ed25519 public key is derived from private key
	pubKey := key.Public()
	token, err := jwt.ParseWithClaims(tokenStr, &TaskClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(*TaskClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}
