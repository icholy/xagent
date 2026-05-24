package runner

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/icholy/xagent/internal/x/common"
	"github.com/icholy/xagent/internal/x/sse"
)

// DefaultSSEReconnectInterval is the wait between reconnect attempts when
// the SSE connection drops.
const DefaultSSEReconnectInterval = 5 * time.Second

// SSESubscriber connects to the server's /events endpoint with a runner
// filter and signals a buffered (size-1) channel each time the server
// pushes an event. The server only forwards events carrying pending work
// for this runner, so any event received is a reason to wake up and poll.
// Reconnects automatically with a fixed backoff on disconnect.
type SSESubscriber struct {
	baseURL   string
	runnerID  string
	client    *http.Client
	log       *slog.Logger
	reconnect time.Duration
	notify    chan struct{}
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
	return &SSESubscriber{
		baseURL:   opts.BaseURL,
		runnerID:  opts.RunnerID,
		client:    cmp.Or(opts.Client, http.DefaultClient),
		log:       cmp.Or(opts.Log, slog.Default()),
		reconnect: cmp.Or(opts.ReconnectInterval, DefaultSSEReconnectInterval),
		notify:    make(chan struct{}, 1),
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
	for {
		err := s.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			s.log.Warn("SSE connection lost, reconnecting", "error", err)
		}
		if !common.SleepContext(ctx, s.reconnect) {
			return ctx.Err()
		}
	}
}

func (s *SSESubscriber) connect(ctx context.Context) error {
	u, err := url.Parse(strings.TrimRight(s.baseURL, "/") + "/events")
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	q := u.Query()
	q.Set("runner", s.runnerID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	r := sse.NewReader(resp.Body)
	for {
		ev, err := r.Read()
		if err != nil {
			return err
		}
		// The reader returns a zero Event with nil error on clean EOF.
		// Real events always carry an Event type from the server.
		if ev.Event == "" {
			return nil
		}
		s.signal()
	}
}

func (s *SSESubscriber) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}
