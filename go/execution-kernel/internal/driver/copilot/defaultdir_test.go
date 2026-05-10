package copilot

import (
	"testing"
)

func TestDefaultChitinDir(t *testing.T) {
	// Normal case: HOME is set
	dir, err := defaultChitinDir()
	if err != nil {
		t.Fatalf("defaultChitinDir() error: %v", err)
	}
	if dir == "" {
		t.Error("defaultChitinDir() returned empty string")
	}
	// Should end with .chitin
	if dir[len(dir)-7:] != ".chitin" {
		t.Errorf("defaultChitinDir() = %q, want path ending in .chitin", dir)
	}
}

func TestDefaultChitinDir_EmptyHome(t *testing.T) {
	// When HOME results in an empty string from UserHomeDir
	t.Setenv("HOME", "")
	_, err := defaultChitinDir()
	if err == nil {
		t.Error("expected error when HOME is empty")
	}
}