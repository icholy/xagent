// Package outbox is a durable, crash-safe backing store for an at-least-once
// outbox. This layer provides only the Store interface, the Record type, and a
// filesystem implementation (FileStore); the outbox engine that drives delivery
// is a separate concern layered on top.
//
// The filesystem implementation ports the proven pattern from the runner's
// taskstate package: one atomic per-record JSON file, written via temp-file +
// fsync + rename so a crash never leaves a half-written record behind.
//
// It is a dependency-free leaf: stdlib only.
package outbox

import "encoding/json"

// Record is one persisted, undelivered message. Seq is a per-outbox monotonic
// sequence number that defines delivery (FIFO) order; it is an implementation
// detail of the store's on-disk ordering and logging — callers never address a
// record by it. Payload is the opaque, JSON-encoded T; the store never decodes
// it.
type Record struct {
	Seq     uint64          `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

// Store is the durable, crash-safe backing store for an outbox: a strict FIFO
// queue. It is single-consumer — the head is stable between a Peek and the Drop
// that follows it — but Append may run concurrently with the consumer.
// Implementations must be safe for concurrent use.
type Store interface {
	// Append durably appends payload to the tail. It must not return until the
	// record is durable.
	Append(payload json.RawMessage) error
	// Peek returns the head record without removing it. ok is false when the
	// queue is empty.
	Peek() (rec Record, ok bool, err error)
	// Drop durably removes the head record. If dead is true the record is moved
	// to the dead-letter area; otherwise it is discarded. Call it after the head
	// has been delivered (dead=false) or judged permanently undeliverable
	// (dead=true). No-op on an empty queue.
	Drop(dead bool) error
	// Len reports the number of live records.
	Len() (int, error)
}
