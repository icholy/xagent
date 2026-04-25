package notifyserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/coder/websocket"
)

const (
	wsPingInterval = 30 * time.Second
	wsPongTimeout  = 10 * time.Second
)

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	caller := apiauth.MustCaller(r.Context())

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ch, cancel, err := s.subscriber.Subscribe(r.Context(), caller.OrgID)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer cancel()

	// Send a ready frame so clients know the subscription is live and any
	// notifications published from this point forward will be delivered.
	ready, err := json.Marshal(model.Notification{Type: "ready", OrgID: caller.OrgID})
	if err != nil {
		return
	}
	if err := conn.Write(r.Context(), websocket.MessageText, ready); err != nil {
		return
	}

	// Start ping loop
	ctx := r.Context()
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, wsPongTimeout)
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
			s.log.Warn("failed to marshal notification", "error", err)
			continue
		}
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			return
		}
	}
}
