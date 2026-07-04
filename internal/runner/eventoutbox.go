package runner

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/cenkalti/backoff/v5"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/x/outbox"
	"github.com/icholy/xagent/internal/xagentclient"
)

// RunnerEventOutboxOptions configures the runner's durable event outbox.
type RunnerEventOutboxOptions struct {
	// Store is the durable backing store. The caller opens it: a FileStore under
	// the runner's persistent state dir in production, a temp-dir store in tests.
	Store outbox.Store
	// Client delivers events to the server via SubmitRunnerEvents.
	Client xagentclient.Client
	// Backoff is the retry policy for transient delivery failures.
	Backoff backoff.BackOff
	// Log receives delivery diagnostics.
	Log *slog.Logger
}

// NewRunnerEventOutbox builds the durable outbox that delivers runner lifecycle
// events (started/stopped/failed) to the server. It is the single source of
// truth for the Deliver closure — proto conversion + SubmitRunnerEvents, with
// permanence classified by isPermanentError so unrecoverable events are
// dead-lettered rather than retried forever — shared by the production wiring
// and the tests.
func NewRunnerEventOutbox(opts RunnerEventOutboxOptions) *outbox.Outbox[model.RunnerEvent] {
	return outbox.New(outbox.Options[model.RunnerEvent]{
		Store: opts.Store,
		Deliver: func(ctx context.Context, ev model.RunnerEvent) (permanent bool, err error) {
			_, err = opts.Client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
				Events: []*xagentv1.RunnerEvent{ev.Proto()},
			})
			return isPermanentError(err), err
		},
		Backoff: opts.Backoff,
		Log:     opts.Log,
	})
}

// isPermanentError returns true if the error indicates a condition that will
// never succeed on retry (e.g. task not found, invalid argument). The outbox
// dead-letters permanent failures instead of retrying them.
func isPermanentError(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeNotFound, connect.CodeInvalidArgument, connect.CodePermissionDenied:
		return true
	default:
		return false
	}
}
