package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/sse"
)

// channelNotifier is the subset of channelTransport that pushTaskChannels
// needs. Defined as an interface so tests can exercise the SSE → channel
// translation without a real transport.
type channelNotifier interface {
	Notify(ctx context.Context, method string, params any) error
}

// channelParams is the params payload of a notifications/claude/channel
// notification: a human-readable content string plus identifier-only meta
// attributes that surface as <channel ...> tag attributes in Claude Code.
type channelParams struct {
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta"`
}

// channelResourceTypes is the allowlist of model.NotificationResource
// types that the bridge forwards to Claude as channel events. Anything
// else (e.g. future resource types unrelated to task work) is dropped.
var channelResourceTypes = map[string]bool{
	"task":      true,
	"event":     true,
	"log":       true,
	"link":      true,
	"task_logs": true,
}

// notificationToChannels translates a model.Notification into zero or more
// channel notification payloads. "ready" notifications and resources whose
// type is not in the allowlist are dropped.
func notificationToChannels(n model.Notification) []channelParams {
	if n.Type != "change" {
		return nil
	}
	var out []channelParams
	for _, r := range n.Resources {
		if !channelResourceTypes[r.Type] {
			continue
		}
		out = append(out, channelParams{
			Content: fmt.Sprintf("%s %d was %s.", r.Type, r.ID, r.Action),
			Meta: map[string]string{
				"action":   r.Action,
				"resource": r.Type,
				"id":       strconv.FormatInt(r.ID, 10),
			},
		})
	}
	return out
}

// pushTaskChannels subscribes to the C2 server's per-org SSE notification
// stream and forwards task-relevant changes as notifications/claude/channel
// events on the given transport. It reconnects with capped exponential
// backoff and exits cleanly when ctx is done.
func pushTaskChannels(ctx context.Context, transport channelNotifier, serverURL, token string) {
	const (
		baseDelay = 1 * time.Second
		maxDelay  = 30 * time.Second
	)
	attempt := 0
	for {
		if err := streamTaskChannels(ctx, transport, serverURL, token); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("xagent channel stream ended", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		delay := baseDelay << attempt
		if delay > maxDelay || delay <= 0 {
			delay = maxDelay
		}
		attempt++
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

// streamTaskChannels opens a single SSE subscription and forwards events
// until the stream ends or ctx is cancelled.
func streamTaskChannels(ctx context.Context, transport channelNotifier, serverURL, token string) error {
	endpoint := strings.TrimRight(serverURL, "/") + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	reader := sse.NewReader(resp.Body)
	for {
		ev, err := reader.Read()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read event: %w", err)
		}
		if ev.ID == "" && ev.Event == "" && ev.Retry == "" && len(ev.Data) == 0 {
			return io.EOF
		}
		if len(ev.Data) == 0 {
			continue
		}
		var n model.Notification
		if err := json.Unmarshal(ev.Data, &n); err != nil {
			slog.Warn("xagent channel: failed to decode notification", "error", err)
			continue
		}
		for _, params := range notificationToChannels(n) {
			if err := transport.Notify(ctx, "notifications/claude/channel", params); err != nil {
				return fmt.Errorf("notify: %w", err)
			}
		}
	}
}
