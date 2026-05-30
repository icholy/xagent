package xagentclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/server/notifyserver"
	"github.com/icholy/xagent/internal/x/sse"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

const (
	testOrgID    = int64(1)
	testRunnerID = "test-runner"
)

// newTestNotifyServer spins up the real notifyserver wrapped in a test
// user middleware so the NotificationClient exercises the production
// code path end-to-end. It returns the httptest server and the local
// pubsub so the test can publish notifications.
func newTestNotifyServer(t *testing.T) (*httptest.Server, *pubsub.LocalPubSub) {
	t.Helper()
	ps := pubsub.NewLocalPubSub()
	srv := notifyserver.New(notifyserver.Options{Subscriber: ps})
	handler := apiauth.WithTestUser(srv.Handler(), &apiauth.UserInfo{
		ID:    "test-user",
		OrgID: testOrgID,
		Type:  apiauth.AuthTypeApp,
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, ps
}

// runClient starts c.Run in a goroutine and ensures it shuts down before
// the test ends.
func runClient(t *testing.T, c *xagentclient.NotificationClient) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		_ = c.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("client did not stop")
		}
	})
}

func recv[T any](t *testing.T, ch <-chan T, d time.Duration, msg string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", msg)
		var zero T
		return zero
	}
}

func TestNotificationClient_DecodesEvents(t *testing.T) {
	t.Parallel()
	// Arrange
	ts, ps := newTestNotifyServer(t)
	received := make(chan model.Notification, 8)
	c := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
		BaseURL: ts.URL,
		Runner:  testRunnerID,
		Handler: func(n model.Notification) { received <- n },
	})
	runClient(t, c)

	// Act + Assert: the ready event flows through decoded.
	ready := recv(t, received, time.Second, "ready notification")
	assert.Equal(t, ready.Type, "ready")

	// A real change event for this runner arrives with its fields intact.
	want := model.Notification{
		Type:   "change",
		OrgID:  testOrgID,
		Runner: testRunnerID,
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 7},
		},
	}
	assert.NilError(t, ps.Publish(t.Context(), want))
	got := recv(t, received, time.Second, "change notification")
	assert.Equal(t, got.Type, want.Type)
	assert.Equal(t, got.OrgID, want.OrgID)
	assert.Equal(t, got.Runner, want.Runner)
	assert.DeepEqual(t, got.Resources, want.Resources)
}

func TestNotificationClient_NoRunnerFilter(t *testing.T) {
	t.Parallel()
	// Arrange
	ts, ps := newTestNotifyServer(t)
	received := make(chan model.Notification, 4)
	c := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
		BaseURL: ts.URL,
		Handler: func(n model.Notification) { received <- n },
	})
	runClient(t, c)
	recv(t, received, time.Second, "ready notification")

	// Act: publish a notification destined for a different runner. With
	// no client-side filter, the server forwards everything in-org.
	want := model.Notification{Type: "change", OrgID: testOrgID, Runner: "other-runner"}
	assert.NilError(t, ps.Publish(t.Context(), want))

	// Assert
	got := recv(t, received, time.Second, "notification with no filter")
	assert.Equal(t, got.Type, want.Type)
	assert.Equal(t, got.Runner, want.Runner)
}

func TestNotificationClient_ReconnectsOnDrop(t *testing.T) {
	t.Parallel()
	// Arrange
	ts, ps := newTestNotifyServer(t)
	received := make(chan model.Notification, 8)
	c := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
		BaseURL:           ts.URL,
		Runner:            testRunnerID,
		ReconnectInterval: 10 * time.Millisecond,
		Handler:           func(n model.Notification) { received <- n },
	})
	runClient(t, c)
	first := recv(t, received, time.Second, "initial ready")
	assert.Equal(t, first.Type, "ready")

	// Act: drop the connection.
	ts.CloseClientConnections()

	// Assert: the client reconnects and we see a second ready.
	second := recv(t, received, 2*time.Second, "ready after reconnect")
	assert.Equal(t, second.Type, "ready")

	// A change event published after the reconnect flows through.
	assert.NilError(t, ps.Publish(t.Context(), model.Notification{
		Type: "change", OrgID: testOrgID, Runner: testRunnerID,
	}))
	got := recv(t, received, time.Second, "change after reconnect")
	assert.Equal(t, got.Type, "change")
}

func TestNotificationClient_SkipsMalformedData(t *testing.T) {
	t.Parallel()
	// Arrange: a tiny standalone handler — the real notifyserver only
	// emits well-formed JSON, so we need a custom one to test that the
	// client survives a bad payload and keeps consuming subsequent events.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw, err := sse.NewServerWriter(w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = sw.Write(sse.Event{Event: "ready", Data: []byte("not-json")})
		_ = sw.Write(sse.Event{Event: "change", Data: []byte(`{"type":"change","org_id":9}`)})
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	received := make(chan model.Notification, 4)
	c := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
		BaseURL: srv.URL,
		Handler: func(n model.Notification) { received <- n },
	})
	runClient(t, c)

	// Act + Assert: malformed event is skipped; subsequent valid event arrives.
	got := recv(t, received, time.Second, "change after malformed")
	assert.Equal(t, got.Type, "change")
	assert.Equal(t, got.OrgID, int64(9))
}
