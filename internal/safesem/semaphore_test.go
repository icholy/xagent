package safesem

import (
	"sync"
	"testing"
)

func TestSemaphore_TryAcquire(t *testing.T) {
	sem := New(3)

	// Should acquire successfully (count goes 0->1->2->3)
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed")
	}
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed")
	}
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed")
	}

	// Should fail - at capacity
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail when at capacity")
	}
}

func TestSemaphore_Release(t *testing.T) {
	sem := New(2)

	// Acquire all capacity
	if !sem.TryAcquire(2) {
		t.Fatal("expected TryAcquire(2) to succeed")
	}

	// Release and acquire again
	sem.Release(1)
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed after release")
	}
}

func TestSemaphore_ReleaseAtZero(t *testing.T) {
	sem := New(2)

	// Release when already at zero - should be a no-op (not panic)
	sem.Release(1)
	sem.Release(1)
	sem.Release(100) // Should still be floored at 0

	// Should be able to acquire full capacity
	if !sem.TryAcquire(2) {
		t.Error("expected TryAcquire(2) to succeed")
	}
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail - at capacity")
	}
}

func TestSemaphore_Set(t *testing.T) {
	sem := New(5)

	// Set to a specific value (e.g., 3 running containers)
	sem.Set(3)
	// Should only be able to acquire 2 more (capacity - current = 5 - 3 = 2)
	if !sem.TryAcquire(2) {
		t.Error("expected TryAcquire(2) to succeed after Set(3)")
	}
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail - at capacity")
	}

	// Set above capacity is allowed (over-limit scenario)
	sem.Set(10)
	// No new acquires should succeed
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail when count > capacity")
	}
	// After releases, count drops and eventually acquires succeed
	sem.Release(1) // 10 -> 9
	sem.Release(1) // 9 -> 8
	sem.Release(1) // 8 -> 7
	sem.Release(1) // 7 -> 6
	sem.Release(1) // 6 -> 5
	// Now at capacity, still can't acquire
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail when count == capacity")
	}
	sem.Release(1) // 5 -> 4
	// Now can acquire
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed when count < capacity")
	}
}

func TestSemaphore_Concurrent(t *testing.T) {
	sem := New(5)

	var wg sync.WaitGroup
	acquired := make(chan struct{}, 100)

	// Start 20 goroutines trying to acquire
	for range 20 {
		wg.Go(func() {
			if sem.TryAcquire(1) {
				acquired <- struct{}{}
				// Hold for a moment then release
				sem.Release(1)
			}
		})
	}

	wg.Wait()
	close(acquired)

	// At least some should have succeeded
	count := 0
	for range acquired {
		count++
	}
	if count == 0 {
		t.Error("expected at least some acquisitions to succeed")
	}
}
