package xmcp

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestProxy(t *testing.T) {
	// Create a temporary socket
	socketPath := t.TempDir() + "/test.sock"

	// Start a simple echo server on the socket
	listener, err := net.Listen("unix", socketPath)
	assert.NilError(t, err)
	defer listener.Close()

	serverReady := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(serverReady)

		// Echo back whatever we receive
		io.Copy(conn, conn)
	}()

	// Use a pipe so we can control stdin/stdout
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	proxy := NewProxy(socketPath, stdinR, stdoutW)

	// Run proxy in goroutine
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go func() {
		done <- proxy.Run(ctx)
	}()

	// Wait for server to accept
	select {
	case <-serverReady:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("server did not accept connection")
	}

	// Send a message
	_, err = stdinW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"test"}` + "\n"))
	assert.NilError(t, err)

	// Read the echoed response with timeout
	readDone := make(chan string, 1)
	go func() {
		buf := make([]byte, 1024)
		n, _ := stdoutR.Read(buf)
		readDone <- string(buf[:n])
	}()

	var output string
	select {
	case output = <-readDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout reading from stdout")
	}

	// Verify we got the echoed response
	assert.Assert(t, len(output) > 0, "expected output, got empty string")
	assert.Assert(t, strings.Contains(output, `"method":"test"`), "expected method:test in output, got: %s", output)

	// Clean up
	stdinW.Close()
	cancel()

	// Wait for proxy to finish
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNewProxy(t *testing.T) {
	stdinR, _ := io.Pipe()
	_, stdoutW := io.Pipe()
	proxy := NewProxy("/test.sock", stdinR, stdoutW)
	assert.Equal(t, proxy.socketPath, "/test.sock")
	assert.Equal(t, proxy.stdin, stdinR)
	assert.Equal(t, proxy.stdout, stdoutW)
}
