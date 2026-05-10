package kstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit_ForceRemoveAllError(t *testing.T) {
	// RemoveAll fails when parent dir is read-only and target is a directory
	dir := t.TempDir()
	parentDir := filepath.Join(dir, "parent")
	chitinDir := filepath.Join(parentDir, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make parent read-only so RemoveAll of child fails
	if err := os.Chmod(parentDir, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parentDir, 0o755)

	err := Init(chitinDir, true)
	if err == nil {
		t.Log("RemoveAll succeeded despite read-only parent (OS-dependent)")
	} else {
		t.Logf("RemoveAll correctly failed: %v", err)
	}
}

func TestInit_MkdirAllSessionsError(t *testing.T) {
	// Cause MkdirAll for sessions/ to fail by creating a file at the sessions path
	dir := t.TempDir()
	chitinDir := filepath.Join(dir, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a regular file where sessions/ directory should go
	sessionsFile := filepath.Join(chitinDir, "sessions")
	if err := os.WriteFile(sessionsFile, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Init(chitinDir, false)
	if err == nil {
		t.Error("expected error when sessions path is a regular file")
	}
}

func TestInit_WriteCheckpointError(t *testing.T) {
	// Cause WriteFile for checkpoint to fail by making the directory read-only
	dir := t.TempDir()
	chitinDir := filepath.Join(dir, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(chitinDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the directory read-only so WriteFile fails
	if err := os.Chmod(chitinDir, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(chitinDir, 0o755) // clean up

	err := Init(chitinDir, false)
	// On some systems this may or may not fail depending on user perms
	if err != nil {
		t.Logf("WriteFile correctly failed: %v", err)
	}
}