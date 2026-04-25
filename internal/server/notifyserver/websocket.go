package notifyserver

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/coder/websocket"
)

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	caller := apiauth.MustCaller(r.Context())

	var orgID int64
	if raw := r.URL.Query().Get("org_id"); raw != "" {
		var err error
		orgID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid org_id", http.StatusBadRequest)
			return
		}
	}
	orgID, err := s.orgResolver.ResolveOrg(r.Context(), caller.ID, orgID)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ch, cancel, err := s.subscriber.Subscribe(r.Context(), orgID)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer cancel()

	// Send a ready frame so clients know the subscription is live and any
	// notifications published from this point forward will be delivered.
	ready, err := json.Marshal(model.Notification{Type: "ready", OrgID: orgID})
	if err != nil {
		return
	}
	if err := conn.Write(r.Context(), websocket.MessageText, ready); err != nil {
		return
	}

	ctx := r.Context()

	// Read loop to detect client disconnection and process control frames.
	// Without this, the connection has no way to observe a client-side close.
	go func() {
		defer conn.CloseNow()
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
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
