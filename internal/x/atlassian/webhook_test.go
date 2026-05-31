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

func TestChangelogAddedLabels(t *testing.T) {
	tests := []struct {
		name      string
		changelog *Changelog
		expected  []string
	}{
		{
			name:      "NilChangelog",
			changelog: nil,
			expected:  nil,
		},
		{
			name:      "NoItems",
			changelog: &Changelog{},
			expected:  nil,
		},
		{
			name: "SingleLabelAdded",
			changelog: &Changelog{Items: []ChangelogItem{
				{Field: "labels", FromString: "", ToString: "xagent"},
			}},
			expected: []string{"xagent"},
		},
		{
			name: "LabelAddedToExistingSet",
			changelog: &Changelog{Items: []ChangelogItem{
				{Field: "labels", FromString: "bug urgent", ToString: "bug urgent xagent"},
			}},
			expected: []string{"xagent"},
		},
		{
			name: "MultipleLabelsAdded",
			changelog: &Changelog{Items: []ChangelogItem{
				{Field: "labels", FromString: "bug", ToString: "bug xagent triage"},
			}},
			expected: []string{"xagent", "triage"},
		},
		{
			name: "LabelRemovedOnly",
			changelog: &Changelog{Items: []ChangelogItem{
				{Field: "labels", FromString: "bug xagent", ToString: "bug"},
			}},
			expected: nil,
		},
		{
			name: "MixedAddAndRemove",
			changelog: &Changelog{Items: []ChangelogItem{
				{Field: "labels", FromString: "bug urgent", ToString: "bug xagent"},
			}},
			expected: []string{"xagent"},
		},
		{
			name: "UnrelatedFieldIgnored",
			changelog: &Changelog{Items: []ChangelogItem{
				{Field: "status", FromString: "To Do", ToString: "In Progress"},
			}},
			expected: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.changelog.AddedLabels()
			assert.DeepEqual(t, got, tt.expected)
		})
	}
}
