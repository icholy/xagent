// Package shellserver owns the server-side registry and HTTP handlers for the
// driver reverse shell (the design in proposals/draft/driver-reverse-shell.md).
//
// It tracks rendezvous sessions by id, owns the org each session belongs to, and
// applies the leg policy the relay is deliberately ignorant of: the pre-upgrade
// reject of an unknown session (a plain HTTP 404 before the WebSocket handshake),
// subprotocol negotiation on the attach leg, and the org-membership check that
// authorizes an operator. The transport mechanics of a single rendezvous live one
// layer down in internal/shell/shellrelay.
//
// The registry is in-memory and therefore assumes a single server instance.
// Cross-instance rendezvous (routing both legs to the session owner, or a shared
// bus) is explicitly out of scope for v1.
package shellserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/shell/shellrelay"
	"github.com/icholy/xagent/internal/shell/shellwire"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Registry tracks rendezvous sessions by id for a single server instance.
type Registry struct {
	log              *slog.Logger
	establishTimeout time.Duration
	onClose          func(session string, orgID int64)

	mu       sync.Mutex
	sessions map[string]*entry
}

// entry is a registered session plus the identity that owns it: the attach leg
// is authorized iff the caller belongs to orgID, and the driver leg iff the
// caller's token is scoped to taskID (the task whose sandbox serves the shell).
type entry struct {
	session *shellrelay.Session
	orgID   int64
	taskID  int64
}

// Options configures New.
//
// A nil Log falls back to slog.Default. EstablishTimeout <= 0 falls back to
// shellrelay.DefaultEstablishTimeout; tests inject a small timeout to exercise
// the establishment-timeout path without sleeping the real default.
//
// OnClose, if non-nil, is invoked exactly once per session after it tears down
// and is evicted from the registry — regardless of teardown reason (normal
// exit, dropped leg, establishment timeout, or Close). It receives the session
// id and owning org, letting the caller react to teardown (e.g. clear the
// task's shell_session) while keeping the registry decoupled from the store.
type Options struct {
	Log              *slog.Logger
	EstablishTimeout time.Duration
	OnClose          func(session string, orgID int64)
}

// New creates a session registry.
func New(opts Options) *Registry {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	establishTimeout := opts.EstablishTimeout
	if establishTimeout <= 0 {
		establishTimeout = shellrelay.DefaultEstablishTimeout
	}
	r := &Registry{
		log:              log,
		establishTimeout: establishTimeout,
		onClose:          opts.OnClose,
		sessions:         make(map[string]*entry),
	}
	r.registerMetrics()
	return r
}

// registerMetrics registers an observable gauge reporting the number of
// currently active shell rendezvous sessions. The callback reads the live
// registry size, so it stays correct across seed and eviction without
// increment/decrement bookkeeping. Registration failures are logged rather
// than fatal — metrics are best-effort and must not block serving shells.
func (r *Registry) registerMetrics() {
	meter := otel.Meter("github.com/icholy/xagent/internal/server/shellserver")
	_, err := meter.Int64ObservableGauge(
		"xagent.shell.active_sessions",
		metric.WithDescription("Number of currently active shell rendezvous sessions."),
		metric.WithUnit("{session}"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			o.Observe(int64(len(r.sessions)))
			return nil
		}),
	)
	if err != nil {
		r.log.Warn("failed to register shell session metric", "error", err)
	}
}

// Seed registers a new rendezvous session owned by orgID and served by taskID's
// sandbox, and starts its establishment timeout. The attach leg is authorized
// against this org; the driver leg against this task.
func (r *Registry) Seed(id string, orgID, taskID int64) error {
	if id == "" {
		return errors.New("shellserver: empty session id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[id]; ok {
		return fmt.Errorf("shellserver: session %q already exists", id)
	}
	session := shellrelay.NewSession(r.establishTimeout, r.log.With("session", id))
	r.sessions[id] = &entry{session: session, orgID: orgID, taskID: taskID}
	// Evict the session from the map once it tears down — regardless of which path
	// (establishment timeout with zero or one leg, a dropped leg, or Close) got
	// there. This is the single place a session leaves the map.
	go func() {
		<-session.Done()
		r.remove(id)
		if r.onClose != nil {
			r.onClose(id, orgID)
		}
	}()
	return nil
}

// Close tears down every live session, disconnecting any connected legs. It is
// the registry-level cleanup hook for server shutdown.
func (r *Registry) Close() {
	r.mu.Lock()
	sessions := make([]*shellrelay.Session, 0, len(r.sessions))
	for _, e := range r.sessions {
		sessions = append(sessions, e.session)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		s.Close()
	}
}

// Has reports whether a session with the given id is registered. Used by tests.
func (r *Registry) Has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.sessions[id]
	return ok
}

// remove deletes the session for id from the map. Idempotent.
func (r *Registry) remove(id string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
}

// lookup returns the entry for id, or nil if none is registered.
func (r *Registry) lookup(id string) *entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

// DriverHandler handles the driver leg (GET /shell/driver?session=<id>).
//
// The surrounding RequireAuth middleware validates the driver's task token (a
// Bearer app JWT) and populates the caller, but a valid token only proves the
// caller is *some* task in the org — not the task that owns this session. Without
// a further check, a compromised agent in task A (holding task A's valid token)
// could dial task B's driver leg and seize B's driver slot (first-leg-wins, and
// the sandbox takes seconds to boot, so there is a race window), landing the
// operator in an attacker-controlled shell.
//
// So we bind the leg to the session's task using the same scope engine GetTask
// uses for its own-task read: the caller must hold task-read scoped to the
// session's task id. Legitimate driver tokens carry
// task.read:{task.id:<own>, task.archived:false} (see agentauth.Scopes), so the
// request must present both the task id and archived:false to satisfy that
// predicate — a token scoped to a different task fails the task.id predicate and
// is rejected before the WebSocket upgrade.
func (r *Registry) DriverHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := req.URL.Query().Get("session")
		e := r.lookup(id)
		if e == nil {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		caller := apiauth.Caller(req.Context())
		if caller == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if !caller.Scopes.Allow(authscope.OpTaskRead,
			authscope.WithTaskID(e.taskID), authscope.WithTaskArchived(false)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			r.log.Debug("driver leg accept failed", "session", id, "error", err)
			return
		}
		_ = e.session.Join(req.Context(), conn)
	})
}

// AttachHandler handles the operator leg (GET /shell/attach?session=<id>).
//
// The session id is not a secret, so access control is by org membership: the
// authenticated caller (populated by the Bearer auth middleware) must belong to
// the session's owning org. Any member of that org may attach (one at a time).
//
// Session existence (404) and the org check (401/403) are enforced before the
// upgrade. The subprotocol is negotiated by websocket.Accept and validated after
// it: it is a non-secret version token, not a credential, and access is already
// gated by the pre-upgrade org check, so a bad/missing subprotocol is an
// upgrade-then-close rather than a pre-upgrade rejection.
func (r *Registry) AttachHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := req.URL.Query().Get("session")
		e := r.lookup(id)
		if e == nil {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		caller := apiauth.Caller(req.Context())
		if caller == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if caller.OrgID != e.orgID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
			Subprotocols: []string{shellwire.Subprotocol},
		})
		if err != nil {
			r.log.Debug("attach leg accept failed", "session", id, "error", err)
			return
		}
		// Accept negotiates the subprotocol; an empty result means the client did
		// not offer the version token we speak.
		if conn.Subprotocol() != shellwire.Subprotocol {
			conn.Close(websocket.StatusPolicyViolation, "unsupported subprotocol")
			return
		}
		_ = e.session.Join(req.Context(), conn)
	})
}
