//go:generate go tool moq -out router_moq_test.go . Router

package webhook

import (
	"context"

	"github.com/icholy/xagent/internal/eventrouter"
)

// Router routes events to subscribed tasks.
type Router interface {
	Route(ctx context.Context, event eventrouter.Event) (int, error)
}
