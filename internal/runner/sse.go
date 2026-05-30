package runner

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/wakeup"
	"github.com/icholy/xagent/internal/xagentclient"
)

// DefaultSSEReconnectInterval is the wait between reconnect attempts when
// the SSE connection drops.
const DefaultSSEReconnectInterval = xagentclient.DefaultSSEReconnectInterval

// SSESubscriber connects to the server's /events endpoint with a runner
// filter and signals a buffered (size-1) channel each time the server
// pushes an event. The server only forwards events carrying pending work
// for this runner, so any event received is a reason to wake up and poll.
// Reconnects automatically with a fixed backoff on disconnect.
type SSESubscriber struct {
	sub    *xagentclient.NotificationSubscriber
	notify wakeup.Chan
}

// SSESubscriberOptions configures an SSESubscriber.
type SSESubscriberOptions struct {
	// BaseURL is the server URL (e.g. https://xagent.choly.ca).
	BaseURL string
	// RunnerID is sent as the ?runner= filter so the server only forwards
	// notifications carrying pending work for this runner.
	RunnerID string
	// Client is the HTTP client used for SSE requests. It must not have a
	// request timeout since SSE connections are long-lived; its transport
	// is expected to attach the runner's bearer token. Defaults to
	// http.DefaultClient (which has no timeout but no auth either).
	Client *http.Client
	// Log is used for connection diagnostics.
	Log *slog.Logger
	// ReconnectInterval is the wait between reconnect attempts after a
	// disconnect. Defaults to DefaultSSEReconnectInterval.
	ReconnectInterval time.Duration
}

// NewSSESubscriber returns a new SSESubscriber.
func NewSSESubscriber(opts SSESubscriberOptions) *SSESubscriber {
	notify := wakeup.New()
	sub := xagentclient.NewNotificationSubscriber(xagentclient.NotificationSubscriberOptions{
		BaseURL:           opts.BaseURL,
		Runner:            opts.RunnerID,
		Client:            opts.Client,
		Log:               opts.Log,
		ReconnectInterval: opts.ReconnectInterval,
		Handler:           func(model.Notification) { notify.Wake() },
	})
	return &SSESubscriber{
		sub:    sub,
		notify: notify,
	}
}

// C returns the wake-up channel. It receives one value per coalesced burst
// of server events.
func (s *SSESubscriber) C() <-chan struct{} {
	return s.notify
}

// Run connects to the SSE endpoint and processes events until ctx is done.
// It reconnects automatically on disconnect. The server sends a `ready`
// event on each (re)connect which naturally signals the wake-up channel,
// so the runner picks up any changes it missed while disconnected.
func (s *SSESubscriber) Run(ctx context.Context) error {
	return s.sub.Run(ctx)
}
