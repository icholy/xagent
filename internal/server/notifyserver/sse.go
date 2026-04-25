package notifyserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
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

	var seq int64

	// Send ready event so clients know the subscription is live.
	ready, err := json.Marshal(model.Notification{Type: "ready", OrgID: orgID})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\nevent: ready\ndata: %s\n\n", seq, ready)
	flusher.Flush()

	ctx := r.Context()
	for n := range ch {
		select {
		case <-ctx.Done():
			return
		default:
		}
		seq++
		data, err := json.Marshal(n)
		if err != nil {
			s.log.Warn("failed to marshal notification", "error", err)
			continue
		}
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, n.Type, data)
		flusher.Flush()
	}
}
