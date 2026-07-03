// Package shellserver is the server-side adapter for the driver reverse shell. It
// holds the in-memory rendezvous relay (internal/shell/shellrelay) and wires the
// attach leg's authorization to the server's auth: the transport mechanics live
// in the relay, while the org-membership policy — which depends on the
// authenticated caller — lives here.
package shellserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/shell/shellrelay"
)

// DefaultEstablishTimeout is the relay's connection-establishment timeout.
const DefaultEstablishTimeout = shellrelay.DefaultEstablishTimeout

// Registry adapts the shell rendezvous relay to the server: it constructs and
// holds the relay registry and exposes the two leg handlers with the attach leg
// authorized by org membership.
type Registry struct {
	relay *shellrelay.Registry
}

// New creates a shell registry backed by the rendezvous relay. establishTimeout
// <= 0 falls back to shellrelay.DefaultEstablishTimeout.
func New(log *slog.Logger, establishTimeout time.Duration) *Registry {
	return &Registry{relay: shellrelay.NewRegistry(log, establishTimeout)}
}

// Seed registers a new rendezvous session owned by orgID.
func (r *Registry) Seed(id string, orgID int64) error {
	return r.relay.Seed(id, orgID)
}

// Close tears down every live session. It is the server-shutdown cleanup hook.
func (r *Registry) Close() {
	r.relay.Close()
}

// DriverHandler returns the handler for the driver leg. Authentication is
// enforced by the surrounding server middleware (the driver's task token).
func (r *Registry) DriverHandler() http.Handler {
	return r.relay.DriverHandler()
}

// AttachHandler returns the handler for the operator leg, authorized against the
// session's owning org. The caller is read from the request context (populated by
// the server's Bearer auth middleware); any member of the task's org may attach.
func (r *Registry) AttachHandler() http.Handler {
	return r.relay.AttachHandler(authorizeOrg)
}

// authorizeOrg allows the attach request iff the authenticated caller belongs to
// the session's owning org. It writes the rejection response and returns false
// when the caller is absent or in a different org.
func authorizeOrg(w http.ResponseWriter, req *http.Request, sessionOrgID int64) bool {
	caller := apiauth.Caller(req.Context())
	if caller == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return false
	}
	if caller.OrgID != sessionOrgID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}
