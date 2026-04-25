package notifyserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"gotest.tools/v3/assert"
	"nhooyr.io/websocket"
)

func TestWebSocket(t *testing.T) {
	t.Parallel()

	ps := pubsub.NewLocalPubSub()
	const orgID int64 = 1
	srv := New(Options{Subscriber: ps})

	ts := httptest.NewServer(apiauth.WithTestUser(srv.Handler(), &apiauth.UserInfo{ID: "u", OrgID: orgID}))
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
	want := model.Notification{
		Type:     "created",
		Resource: "task",
		ID:       42,
		OrgID:    orgID,
		Version:  1,
		Time:     time.Now().Truncate(time.Second),
	}
	err = ps.Publish(ctx, orgID, want)
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

	ps := pubsub.NewLocalPubSub()
	const orgA, orgB int64 = 1, 2
	srv := New(Options{Subscriber: ps})
	ts := httptest.NewServer(apiauth.WithTestUser(srv.Handler(), &apiauth.UserInfo{ID: "u", OrgID: orgB}))
	defer ts.Close()

	// Connect org B subscriber
	ctx := t.Context()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	connB, _, err := websocket.Dial(ctx, wsURL, nil)
	assert.NilError(t, err)
	defer connB.CloseNow()
	readReady(t, ctx, connB)

	// Publish to org A only
	err = ps.Publish(ctx, orgA, model.Notification{
		Type:     "created",
		Resource: "task",
		ID:       1,
		OrgID:    orgA,
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
	var n model.Notification
	assert.NilError(t, json.Unmarshal(data, &n))
	assert.Equal(t, n.Type, "ready")
}
