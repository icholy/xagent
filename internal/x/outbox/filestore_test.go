package outbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

func TestAppendList(t *testing.T) {
	// Arrange
	s, err := Open(t.TempDir())
	assert.NilError(t, err)

	// Act
	seq1, err := s.Append(json.RawMessage(`{"n":1}`))
	assert.NilError(t, err)
	seq2, err := s.Append(json.RawMessage(`{"n":2}`))
	assert.NilError(t, err)
	seq3, err := s.Append(json.RawMessage(`{"n":3}`))
	assert.NilError(t, err)

	// Assert: Seqs are strictly increasing and List returns them in order.
	assert.Equal(t, seq1, uint64(1))
	assert.Equal(t, seq2, uint64(2))
	assert.Equal(t, seq3, uint64(3))

	records, err := s.List()
	assert.NilError(t, err)
	assert.Equal(t, len(records), 3)
	assert.Equal(t, records[0].Seq, uint64(1))
	assert.Equal(t, string(records[0].Payload), `{"n":1}`)
	assert.Equal(t, records[1].Seq, uint64(2))
	assert.Equal(t, records[2].Seq, uint64(3))
}

func TestList_Empty(t *testing.T) {
	s, err := Open(t.TempDir())
	assert.NilError(t, err)

	records, err := s.List()
	assert.NilError(t, err)
	assert.Equal(t, len(records), 0)
}

func TestRemove(t *testing.T) {
	s, err := Open(t.TempDir())
	assert.NilError(t, err)
	seq, err := s.Append(json.RawMessage(`{"n":1}`))
	assert.NilError(t, err)

	assert.NilError(t, s.Remove(seq))

	records, err := s.List()
	assert.NilError(t, err)
	assert.Equal(t, len(records), 0)
}

func TestRemove_Idempotent(t *testing.T) {
	s, err := Open(t.TempDir())
	assert.NilError(t, err)
	seq, err := s.Append(json.RawMessage(`{"n":1}`))
	assert.NilError(t, err)

	// Removing the same record twice, and an absent record, are all no-ops.
	assert.NilError(t, s.Remove(seq))
	assert.NilError(t, s.Remove(seq))
	assert.NilError(t, s.Remove(999))
}

func TestDeadLetter(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	s, err := Open(dir)
	assert.NilError(t, err)
	seq1, err := s.Append(json.RawMessage(`{"n":1}`))
	assert.NilError(t, err)
	seq2, err := s.Append(json.RawMessage(`{"n":2}`))
	assert.NilError(t, err)

	// Act
	assert.NilError(t, s.DeadLetter(seq1))

	// Assert: the dead-lettered record leaves the live set...
	records, err := s.List()
	assert.NilError(t, err)
	assert.Equal(t, len(records), 1)
	assert.Equal(t, records[0].Seq, seq2)

	// ...and lands under <dir>/dead/<seq>.json.
	_, err = os.Stat(filepath.Join(dir, "dead", formatSeq(seq1)+".json"))
	assert.NilError(t, err)
}

func TestSeqMonotonicAcrossRestart(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	s, err := Open(dir)
	assert.NilError(t, err)
	seq1, err := s.Append(json.RawMessage(`{"n":1}`))
	assert.NilError(t, err)
	seq2, err := s.Append(json.RawMessage(`{"n":2}`))
	assert.NilError(t, err)

	// Act: re-Open the same directory (simulating a restart).
	s2, err := Open(dir)
	assert.NilError(t, err)
	seq3, err := s2.Append(json.RawMessage(`{"n":3}`))

	// Assert: the next Seq continues past the highest live record.
	assert.NilError(t, err)
	assert.Equal(t, seq3, seq2+1)
	assert.Assert(t, seq3 > seq1)
}

func TestSeqMonotonicAcrossDeadLetterAndRestart(t *testing.T) {
	// Arrange: append two records, dead-letter the highest, then remove the rest
	// so the live directory is empty but the dead directory holds Seq 2.
	dir := t.TempDir()
	s, err := Open(dir)
	assert.NilError(t, err)
	seq1, err := s.Append(json.RawMessage(`{"n":1}`))
	assert.NilError(t, err)
	seq2, err := s.Append(json.RawMessage(`{"n":2}`))
	assert.NilError(t, err)
	assert.NilError(t, s.DeadLetter(seq2))
	assert.NilError(t, s.Remove(seq1))

	// Act: re-Open with no live records remaining.
	s2, err := Open(dir)
	assert.NilError(t, err)
	seq3, err := s2.Append(json.RawMessage(`{"n":3}`))

	// Assert: seeding scans the dead dir too, so Seq never repeats.
	assert.NilError(t, err)
	assert.Equal(t, seq3, seq2+1)
}

func TestList_IgnoresGarbage(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	s, err := Open(dir)
	assert.NilError(t, err)
	_, err = s.Append(json.RawMessage(`{"n":1}`))
	assert.NilError(t, err)
	_, err = s.Append(json.RawMessage(`{"n":2}`))
	assert.NilError(t, err)

	// Drop stray files that must be ignored by List.
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, ".tmp-leftover"), []byte("garbage"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "not-a-number.json"), []byte("{}"), 0o644))

	// Act
	records, err := s.List()

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(records), 2)
	assert.Equal(t, records[0].Seq, uint64(1))
	assert.Equal(t, records[1].Seq, uint64(2))
}

func TestAppend_Concurrent(t *testing.T) {
	s, err := Open(t.TempDir())
	assert.NilError(t, err)

	const n = 50
	var wg sync.WaitGroup
	seqs := make([]uint64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seq, err := s.Append(json.RawMessage(`{}`))
			assert.NilError(t, err)
			seqs[i] = seq
		}(i)
	}
	wg.Wait()

	// Every Append returned a distinct Seq and List sees all n records.
	seen := make(map[uint64]bool, n)
	for _, seq := range seqs {
		assert.Assert(t, !seen[seq], "duplicate Seq %d", seq)
		seen[seq] = true
	}
	records, err := s.List()
	assert.NilError(t, err)
	assert.Equal(t, len(records), n)
}
