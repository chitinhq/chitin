package kstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit_CreatesStateDir(t *testing.T) {
	dir := t.TempDir()
	chitinDir := filepath.Join(dir, ".chitin")
	if err := Init(chitinDir, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(chitinDir)
	if err != nil || !info.IsDir() {
		t.Fatalf(".chitin dir missing: %v", err)
	}
	for _, sub := range []string{"sessions"} {
		if _, err := os.Stat(filepath.Join(chitinDir, sub)); err != nil {
			t.Errorf("missing subdir %s: %v", sub, err)
		}
	}
	cp := filepath.Join(chitinDir, "transcript_checkpoint.json")
	b, err := os.ReadFile(cp)
	if err != nil {
		t.Fatalf("missing checkpoint file: %v", err)
	}
	if string(b) != "{}" {
		t.Errorf("expected empty-object checkpoint, got %q", b)
	}
}

func TestInit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	chitinDir := filepath.Join(dir, ".chitin")
	if err := Init(chitinDir, false); err != nil {
		t.Fatal(err)
	}
	if err := Init(chitinDir, false); err != nil {
		t.Errorf("second Init should be a no-op, got error: %v", err)
	}
}

func TestInit_ForceWipes(t *testing.T) {
	dir := t.TempDir()
	chitinDir := filepath.Join(dir, ".chitin")
	if err := Init(chitinDir, false); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(chitinDir, "events-xyz.jsonl")
	if err := os.WriteFile(marker, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Init(chitinDir, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("expected marker wiped by --force, got err=%v", err)
	}
}
