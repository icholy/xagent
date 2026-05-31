package githubx

import (
	"crypto/rsa"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ParsePrivateKey parses a PEM-encoded RSA private key, tolerating keys that a
// secret store has collapsed onto a single line. sops/jq/fly serialize the key
// with its real newlines escaped as literal "\n" (and sometimes wrapped in
// quotes), which the PEM decoder can't parse; this restores the real newlines
// before parsing.
func ParsePrivateKey(key []byte) (*rsa.PrivateKey, error) {
	s := strings.TrimSpace(string(key))
	s = strings.Trim(s, `"`)
	s = strings.ReplaceAll(s, `\r\n`, "\n")
	s = strings.ReplaceAll(s, `\n`, "\n")
	return jwt.ParseRSAPrivateKeyFromPEM([]byte(s))
}
