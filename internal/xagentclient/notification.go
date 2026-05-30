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

// NotificationSubscriber connects to the server's /events SSE endpoint,
// decodes each event into a model.Notification, and hands it to a caller
// supplied handler. Reconnects automatically on disconnect.
type NotificationSubscriber struct {
	baseURL   string
	runner    string
	client    *http.Client
	log       *slog.Logger
	reconnect time.Duration
	handler   func(model.Notification)
}

// NotificationSubscriberOptions configures a NotificationSubscriber.
type NotificationSubscriberOptions struct {
	// BaseURL is the server URL (e.g. https://xagent.choly.ca).
	BaseURL string
	// Runner is sent as the ?runner= filter; when empty, no filter is sent
	// and the server forwards the whole-org stream.
	Runner string
	// Client is the HTTP client used for SSE requests. It must not have a
	// request timeout since SSE connections are long-lived; its transport
	// is expected to attach authentication. Defaults to http.DefaultClient
	// (which has no timeout but no auth either).
	Client *http.Client
	// Log is used for connection diagnostics.
	Log *slog.Logger
	// ReconnectInterval is the wait between reconnect attempts after a
	// disconnect. Defaults to DefaultSSEReconnectInterval.
	ReconnectInterval time.Duration
	// Handler is called once per received event with the decoded
	// notification. Required.
	Handler func(model.Notification)
}

// NewNotificationSubscriber returns a new NotificationSubscriber.
func NewNotificationSubscriber(opts NotificationSubscriberOptions) *NotificationSubscriber {
	return &NotificationSubscriber{
		baseURL:   opts.BaseURL,
		runner:    opts.Runner,
		client:    cmp.Or(opts.Client, http.DefaultClient),
		log:       cmp.Or(opts.Log, slog.Default()),
		reconnect: cmp.Or(opts.ReconnectInterval, DefaultSSEReconnectInterval),
		handler:   opts.Handler,
	}
}

// Run connects to the SSE endpoint and dispatches events to the handler
// until ctx is done. It reconnects automatically on disconnect.
func (s *NotificationSubscriber) Run(ctx context.Context) error {
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

func (s *NotificationSubscriber) connect(ctx context.Context) error {
	u, err := url.Parse(strings.TrimRight(s.baseURL, "/") + "/events")
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if s.runner != "" {
		q := u.Query()
		q.Set("runner", s.runner)
		u.RawQuery = q.Encode()
	}
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
		var n model.Notification
		if err := json.Unmarshal(ev.Data, &n); err != nil {
			s.log.Warn("failed to decode notification", "error", err)
			continue
		}
		s.handler(n)
	}
}
