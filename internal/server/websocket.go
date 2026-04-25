package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/icholy/xagent/internal/apiauth"
	"nhooyr.io/websocket"
)

const maxConnsPerOrg = 100

type wsConfig struct {
	pingInterval time.Duration
}

// orgConns tracks active WebSocket connections per org.
type orgConns struct {
	mu     sync.Mutex
	counts map[int64]int
}

func (oc *orgConns) add(orgID int64) bool {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	if oc.counts[orgID] >= maxConnsPerOrg {
		return false
	}
	oc.counts[orgID]++
	return true
}

func (oc *orgConns) remove(orgID int64) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.counts[orgID]--
	if oc.counts[orgID] <= 0 {
		delete(oc.counts, orgID)
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	caller := apiauth.MustCaller(r.Context())

	if !s.wsConns.add(caller.OrgID) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.wsConns.remove(caller.OrgID)
		return
	}
	defer func() {
		conn.CloseNow()
		s.wsConns.remove(caller.OrgID)
	}()

	ch, cancel, err := s.subscriber.Subscribe(r.Context(), caller.OrgID)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer cancel()

	// Start ping loop
	ctx := r.Context()
	go func() {
		ticker := time.NewTicker(s.wsCfg.pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
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
