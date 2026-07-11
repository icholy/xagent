package xagentclient

import (
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// SubmittedRunnerEvents returns every runner event submitted across all
// SubmitRunnerEvents calls, flattened in submission order.
func (mock *ClientMock) SubmittedRunnerEvents() []*xagentv1.RunnerEvent {
	var events []*xagentv1.RunnerEvent
	for _, call := range mock.SubmitRunnerEventsCalls() {
		events = append(events, call.SubmitRunnerEventsRequest.GetEvents()...)
	}
	return events
}
