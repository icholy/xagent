package xmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
)

// Proxy implements an stdio MCP server that proxies tool calls through a Unix domain socket.
// It reads JSON-RPC messages from stdin and forwards them to a remote MCP server
// over a Unix socket, then writes responses back to stdout.
type Proxy struct {
	socketPath string
	conn       net.Conn
	stdin      io.Reader
	stdout     io.Writer
	mu         sync.Mutex
}

// NewProxy creates a new Proxy that will connect to the given Unix socket path.
func NewProxy(socketPath string) *Proxy {
	return &Proxy{
		socketPath: socketPath,
		stdin:      os.Stdin,
		stdout:     os.Stdout,
	}
}

// Run starts the proxy and blocks until the context is cancelled or an error occurs.
// It connects to the Unix socket and proxies messages between stdio and the socket.
func (p *Proxy) Run(ctx context.Context) error {
	conn, err := net.Dial("unix", p.socketPath)
	if err != nil {
		return err
	}
	p.conn = conn
	defer p.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	// Forward stdin to socket
	go func() {
		errCh <- p.forwardToSocket(ctx)
	}()

	// Forward socket to stdout
	go func() {
		errCh <- p.forwardToStdout(ctx)
	}()

	select {
	case err := <-errCh:
		cancel()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// forwardToSocket reads JSON-RPC messages from stdin and writes them to the socket.
func (p *Proxy) forwardToSocket(ctx context.Context) error {
	scanner := bufio.NewScanner(p.stdin)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Validate it's valid JSON
		if !json.Valid(line) {
			continue
		}

		p.mu.Lock()
		_, err := p.conn.Write(append(line, '\n'))
		p.mu.Unlock()
		if err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

// forwardToStdout reads JSON-RPC messages from the socket and writes them to stdout.
func (p *Proxy) forwardToStdout(ctx context.Context) error {
	scanner := bufio.NewScanner(p.conn)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		_, err := p.stdout.Write(append(line, '\n'))
		if err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

// Close closes the connection to the Unix socket.
func (p *Proxy) Close() error {
	if p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn = nil
	return err
}

// SetIO allows overriding stdin/stdout for testing.
func (p *Proxy) SetIO(stdin io.Reader, stdout io.Writer) {
	p.stdin = stdin
	p.stdout = stdout
}

// SocketTransport implements mcp.Transport for connecting to a Unix socket.
type SocketTransport struct {
	SocketPath string
}

// Connect implements the mcp.Transport interface.
func (t *SocketTransport) Connect(ctx context.Context) (*socketConnection, error) {
	conn, err := net.Dial("unix", t.SocketPath)
	if err != nil {
		return nil, err
	}
	return &socketConnection{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
	}, nil
}

type socketConnection struct {
	conn    net.Conn
	scanner *bufio.Scanner
	mu      sync.Mutex
	closed  bool
}

// Read reads the next JSON-RPC message from the connection.
func (c *socketConnection) Read(ctx context.Context) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("connection closed")
	}
	c.mu.Unlock()

	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return json.RawMessage(c.scanner.Bytes()), nil
}

// Write writes a JSON-RPC message to the connection.
func (c *socketConnection) Write(ctx context.Context, msg json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errors.New("connection closed")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(append(data, '\n'))
	return err
}

// Close closes the connection.
func (c *socketConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}

// SessionID returns a unique session identifier.
func (c *socketConnection) SessionID() string {
	return c.conn.LocalAddr().String()
}
