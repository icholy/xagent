//go:generate go tool moq -out router_moq_test.go . Router

package webhookserver

import (
	"context"

	"github.com/icholy/xagent/internal/eventrouter"
)

// Router routes events to subscribed tasks.
type Router interface {
	Route(ctx context.Context, input eventrouter.InputEvent) (int, error)
}
