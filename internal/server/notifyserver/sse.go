package notifyserver

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/sse"
)

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
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

	s.streamSSE(w, r, orgID, func(n model.Notification) bool {
		return n.UserID != caller.ID
	})
}

func (s *Server) handleRunnerSSE(w http.ResponseWriter, r *http.Request) {
	caller := apiauth.MustCaller(r.Context())
	orgID := caller.OrgID

	s.streamSSE(w, r, orgID, func(n model.Notification) bool {
		for _, r := range n.Resources {
			if r.Type == "task" {
				return true
			}
		}
		return false
	})
}

func (s *Server) streamSSE(w http.ResponseWriter, r *http.Request, orgID int64, filter func(model.Notification) bool) {
	ch, cancel, err := s.subscriber.Subscribe(r.Context(), orgID)
	if err != nil {
		http.Error(w, "subscribe failed", http.StatusInternalServerError)
		return
	}
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sw := sse.NewWriter(w)
	var seq int64

	// Send ready event so clients know the subscription is live.
	data, err := json.Marshal(model.Notification{Type: "ready", OrgID: orgID})
	if err != nil {
		return
	}
	if err := sw.Write(sse.Event{
		ID:    strconv.FormatInt(seq, 10),
		Event: "ready",
		Data:  data,
	}); err != nil {
		return
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case n := <-ch:
			if !filter(n) {
				continue
			}
			seq++
			data, err := json.Marshal(n)
			if err != nil {
				s.log.Warn("failed to marshal notification", "error", err)
				continue
			}
			if err := sw.Write(sse.Event{
				ID:    strconv.FormatInt(seq, 10),
				Event: n.Type,
				Data:  data,
			}); err != nil {
				return
			}
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}
