package command

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/icholy/xagent/internal/shellwire"
	"gotest.tools/v3/assert"
)

// fakeConn is an in-memory wsConn: toClient carries frames the peer sends to the
// pump (its Read side), fromClient collects frames the pump writes (its Write
// side). It lets pump be exercised without a real WebSocket dial.
type fakeConn struct {
	toClient   chan []byte
	fromClient chan []byte
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		toClient:   make(chan []byte, 8),
		fromClient: make(chan []byte, 8),
	}
}

func (f *fakeConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case msg, ok := <-f.toClient:
		if !ok {
			return 0, nil, io.EOF
		}
		return websocket.MessageBinary, msg, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (f *fakeConn) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	// Copy: pump reuses its stdin buffer across writes.
	cp := append([]byte(nil), p...)
	select {
	case f.fromClient <- cp:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pushExit delivers an exit frame carrying code to the pump.
func (f *fakeConn) pushExit(t *testing.T, code int) {
	t.Helper()
	select {
	case f.toClient <- shellwire.Exit(code):
	case <-time.After(5 * time.Second):
		t.Fatal("timed out delivering exit frame")
	}
}

// pumpResult is the outcome of a pump run.
type pumpResult struct {
	code int
	err  error
}

// runPump starts pump in the background and returns a channel with its result.
func runPump(t *testing.T, conn wsConn, in io.Reader, out io.Writer, resize <-chan winSize) <-chan pumpResult {
	t.Helper()
	done := make(chan pumpResult, 1)
	go func() {
		code, err := pump(t.Context(), conn, in, out, resize)
		done <- pumpResult{code: code, err: err}
	}()
	return done
}

// readFrame reads and parses the next frame the pump wrote.
func readFrame(t *testing.T, conn *fakeConn) shellwire.Frame {
	t.Helper()
	select {
	case msg := <-conn.fromClient:
		frame, err := shellwire.Parse(msg)
		assert.NilError(t, err)
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for outgoing frame")
		return shellwire.Frame{}
	}
}

// waitResult waits for pump to return, asserts no error, and returns the code.
func waitResult(t *testing.T, done <-chan pumpResult) int {
	t.Helper()
	select {
	case r := <-done:
		assert.NilError(t, r.err)
		return r.code
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pump to return")
		return 0
	}
}

func TestPump_StdinToDataFrame(t *testing.T) {
	t.Parallel()
	// Arrange
	conn := newFakeConn()
	inR, inW := io.Pipe()
	t.Cleanup(func() { _ = inW.Close() })
	done := runPump(t, conn, inR, io.Discard, nil)

	// Act: local input is framed as a data frame.
	_, err := inW.Write([]byte("ls -la\n"))
	assert.NilError(t, err)

	// Assert
	frame := readFrame(t, conn)
	assert.Equal(t, frame.Type, shellwire.TypeData)
	assert.Equal(t, string(frame.Payload), "ls -la\n")

	conn.pushExit(t, 0)
	assert.Equal(t, waitResult(t, done), 0)
}

func TestPump_DataFrameToStdout(t *testing.T) {
	t.Parallel()
	// Arrange
	conn := newFakeConn()
	outR, outW := io.Pipe()
	t.Cleanup(func() { _ = outR.Close() })
	done := runPump(t, conn, strings.NewReader(""), outW, nil)

	// Act: an incoming data frame is written to stdout.
	conn.toClient <- shellwire.Data([]byte("hello\n"))

	// Assert
	buf := make([]byte, len("hello\n"))
	_, err := io.ReadFull(outR, buf)
	assert.NilError(t, err)
	assert.Equal(t, string(buf), "hello\n")

	conn.pushExit(t, 0)
	assert.Equal(t, waitResult(t, done), 0)
}

func TestPump_ExitFrameReturnsCode(t *testing.T) {
	t.Parallel()
	// Arrange
	conn := newFakeConn()
	done := runPump(t, conn, strings.NewReader(""), io.Discard, nil)

	// Act
	conn.pushExit(t, 7)

	// Assert
	assert.Equal(t, waitResult(t, done), 7)
}

func TestPump_ResizeFrame(t *testing.T) {
	t.Parallel()
	// Arrange
	conn := newFakeConn()
	resize := make(chan winSize, 1)
	done := runPump(t, conn, strings.NewReader(""), io.Discard, resize)

	// Act: a terminal size becomes a resize frame.
	resize <- winSize{rows: 40, cols: 100}

	// Assert
	frame := readFrame(t, conn)
	rows, cols, err := frame.ResizeDims()
	assert.NilError(t, err)
	assert.Equal(t, rows, uint16(40))
	assert.Equal(t, cols, uint16(100))

	conn.pushExit(t, 0)
	assert.Equal(t, waitResult(t, done), 0)
}
