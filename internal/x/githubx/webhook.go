package githubx

import (
	"fmt"
	"net/http"

	"github.com/google/go-github/v68/github"
)

// ParseWebHook parses a webhook request.
// If a secret is provided, the signature is validated.
func ParseWebHook(body []byte, h http.Header, secret []byte) (any, error) {
	if len(secret) > 0 {
		signature := h.Get(github.SHA256SignatureHeader)
		if signature == "" {
			signature = h.Get(github.SHA1SignatureHeader)
		}
		if signature == "" {
			return nil, fmt.Errorf("missing request signature header: %s", github.SHA256SignatureHeader)
		}
		if err := github.ValidateSignature(signature, body, secret); err != nil {
			return nil, err
		}
	}
	eventType := h.Get(github.EventTypeHeader)
	if eventType == "" {
		return nil, fmt.Errorf("missing request event type header: %s", github.EventTypeHeader)
	}
	return github.ParseWebHook(eventType, body)
}
