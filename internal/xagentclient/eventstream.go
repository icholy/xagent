package xagentclient

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/common"
	"github.com/icholy/xagent/internal/x/sse"
)

// DefaultSSEReconnectInterval is the wait between reconnect attempts when
// the SSE connection drops.
const DefaultSSEReconnectInterval = 5 * time.Second

// EventStreamClient connects to the server's /events SSE endpoint,
// decodes each event into a model.Notification, and hands it to a caller
// supplied handler. Reconnects automatically on disconnect.
type EventStreamClient struct {
	baseURL   string
	runner    string
	http      *http.Client
	log       *slog.Logger
	reconnect time.Duration
	handler   func(model.Notification)
}

// EventStreamClientOptions configures an EventStreamClient.
type EventStreamClientOptions struct {
	// BaseURL is the server URL (e.g. https://xagent.choly.ca).
	BaseURL string
	// Runner is sent as the ?runner= filter; when empty, no filter is sent
	// and the server forwards the whole-org stream.
	Runner string
	// HTTPClient is the HTTP client used for SSE requests. It must not
	// have a request timeout since SSE connections are long-lived; its
	// transport is expected to attach authentication. Defaults to
	// http.DefaultClient (no timeout, no auth). Use NewEventStreamHTTPClient
	// for a token-authed client suitable for this stream.
	HTTPClient *http.Client
	// Log is used for connection diagnostics.
	Log *slog.Logger
	// ReconnectInterval is the wait between reconnect attempts after a
	// disconnect. Defaults to DefaultSSEReconnectInterval.
	ReconnectInterval time.Duration
	// Handler is called once per received event with the decoded
	// notification. Required.
	Handler func(model.Notification)
}

// NewEventStreamClient returns a new EventStreamClient.
func NewEventStreamClient(opts EventStreamClientOptions) *EventStreamClient {
	return &EventStreamClient{
		baseURL:   opts.BaseURL,
		runner:    opts.Runner,
		http:      cmp.Or(opts.HTTPClient, http.DefaultClient),
		log:       cmp.Or(opts.Log, slog.Default()),
		reconnect: cmp.Or(opts.ReconnectInterval, DefaultSSEReconnectInterval),
		handler:   opts.Handler,
	}
}

// Run connects to the SSE endpoint and dispatches events to the handler
// until ctx is done. It reconnects automatically on disconnect.
func (c *EventStreamClient) Run(ctx context.Context) error {
	for {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			c.log.Warn("SSE connection lost, reconnecting", "error", err)
		}
		if !common.SleepContext(ctx, c.reconnect) {
			return ctx.Err()
		}
	}
}

func (c *EventStreamClient) connect(ctx context.Context) error {
	u, err := url.Parse(strings.TrimRight(c.baseURL, "/") + "/events")
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if c.runner != "" {
		q := u.Query()
		q.Set("runner", c.runner)
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
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
		var n model.Notification
		if err := json.Unmarshal(ev.Data, &n); err != nil {
			c.log.Warn("failed to decode notification", "error", err)
			continue
		}
		c.handler(n)
	}
}
