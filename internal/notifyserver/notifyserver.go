// Package notifyserver serves the WebSocket endpoint that fans out
// pubsub notifications to connected clients.
package notifyserver

import (
	"log/slog"
	"net/http"

	"github.com/icholy/xagent/internal/pubsub"
)

// Server handles WebSocket subscriptions backed by a pubsub.Subscriber.
type Server struct {
	log        *slog.Logger
	subscriber pubsub.Subscriber
}

// Options configures a Server.
type Options struct {
	Log        *slog.Logger
	Subscriber pubsub.Subscriber
}

// New returns a new Server.
func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:        log,
		subscriber: opts.Subscriber,
	}
}

// Handler returns the WebSocket HTTP handler. The caller is responsible for
// wrapping it with authentication middleware that populates apiauth.UserInfo
// in the request context.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.handleWebSocket)
}
