package xmcp

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
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

func TestSocketTransport(t *testing.T) {
	// Create a temporary socket
	socketPath := t.TempDir() + "/test.sock"

	// Start a simple server that sends a message
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

		// Send a JSON-RPC response
		conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"pong"}` + "\n"))
	}()

	// Connect using SocketTransport
	transport := &SocketTransport{SocketPath: socketPath}
	conn, err := transport.Connect(context.Background())
	assert.NilError(t, err)
	defer conn.Close()

	// Wait for server
	select {
	case <-serverReady:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("server did not accept")
	}

	// Read the message
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	msg, err := conn.Read(ctx)
	assert.NilError(t, err)
	assert.Assert(t, msg != nil, "expected message, got nil")

	// Verify it's a response
	resp, ok := msg.(*jsonrpc.Response)
	assert.Assert(t, ok, "expected Response, got %T", msg)
	// ID comes as int64 from the MCP library
	id, ok := resp.ID.Raw().(int64)
	assert.Assert(t, ok, "expected int64 ID, got %T", resp.ID.Raw())
	assert.Equal(t, id, int64(1))
}

func TestNewProxy(t *testing.T) {
	// Test with nil stdin/stdout (defaults to os.Stdin/os.Stdout)
	proxy := NewProxy("/test.sock", nil, nil)
	assert.Equal(t, proxy.socketPath, "/test.sock")
	assert.Equal(t, proxy.stdin, os.Stdin)
}
