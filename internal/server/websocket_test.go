package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
	"nhooyr.io/websocket"
)

func TestWebSocket(t *testing.T) {
	t.Parallel()

	ps := pubsub.NewLocalPubSub()
	st := teststore.New(t)
	srv := New(Options{
		Store:      st,
		Publisher:  ps,
		Subscriber: ps,
	})
	org := teststore.CreateOrg(t, st, nil)

	// Create HTTP test server with auth context injected
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(apiauth.WithUser(r.Context(), &apiauth.UserInfo{ID: org.UserID, OrgID: org.OrgID}))
		srv.handleWebSocket(w, r)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Connect WebSocket client
	ctx := t.Context()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	assert.NilError(t, err)
	defer conn.CloseNow()

	// Read the server's ready frame to ensure Subscribe has registered
	// before we publish.
	readReady(t, ctx, conn)

	// Publish a notification
	want := pubsub.Notification{
		Type:     "created",
		Resource: "task",
		ID:       42,
		OrgID:    org.OrgID,
		Version:  1,
		Time:     time.Now().Truncate(time.Second),
	}
	err = ps.Publish(ctx, org.OrgID, want)
	assert.NilError(t, err)

	// Read the notification from the WebSocket
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	typ, data, err := conn.Read(readCtx)
	assert.NilError(t, err)
	assert.Equal(t, typ, websocket.MessageText)

	var got pubsub.Notification
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

	ps := pubsub.NewLocalPubSub()
	st := teststore.New(t)
	srv := New(Options{
		Store:      st,
		Publisher:  ps,
		Subscriber: ps,
	})
	orgA := teststore.CreateOrg(t, st, nil)
	orgB := teststore.CreateOrg(t, st, nil)

	// Create test server that routes to the correct org based on query param
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgIDStr := r.URL.Query().Get("org")
		var user *apiauth.UserInfo
		if orgIDStr == "a" {
			user = &apiauth.UserInfo{ID: orgA.UserID, OrgID: orgA.OrgID}
		} else {
			user = &apiauth.UserInfo{ID: orgB.UserID, OrgID: orgB.OrgID}
		}
		r = r.WithContext(apiauth.WithUser(r.Context(), user))
		srv.handleWebSocket(w, r)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx := t.Context()
	baseURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Connect org B subscriber
	connB, _, err := websocket.Dial(ctx, baseURL+"/?org=b", nil)
	assert.NilError(t, err)
	defer connB.CloseNow()
	readReady(t, ctx, connB)

	// Publish to org A only
	err = ps.Publish(ctx, orgA.OrgID, pubsub.Notification{
		Type:     "created",
		Resource: "task",
		ID:       1,
		OrgID:    orgA.OrgID,
	})
	assert.NilError(t, err)

	// Org B should not receive anything - read should fail
	readCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, _, err = connB.Read(readCtx)
	assert.Assert(t, err != nil, "expected read to fail for wrong org")
}

func readReady(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	assert.NilError(t, err)
	var n pubsub.Notification
	assert.NilError(t, json.Unmarshal(data, &n))
	assert.Equal(t, n.Type, "ready")
}
