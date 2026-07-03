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
	"github.com/icholy/xagent/internal/shellwire"
	"gotest.tools/v3/assert"
)

// testOrg is the org that owns seeded sessions in these tests.
const testOrg int64 = 1

// allowAll is an AuthorizeAttach that admits every attach request. The org
// policy is exercised at the server layer (internal/server/shellserver); these
// tests cover the relay's transport mechanics.
func allowAll(http.ResponseWriter, *http.Request, int64) bool { return true }

// newTestServer mounts the relay's two legs on an httptest server, wiring the
// attach leg with the given authorizer. Cleanups run LIFO, so the registry is
// torn down before the server is closed — that unblocks any handler goroutine
// parked waiting for its peer.
func newTestServer(t *testing.T, reg *shellrelay.Registry, authorize shellrelay.AuthorizeAttach) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	mux.Handle("GET /shell/{session}/attach", reg.AttachHandler(authorize))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	return srv
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

// dialDriver connects the driver leg. The relay handler mounts behind auth
// middleware in production; here we exercise the relay directly.
func dialDriver(t *testing.T, srv *httptest.Server, session string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv, "/shell/"+session+"/driver"), nil)
	assert.NilError(t, err)
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

// dialAttach connects the operator leg with the given subprotocol tokens.
func dialAttach(t *testing.T, srv *httptest.Server, session string, subprotocols ...string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, wsURL(srv, "/shell/"+session+"/attach"), &websocket.DialOptions{
		Subprotocols: subprotocols,
	})
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

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	assert.Assert(t, cond(), "condition not met within %s", timeout)
}

func TestRelayPassesBytesBothDirections(t *testing.T) {
	t.Parallel()
	// Arrange
	reg := shellrelay.NewRegistry(nil, time.Minute)
	srv := newTestServer(t, reg, allowAll)
	assert.NilError(t, reg.Seed("s1", testOrg))
	driver := dialDriver(t, srv, "s1")
	attach, resp, err := dialAttach(t, srv, "s1", shellwire.Subprotocol)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusSwitchingProtocols)
	assert.Equal(t, attach.Subprotocol(), shellwire.Subprotocol)
	t.Cleanup(func() { attach.Close(websocket.StatusNormalClosure, "test done") })

	// Act + Assert: driver -> attach, including arbitrary non-UTF-8 bytes.
	payload := []byte{0x00, 0x01, 0xff, 0xfe, 0x80, 'h', 'i', 0x00}
	send(t, driver, payload)
	typ, got := recv(t, attach)
	assert.Equal(t, typ, websocket.MessageBinary)
	assert.DeepEqual(t, got, payload)

	// Act + Assert: attach -> driver.
	reply := []byte{0x02, 0xde, 0xad, 0xbe, 0xef}
	send(t, attach, reply)
	typ, got = recv(t, driver)
	assert.Equal(t, typ, websocket.MessageBinary)
	assert.DeepEqual(t, got, reply)
}

func TestAttachRejectsVersionMismatch(t *testing.T) {
	t.Parallel()
	// Arrange
	reg := shellrelay.NewRegistry(nil, time.Minute)
	srv := newTestServer(t, reg, allowAll)
	assert.NilError(t, reg.Seed("s1", testOrg))

	// Act: wrong version token.
	_, resp, err := dialAttach(t, srv, "s1", "xagent-shell.v99")

	// Assert
	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusBadRequest)
}

func TestAttachRejectsUnknownSession(t *testing.T) {
	t.Parallel()
	// Arrange: no session seeded.
	reg := shellrelay.NewRegistry(nil, time.Minute)
	srv := newTestServer(t, reg, allowAll)

	// Act
	_, resp, err := dialAttach(t, srv, "nope", shellwire.Subprotocol)

	// Assert
	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusNotFound)
}

func TestAttachRunsAuthorizer(t *testing.T) {
	t.Parallel()
	// Arrange: an authorizer that rejects, standing in for a failed org check.
	reg := shellrelay.NewRegistry(nil, time.Minute)
	reject := func(w http.ResponseWriter, _ *http.Request, orgID int64) bool {
		assert.Equal(t, orgID, testOrg)
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	srv := newTestServer(t, reg, reject)
	assert.NilError(t, reg.Seed("s1", testOrg))

	// Act
	_, resp, err := dialAttach(t, srv, "s1", shellwire.Subprotocol)

	// Assert
	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusForbidden)
}

func TestDriverRejectsUnknownSession(t *testing.T) {
	t.Parallel()
	// Arrange
	reg := shellrelay.NewRegistry(nil, time.Minute)
	srv := newTestServer(t, reg, allowAll)

	// Act
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv, "/shell/nope/driver"), nil)

	// Assert
	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusNotFound)
}

func TestEstablishTimeoutTearsDownLoneLeg(t *testing.T) {
	t.Parallel()
	// Arrange: short, injected establishment timeout.
	reg := shellrelay.NewRegistry(nil, 100*time.Millisecond)
	srv := newTestServer(t, reg, allowAll)
	assert.NilError(t, reg.Seed("s1", testOrg))

	// Act: connect only the driver leg.
	driver := dialDriver(t, srv, "s1")

	// Assert: the session is torn down and the lone leg is closed.
	waitFor(t, 3*time.Second, func() bool { return !reg.Has("s1") })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, err := driver.Read(ctx)
	assert.Assert(t, err != nil, "lone driver leg should be closed after establishment timeout")
}

func TestClosingOneLegTearsDownSession(t *testing.T) {
	t.Parallel()
	// Arrange: both legs connected.
	reg := shellrelay.NewRegistry(nil, time.Minute)
	srv := newTestServer(t, reg, allowAll)
	assert.NilError(t, reg.Seed("s1", testOrg))
	driver := dialDriver(t, srv, "s1")
	attach, _, err := dialAttach(t, srv, "s1", shellwire.Subprotocol)
	assert.NilError(t, err)
	// Round-trip once so both legs are actively relaying.
	send(t, driver, []byte{0x00, 'x'})
	recv(t, attach)

	// Act: close the driver leg.
	driver.Close(websocket.StatusNormalClosure, "bye")

	// Assert: the attach leg is closed and the session removed.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, readErr := attach.Read(ctx)
	assert.Assert(t, readErr != nil, "peer leg should be closed when one leg closes")
	waitFor(t, 3*time.Second, func() bool { return !reg.Has("s1") })
}

func TestSeedRejectsDuplicate(t *testing.T) {
	t.Parallel()
	// Arrange
	reg := shellrelay.NewRegistry(nil, time.Minute)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	// Act
	err := reg.Seed("s1", testOrg)

	// Assert
	assert.ErrorContains(t, err, "already exists")
}
