package safesem

import (
	"sync"
)

// Semaphore tracks the count of acquired resources against a capacity limit.
// Unlike golang.org/x/sync/semaphore, releasing below zero is a no-op,
// which prevents panics when events arrive out of order.
type Semaphore struct {
	mu       sync.Mutex
	count    int64
	capacity int64
}

// New creates a new Semaphore with the given capacity.
// Count starts at zero (no resources acquired).
func New(capacity int64) *Semaphore {
	return &Semaphore{
		capacity: capacity,
		count:    0,
	}
}

// TryAcquire attempts to acquire n slots without blocking.
// Returns true if count + n <= capacity, incrementing count.
// Returns false if acquiring would exceed capacity.
func (s *Semaphore) TryAcquire(n int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.count+n <= s.capacity {
		s.count += n
		return true
	}
	return false
}

// Release decrements the count by n.
// If this would go below zero, count is floored at zero.
// This is safe to call even when count is already zero.
func (s *Semaphore) Release(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count -= n
	if s.count < 0 {
		s.count = 0
	}
}

// Set sets the count to n directly.
// The count can exceed capacity (for over-limit scenarios).
// This is useful for initializing the semaphore to match existing state.
func (s *Semaphore) Set(n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = n
}
