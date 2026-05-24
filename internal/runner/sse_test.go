package runner

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/server/notifyserver"
)

const (
	testOrgID    = int64(1)
	testRunnerID = "test-runner"
)

// newTestNotifyServer spins up the real notifyserver wrapped in a test
// user middleware so the SSE subscriber exercises the production code
// path end-to-end. It returns the httptest server and the local pubsub
// so the test can publish notifications.
func newTestNotifyServer(t *testing.T) (*httptest.Server, *pubsub.LocalPubSub) {
	t.Helper()
	ps := pubsub.NewLocalPubSub()
	srv := notifyserver.New(notifyserver.Options{Subscriber: ps})
	handler := apiauth.WithTestUser(srv.Handler(), &apiauth.UserInfo{
		ID:    "runner-user",
		OrgID: testOrgID,
		Type:  apiauth.AuthTypeApp,
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, ps
}

// runSubscriber starts sub.Run in a goroutine and ensures it shuts down
// before the test ends.
func runSubscriber(t *testing.T, sub *SSESubscriber) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		_ = sub.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("subscriber did not stop")
		}
	})
}

// waitForSignal asserts a signal arrives on ch within d.
func waitForSignal(t *testing.T, ch <-chan struct{}, d time.Duration, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", msg)
	}
}

// changeForRunner is a notification the server will forward to a runner
// stream filtered by runner=testRunnerID.
func changeForRunner() model.Notification {
	return model.Notification{
		Type:      "change",
		OrgID:     testOrgID,
		Runner:    testRunnerID,
		Resources: []model.NotificationResource{{Action: "updated", Type: "task", ID: 1}},
	}
}

func TestSSESubscriber_SignalsOnChange(t *testing.T) {
	t.Parallel()
	ts, ps := newTestNotifyServer(t)

	sub := NewSSESubscriber(SSESubscriberOptions{
		BaseURL:  ts.URL,
		RunnerID: testRunnerID,
	})
	runSubscriber(t, sub)

	// The server's `ready` event on connect signals so the runner picks
	// up anything missed while disconnected. Drain it.
	waitForSignal(t, sub.C(), time.Second, "ready signal")

	// A change event for this runner should trigger a signal.
	err := ps.Publish(t.Context(), changeForRunner())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitForSignal(t, sub.C(), time.Second, "change signal")
}

func TestSSESubscriber_IgnoresOtherRunners(t *testing.T) {
	t.Parallel()
	ts, ps := newTestNotifyServer(t)

	sub := NewSSESubscriber(SSESubscriberOptions{
		BaseURL:  ts.URL,
		RunnerID: testRunnerID,
	})
	runSubscriber(t, sub)

	waitForSignal(t, sub.C(), time.Second, "ready signal")

	// Notifications with no runner (no pending work) or for a different
	// runner must be filtered out server-side and never reach us.
	for _, n := range []model.Notification{
		{Type: "change", OrgID: testOrgID},
		{Type: "change", OrgID: testOrgID, Runner: "other-runner"},
	} {
		if err := ps.Publish(t.Context(), n); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	select {
	case <-sub.C():
		t.Fatal("received signal for a notification that should have been filtered server-side")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSSESubscriber_CoalescesBursts(t *testing.T) {
	t.Parallel()
	ts, ps := newTestNotifyServer(t)

	sub := NewSSESubscriber(SSESubscriberOptions{
		BaseURL:  ts.URL,
		RunnerID: testRunnerID,
	})
	runSubscriber(t, sub)

	waitForSignal(t, sub.C(), time.Second, "ready signal")

	// Publish a burst of change events for this runner.
	const N = 50
	for range N {
		if err := ps.Publish(t.Context(), changeForRunner()); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	// Give the subscriber a moment to drain the SSE stream.
	time.Sleep(100 * time.Millisecond)

	// Burst must coalesce into a single signal on the size-1 channel.
	waitForSignal(t, sub.C(), time.Second, "coalesced signal")
	select {
	case <-sub.C():
		t.Fatal("burst was not coalesced into a single signal")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSSESubscriber_ReconnectsAndSignals(t *testing.T) {
	t.Parallel()
	ts, ps := newTestNotifyServer(t)

	sub := NewSSESubscriber(SSESubscriberOptions{
		BaseURL:           ts.URL,
		RunnerID:          testRunnerID,
		ReconnectInterval: 10 * time.Millisecond,
	})
	runSubscriber(t, sub)

	waitForSignal(t, sub.C(), time.Second, "ready signal")

	// Force the server to close all active client connections.
	ts.CloseClientConnections()

	// Subscriber must reconnect; the server's `ready` event on the new
	// connection signals so the runner catches anything that changed
	// during the gap.
	waitForSignal(t, sub.C(), 2*time.Second, "reconnect ready signal")

	// And once reconnected, change events flow as before.
	if err := ps.Publish(t.Context(), changeForRunner()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	waitForSignal(t, sub.C(), time.Second, "change signal after reconnect")
}
