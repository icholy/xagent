package notifyserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"gotest.tools/v3/assert"
	"github.com/coder/websocket"
)

// allowOrgResolver returns a mock that allows the given user to subscribe
// to any of the configured orgs and rejects everything else.
func allowOrgResolver(userID string, allow ...int64) *OrgResolverMock {
	allowed := make(map[int64]bool, len(allow))
	for _, id := range allow {
		allowed[id] = true
	}
	return &OrgResolverMock{
		ResolveOrgFunc: func(ctx context.Context, callerID string, orgID int64) (int64, error) {
			if callerID != userID || !allowed[orgID] {
				return 0, fmt.Errorf("user %s not a member of org %d", callerID, orgID)
			}
			return orgID, nil
		},
	}
}

func TestWebSocket(t *testing.T) {
	t.Parallel()

	ps := pubsub.NewLocalPubSub()
	const orgID int64 = 1
	srv := New(Options{
		Subscriber:  ps,
		OrgResolver: allowOrgResolver("u", orgID),
	})

	ts := httptest.NewServer(apiauth.WithTestUser(srv.Handler(), &apiauth.UserInfo{ID: "u", OrgID: orgID}))
	defer ts.Close()

	// Connect WebSocket client
	ctx := t.Context()
	wsURL := fmt.Sprintf("ws%s/ws?org_id=%d", strings.TrimPrefix(ts.URL, "http"), orgID)
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
	assert.DeepEqual(t, got, want)
}

func TestWebSocket_OrgIsolation(t *testing.T) {
	t.Parallel()

	ps := pubsub.NewLocalPubSub()
	const orgA, orgB int64 = 1, 2
	srv := New(Options{
		Subscriber:  ps,
		OrgResolver: allowOrgResolver("u", orgB),
	})
	ts := httptest.NewServer(apiauth.WithTestUser(srv.Handler(), &apiauth.UserInfo{ID: "u", OrgID: orgB}))
	defer ts.Close()

	// Connect org B subscriber
	ctx := t.Context()
	wsURL := fmt.Sprintf("ws%s/ws?org_id=%d", strings.TrimPrefix(ts.URL, "http"), orgB)
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
