package xmcp

import (
	"context"
	"io"
	"net"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Proxy implements an stdio MCP proxy that forwards messages through a Unix domain socket.
// It uses the MCP library's transport infrastructure to handle JSON-RPC message framing.
type Proxy struct {
	socketPath string
	stdin      io.ReadCloser
	stdout     io.WriteCloser
}

// NewProxy creates a new Proxy that will connect to the given Unix socket path.
// If stdin is nil, os.Stdin is used. If stdout is nil, os.Stdout is used.
func NewProxy(socketPath string, stdin io.ReadCloser, stdout io.WriteCloser) *Proxy {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = nopWriteCloser{os.Stdout}
	}
	return &Proxy{
		socketPath: socketPath,
		stdin:      stdin,
		stdout:     stdout,
	}
}

// Run starts the proxy and blocks until the context is cancelled or an error occurs.
// It connects to the Unix socket and creates bidirectional message forwarding
// between stdio and the socket using MCP transports.
func (p *Proxy) Run(ctx context.Context) error {
	// Connect to the Unix socket
	conn, err := net.Dial("unix", p.socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Create transports using the MCP library
	stdioTransport := &mcp.IOTransport{Reader: p.stdin, Writer: p.stdout}
	socketTransport := &mcp.IOTransport{Reader: conn, Writer: conn}

	// Connect both transports to get connections
	stdioConn, err := stdioTransport.Connect(ctx)
	if err != nil {
		return err
	}
	defer stdioConn.Close()

	socketConn, err := socketTransport.Connect(ctx)
	if err != nil {
		return err
	}
	defer socketConn.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	// Forward stdin to socket
	go func() {
		errCh <- forward(ctx, stdioConn, socketConn)
	}()

	// Forward socket to stdout
	go func() {
		errCh <- forward(ctx, socketConn, stdioConn)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// forward reads messages from src and writes them to dst.
func forward(ctx context.Context, src, dst mcp.Connection) error {
	for {
		msg, err := src.Read(ctx)
		if err != nil {
			return err
		}
		if err := dst.Write(ctx, msg); err != nil {
			return err
		}
	}
}

// nopWriteCloser wraps an io.Writer to implement io.WriteCloser with a no-op Close.
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// SocketTransport implements mcp.Transport for connecting to a Unix domain socket.
type SocketTransport struct {
	SocketPath string
}

// Connect implements the mcp.Transport interface by dialing the Unix socket
// and returning an MCP Connection.
func (t *SocketTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := net.Dial("unix", t.SocketPath)
	if err != nil {
		return nil, err
	}
	transport := &mcp.IOTransport{Reader: conn, Writer: conn}
	return transport.Connect(ctx)
}
