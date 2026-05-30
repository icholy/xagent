package wakeup

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestChan_NoSignalPending(t *testing.T) {
	c := New()
	select {
	case <-c:
		t.Fatal("expected no signal pending on fresh Chan")
	default:
	}
}

func TestChan_WakeDeliversOne(t *testing.T) {
	c := New()
	c.Wake()

	select {
	case <-c:
	default:
		t.Fatal("expected one signal after Wake")
	}

	select {
	case <-c:
		t.Fatal("expected no further signal after first receive")
	default:
	}
}

func TestChan_WakeCoalesces(t *testing.T) {
	c := New()
	for range 5 {
		c.Wake()
	}

	got := 0
	for {
		select {
		case <-c:
			got++
			continue
		default:
		}
		break
	}
	assert.Equal(t, got, 1, "expected coalesced bursts to deliver exactly one signal")
}
