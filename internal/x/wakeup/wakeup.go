package wakeup

// Chan is an edge-triggered, coalescing wake-up signal. Multiple Wake
// calls between receives collapse into a single notification. Receivers
// select on the channel directly (`case <-c:`).
type Chan chan struct{}

// New returns a ready-to-use wakeup Chan.
func New() Chan { return make(Chan, 1) }

// Wake signals without blocking. If a signal is already pending, the call
// is a no-op (signals coalesce).
func (c Chan) Wake() {
	select {
	case c <- struct{}{}:
	default:
	}
}
