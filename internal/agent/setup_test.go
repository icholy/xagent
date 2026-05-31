package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	ConfigDir = t.TempDir()
	return &Driver{
		TaskID: 1,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestSetup_MidListFailureLeavesResumeIndex(t *testing.T) {
	// Arrange
	d := newTestDriver(t)
	cfg := &Config{
		Commands: []string{"true", "true", "false", "true"},
	}

	// Act
	err := d.setup(context.Background(), cfg)

	// Assert
	assert.ErrorContains(t, err, "setup command 2 failed")
	assert.Equal(t, cfg.SetupCommandsCompleted, 2)

	loaded, err := LoadConfig(d.TaskID)
	assert.NilError(t, err)
	assert.Equal(t, loaded.SetupCommandsCompleted, 2)
}

func TestSetup_ResumesFromSavedIndex(t *testing.T) {
	// Arrange
	d := newTestDriver(t)
	marker3 := filepath.Join(t.TempDir(), "marker3")
	marker4 := filepath.Join(t.TempDir(), "marker4")
	cfg := &Config{
		Commands: []string{
			"exit 1", // would fail if re-run
			"exit 1", // would fail if re-run
			"touch " + marker3,
			"touch " + marker4,
		},
		SetupCommandsCompleted: 2,
	}

	// Act
	err := d.setup(context.Background(), cfg)

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, cfg.SetupCommandsCompleted, 4)
	assertFileExists(t, marker3)
	assertFileExists(t, marker4)
}

func TestSetup_AlreadyCompleteSkipsLoop(t *testing.T) {
	// Arrange
	d := newTestDriver(t)
	cfg := &Config{
		Commands:               []string{"exit 1"}, // would fail if executed
		SetupCommandsCompleted: 1,
	}

	// Act
	err := d.setup(context.Background(), cfg)

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, cfg.SetupCommandsCompleted, 1)
}

func TestSetup_NoCountRunsFromZero(t *testing.T) {
	// Arrange
	d := newTestDriver(t)
	marker0 := filepath.Join(t.TempDir(), "marker0")
	marker1 := filepath.Join(t.TempDir(), "marker1")
	cfg := &Config{
		Commands: []string{
			"touch " + marker0,
			"touch " + marker1,
		},
	}

	// Act
	err := d.setup(context.Background(), cfg)

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, cfg.SetupCommandsCompleted, 2)
	assertFileExists(t, marker0)
	assertFileExists(t, marker1)
}

func TestSetup_LastCommandFailureLeavesIncompleteCount(t *testing.T) {
	// Arrange
	d := newTestDriver(t)
	cfg := &Config{
		Commands: []string{"true", "false"},
	}

	// Act
	err := d.setup(context.Background(), cfg)

	// Assert
	assert.ErrorContains(t, err, "setup command 1 failed")
	assert.Equal(t, cfg.SetupCommandsCompleted, 1)
}

func TestSetup_OutOfRangeCountClampsToZero(t *testing.T) {
	// Arrange
	d := newTestDriver(t)
	marker := filepath.Join(t.TempDir(), "marker")
	cfg := &Config{
		Commands:               []string{"touch " + marker},
		SetupCommandsCompleted: 99,
	}

	// Act
	err := d.setup(context.Background(), cfg)

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, cfg.SetupCommandsCompleted, 1)
	assertFileExists(t, marker)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NilError(t, err, "expected file %s to exist", path)
}
