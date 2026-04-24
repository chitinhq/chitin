package copilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewClient_ResolvesBinaryViaLookPath(t *testing.T) {
	// Set up a fake 'copilot' binary on a temp PATH.
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "copilot")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+origPath)

	c, err := NewClient(ClientOpts{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() {
		if c != nil {
			_ = c.Close()
		}
	}()

	if c.BinaryPath != fakeBin {
		t.Errorf("BinaryPath: got %q, want %q", c.BinaryPath, fakeBin)
	}
}

func TestNewClient_FailsFastOnMissingBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // PATH with no copilot binary

	_, err := NewClient(ClientOpts{})
	if err == nil {
		t.Fatal("expected error when copilot binary missing")
	}
	if !strings.Contains(err.Error(), "copilot") {
		t.Errorf("error should mention copilot binary: %v", err)
	}
}

func TestNewClient_UsesExplicitCLIPath(t *testing.T) {
	// Explicit CLIPath should skip LookPath entirely.
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "my-copilot")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake: %v", err)
	}

	c, err := NewClient(ClientOpts{CLIPath: fakeBin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() {
		if c != nil {
			_ = c.Close()
		}
	}()

	if c.BinaryPath != fakeBin {
		t.Errorf("BinaryPath: got %q, want %q", c.BinaryPath, fakeBin)
	}
}
