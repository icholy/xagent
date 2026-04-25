package websocketserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
	"nhooyr.io/websocket"
)

func TestWebSocket(t *testing.T) {
	t.Parallel()

	wss := New()
	orgID := int64(1)
	userID := "user-1"

	// Create HTTP test server with auth context injected
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(apiauth.WithUser(r.Context(), &apiauth.UserInfo{ID: userID, OrgID: orgID}))
		wss.handleWebSocket(w, r)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Connect WebSocket client
	ctx := t.Context()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	assert.NilError(t, err)
	defer conn.CloseNow()

	// Publish a notification
	want := model.Notification{
		Type:     "created",
		Resource: "task",
		ID:       42,
		OrgID:    orgID,
		Version:  1,
		Time:     time.Now().Truncate(time.Second),
	}
	err = wss.Publish(ctx, orgID, want)
	assert.NilError(t, err)

	// Read the notification from the WebSocket
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	typ, data, err := conn.Read(readCtx)
	assert.NilError(t, err)
	assert.Equal(t, typ, websocket.MessageText)

	var got model.Notification
	err = json.Unmarshal(data, &got)
	assert.NilError(t, err)
	assert.Equal(t, got.Type, want.Type)
	assert.Equal(t, got.Resource, want.Resource)
	assert.Equal(t, got.ID, want.ID)
	assert.Equal(t, got.OrgID, want.OrgID)
	assert.Equal(t, got.Version, want.Version)
}

func TestWebSocket_OrgIsolation(t *testing.T) {
	t.Parallel()

	wss := New()
	orgAID := int64(1)
	orgBID := int64(2)

	// Create test server that routes to the correct org based on query param
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgIDStr := r.URL.Query().Get("org")
		var user *apiauth.UserInfo
		if orgIDStr == "a" {
			user = &apiauth.UserInfo{ID: "user-a", OrgID: orgAID}
		} else {
			user = &apiauth.UserInfo{ID: "user-b", OrgID: orgBID}
		}
		r = r.WithContext(apiauth.WithUser(r.Context(), user))
		wss.handleWebSocket(w, r)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx := t.Context()
	baseURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect org B subscriber
	connB, _, err := websocket.Dial(ctx, baseURL+"/?org=b", nil)
	assert.NilError(t, err)
	defer connB.CloseNow()

	// Publish to org A only
	err = wss.Publish(ctx, orgAID, model.Notification{
		Type:     "created",
		Resource: "task",
		ID:       1,
		OrgID:    orgAID,
	})
	assert.NilError(t, err)

	// Org B should not receive anything - read should fail
	readCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, _, err = connB.Read(readCtx)
	assert.Assert(t, err != nil, "expected read to fail for wrong org")
}
