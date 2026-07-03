package server

import (
	"net/http"

	"github.com/icholy/xagent/internal/auth/apiauth"
)

// authorizeShellAttach authorizes the operator attach leg of a debug-shell
// session: the authenticated caller must belong to the session's owning org. It
// is the org-membership policy the shell relay defers to the server layer — the
// relay supplies the session's org, the server knows the caller (populated by the
// Bearer auth middleware). Any member of the task's org may attach (one at a
// time). On rejection it writes the response and returns false.
func authorizeShellAttach(w http.ResponseWriter, req *http.Request, sessionOrgID int64) bool {
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
