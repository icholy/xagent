package xmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
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

	// Use a pipe so we can control when stdin "closes"
	stdinR, stdinW := io.Pipe()
	stdout := &bytes.Buffer{}

	proxy := NewProxy(socketPath)
	proxy.SetIO(stdinR, stdout)

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

	// Give time for echo to complete
	time.Sleep(50 * time.Millisecond)

	// Close stdin to signal EOF
	stdinW.Close()

	// Wait for proxy to finish
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		cancel()
	}

	// Verify we got the echoed response
	output := stdout.String()
	assert.Assert(t, len(output) > 0, "expected output, got empty string")

	// Parse the output as JSON to verify it's valid
	// Output may have trailing newline, so trim it
	var msg map[string]any
	err = json.Unmarshal([]byte(strings.TrimSpace(output)), &msg)
	assert.NilError(t, err)
	assert.Equal(t, msg["method"], "test")
}

func TestSocketTransport(t *testing.T) {
	// Create a temporary socket
	socketPath := t.TempDir() + "/test.sock"

	// Start a simple server
	listener, err := net.Listen("unix", socketPath)
	assert.NilError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read message and respond
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		conn.Write(buf[:n])
	}()

	// Connect using SocketTransport
	transport := &SocketTransport{SocketPath: socketPath}
	conn, err := transport.Connect(context.Background())
	assert.NilError(t, err)
	defer conn.Close()

	// Write a message
	msg := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	err = conn.Write(context.Background(), msg)
	assert.NilError(t, err)

	// Session ID should be non-empty
	assert.Assert(t, conn.SessionID() != "", "expected non-empty session ID")
}

func TestProxyClose(t *testing.T) {
	proxy := NewProxy("/nonexistent.sock")

	// Close on unconnected proxy should not error
	err := proxy.Close()
	assert.NilError(t, err)
}

func TestNewProxy(t *testing.T) {
	proxy := NewProxy("/test.sock")
	assert.Equal(t, proxy.socketPath, "/test.sock")
	assert.Equal(t, proxy.stdin, os.Stdin)
	assert.Equal(t, proxy.stdout, os.Stdout)
}
