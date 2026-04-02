package atlassian

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestVerifyWebhook(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"test": "payload"}`)
	validSig := SignWebhook(body, secret)

	tests := []struct {
		name      string
		signature string
		errMsg    string
	}{
		{
			name:      "ValidSignature",
			signature: validSig,
			errMsg:    "",
		},
		{
			name:      "InvalidSignature",
			signature: "sha256=deadbeef",
			errMsg:    "signature mismatch",
		},
		{
			name:      "MissingSignature",
			signature: "",
			errMsg:    "missing X-Hub-Signature header",
		},
		{
			name:      "UnsupportedFormat",
			signature: "sha1=abc",
			errMsg:    "unsupported signature format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyWebhook(body, tt.signature, secret)
			if tt.errMsg == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.errMsg)
			}
		})
	}
}

func TestIssueBrowseURL(t *testing.T) {
	tests := []struct {
		name     string
		issue    Issue
		expected string
	}{
		{
			name: "ValidSelfLink",
			issue: Issue{
				Key:  "PROJ-123",
				Self: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
			},
			expected: "https://mycompany.atlassian.net/browse/PROJ-123",
		},
		{
			name: "InvalidSelfLink",
			issue: Issue{
				Key:  "PROJ-123",
				Self: "invalid-url",
			},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.issue.BrowseURL()
			assert.Equal(t, got, tt.expected)
		})
	}
}
