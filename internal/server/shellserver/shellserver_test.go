package shellserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/server/shellserver"
	"github.com/icholy/xagent/internal/shell/shellwire"
	"gotest.tools/v3/assert"
)

// testOrg owns seeded sessions in these tests.
const testOrg int64 = 1

// newServer mounts the registry's two legs on an httptest server. The attach leg
// is wrapped with WithTestUser so the org check has a caller to authorize,
// standing in for the Bearer auth middleware in production. Cleanups run LIFO, so
// the registry is torn down before the server — that unblocks any handler
// goroutine parked waiting for its peer.
func newServer(t *testing.T, reg *shellserver.Registry, caller *apiauth.UserInfo) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	mux.Handle("GET /shell/{session}/attach", apiauth.WithTestUser(reg.AttachHandler(), caller))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	return srv
}

// noCallerServer mounts the attach leg without injecting a caller, to exercise
// the 401 path.
func noCallerServer(t *testing.T, reg *shellserver.Registry) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	mux.Handle("GET /shell/{session}/attach", reg.AttachHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	return srv
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func dialDriver(t *testing.T, srv *httptest.Server, session string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv, "/shell/"+session+"/driver"), nil)
	assert.NilError(t, err)
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

func dialAttach(t *testing.T, srv *httptest.Server, session string, subprotocols ...string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, wsURL(srv, "/shell/"+session+"/attach"), &websocket.DialOptions{
		Subprotocols: subprotocols,
	})
}

func send(t *testing.T, conn *websocket.Conn, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	assert.NilError(t, conn.Write(ctx, websocket.MessageBinary, data))
}

func recv(t *testing.T, conn *websocket.Conn) (websocket.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	assert.NilError(t, err)
	return typ, data
}

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

// member is a caller belonging to testOrg.
func member() *apiauth.UserInfo { return &apiauth.UserInfo{ID: "op", OrgID: testOrg} }

func TestRelayPassesBytesBothDirections(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	srv := newServer(t, reg, member())
	assert.NilError(t, reg.Seed("s1", testOrg))
	driver := dialDriver(t, srv, "s1")
	attach, resp, err := dialAttach(t, srv, "s1", shellwire.Subprotocol)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusSwitchingProtocols)
	assert.Equal(t, attach.Subprotocol(), shellwire.Subprotocol)
	t.Cleanup(func() { attach.Close(websocket.StatusNormalClosure, "test done") })

	// driver -> attach, including arbitrary non-UTF-8 bytes.
	payload := []byte{0x00, 0x01, 0xff, 0xfe, 0x80, 'h', 'i', 0x00}
	send(t, driver, payload)
	typ, got := recv(t, attach)
	assert.Equal(t, typ, websocket.MessageBinary)
	assert.DeepEqual(t, got, payload)

	// attach -> driver.
	reply := []byte{0x02, 0xde, 0xad, 0xbe, 0xef}
	send(t, attach, reply)
	typ, got = recv(t, driver)
	assert.Equal(t, typ, websocket.MessageBinary)
	assert.DeepEqual(t, got, reply)
}

func TestAttachRejectsVersionMismatch(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	srv := newServer(t, reg, member())
	assert.NilError(t, reg.Seed("s1", testOrg))

	_, resp, err := dialAttach(t, srv, "s1", "xagent-shell.v99")

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusBadRequest)
}

func TestAttachRejectsUnknownSession(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	srv := newServer(t, reg, member())

	_, resp, err := dialAttach(t, srv, "nope", shellwire.Subprotocol)

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusNotFound)
}

func TestAttachRejectsForeignOrg(t *testing.T) {
	t.Parallel()
	// The caller belongs to a different org than the session's owner.
	reg := shellserver.New(nil, time.Minute)
	srv := newServer(t, reg, &apiauth.UserInfo{ID: "op", OrgID: testOrg + 1})
	assert.NilError(t, reg.Seed("s1", testOrg))

	_, resp, err := dialAttach(t, srv, "s1", shellwire.Subprotocol)

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusForbidden)
}

func TestAttachRejectsMissingCaller(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	srv := noCallerServer(t, reg)
	assert.NilError(t, reg.Seed("s1", testOrg))

	_, resp, err := dialAttach(t, srv, "s1", shellwire.Subprotocol)

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusUnauthorized)
}

func TestDriverRejectsUnknownSession(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	srv := newServer(t, reg, member())

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv, "/shell/nope/driver"), nil)

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusNotFound)
}

func TestEstablishTimeoutEvictsSession(t *testing.T) {
	t.Parallel()
	// Short, injected establishment timeout: connect only the driver leg.
	reg := shellserver.New(nil, 100*time.Millisecond)
	srv := newServer(t, reg, member())
	assert.NilError(t, reg.Seed("s1", testOrg))
	driver := dialDriver(t, srv, "s1")

	// The session is evicted from the map and the lone leg is closed.
	waitFor(t, 3*time.Second, func() bool { return !reg.Has("s1") })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, err := driver.Read(ctx)
	assert.Assert(t, err != nil, "lone driver leg should be closed after establishment timeout")
}

func TestClosingOneLegEvictsSession(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	srv := newServer(t, reg, member())
	assert.NilError(t, reg.Seed("s1", testOrg))
	driver := dialDriver(t, srv, "s1")
	attach, _, err := dialAttach(t, srv, "s1", shellwire.Subprotocol)
	assert.NilError(t, err)
	send(t, driver, []byte{0x00, 'x'})
	recv(t, attach)

	driver.Close(websocket.StatusNormalClosure, "bye")

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, readErr := attach.Read(ctx)
	assert.Assert(t, readErr != nil, "peer leg should be closed when one leg closes")
	waitFor(t, 3*time.Second, func() bool { return !reg.Has("s1") })
}

func TestSeedRejectsDuplicate(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	err := reg.Seed("s1", testOrg)

	assert.ErrorContains(t, err, "already exists")
}

func TestSeedRejectsEmptyID(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	t.Cleanup(reg.Close)

	err := reg.Seed("", testOrg)

	assert.ErrorContains(t, err, "empty session id")
}
