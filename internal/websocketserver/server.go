package websocketserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"nhooyr.io/websocket"
)

const (
	defaultPingInterval = 30 * time.Second
	defaultPongTimeout  = 10 * time.Second
)

// Server manages WebSocket connections backed by a local pub/sub.
type Server struct {
	ps           *pubsub.LocalPubSub
	pingInterval time.Duration
	pongTimeout  time.Duration
}

// Option configures the Server.
type Option func(*Server)

// WithPingInterval sets the WebSocket ping interval.
func WithPingInterval(d time.Duration) Option {
	return func(s *Server) {
		s.pingInterval = d
	}
}

// New returns a new WebSocket server.
func New(opts ...Option) *Server {
	s := &Server{
		ps:           pubsub.NewLocalPubSub(),
		pingInterval: defaultPingInterval,
		pongTimeout:  defaultPongTimeout,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Publish delegates to the internal pub/sub.
func (s *Server) Publish(ctx context.Context, orgID int64, n model.Notification) error {
	return s.ps.Publish(ctx, orgID, n)
}

// Handler returns an http.Handler that upgrades connections to WebSocket
// and streams notifications for the authenticated caller's org.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.handleWebSocket)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	caller := apiauth.MustCaller(r.Context())

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ch, cancel, err := s.ps.Subscribe(r.Context(), caller.OrgID)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer cancel()

	// Start ping loop
	ctx := r.Context()
	go func() {
		ticker := time.NewTicker(s.pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, s.pongTimeout)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					conn.CloseNow()
					return
				}
			}
		}
	}()

	for n := range ch {
		data, err := json.Marshal(n)
		if err != nil {
			slog.Warn("failed to marshal notification", "error", err)
			continue
		}
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			return
		}
	}
}
