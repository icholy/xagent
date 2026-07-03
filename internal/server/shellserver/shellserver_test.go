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
	"github.com/icholy/xagent/internal/shellwire"
	"gotest.tools/v3/assert"
)

// testOrg is the org that owns seeded sessions in these tests.
const testOrg int64 = 1

// newTestServer mounts the adapter's two legs on an httptest server. The attach
// leg is wrapped so a caller with the given org is injected into the request
// context — apiauth.WithTestUser stands in for the Bearer auth middleware that
// guards the route in production. Cleanups run LIFO, so the registry is torn down
// before the server is closed, unblocking any parked handler goroutine.
func newTestServer(t *testing.T, reg *shellserver.Registry, callerOrg int64) *httptest.Server {
	t.Helper()
	caller := &apiauth.UserInfo{ID: "tester", OrgID: callerOrg}
	mux := http.NewServeMux()
	mux.Handle("GET /shell/{session}/driver", reg.DriverHandler())
	mux.Handle("GET /shell/{session}/attach", apiauth.WithTestUser(reg.AttachHandler(), caller))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)
	return srv
}

func dialDriver(t *testing.T, srv *httptest.Server, session string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell/" + session + "/driver"
	conn, _, err := websocket.Dial(ctx, url, nil)
	assert.NilError(t, err)
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

func dialAttach(t *testing.T, srv *httptest.Server, session string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell/" + session + "/attach"
	return websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
	})
}

func TestAttachAcceptsMatchingOrg(t *testing.T) {
	t.Parallel()
	// Arrange: session and caller share an org.
	reg := shellserver.New(nil, time.Minute)
	srv := newTestServer(t, reg, testOrg)
	assert.NilError(t, reg.Seed("s1", testOrg))
	dialDriver(t, srv, "s1")

	// Act
	attach, resp, err := dialAttach(t, srv, "s1")

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusSwitchingProtocols)
	t.Cleanup(func() { attach.Close(websocket.StatusNormalClosure, "test done") })
}

func TestAttachRejectsDifferentOrg(t *testing.T) {
	t.Parallel()
	// Arrange: caller's org differs from the session's owning org.
	reg := shellserver.New(nil, time.Minute)
	srv := newTestServer(t, reg, testOrg+1)
	assert.NilError(t, reg.Seed("s1", testOrg))

	// Act
	_, resp, err := dialAttach(t, srv, "s1")

	// Assert
	assert.Assert(t, err != nil)
	assert.Equal(t, resp.StatusCode, http.StatusForbidden)
}
