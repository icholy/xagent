// Package shellrelay implements the server-side rendezvous relay for the driver
// reverse shell (the design in proposals/draft/driver-reverse-shell.md).
//
// A rendezvous session bridges two WebSocket legs: a driver leg (dialed from
// inside the sandbox) and an attach leg (dialed by the operator's CLI or, later,
// a browser terminal). Once both legs are connected the relay copies WebSocket
// frames verbatim in both directions. The relay is a mode-agnostic byte pump: it
// never parses or interprets the frame payload. The end-to-end [1-byte type]
// [payload] framing is a contract between the driver and the client, opaque to
// the server.
//
// This package holds only the transport mechanics: the session registry, the
// two-leg rendezvous, the establishment timeout, the verbatim pump, and the
// subprotocol negotiation. Authorization policy that depends on the caller (the
// attach leg's org check) is injected by the server layer as an AuthorizeAttach
// callback — see internal/server/shellserver.
//
// The registry is in-memory and therefore assumes a single server instance.
// Cross-instance rendezvous (routing both legs to the session owner, or a shared
// bus) is explicitly out of scope for v1.
package shellrelay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/icholy/xagent/internal/shell"
	"github.com/icholy/xagent/internal/shell/shellwire"
)

// DefaultEstablishTimeout is the default connection-establishment timeout: if
// both legs are not connected within it, the session is torn down and any
// already-connected leg is disconnected. This is distinct from the idle/
// max-session timeout, which is a later step and not implemented here.
const DefaultEstablishTimeout = 30 * time.Second

var (
	errLegTaken = errors.New("shellrelay: leg already connected")
	errTornDown = errors.New("shellrelay: session torn down")
)

// AuthorizeAttach authorizes an attach request against the session's owning org.
// It runs after the subprotocol and session-existence checks pass but before the
// WebSocket is accepted. It returns true to allow the connection; on rejection it
// must write the HTTP response itself and return false.
//
// The org-match policy lives at the server layer (internal/server/shellserver),
// which reads the authenticated caller from the request context. The relay only
// knows the session's owning org, so it hands that org to the callback.
type AuthorizeAttach func(w http.ResponseWriter, req *http.Request, sessionOrgID int64) bool

// Registry tracks rendezvous sessions by id for a single server instance.
type Registry struct {
	log              *slog.Logger
	establishTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*session
}

// session holds the two legs of a rendezvous plus its lifecycle state. All
// mutable fields are guarded by mu.
type session struct {
	id string
	// orgID owns the session: the attach leg is authorized iff the caller
	// belongs to this org. It is supplied via Seed (OpenShell derives it from the
	// target task).
	orgID int64

	mu        sync.Mutex
	driver    *websocket.Conn // driver leg, nil until connected
	attach    *websocket.Conn // operator leg, nil until connected
	ready     chan struct{}   // closed once both legs are connected
	done      chan struct{}   // closed on teardown
	timer     *time.Timer     // establishment timeout
	closeOnce sync.Once
}

// NewRegistry creates a session registry. establishTimeout <= 0 falls back to
// DefaultEstablishTimeout. Tests inject a small timeout to exercise the
// establishment-timeout path without sleeping the real default.
func NewRegistry(log *slog.Logger, establishTimeout time.Duration) *Registry {
	if log == nil {
		log = slog.Default()
	}
	if establishTimeout <= 0 {
		establishTimeout = DefaultEstablishTimeout
	}
	return &Registry{
		log:              log,
		establishTimeout: establishTimeout,
		sessions:         make(map[string]*session),
	}
}

// Seed registers a new rendezvous session owned by orgID and starts the
// establishment timeout. The attach leg is authorized against this org.
func (r *Registry) Seed(id string, orgID int64) error {
	if id == "" {
		return errors.New("shellrelay: empty session id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[id]; ok {
		return fmt.Errorf("shellrelay: session %q already exists", id)
	}
	s := &session{
		id:    id,
		orgID: orgID,
		ready: make(chan struct{}),
		done:  make(chan struct{}),
	}
	// Assign the timer under s.mu so the AfterFunc callback (which stops it via
	// teardown) has a happens-before edge to this write.
	s.mu.Lock()
	s.timer = time.AfterFunc(r.establishTimeout, func() {
		r.teardown(s, "establishment timeout")
	})
	s.mu.Unlock()
	r.sessions[id] = s
	return nil
}

// Close tears down every live session, disconnecting any connected legs. It is
// the registry-level cleanup hook for server shutdown.
func (r *Registry) Close() {
	r.mu.Lock()
	sessions := make([]*session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		r.teardown(s, "registry closed")
	}
}

// Has reports whether a session with the given id is registered. Used by tests.
func (r *Registry) Has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.sessions[id]
	return ok
}

// lookup returns the session for id, or nil if none is registered.
func (r *Registry) lookup(id string) *session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

