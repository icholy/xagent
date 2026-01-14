package safesem

import (
	"sync"
)

// Semaphore is a weighted semaphore that safely handles releases at capacity.
// Unlike golang.org/x/sync/semaphore, releasing when count is at capacity is a no-op,
// which prevents panics when events arrive out of order.
type Semaphore struct {
	mu       sync.Mutex
	count    int64
	capacity int64
}

// New creates a new Semaphore with the given capacity.
// All permits start as available.
func New(capacity int64) *Semaphore {
	return &Semaphore{
		capacity: capacity,
		count:    capacity,
	}
}

// TryAcquire attempts to acquire n permits without blocking.
// Returns true if successful, false if permits are not available.
func (s *Semaphore) TryAcquire(n int64) bool {
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
func (s *Semaphore) Release(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count += n
	if s.count > s.capacity {
		s.count = s.capacity
	}
}

// Set sets the available permit count to n.
// The count can be negative (when running exceeds capacity) or above capacity.
// This is useful for initializing the semaphore to match existing state.
func (s *Semaphore) Set(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = n
}
