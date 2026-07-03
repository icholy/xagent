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
	// The attach leg is wrapped with a test caller in testOrg so the inline org
	// check admits it, standing in for the Bearer auth middleware in production.
	reg := shellserver.New(nil, time.Minute)
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	mux.Handle("GET /shell/{session}/attach", apiauth.WithTestUser(reg.AttachHandler(), &apiauth.UserInfo{ID: "op", OrgID: testOrg}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	driver, _, err := websocket.Dial(ctx, base+"/shell/s1/driver", nil)
	assert.NilError(t, err)
	t.Cleanup(func() { driver.Close(websocket.StatusNormalClosure, "test done") })
	attach, resp, err := websocket.Dial(ctx, base+"/shell/s1/attach", &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
	})
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
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/attach", apiauth.WithTestUser(reg.AttachHandler(), &apiauth.UserInfo{ID: "op", OrgID: testOrg}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	// The subprotocol is negotiated by websocket.Accept, so an unsupported version
	// completes the upgrade (no matching subprotocol selected) and is then closed
	// by the handler as a policy violation rather than rejected pre-upgrade.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/shell/s1/attach", &websocket.DialOptions{
		Subprotocols: []string{"xagent-shell.v99"},
	})
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusSwitchingProtocols)
	assert.Equal(t, conn.Subprotocol(), "") // the server declined the unknown token

	// The connection is unusable: the handler closed it with a policy violation.
	_, _, readErr := conn.Read(ctx)
	assert.Assert(t, readErr != nil)
	assert.Equal(t, websocket.CloseStatus(readErr), websocket.StatusPolicyViolation)
}

func TestAttachRejectsUnknownSession(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/attach", apiauth.WithTestUser(reg.AttachHandler(), &apiauth.UserInfo{ID: "op", OrgID: testOrg}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/shell/nope/attach", &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
	})

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusNotFound)
}

func TestAttachRejectsForeignOrg(t *testing.T) {
	t.Parallel()
	// The caller belongs to a different org than the session's owner.
	reg := shellserver.New(nil, time.Minute)
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/attach", apiauth.WithTestUser(reg.AttachHandler(), &apiauth.UserInfo{ID: "op", OrgID: testOrg + 1}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/shell/s1/attach", &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
	})

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusForbidden)
}

func TestAttachRejectsMissingCaller(t *testing.T) {
	t.Parallel()
	// The attach handler is mounted without a caller in context, exercising the
	// 401 path.
	reg := shellserver.New(nil, time.Minute)
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/attach", reg.AttachHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/shell/s1/attach", &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
	})

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusUnauthorized)
}

func TestDriverRejectsUnknownSession(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/shell/nope/driver", nil)

	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusNotFound)
}

func TestEstablishTimeoutEvictsSession(t *testing.T) {
	t.Parallel()
	// Short, injected establishment timeout: connect only the driver leg.
	reg := shellserver.New(nil, 100*time.Millisecond)
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	driver, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/shell/s1/driver", nil)
	assert.NilError(t, err)
	t.Cleanup(func() { driver.Close(websocket.StatusNormalClosure, "test done") })

	// The session is evicted from the map and the lone leg is closed.
	waitFor(t, 3*time.Second, func() bool { return !reg.Has("s1") })
	_, _, err = driver.Read(ctx)
	assert.Assert(t, err != nil, "lone driver leg should be closed after establishment timeout")
}

func TestClosingOneLegEvictsSession(t *testing.T) {
	t.Parallel()
	reg := shellserver.New(nil, time.Minute)
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	mux.Handle("GET /shell/{session}/attach", apiauth.WithTestUser(reg.AttachHandler(), &apiauth.UserInfo{ID: "op", OrgID: testOrg}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	assert.NilError(t, reg.Seed("s1", testOrg))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	driver, _, err := websocket.Dial(ctx, base+"/shell/s1/driver", nil)
	assert.NilError(t, err)
	t.Cleanup(func() { driver.Close(websocket.StatusNormalClosure, "test done") })
	attach, _, err := websocket.Dial(ctx, base+"/shell/s1/attach", &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
	})
	assert.NilError(t, err)
	send(t, driver, []byte{0x00, 'x'})
	recv(t, attach)

	driver.Close(websocket.StatusNormalClosure, "bye")

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
