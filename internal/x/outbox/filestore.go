package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// seqDigits is the zero-padded width of a uint64 Seq in a filename, so lexical
// filename order equals numeric Seq order. math.MaxUint64 is 20 digits.
const seqDigits = 20

// FileStore is a crash-safe, concurrency-safe Store backed by one atomic JSON
// file per record. Live records live at <dir>/<seq>.json and dead-lettered ones
// at <dir>/dead/<seq>.json, where <seq> is a 20-digit zero-padded uint64.
type FileStore struct {
	mu      sync.Mutex
	dir     string
	deadDir string
	next    uint64 // next Seq to assign; guarded by mu
}

// Open returns a FileStore backed by dir, creating dir and its dead-letter
// subdirectory if they do not exist. The Seq counter is seeded from the maximum
// existing filename across both the live and dead directories, so sequence
// numbers never repeat even after dead-lettering or a restart.
func Open(dir string) (*FileStore, error) {
	deadDir := filepath.Join(dir, "dead")
	if err := os.MkdirAll(deadDir, 0o755); err != nil {
		return nil, fmt.Errorf("outbox: create dir: %w", err)
	}
	s := &FileStore{dir: dir, deadDir: deadDir}

	max, err := maxSeq(dir)
	if err != nil {
		return nil, err
	}
	maxDead, err := maxSeq(deadDir)
	if err != nil {
		return nil, err
	}
	if maxDead > max {
		max = maxDead
	}
	s.next = max + 1
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
// concurrent List never observes a half-written file.
func (s *FileStore) Append(payload json.RawMessage) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seq := s.next
	rec := Record{Seq: seq, Payload: payload}
	data, err := json.Marshal(rec)
	if err != nil {
		return 0, fmt.Errorf("outbox: marshal record: %w", err)
	}
	if err := writeFileAtomic(s.dir, s.livePath(seq), data); err != nil {
		return 0, err
	}
	s.next = seq + 1
	return seq, nil
}

// List returns an iterator over all undelivered records in ascending Seq order.
// Only real <seq>.json records are considered; leftover temp files and any other
// files that don't parse as <uint64>.json are ignored, so an interrupted write
// can never corrupt a listing.
//
// The snapshot is fully materialized under the lock and the lock is released
// before the caller iterates, so Remove or DeadLetter may be called on records
// during iteration — including the record currently being yielded — without
// deadlocking or affecting the walk.
func (s *FileStore) List() (iter.Seq[Record], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("outbox: read dir: %w", err)
	}

	var records []Record
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := parseRecordName(entry.Name()); !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("outbox: read record %s: %w", entry.Name(), err)
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("outbox: unmarshal record %s: %w", entry.Name(), err)
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Seq < records[j].Seq
	})
	return slices.Values(records), nil
}

// Remove deletes the record with the given Seq. It is idempotent: removing an
// absent record is not an error.
func (s *FileStore) Remove(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.livePath(seq)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("outbox: remove record %d: %w", seq, err)
	}
	return nil
}

// DeadLetter atomically moves the record with the given Seq out of the live set
// into the dead-letter area.
func (s *FileStore) DeadLetter(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Rename(s.livePath(seq), s.deadPath(seq)); err != nil {
		return fmt.Errorf("outbox: dead-letter record %d: %w", seq, err)
	}
	return nil
}

// writeFileAtomic crash-safely writes data to path by marshalling to a temp file
// in tmpDir (which must be on the same filesystem as path), fsync'ing it, and
// renaming it over the target.
func writeFileAtomic(tmpDir, path string, data []byte) error {
	tmp, err := os.CreateTemp(tmpDir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("outbox: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any error before the rename succeeds.
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("outbox: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("outbox: fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("outbox: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("outbox: rename temp file: %w", err)
	}
	committed = true
	return nil
}

// maxSeq returns the largest Seq among the <seq>.json record files in dir, or 0
// if there are none. Non-record files are ignored.
func maxSeq(dir string) (uint64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("outbox: read dir: %w", err)
	}
	var max uint64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seq, ok := parseRecordName(entry.Name())
		if !ok {
			continue
		}
		if seq > max {
			max = seq
		}
	}
	return max, nil
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
