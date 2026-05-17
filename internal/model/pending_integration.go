package model

import "time"

// PendingIntegrationType identifies the kind of integration a pending row belongs to.
type PendingIntegrationType string

const (
	PendingIntegrationTypeGitHub PendingIntegrationType = "github"
)

// PendingIntegration is a generic row for external clients that have started a
// registration / handshake but have not yet been promoted to a fully-linked
// integration. The (Type, ExternalID) pair is the primary key.
type PendingIntegration struct {
	Type       PendingIntegrationType    `json:"type"`
	ExternalID string                    `json:"external_id"`
	Options    PendingIntegrationOptions `json:"options"`
	CreatedAt  time.Time                 `json:"created_at"`
}

// PendingIntegrationOptions is the typed payload stored in the JSONB options
// column. Each integration type uses a subset of the fields.
type PendingIntegrationOptions struct {
	GitHub *GitHubPendingIntegration `json:"github,omitempty"`
}

// GitHubPendingIntegration captures who initiated a GitHub App installation
// from the App's installation webhook. SenderGitHubUserID is the GitHub user
// who actually clicked "Install" and is the only field used to authorize the
// subsequent LinkGitHubInstallation call.
type GitHubPendingIntegration struct {
	SenderGitHubUserID int64  `json:"sender_github_user_id"`
	AccountLogin       string `json:"account_login"`
	AccountType        string `json:"account_type"`
}
