// Package outbox is a durable, crash-safe backing store for an at-least-once
// outbox. This layer provides only the Store interface, the Record type, and a
// filesystem implementation (FileStore); the outbox engine that drives delivery
// is a separate concern layered on top.
//
// The filesystem implementation ports the proven pattern from the runner's
// taskstate package: one atomic per-record JSON file, written via temp-file +
// fsync + rename so a concurrent List never observes a half-written file.
//
// It is a dependency-free leaf: stdlib only.
package outbox

import "encoding/json"

// Record is one persisted, undelivered message. Seq is a per-outbox monotonic
// sequence number that defines delivery (FIFO) order. Payload is the opaque,
// JSON-encoded T; the store never decodes it.
type Record struct {
	Seq     uint64          `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

// Store is the durable, crash-safe backing store for an outbox. Implementations
// must be safe for concurrent use.
type Store interface {
	// Append durably persists payload under a new, strictly increasing Seq and
	// returns the assigned Seq. It must not return until the record is durable.
	Append(payload json.RawMessage) (uint64, error)
	// List returns a snapshot copy of all undelivered records in ascending Seq
	// order. The caller may Remove or DeadLetter records while ranging the result
	// because it holds its own copy.
	List() ([]Record, error)
	// Remove deletes the record with the given Seq. It is idempotent.
	Remove(seq uint64) error
	// DeadLetter atomically moves the record with the given Seq out of the live
	// set into the dead-letter area.
	DeadLetter(seq uint64) error
}
