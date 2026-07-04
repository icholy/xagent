package shellserver

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"gotest.tools/v3/assert"
)

// collectActiveSessions collects the active-shell-sessions gauge from reader and
// returns its single data point's value, failing the test if the metric is absent.
func collectActiveSessions(t *testing.T, reader sdkmetric.Reader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	assert.NilError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "xagent.shell.active_sessions" {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[int64])
			assert.Assert(t, ok, "expected int64 gauge, got %T", m.Data)
			assert.Equal(t, len(g.DataPoints), 1)
			return g.DataPoints[0].Value
		}
	}
	t.Fatal("xagent.shell.active_sessions metric not found")
	return 0
}

// TestActiveSessionsGaugeTracksSeedAndEviction wires the observable gauge to a
// manual reader and asserts the reported value follows the live registry: it
// rises as sessions are seeded and falls back to zero once they self-evict on
// their establishment timeout.
func TestActiveSessionsGaugeTracksSeedAndEviction(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	// A short establishment timeout self-evicts sessions with no legs attached,
	// exercising the eviction path without a live rendezvous.
	r := New(Options{EstablishTimeout: 50 * time.Millisecond})
	r.registerMetrics(mp.Meter(meterName))

	assert.Equal(t, collectActiveSessions(t, reader), int64(0))

	assert.NilError(t, r.Seed("s1", 1, 7))
	assert.NilError(t, r.Seed("s2", 1, 7))
	assert.Equal(t, collectActiveSessions(t, reader), int64(2))

	// After the establishment timeout fires, both sessions tear down and the
	// eviction goroutine removes them, so the gauge returns to zero.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if collectActiveSessions(t, reader) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	assert.Equal(t, collectActiveSessions(t, reader), int64(0))
}
