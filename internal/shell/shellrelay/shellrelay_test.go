package shellrelay_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/icholy/xagent/internal/shell/shellrelay"
	"gotest.tools/v3/assert"
)

// joinServer stands up an httptest server whose sole handler accepts the
// WebSocket and hands it to s.Join. Both legs dial the same handler; the Session
// pairs them by arrival order. Cleanups run LIFO, so the session is closed before
// the server — that unblocks any handler goroutine parked waiting for its peer.
func joinServer(t *testing.T, s *shellrelay.Session) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		_ = s.Join(req.Context(), conn)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// dial connects a leg to the join server.
func dial(t *testing.T, srv *httptest.Server) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, wsURL(srv), nil)
}

// dialLeg connects a leg and fails the test if the handshake errors.
func dialLeg(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := dial(t, srv)
	assert.NilError(t, err)
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

// send writes a binary message and fails the test on error.
func send(t *testing.T, conn *websocket.Conn, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	assert.NilError(t, conn.Write(ctx, websocket.MessageBinary, data))
}

// recv reads one message and fails the test on error.
func recv(t *testing.T, conn *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	assert.NilError(t, err)
	return typ, data
}

func TestSessionPassesBytesBothDirections(t *testing.T) {
	t.Parallel()
	// Arrange: both legs joined.
	s := shellrelay.NewSession(time.Minute, nil)
	srv := joinServer(t, s)
	legA := dialLeg(t, srv)
	legB := dialLeg(t, srv)

	// Act + Assert: A -> B, including arbitrary non-UTF-8 bytes.
	payload := []byte{0x00, 0x01, 0xff, 0xfe, 0x80, 'h', 'i', 0x00}
	send(t, legA, payload)
	typ, got := recv(t, legB)
	assert.Equal(t, typ, websocket.MessageBinary)
	assert.DeepEqual(t, got, payload)

	// Act + Assert: B -> A.
	reply := []byte{0x02, 0xde, 0xad, 0xbe, 0xef}
	send(t, legB, reply)
	typ, got = recv(t, legA)
	assert.Equal(t, typ, websocket.MessageBinary)
	assert.DeepEqual(t, got, reply)
}

func TestSessionRejectsThirdLeg(t *testing.T) {
	t.Parallel()
	// Arrange: both legs joined and actively relaying.
	s := shellrelay.NewSession(time.Minute, nil)
	srv := joinServer(t, s)
	legA := dialLeg(t, srv)
	legB := dialLeg(t, srv)
	send(t, legA, []byte{0x00, 'x'})
	recv(t, legB)

	// Act: a third leg dials in. The handshake succeeds (the reject is a policy
	// close after Accept), so the leg is closed as soon as it reads.
	third := dialLeg(t, srv)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, err := third.Read(ctx)

	// Assert: the third leg is closed, and the first two are undisturbed.
	assert.Assert(t, err != nil, "third leg should be rejected")
	send(t, legB, []byte{0x00, 'y'})
	typ, got := recv(t, legA)
	assert.Equal(t, typ, websocket.MessageBinary)
	assert.DeepEqual(t, got, []byte{0x00, 'y'})
}

func TestSessionEstablishTimeoutTearsDownLoneLeg(t *testing.T) {
	t.Parallel()
	// Arrange: short, injected establishment timeout.
	s := shellrelay.NewSession(100*time.Millisecond, nil)
	srv := joinServer(t, s)

	// Act: connect only one leg.
	lone := dialLeg(t, srv)

	// Assert: the session is torn down and the lone leg is closed.
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("session was not torn down by the establishment timeout")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, err := lone.Read(ctx)
	assert.Assert(t, err != nil, "lone leg should be closed after establishment timeout")
}

func TestSessionClosingOneLegTearsDownPeer(t *testing.T) {
	t.Parallel()
	// Arrange: both legs connected and actively relaying.
	s := shellrelay.NewSession(time.Minute, nil)
	srv := joinServer(t, s)
	legA := dialLeg(t, srv)
	legB := dialLeg(t, srv)
	send(t, legA, []byte{0x00, 'x'})
	recv(t, legB)

	// Act: close one leg.
	legA.Close(websocket.StatusNormalClosure, "bye")

	// Assert: the peer leg is closed and the session is done.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, readErr := legB.Read(ctx)
	assert.Assert(t, readErr != nil, "peer leg should be closed when one leg closes")
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("session was not torn down when a leg closed")
	}
}

func TestSessionCloseTearsDownAndSignalsDone(t *testing.T) {
	t.Parallel()
	// Arrange: both legs joined.
	s := shellrelay.NewSession(time.Minute, nil)
	srv := joinServer(t, s)
	legA := dialLeg(t, srv)
	legB := dialLeg(t, srv)
	send(t, legA, []byte{0x00, 'x'})
	recv(t, legB)

	// Act: explicit Close, twice to prove idempotence.
	s.Close()
	s.Close()

	// Assert: Done fires and both legs are closed.
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Done was not signaled after Close")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, errA := legA.Read(ctx)
	_, _, errB := legB.Read(ctx)
	assert.Assert(t, errA != nil, "leg A should be closed after Close")
	assert.Assert(t, errB != nil, "leg B should be closed after Close")
}
