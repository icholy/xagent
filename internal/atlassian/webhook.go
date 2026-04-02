package atlassian

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// WebhookPayload represents the relevant fields of a Jira Cloud webhook payload.
type WebhookPayload struct {
	WebhookEvent string   `json:"webhookEvent"`
	Comment      *Comment `json:"comment"`
	Issue        *Issue   `json:"issue"`
}

// Comment represents a Jira issue comment from a webhook payload.
type Comment struct {
	Body   string `json:"body"`
	Author User   `json:"author"`
}

// User represents a Jira user reference in a webhook payload.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

// Issue represents a Jira issue from a webhook payload.
type Issue struct {
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
	Self   string      `json:"self"`
}

// IssueFields represents the fields of a Jira issue.
type IssueFields struct {
	Summary string `json:"summary"`
}

// ParseWebhook parses a Jira webhook payload from JSON.
func ParseWebhook(body []byte) (*WebhookPayload, error) {
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// IssueURL constructs the browse URL for a Jira issue from the API self link and issue key.
// The self link is like "https://mycompany.atlassian.net/rest/api/2/issue/12345".
func IssueURL(selfLink, issueKey string) string {
	idx := strings.Index(selfLink, "/rest/")
	if idx == -1 {
		return ""
	}
	return selfLink[:idx] + "/browse/" + issueKey
}

// SignWebhook computes the HMAC-SHA256 signature for a webhook payload.
// Returns the signature in "sha256=<hex>" format.
func SignWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhook verifies the HMAC-SHA256 signature from Jira Cloud webhooks.
// The signature header value should be in the format "sha256=<hex>".
func VerifyWebhook(body []byte, signature, secret string) error {
	if signature == "" {
		return fmt.Errorf("missing X-Hub-Signature header")
	}
	parts := strings.SplitN(signature, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return fmt.Errorf("unsupported signature format: %s", signature)
	}
	sigBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(sigBytes, expected) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
