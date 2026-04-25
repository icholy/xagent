package notifyserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"gotest.tools/v3/assert"
)

// sseEvent represents a parsed SSE event.
type sseEvent struct {
	ID    string
	Event string
	Data  string
}

// readSSEEvent reads the next SSE event from a bufio.Scanner that splits on lines.
func readSSEEvent(scanner *bufio.Scanner) (sseEvent, bool) {
	var ev sseEvent
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// Empty line marks the end of an event.
			return ev, true
		}
		if strings.HasPrefix(line, "id: ") {
			ev.ID = strings.TrimPrefix(line, "id: ")
		} else if strings.HasPrefix(line, "event: ") {
			ev.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			ev.Data = strings.TrimPrefix(line, "data: ")
		}
	}
	return ev, false
}

func TestSSE(t *testing.T) {
	t.Parallel()

	ps := pubsub.NewLocalPubSub()
	const orgID int64 = 1
	srv := New(Options{
		Subscriber:  ps,
		OrgResolver: allowOrgResolver("u", orgID),
	})

	ts := httptest.NewServer(apiauth.WithTestUser(srv.SSEHandler(), &apiauth.UserInfo{ID: "u", OrgID: orgID}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/events?org_id=%d", ts.URL, orgID), nil)
	assert.NilError(t, err)

	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Equal(t, resp.Header.Get("Content-Type"), "text/event-stream")

	scanner := bufio.NewScanner(resp.Body)

	// Read the ready event.
	ev, ok := readSSEEvent(scanner)
	assert.Assert(t, ok, "expected ready event")
	assert.Equal(t, ev.Event, "ready")
	assert.Equal(t, ev.ID, "0")

	// Publish a notification.
	want := model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "created", Type: "task", ID: 42}},
		OrgID:     orgID,
		Time:      time.Now().Truncate(time.Second),
	}
	err = ps.Publish(ctx, want)
	assert.NilError(t, err)

	// Read the change event.
	ev, ok = readSSEEvent(scanner)
	assert.Assert(t, ok, "expected change event")
	assert.Equal(t, ev.Event, "change")
	assert.Equal(t, ev.ID, "1")

	var got model.Notification
	err = json.Unmarshal([]byte(ev.Data), &got)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, want)
}

func TestSSE_OrgIsolation(t *testing.T) {
	t.Parallel()

	ps := pubsub.NewLocalPubSub()
	const orgA, orgB int64 = 1, 2
	srv := New(Options{
		Subscriber:  ps,
		OrgResolver: allowOrgResolver("u", orgB),
	})

	ts := httptest.NewServer(apiauth.WithTestUser(srv.SSEHandler(), &apiauth.UserInfo{ID: "u", OrgID: orgB}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/events?org_id=%d", ts.URL, orgB), nil)
	assert.NilError(t, err)

	resp, err := http.DefaultClient.Do(req)
	assert.NilError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, resp.StatusCode, http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)

	// Read the ready event.
	ev, ok := readSSEEvent(scanner)
	assert.Assert(t, ok, "expected ready event")
	assert.Equal(t, ev.Event, "ready")

	// Publish to org A only.
	err = ps.Publish(ctx, model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "created", Type: "task", ID: 1}},
		OrgID:     orgA,
	})
	assert.NilError(t, err)

	// Publish to org B.
	wantB := model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "updated", Type: "task", ID: 2}},
		OrgID:     orgB,
		Time:      time.Now().Truncate(time.Second),
	}
	err = ps.Publish(ctx, wantB)
	assert.NilError(t, err)

	// Should only receive the org B notification.
	ev, ok = readSSEEvent(scanner)
	assert.Assert(t, ok, "expected change event for org B")
	assert.Equal(t, ev.Event, "change")

	var got model.Notification
	err = json.Unmarshal([]byte(ev.Data), &got)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, wantB)
}
