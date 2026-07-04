package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/icholy/xagent/internal/x/atomicio"
)

// seqDigits is the zero-padded width of a uint64 Seq in a filename, so lexical
// filename order equals numeric Seq order. math.MaxUint64 is 20 digits.
const seqDigits = 20

// FileStore is a crash-safe, concurrency-safe Store backed by one atomic JSON
// file per record. Live records live at <dir>/<seq>.json and dead-lettered ones
// at <dir>/dead/<seq>.json, where <seq> is a 20-digit zero-padded uint64.
//
// The set of live records is loaded into an in-memory index at Open and kept in
// sync by every mutation, so List and Append never read the live directory
// again — disk is read only at startup. Writes stay per-record atomic
// (temp-file + fsync + rename) for durability.
type FileStore struct {
	mu      sync.Mutex
	dir     string
	deadDir string
	next    uint64            // next Seq to assign; guarded by mu
	records map[uint64]Record // live records, keyed by Seq; guarded by mu
}

// Open returns a FileStore backed by dir, creating dir and its dead-letter
// subdirectory if they do not exist. It reads every live record into memory;
// a live record file that cannot be read or decoded fails Open, so corrupt
// durable state surfaces loudly rather than silently dropping a message. The
// Seq counter is seeded from the maximum filename across both the live and dead
// directories, so sequence numbers never repeat even after dead-lettering or a
// restart.
func Open(dir string) (*FileStore, error) {
	deadDir := filepath.Join(dir, "dead")
	if err := os.MkdirAll(deadDir, 0o755); err != nil {
		return nil, fmt.Errorf("outbox: create dir: %w", err)
	}
	s := &FileStore{
		dir:     dir,
		deadDir: deadDir,
		records: make(map[uint64]Record),
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("outbox: read dir: %w", err)
	}
	var liveMax uint64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seq, ok := parseRecordName(entry.Name())
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("outbox: read record %d: %w", seq, err)
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("outbox: unmarshal record %d: %w", seq, err)
		}
		s.records[seq] = rec
		liveMax = max(liveMax, seq)
	}

	deadMax, err := maxSeq(deadDir)
	if err != nil {
		return nil, err
	}
	s.next = max(liveMax, deadMax) + 1
	return s, nil
}

// livePath returns the on-disk file path for a live record's Seq.
func (s *FileStore) livePath(seq uint64) string {
	return filepath.Join(s.dir, formatSeq(seq)+".json")
}

// deadPath returns the on-disk file path for a dead-lettered record's Seq.
func (s *FileStore) deadPath(seq uint64) string {
	return filepath.Join(s.deadDir, formatSeq(seq)+".json")
}

// Append durably persists payload under a new, strictly increasing Seq and
// returns the assigned Seq. The write is crash-safe: the record is marshalled to
// a temp file in the same directory, fsync'd, and renamed over the target, so a
// concurrent List never observes a half-written file. The record is then added
// to the in-memory index.
func (s *FileStore) Append(payload json.RawMessage) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seq := s.next
	rec := Record{Seq: seq, Payload: payload}
	data, err := json.Marshal(rec)
	if err != nil {
		return 0, fmt.Errorf("outbox: marshal record: %w", err)
	}
	if err := atomicio.WriteFile(s.livePath(seq), data); err != nil {
		return 0, err
	}
	s.records[seq] = rec
	s.next = seq + 1
	return seq, nil
}

// List returns a snapshot copy of all undelivered records in ascending Seq
// order. It reads only the in-memory index, so the caller may Remove or
// DeadLetter records while ranging the result — it holds its own copy. The
// returned error is always nil; it exists to satisfy the Store interface, whose
// other implementations may fail.
func (s *FileStore) List() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, rec)
	}
	slices.SortFunc(records, func(a, b Record) int {
		switch {
		case a.Seq < b.Seq:
			return -1
		case a.Seq > b.Seq:
			return 1
		default:
			return 0
		}
	})
	return records, nil
}

// Remove deletes the record with the given Seq. It is idempotent: removing an
// absent record is not an error.
func (s *FileStore) Remove(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.livePath(seq)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("outbox: remove record %d: %w", seq, err)
	}
	delete(s.records, seq)
	return nil
}

// DeadLetter atomically moves the record with the given Seq out of the live set
// into the dead-letter area and drops it from the in-memory index.
func (s *FileStore) DeadLetter(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Rename(s.livePath(seq), s.deadPath(seq)); err != nil {
		return fmt.Errorf("outbox: dead-letter record %d: %w", seq, err)
	}
	delete(s.records, seq)
	return nil
}

// maxSeq returns the largest Seq among the <seq>.json record files in dir, or 0
// if there are none. Non-record files are ignored.
func maxSeq(dir string) (uint64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("outbox: read dir: %w", err)
	}
	var highest uint64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seq, ok := parseRecordName(entry.Name())
		if !ok {
			continue
		}
		highest = max(highest, seq)
	}
	return highest, nil
}

// formatSeq renders seq as a 20-digit zero-padded string so lexical order equals
// numeric order.
func formatSeq(seq uint64) string {
	return fmt.Sprintf("%0*d", seqDigits, seq)
}

// parseRecordName reports whether name is a valid <uint64>.json record file and
// returns the Seq if so.
func parseRecordName(name string) (uint64, bool) {
	base, ok := strings.CutSuffix(name, ".json")
	if !ok {
		return 0, false
	}
	seq, err := strconv.ParseUint(base, 10, 64)
	if err != nil {
		return 0, false
	}
	return seq, true
}
