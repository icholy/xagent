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

// NotificationClient connects to the server's /events SSE endpoint,
// decodes each event into a model.Notification, and hands it to a caller
// supplied handler. Reconnects automatically on disconnect.
type NotificationClient struct {
	baseURL   string
	runner    string
	http      *http.Client
	log       *slog.Logger
	reconnect time.Duration
	handler   func(model.Notification)
}

// NotificationClientOptions configures a NotificationClient.
type NotificationClientOptions struct {
	// BaseURL is the server URL (e.g. https://xagent.choly.ca).
	BaseURL string
	// Runner is sent as the ?runner= filter; when empty, no filter is sent
	// and the server forwards the whole-org stream.
	Runner string
	// Token is the bearer token sent with each SSE request. The client
	// builds its own no-timeout http.Client internally (SSE connections
	// are long-lived).
	Token string
	// Log is used for connection diagnostics.
	Log *slog.Logger
	// ReconnectInterval is the wait between reconnect attempts after a
	// disconnect. Defaults to DefaultSSEReconnectInterval.
	ReconnectInterval time.Duration
	// Handler is called once per received event with the decoded
	// notification. Required.
	Handler func(model.Notification)
}

// NewNotificationClient returns a new NotificationClient.
func NewNotificationClient(opts NotificationClientOptions) *NotificationClient {
	return &NotificationClient{
		baseURL: opts.BaseURL,
		runner:  opts.Runner,
		http: &http.Client{
			Transport: &AuthTransport{Transport: http.DefaultTransport, Token: opts.Token},
		},
		log:       cmp.Or(opts.Log, slog.Default()),
		reconnect: cmp.Or(opts.ReconnectInterval, DefaultSSEReconnectInterval),
		handler:   opts.Handler,
	}
}

// Run connects to the SSE endpoint and dispatches events to the handler
// until ctx is done. It reconnects automatically on disconnect.
func (c *NotificationClient) Run(ctx context.Context) error {
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

func (c *NotificationClient) connect(ctx context.Context) error {
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
