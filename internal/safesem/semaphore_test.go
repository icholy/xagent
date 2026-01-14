package safesem

import (
	"sync"
	"testing"
)

func TestSemaphore_TryAcquire(t *testing.T) {
	sem := New(3)

	// Should acquire successfully
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed")
	}
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed")
	}
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed")
	}

	// Should fail - no permits left
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail when no permits available")
	}
}

func TestSemaphore_Release(t *testing.T) {
	sem := New(2)

	// Acquire all permits
	if !sem.TryAcquire(2) {
		t.Fatal("expected TryAcquire(2) to succeed")
	}

	// Release and acquire again
	sem.Release(1)
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed after release")
	}
}

func TestSemaphore_ReleaseAtCapacity(t *testing.T) {
	sem := New(2)

	// Release when already at capacity - should be a no-op (not panic)
	sem.Release(1)
	sem.Release(1)
	sem.Release(100) // Should still be capped at capacity

	// Should only be able to acquire capacity amount
	if !sem.TryAcquire(2) {
		t.Error("expected TryAcquire(2) to succeed")
	}
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail - release should have been capped at capacity")
	}
}

func TestSemaphore_Set(t *testing.T) {
	sem := New(5)

	// Set to a specific value
	sem.Set(2)
	if !sem.TryAcquire(2) {
		t.Error("expected TryAcquire(2) to succeed after Set(2)")
	}
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail - count should be 0")
	}

	// Set above capacity is allowed (bypasses capacity)
	sem.Set(100)
	if !sem.TryAcquire(100) {
		t.Error("expected TryAcquire(100) to succeed after Set(100)")
	}
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail - count should be 0")
	}

	// Set to negative is allowed (more running than capacity)
	// Needs releases to bring count back up before acquires succeed
	sem.Set(-2)
	if sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to fail after Set(-2)")
	}
	// After 3 releases, count should be 1
	sem.Release(1)
	sem.Release(1)
	sem.Release(1)
	if !sem.TryAcquire(1) {
		t.Error("expected TryAcquire(1) to succeed after releases brought count positive")
	}
}

func TestSemaphore_Concurrent(t *testing.T) {
	sem := New(5)

	var wg sync.WaitGroup
	acquired := make(chan struct{}, 100)

	// Start 20 goroutines trying to acquire
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sem.TryAcquire(1) {
				acquired <- struct{}{}
				// Hold for a moment then release
				sem.Release(1)
			}
		}()
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
