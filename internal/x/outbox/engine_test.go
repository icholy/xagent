package outbox

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
	"gotest.tools/v3/assert"
)

type msg struct {
	N int
}

// recorder collects the payloads passed to Deliver in call order.
type recorder struct {
	mu  sync.Mutex
	got []int
}

func (r *recorder) add(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, n)
}

func (r *recorder) snapshot() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.got...)
}

// waitFor polls cond until it returns true or the deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestOutbox_FIFO(t *testing.T) {
	// Arrange
	store, err := Open(t.TempDir())
	assert.NilError(t, err)
	var rec recorder
	ob := New(Options[msg]{
		Store:   store,
		Backoff: backoff.NewConstantBackOff(0),
		Deliver: func(ctx context.Context, m msg) (bool, error) {
			rec.add(m.N)
			return false, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ob.Run(ctx)

	// Act
	assert.NilError(t, ob.Enqueue(msg{N: 1}))
	assert.NilError(t, ob.Enqueue(msg{N: 2}))
	assert.NilError(t, ob.Enqueue(msg{N: 3}))

	// Assert: delivered in enqueue order, then the store fully drains.
	waitFor(t, func() bool { return len(rec.snapshot()) == 3 })
	assert.DeepEqual(t, rec.snapshot(), []int{1, 2, 3})
	waitFor(t, func() bool { n, _ := ob.Len(); return n == 0 })
}

func TestOutbox_TransientRetry(t *testing.T) {
	// Arrange: the head fails transiently three times, then succeeds. Later
	// messages must stay blocked behind it (head-of-line blocking).
	store, err := Open(t.TempDir())
	assert.NilError(t, err)
	var rec recorder
	var attempts int
	var mu sync.Mutex
	ob := New(Options[msg]{
		Store:   store,
		Backoff: backoff.NewConstantBackOff(time.Millisecond),
		Deliver: func(ctx context.Context, m msg) (bool, error) {
			rec.add(m.N)
			if m.N == 1 {
				mu.Lock()
				attempts++
				n := attempts
				mu.Unlock()
				if n < 4 {
					return false, assertError("transient")
				}
			}
			return false, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ob.Run(ctx)

	// Act
	assert.NilError(t, ob.Enqueue(msg{N: 1}))
	assert.NilError(t, ob.Enqueue(msg{N: 2}))

	// Assert: head retried until success, everything delivered in order, and 2
	// was never attempted before 1 succeeded.
	waitFor(t, func() bool { n, _ := ob.Len(); return n == 0 })
	assert.DeepEqual(t, rec.snapshot(), []int{1, 1, 1, 1, 2})
}

func TestOutbox_PermanentDeadLetter(t *testing.T) {
	// Arrange: the middle message fails permanently and must be dead-lettered
	// so the queue advances to the next message.
	dir := t.TempDir()
	store, err := Open(dir)
	assert.NilError(t, err)
	var rec recorder
	ob := New(Options[msg]{
		Store:   store,
		Backoff: backoff.NewConstantBackOff(0),
		Deliver: func(ctx context.Context, m msg) (bool, error) {
			rec.add(m.N)
			if m.N == 2 {
				return true, assertError("permanent")
			}
			return false, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ob.Run(ctx)

	// Act
	assert.NilError(t, ob.Enqueue(msg{N: 1}))
	assert.NilError(t, ob.Enqueue(msg{N: 2}))
	assert.NilError(t, ob.Enqueue(msg{N: 3}))

	// Assert: 2 is attempted once, then dead-lettered; 1 and 3 delivered; the
	// live queue drains and the dead-letter file survives.
	waitFor(t, func() bool { n, _ := ob.Len(); return n == 0 })
	assert.DeepEqual(t, rec.snapshot(), []int{1, 2, 3})

	dead, err := os.ReadDir(filepath.Join(dir, "dead"))
	assert.NilError(t, err)
	assert.Equal(t, len(dead), 1)
}

func TestOutbox_Len(t *testing.T) {
	// Arrange: no Run, so nothing is delivered.
	store, err := Open(t.TempDir())
	assert.NilError(t, err)
	ob := New(Options[msg]{
		Store:   store,
		Deliver: func(ctx context.Context, m msg) (bool, error) { return false, nil },
	})

	// Act
	assert.NilError(t, ob.Enqueue(msg{N: 1}))
	assert.NilError(t, ob.Enqueue(msg{N: 2}))

	// Assert
	n, err := ob.Len()
	assert.NilError(t, err)
	assert.Equal(t, n, 2)
}

func TestOutbox_StartupRecovery(t *testing.T) {
	// Arrange: persist records with one outbox instance, then simulate a
	// restart by re-opening the same directory into a fresh outbox.
	dir := t.TempDir()
	seed, err := Open(dir)
	assert.NilError(t, err)
	assert.NilError(t, seed.Append([]byte(`{"N":1}`)))
	assert.NilError(t, seed.Append([]byte(`{"N":2}`)))
	assert.NilError(t, seed.Append([]byte(`{"N":3}`)))

	store, err := Open(dir)
	assert.NilError(t, err)
	var rec recorder
	ob := New(Options[msg]{
		Store:   store,
		Backoff: backoff.NewConstantBackOff(0),
		Deliver: func(ctx context.Context, m msg) (bool, error) {
			rec.add(m.N)
			return false, nil
		},
	})

	// Act: the first pass of Run redelivers everything already persisted.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ob.Run(ctx)

	// Assert
	waitFor(t, func() bool { return len(rec.snapshot()) == 3 })
	assert.DeepEqual(t, rec.snapshot(), []int{1, 2, 3})
	waitFor(t, func() bool { n, _ := ob.Len(); return n == 0 })
}

// assertError is a trivial error for scripted Deliver failures.
type assertError string

func (e assertError) Error() string { return string(e) }
