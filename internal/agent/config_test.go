package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConfigAtomic(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	origConfigDir := ConfigDir
	ConfigDir = tmpDir
	defer func() { ConfigDir = origConfigDir }()

	taskID := "test-task"
	cfg := &Config{
		Cwd:     "/test/dir",
		Prompt:  "test prompt",
		Setup:   true,
		Started: false,
	}

	// Save the config
	if err := SaveConfig(taskID, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify the config file exists
	path := ConfigPath(taskID)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Config file was not created: %s", path)
	}

	// Verify the temp file was cleaned up
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("Temporary file was not cleaned up: %s", tmpPath)
	}

	// Verify we can load the config back
	loaded, err := LoadConfig(taskID)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Cwd != cfg.Cwd {
		t.Errorf("Expected Cwd=%s, got %s", cfg.Cwd, loaded.Cwd)
	}
	if loaded.Setup != cfg.Setup {
		t.Errorf("Expected Setup=%v, got %v", cfg.Setup, loaded.Setup)
	}
}

func TestSaveConfigPreservesExistingOnFailure(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	origConfigDir := ConfigDir
	ConfigDir = tmpDir
	defer func() { ConfigDir = origConfigDir }()

	taskID := "test-task"

	// Save initial config
	initialCfg := &Config{
		Cwd:     "/initial",
		Setup:   true,
		Started: true,
	}
	if err := SaveConfig(taskID, initialCfg); err != nil {
		t.Fatalf("Initial SaveConfig failed: %v", err)
	}

	// Make the target directory read-only to simulate a write failure scenario
	// (this tests that the original file remains untouched on failure)
	path := ConfigPath(taskID)
	dir := filepath.Dir(path)

	// Save original permissions
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Failed to stat directory: %v", err)
	}
	origPerm := info.Mode()
	defer os.Chmod(dir, origPerm) // Restore permissions after test

	// Make directory read-only
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Failed to chmod directory: %v", err)
	}

	// Try to save a new config - this should fail
	newCfg := &Config{
		Cwd:     "/new",
		Setup:   false,
		Started: false,
	}
	err = SaveConfig(taskID, newCfg)
	if err == nil {
		t.Fatalf("Expected SaveConfig to fail with read-only directory")
	}

	// Restore permissions so we can read
	if err := os.Chmod(dir, origPerm); err != nil {
		t.Fatalf("Failed to restore directory permissions: %v", err)
	}

	// Verify the original config is still intact
	loaded, err := LoadConfig(taskID)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Cwd != initialCfg.Cwd {
		t.Errorf("Original config was corrupted: Expected Cwd=%s, got %s", initialCfg.Cwd, loaded.Cwd)
	}
	if loaded.Setup != initialCfg.Setup {
		t.Errorf("Original config was corrupted: Expected Setup=%v, got %v", initialCfg.Setup, loaded.Setup)
	}
}
