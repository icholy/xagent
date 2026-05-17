package model

import "time"

// PendingIntegrationType identifies the kind of integration a pending row belongs to.
type PendingIntegrationType string

const (
	PendingIntegrationTypeMCP PendingIntegrationType = "mcp"
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
	MCP *MCPPendingIntegration `json:"mcp,omitempty"`
}

// MCPPendingIntegration carries the data captured during an MCP Dynamic Client
// Registration call (RFC 7591).
type MCPPendingIntegration struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}
