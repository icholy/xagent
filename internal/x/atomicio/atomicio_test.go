package atomicio

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestWriteFile(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	// Act
	assert.NilError(t, WriteFile(path, []byte(`{"n":1}`), 0o600))

	// Assert: the file holds the data and no temp file is left behind.
	data, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, string(data), `{"n":1}`)

	entries, err := os.ReadDir(dir)
	assert.NilError(t, err)
	assert.Equal(t, len(entries), 1)
	assert.Equal(t, entries[0].Name(), "record.json")
}

func TestWriteFile_Overwrite(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	assert.NilError(t, WriteFile(path, []byte("old"), 0o600))

	// Act
	assert.NilError(t, WriteFile(path, []byte("new"), 0o600))

	// Assert: the target is replaced and no temp file lingers.
	data, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, string(data), "new")

	entries, err := os.ReadDir(dir)
	assert.NilError(t, err)
	assert.Equal(t, len(entries), 1)
}

func TestWriteFile_Perm(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	// Act - perm is applied to the target despite os.CreateTemp's 0600 default.
	assert.NilError(t, WriteFile(path, []byte("x"), 0o666))

	// Assert
	info, err := os.Stat(path)
	assert.NilError(t, err)
	assert.Equal(t, info.Mode().Perm(), os.FileMode(0o666))
}

func TestWriteFile_BadDir(t *testing.T) {
	// Writing into a directory that does not exist fails at temp-file creation
	// and leaves nothing behind.
	err := WriteFile(filepath.Join(t.TempDir(), "missing", "record.json"), []byte("x"), 0o600)
	assert.ErrorContains(t, err, "create temp file")
}
