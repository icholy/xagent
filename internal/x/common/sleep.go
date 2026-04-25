package common

import (
	"context"
	"time"
)

// SleepContext sleeps for the specified duration or until the context is canceled.
// Returns true if the sleep completed, false if the context was canceled.
func SleepContext(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