// teardown deletes the session and closes both legs. It is idempotent: repeated
// calls (from the timer and from either pump goroutine) collapse to one via
// closeOnce.
func (r *Registry) teardown(s *session, reason string) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if s.timer != nil {
			s.timer.Stop()
		}
		driver, attach := s.driver, s.attach
		s.mu.Unlock()

		close(s.done)

		r.mu.Lock()
		delete(r.sessions, s.id)
		r.mu.Unlock()
		if driver != nil {
			driver.Close(websocket.StatusNormalClosure, reason)
		}
		if attach != nil {
			attach.Close(websocket.StatusNormalClosure, reason)
		}
		r.log.Debug("shell session torn down", "session", s.id, "reason", reason)
	})
}

// attachLeg registers conn as the driver or attach leg and returns the ready
// channel, which is closed once both legs are present. It errors if that leg is
// already taken or the session is being torn down.
func (s *session) attachLeg(conn *websocket.Conn, driver bool) (<-chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return nil, errTornDown
	default:
	}
	if driver {
		if s.driver != nil {
			return nil, errLegTaken
		}
		s.driver = conn
	} else {
		if s.attach != nil {
			return nil, errLegTaken
		}
		s.attach = conn
	}
	if s.driver != nil && s.attach != nil {
		s.timer.Stop()
		close(s.ready)
	}
	return s.ready, nil
}

// peer returns the opposite leg from the caller's perspective.
func (s *session) peer(driver bool) *websocket.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if driver {
		return s.attach
	}
	return s.driver
}

// DriverHandler handles the driver leg (GET /shell/{session}/driver).
//
// Authentication is expected to be enforced by the surrounding middleware: this
// handler is mounted behind the same server auth as the other driver->server
// endpoints, which validates the driver's task token (a Bearer app JWT).
func (r *Registry) DriverHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("session")
		s := r.lookup(id)
		if s == nil {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			r.log.Debug("driver leg accept failed", "session", id, "error", err)
			return
		}
		conn.SetReadLimit(shell.ReadLimit)
		r.relay(s, conn, true)
	})
}

// AttachHandler handles the operator leg (GET /shell/{session}/attach).
//
// The subprotocol negotiates the version token only; it carries no credential.
// The session id is not a secret, so access control is by org membership: the
// authorize callback (wired by the server layer) decides whether the caller may
// attach to the session's owning org.
func (r *Registry) AttachHandler(authorize AuthorizeAttach) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := req.PathValue("session")
		if version := parseVersion(req); version != shellwire.Subprotocol {
			http.Error(w, "unsupported subprotocol", http.StatusBadRequest)
			return
		}
		s := r.lookup(id)
		if s == nil {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		if !authorize(w, req, s.orgID) {
			// The callback wrote the rejection response.
			return
		}
		conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
			Subprotocols: []string{shellwire.Subprotocol},
		})
		if err != nil {
			r.log.Debug("attach leg accept failed", "session", id, "error", err)
			return
		}
		conn.SetReadLimit(shell.ReadLimit)
		r.relay(s, conn, false)
	})
}

// relay registers conn as a leg, waits for the other leg (or teardown), then
// pumps frames from conn to its peer verbatim. When the pump ends — for any
// reason — the whole session is torn down, closing both legs.
func (r *Registry) relay(s *session, conn *websocket.Conn, driver bool) {
	ready, err := s.attachLeg(conn, driver)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "leg unavailable")
		return
	}
	select {
	case <-ready:
	case <-s.done:
		conn.Close(websocket.StatusNormalClosure, "session closed")
		return
	}
	peer := s.peer(driver)
	// Pump until either side errors/closes; the read error is the normal
	// end-of-session signal.
	pumpErr := pump(context.Background(), peer, conn)
	r.teardown(s, closeReason(pumpErr))
}

// pump copies whole WebSocket messages from src to dst verbatim, preserving the
// message type, until a read or write error occurs. It never inspects the
// payload — the server is a mode-agnostic byte pump.
func pump(ctx context.Context, dst, src *websocket.Conn) error {
	for {
		typ, data, err := src.Read(ctx)
		if err != nil {
			return err
		}
		if err := dst.Write(ctx, typ, data); err != nil {
			return err
		}
	}
}

// parseVersion returns the first entry of the client's Sec-WebSocket-Protocol
// offer, which per the contract is the version token. The header is a
// comma-separated list; a missing header yields "".
func parseVersion(req *http.Request) string {
	for _, v := range req.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				return p
			}
		}
	}
	return ""
}

// closeReason renders a short teardown reason from the pump's terminating error.
func closeReason(err error) string {
	if err == nil {
		return "closed"
	}
	if status := websocket.CloseStatus(err); status != -1 {
		return "peer closed"
	}
	return "relay ended"
}
