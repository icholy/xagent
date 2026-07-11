// Package atomicio provides crash-safe atomic file writes: data is written to a
// temp file in the target's directory, fsync'd, and renamed over the target, so
// a concurrent reader never observes a half-written file and an interrupted
// write leaves either the old contents or nothing partial behind.
//
// It is a dependency-free leaf: stdlib only.
package atomicio

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile atomically writes data to path with the given file permissions. The
// data is written to a temp file in path's directory (so the final rename stays
// on one filesystem), chmod'd to perm, fsync'd, and renamed over path. On any
// error before the rename succeeds, the temp file is cleaned up and path is left
// untouched. perm mirrors os.WriteFile's mode argument; because os.CreateTemp
// always creates the temp file 0600, WriteFile chmods it so perm carries onto
// the target.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("atomicio: create temp file: %w", err)
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
		return fmt.Errorf("atomicio: write temp file: %w", err)
	}
	// os.CreateTemp makes the file 0600; set the caller's perm before the
	// rename so it carries onto the target.
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomicio: chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomicio: fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomicio: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomicio: rename temp file: %w", err)
	}
	committed = true
	return nil
}
