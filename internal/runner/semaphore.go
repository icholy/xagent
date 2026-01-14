package runner

import (
	"context"
	"sync"
)

// SafeSemaphore is a weighted semaphore that safely handles releases at capacity.
// Unlike golang.org/x/sync/semaphore, releasing when count is at capacity is a no-op,
// which prevents panics when events arrive out of order.
type SafeSemaphore struct {
	mu       sync.Mutex
	cond     *sync.Cond
	count    int64
	capacity int64
}

// NewSafeSemaphore creates a new SafeSemaphore with the given capacity.
// All permits start as available.
func NewSafeSemaphore(capacity int64) *SafeSemaphore {
	s := &SafeSemaphore{
		capacity: capacity,
		count:    capacity,
	}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire blocks until n permits are available or context is cancelled.
func (s *SafeSemaphore) Acquire(ctx context.Context, n int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for s.count < n {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Use a channel to make cond.Wait cancellable
		done := make(chan struct{})
		go func() {
			s.mu.Lock()
			s.cond.Wait()
			s.mu.Unlock()
			close(done)
		}()

		s.mu.Unlock()
		select {
		case <-ctx.Done():
			s.mu.Lock()
			// Wake the goroutine so it can exit
			s.cond.Broadcast()
			return ctx.Err()
		case <-done:
			s.mu.Lock()
		}
	}

	s.count -= n
	return nil
}

// TryAcquire attempts to acquire n permits without blocking.
// Returns true if successful, false if permits are not available.
func (s *SafeSemaphore) TryAcquire(n int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.count >= n {
		s.count -= n
		return true
	}
	return false
}

// Release adds n permits back to the semaphore.
// If this would exceed capacity, count is capped at capacity.
// This is safe to call even when the semaphore is at full capacity.
func (s *SafeSemaphore) Release(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count += n
	if s.count > s.capacity {
		s.count = s.capacity
	}
	s.cond.Broadcast()
}
