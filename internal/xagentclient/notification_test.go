package xagentclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/sse"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

// fakeNotifyServer is a minimal SSE server that emits a ready event on
// each connection and then forwards any events written to its channel
// until the client disconnects.
type fakeNotifyServer struct {
	*httptest.Server
	events    chan model.Notification
	connects  atomic.Int32
	mu        sync.Mutex
	lastQuery string
}

func newFakeNotifyServer(t *testing.T) *fakeNotifyServer {
	t.Helper()
	f := &fakeNotifyServer{events: make(chan model.Notification, 16)}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastQuery = r.URL.RawQuery
		f.mu.Unlock()
		f.connects.Add(1)
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sw := sse.NewWriter(w)
		data, _ := json.Marshal(model.Notification{Type: "ready"})
		if err := sw.Write(sse.Event{Event: "ready", Data: data}); err != nil {
			return
		}
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case n := <-f.events:
				d, _ := json.Marshal(n)
				if err := sw.Write(sse.Event{Event: n.Type, Data: d}); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
	f.Server = httptest.NewServer(handler)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeNotifyServer) query() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastQuery
}

// runSubscriber starts sub.Run in a goroutine and ensures it shuts down
// before the test ends.
func runSubscriber(t *testing.T, sub *xagentclient.NotificationSubscriber) {
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

func TestNotificationSubscriber_DecodesEvents(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFakeNotifyServer(t)
	received := make(chan model.Notification, 8)
	sub := xagentclient.NewNotificationSubscriber(xagentclient.NotificationSubscriberOptions{
		BaseURL: f.URL,
		Runner:  "runner-1",
		Handler: func(n model.Notification) { received <- n },
	})
	runSubscriber(t, sub)

	// Act + Assert: the ready event flows through decoded.
	ready := recv(t, received, time.Second, "ready notification")
	assert.Equal(t, ready.Type, "ready")

	// And a real change event arrives with its fields intact.
	want := model.Notification{
		Type:   "change",
		OrgID:  42,
		Runner: "runner-1",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: 7},
		},
	}
	f.events <- want
	got := recv(t, received, time.Second, "change notification")
	assert.Equal(t, got.Type, want.Type)
	assert.Equal(t, got.OrgID, want.OrgID)
	assert.Equal(t, got.Runner, want.Runner)
	assert.DeepEqual(t, got.Resources, want.Resources)

	// The runner filter must be on the query string.
	assert.Equal(t, f.query(), "runner=runner-1")
}

func TestNotificationSubscriber_NoRunnerFilter(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFakeNotifyServer(t)
	received := make(chan model.Notification, 4)
	sub := xagentclient.NewNotificationSubscriber(xagentclient.NotificationSubscriberOptions{
		BaseURL: f.URL,
		Handler: func(n model.Notification) { received <- n },
	})
	runSubscriber(t, sub)

	// Act
	recv(t, received, time.Second, "ready notification")

	// Assert: empty Runner option means no ?runner= filter is sent.
	assert.Equal(t, f.query(), "")
}

func TestNotificationSubscriber_ReconnectsOnDrop(t *testing.T) {
	t.Parallel()
	// Arrange
	f := newFakeNotifyServer(t)
	received := make(chan model.Notification, 8)
	sub := xagentclient.NewNotificationSubscriber(xagentclient.NotificationSubscriberOptions{
		BaseURL:           f.URL,
		Runner:            "runner-1",
		ReconnectInterval: 10 * time.Millisecond,
		Handler:           func(n model.Notification) { received <- n },
	})
	runSubscriber(t, sub)
	recv(t, received, time.Second, "initial ready")

	// Act: drop the connection.
	f.CloseClientConnections()

	// Assert: the subscriber reconnects and we see a second ready, and a
	// change event posted afterward flows through.
	recv(t, received, 2*time.Second, "ready after reconnect")
	assert.Assert(t, f.connects.Load() >= 2, "expected at least 2 connections, got %d", f.connects.Load())

	f.events <- model.Notification{Type: "change", OrgID: 1, Runner: "runner-1"}
	got := recv(t, received, time.Second, "change after reconnect")
	assert.Equal(t, got.Type, "change")
}

func TestNotificationSubscriber_SkipsMalformedData(t *testing.T) {
	t.Parallel()
	// Arrange: a handler that writes a malformed event followed by a valid one.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// Malformed JSON payload — subscriber must log and continue.
		_, _ = io.WriteString(w, "event: ready\ndata: not-json\n\n")
		// Valid event afterwards.
		data, _ := json.Marshal(model.Notification{Type: "change", OrgID: 9})
		sw := sse.NewWriter(w)
		_ = sw.Write(sse.Event{Event: "change", Data: data})
		flusher.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	received := make(chan model.Notification, 4)
	sub := xagentclient.NewNotificationSubscriber(xagentclient.NotificationSubscriberOptions{
		BaseURL: srv.URL,
		Handler: func(n model.Notification) { received <- n },
	})
	runSubscriber(t, sub)

	// Act + Assert: malformed event is skipped; subsequent valid event arrives.
	got := recv(t, received, time.Second, "change after malformed")
	assert.Equal(t, got.Type, "change")
	assert.Equal(t, got.OrgID, int64(9))
}
