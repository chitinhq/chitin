package hookinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAdapter_EmptyPath(t *testing.T) {
	if err := ValidateAdapter(""); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestValidateAdapter_RelativePath(t *testing.T) {
	if err := ValidateAdapter("relative/path/to/binary"); err == nil {
		t.Error("expected error for relative path")
	}
}

func TestValidateAdapter_NonexistentPath(t *testing.T) {
	if err := ValidateAdapter("/nonexistent/path/to/binary"); err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestValidateAdapter_DirectoryPath(t *testing.T) {
	dir := t.TempDir()
	if err := ValidateAdapter(dir); err == nil {
		t.Error("expected error for directory path")
	}
}

func TestValidateAdapter_ExecutableFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "adapter")
	if err := os.WriteFile(f, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAdapter(f); err != nil {
		t.Errorf("expected no error for executable file, got: %v", err)
	}
}

func TestValidateAdapter_NonExecutableFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "adapter")
	if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAdapter(f); err == nil {
		t.Error("expected error for non-executable file")
	}
}

func TestValidateAdapterShell_Empty(t *testing.T) {
	if err := ValidateAdapterShell(""); err == nil {
		t.Error("expected error for empty shell command")
	}
}

func TestValidateAdapterShell_Valid(t *testing.T) {
	if err := ValidateAdapterShell("chitin-kernel gate evaluate --hook-stdin"); err != nil {
		t.Errorf("expected no error for valid shell command, got: %v", err)
	}
}