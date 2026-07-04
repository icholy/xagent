package xagentclient

import (
	"cmp"
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/cenkalti/backoff/v5"
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

// DefaultBackOff returns the backoff schedule used when RetryOptions.NewBackOff
// is nil: exponential growth (with jitter) from DefaultInitialBackoff, capped
// at DefaultMaxBackoff.
func DefaultBackOff() backoff.BackOff {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = DefaultInitialBackoff
	bo.MaxInterval = DefaultMaxBackoff
	return bo
}

// RetryOptions configures RetryInterceptor.
type RetryOptions struct {
	// MaxRetries is the number of retry attempts after the initial call.
	// Defaults to DefaultMaxRetries when zero. Use a negative value to
	// disable retries.
	MaxRetries int
	// NewBackOff constructs the backoff schedule for a single request. It is
	// invoked once per call because a backoff.BackOff is stateful and the
	// client is used concurrently, so each request needs its own instance.
	// Defaults to DefaultBackOff when nil.
	NewBackOff func() backoff.BackOff
}

// RetryInterceptor retries failed unary requests, delegating the backoff
// schedule to cenkalti/backoff. Only transient errors (connect.CodeUnavailable,
// which Connect reports for connection failures and unreachable servers) are
// retried, so a retried request is one that most likely never reached the
// server. Streaming calls are passed through unchanged.
//
// Connect has no built-in retry mechanism (retries are left to interceptors),
// so the interceptor plumbing and the retryable-code policy are ours; only the
// backoff schedule comes from the library.
type RetryInterceptor struct {
	maxRetries int
	newBackOff func() backoff.BackOff
}

// NewRetryInterceptor builds a RetryInterceptor from opts, applying defaults
// for any zero-valued field.
func NewRetryInterceptor(opts RetryOptions) *RetryInterceptor {
	newBackOff := opts.NewBackOff
	if newBackOff == nil {
		newBackOff = DefaultBackOff
	}
	return &RetryInterceptor{
		maxRetries: cmp.Or(opts.MaxRetries, DefaultMaxRetries),
		newBackOff: newBackOff,
	}
}

// WrapUnary retries the call while it fails with a retryable error, backing off
// according to the configured schedule between attempts.
func (i *RetryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		// maxTries counts the initial attempt plus retries; clamp to at least
		// one so a negative MaxRetries disables retries without underflowing.
		maxTries := uint(max(i.maxRetries+1, 1))
		return backoff.Retry(ctx, func() (connect.AnyResponse, error) {
			res, err := next(ctx, req)
			if err != nil && !isRetryable(err) {
				// Permanent stops the retry loop and unwraps to the original error.
				return nil, backoff.Permanent(err)
			}
			return res, err
		},
			backoff.WithBackOff(i.newBackOff()),
			backoff.WithMaxTries(maxTries),
			// Bound retries by attempt count only, not wall-clock time.
			backoff.WithMaxElapsedTime(0),
		)
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
