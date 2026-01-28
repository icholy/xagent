package xmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

// Proxy creates bidirectional message forwarding between two MCP transports.
// It blocks until the context is cancelled or an error occurs.
func Proxy(ctx context.Context, t1, t2 mcp.Transport) error {
	conn1, err := t1.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn1.Close()

	conn2, err := t2.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn2.Close()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return forward(ctx, conn1, conn2)
	})

	g.Go(func() error {
		return forward(ctx, conn2, conn1)
	})

	return g.Wait()
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
