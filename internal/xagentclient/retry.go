package xagentclient

import (
	"cmp"
	"context"
	"math/rand/v2"
	"time"

	"connectrpc.com/connect"
)

// Retry defaults used when a RetryOptions field is left zero.
const (
	// DefaultMaxRetries is the number of retry attempts after the initial call.
	DefaultMaxRetries = 4
	// DefaultInitialBackoff is the delay before the first retry.
	DefaultInitialBackoff = 100 * time.Millisecond
	// DefaultMaxBackoff caps the delay between retries.
	DefaultMaxBackoff = 5 * time.Second
)

// RetryOptions configures RetryInterceptor.
type RetryOptions struct {
	// MaxRetries is the number of retry attempts after the initial call.
	// Defaults to DefaultMaxRetries when zero. Use a negative value to
	// disable retries.
	MaxRetries int
	// InitialBackoff is the delay before the first retry.
	// Defaults to DefaultInitialBackoff when zero.
	InitialBackoff time.Duration
	// MaxBackoff caps the delay between retries.
	// Defaults to DefaultMaxBackoff when zero.
	MaxBackoff time.Duration
}

// RetryInterceptor retries failed unary requests with exponential backoff and
// jitter. Only transient errors (connect.CodeUnavailable, which Connect
// reports for connection failures and unreachable servers) are retried, so a
// retried request is one that most likely never reached the server. Streaming
// calls are passed through unchanged.
type RetryInterceptor struct {
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

// NewRetryInterceptor builds a RetryInterceptor from opts, applying defaults
// for any zero-valued field.
func NewRetryInterceptor(opts RetryOptions) *RetryInterceptor {
	return &RetryInterceptor{
		maxRetries:     cmp.Or(opts.MaxRetries, DefaultMaxRetries),
		initialBackoff: cmp.Or(opts.InitialBackoff, DefaultInitialBackoff),
		maxBackoff:     cmp.Or(opts.MaxBackoff, DefaultMaxBackoff),
	}
}

// WrapUnary retries the call while it fails with a retryable error, sleeping
// with exponential backoff between attempts.
func (i *RetryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		backoff := i.initialBackoff
		for attempt := 0; ; attempt++ {
			res, err := next(ctx, req)
			if err == nil || attempt >= i.maxRetries || !isRetryable(err) {
				return res, err
			}
			if !sleepWithJitter(ctx, backoff) {
				return res, err
			}
			backoff = min(backoff*2, i.maxBackoff)
		}
	}
}

// WrapStreamingClient passes streaming calls through unchanged; they are not
// retried because their request bodies cannot be safely replayed.
func (i *RetryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler is unused on the client but required to satisfy
// connect.Interceptor.
func (i *RetryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// isRetryable reports whether err represents a transient failure that is safe
// to retry. Connect maps connection failures and unreachable servers to
// CodeUnavailable, which is the code we retry.
func isRetryable(err error) bool {
	return connect.CodeOf(err) == connect.CodeUnavailable
}

// sleepWithJitter waits for a randomized duration in [d/2, d) and reports
// whether it completed. It returns false if ctx is cancelled first.
func sleepWithJitter(ctx context.Context, d time.Duration) bool {
	jittered := d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
	t := time.NewTimer(jittered)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
