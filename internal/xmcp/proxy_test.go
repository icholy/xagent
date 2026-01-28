package xmcp

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

func TestProxy(t *testing.T) {
	// Create two pairs of in-memory transports.
	// Pair 1: stdioCli <-> stdioSrv (represents the stdio side)
	// Pair 2: socketCli <-> socketSrv (represents the socket side)
	stdioCli, stdioSrv := mcp.NewInMemoryTransports()
	socketCli, socketSrv := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Start the proxy between stdioSrv and socketCli.
	// This bridges the stdio server side to the socket client side.
	errCh := make(chan error, 1)
	go func() {
		errCh <- Proxy(ctx, stdioSrv, socketCli)
	}()

	// Connect to both ends of the proxy.
	stdioConn, err := stdioCli.Connect(ctx)
	assert.NilError(t, err)
	defer stdioConn.Close()

	socketConn, err := socketSrv.Connect(ctx)
	assert.NilError(t, err)
	defer socketConn.Close()

	// Send a message from the stdio side.
	id, err := jsonrpc.MakeID(float64(1))
	assert.NilError(t, err)
	msg := &jsonrpc.Request{
		ID:     id,
		Method: "test/ping",
	}
	err = stdioConn.Write(ctx, msg)
	assert.NilError(t, err)

	// Read the message on the socket side.
	received, err := socketConn.Read(ctx)
	assert.NilError(t, err)

	// Verify the message came through correctly.
	receivedReq, ok := received.(*jsonrpc.Request)
	assert.Assert(t, ok, "expected *jsonrpc.Request, got %T", received)
	assert.Equal(t, receivedReq.Method, "test/ping")

	// Send a response back from the socket side.
	resp := &jsonrpc.Response{
		ID:     id,
		Result: []byte(`{"status":"pong"}`),
	}
	err = socketConn.Write(ctx, resp)
	assert.NilError(t, err)

	// Read the response on the stdio side.
	receivedResp, err := stdioConn.Read(ctx)
	assert.NilError(t, err)

	// Verify the response came through correctly.
	receivedRespObj, ok := receivedResp.(*jsonrpc.Response)
	assert.Assert(t, ok, "expected *jsonrpc.Response, got %T", receivedResp)
	assert.Equal(t, string(receivedRespObj.Result), `{"status":"pong"}`)

	// Cancel context to stop the proxy.
	cancel()
}
