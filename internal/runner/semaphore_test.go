package runner

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSafeSemaphore_TryAcquire(t *testing.T) {
	sem := NewSafeSemaphore(3)

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

func TestSafeSemaphore_Release(t *testing.T) {
	sem := NewSafeSemaphore(2)

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

func TestSafeSemaphore_ReleaseAtCapacity(t *testing.T) {
	sem := NewSafeSemaphore(2)

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

func TestSafeSemaphore_Acquire(t *testing.T) {
	sem := NewSafeSemaphore(1)

	ctx := context.Background()
	if err := sem.Acquire(ctx, 1); err != nil {
		t.Fatalf("expected Acquire to succeed: %v", err)
	}

	// Acquire in goroutine, should block then succeed after release
	done := make(chan struct{})
	go func() {
		if err := sem.Acquire(ctx, 1); err != nil {
			t.Errorf("expected Acquire to succeed: %v", err)
		}
		close(done)
	}()

	// Give goroutine time to block
	time.Sleep(10 * time.Millisecond)

	select {
	case <-done:
		t.Error("goroutine should be blocked waiting for permit")
	default:
	}

	// Release permit
	sem.Release(1)

	// Goroutine should complete
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Error("goroutine should have completed after release")
	}
}

func TestSafeSemaphore_AcquireContextCancel(t *testing.T) {
	sem := NewSafeSemaphore(1)

	// Acquire the only permit
	if !sem.TryAcquire(1) {
		t.Fatal("expected TryAcquire to succeed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		done <- sem.Acquire(ctx, 1)
	}()

	// Give goroutine time to block
	time.Sleep(10 * time.Millisecond)

	// Cancel context
	cancel()

	// Should return context error
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Acquire should have returned after context cancel")
	}
}

func TestSafeSemaphore_Concurrent(t *testing.T) {
	sem := NewSafeSemaphore(5)
	ctx := context.Background()

	var wg sync.WaitGroup
	counter := 0
	var mu sync.Mutex

	// Start 10 goroutines, each trying to acquire and release
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sem.Acquire(ctx, 1); err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			mu.Lock()
			counter++
			current := counter
			mu.Unlock()

			if current > 5 {
				t.Errorf("concurrent count %d exceeds capacity 5", current)
			}

			time.Sleep(5 * time.Millisecond)

			mu.Lock()
			counter--
			mu.Unlock()
			sem.Release(1)
		}()
	}

	wg.Wait()
}
