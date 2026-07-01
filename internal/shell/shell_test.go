package shell_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/server/shellrelay"
	"github.com/icholy/xagent/internal/shell"
	"github.com/icholy/xagent/internal/shellwire"
	"gotest.tools/v3/assert"
)

// testOrg owns the seeded shell session in these tests.
const testOrg int64 = 1

// newRelayServer mounts the real relay's two legs on an httptest server. The
// attach leg is wrapped with apiauth.WithTestUser so a caller in callerOrg is
// injected into the request context, standing in for the Bearer auth middleware
// that guards the route in production. Cleanups run LIFO: the registry is torn
// down before the server so parked handler goroutines are unblocked.
func newRelayServer(t *testing.T, reg *shellrelay.Registry, callerOrg int64) *httptest.Server {
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

// dialAttach connects the operator leg negotiating the shell subprotocol.
func dialAttach(t *testing.T, srv *httptest.Server, session string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/shell/" + session + "/attach"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
	})
	assert.NilError(t, err)
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "test done") })
	return conn
}

// sendData writes a data frame carrying the given input.
func sendData(t *testing.T, conn *websocket.Conn, input string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	assert.NilError(t, conn.Write(ctx, websocket.MessageBinary, shellwire.Data([]byte(input))))
}

// readUntil accumulates data frames until their concatenation contains substr,
// failing on an unexpected exit frame or timeout.
func readUntil(t *testing.T, conn *websocket.Conn, substr string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var buf bytes.Buffer
	for {
		typ, msg, err := conn.Read(ctx)
		assert.NilError(t, err, "waiting for %q, got so far: %q", substr, buf.String())
		assert.Equal(t, typ, websocket.MessageBinary)
		frame, err := shellwire.Parse(msg)
		assert.NilError(t, err)
		assert.Assert(t, frame.Type != shellwire.TypeExit, "unexpected exit frame; output so far: %q", buf.String())
		if frame.Type == shellwire.TypeData {
			buf.Write(frame.Payload)
			if strings.Contains(buf.String(), substr) {
				return buf.String()
			}
		}
	}
}

// readExitCode reads frames until an exit frame arrives and returns its code.
func readExitCode(t *testing.T, conn *websocket.Conn) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		typ, msg, err := conn.Read(ctx)
		assert.NilError(t, err)
		assert.Equal(t, typ, websocket.MessageBinary)
		frame, err := shellwire.Parse(msg)
		assert.NilError(t, err)
		if frame.Type == shellwire.TypeExit {
			code, err := frame.ExitCode()
			assert.NilError(t, err)
			return code
		}
	}
}

func TestServe(t *testing.T) {
	t.Parallel()
	// Arrange: real relay, a seeded session, and Serve pointed at it.
	reg := shellrelay.NewRegistry(nil, time.Minute)
	srv := newRelayServer(t, reg, testOrg)
	assert.NilError(t, reg.Seed("s1", testOrg))

	serveErr := make(chan error, 1)
	go func() { serveErr <- shell.Serve(t.Context(), srv.URL, "driver-token", "s1", slog.Default()) }()

	attach := dialAttach(t, srv, "s1")

	// Act + Assert: a command echoes back through the PTY as data frames.
	sendData(t, attach, "echo hello\n")
	readUntil(t, attach, "hello")

	// Act + Assert: a resize frame applies without error — stty reports the new
	// size back through the PTY.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	assert.NilError(t, attach.Write(ctx, websocket.MessageBinary, shellwire.Resize(40, 100)))
	sendData(t, attach, "stty size\n")
	readUntil(t, attach, "40 100")

	// Act + Assert: ending the shell delivers an exit frame carrying its code.
	sendData(t, attach, "exit 0\n")
	assert.Equal(t, readExitCode(t, attach), 0)

	select {
	case err := <-serveErr:
		assert.NilError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after shell exited")
	}
}

func TestServe_ExitCode(t *testing.T) {
	t.Parallel()
	// Arrange
	reg := shellrelay.NewRegistry(nil, time.Minute)
	srv := newRelayServer(t, reg, testOrg)
	assert.NilError(t, reg.Seed("s2", testOrg))

	serveErr := make(chan error, 1)
	go func() { serveErr <- shell.Serve(t.Context(), srv.URL, "driver-token", "s2", slog.Default()) }()

	attach := dialAttach(t, srv, "s2")

	// Act: exit with a non-zero status.
	sendData(t, attach, "exit 7\n")

	// Assert: the exit frame carries the shell's exit code.
	assert.Equal(t, readExitCode(t, attach), 7)
	select {
	case err := <-serveErr:
		assert.NilError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after shell exited")
	}
}
