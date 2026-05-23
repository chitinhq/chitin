package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRepoRoot_FromFlag(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveRepoRoot(dir)
	if err != nil {
		t.Fatalf("resolveRepoRoot: %v", err)
	}
	// Compare via filepath.EvalSymlinks because t.TempDir() may itself live
	// under a symlinked tmpdir (e.g., /tmp/... → /private/tmp/... on macOS,
	// though we're on Linux here it doesn't hurt to be strict).
	wantEval, _ := filepath.EvalSymlinks(dir)
	gotEval, _ := filepath.EvalSymlinks(got)
	if wantEval != gotEval {
		t.Errorf("got %q, want %q", gotEval, wantEval)
	}
}

func TestResolveRepoRoot_FromEnvWhenFlagEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHITIN_REPO_ROOT", dir)
	got, err := resolveRepoRoot("")
	if err != nil {
		t.Fatalf("resolveRepoRoot: %v", err)
	}
	wantEval, _ := filepath.EvalSymlinks(dir)
	gotEval, _ := filepath.EvalSymlinks(got)
	if wantEval != gotEval {
		t.Errorf("got %q, want %q (env)", gotEval, wantEval)
	}
}

func TestResolveRepoRoot_FlagBeatsEnv(t *testing.T) {
	envDir := t.TempDir()
	flagDir := t.TempDir()
	t.Setenv("CHITIN_REPO_ROOT", envDir)
	got, err := resolveRepoRoot(flagDir)
	if err != nil {
		t.Fatalf("resolveRepoRoot: %v", err)
	}
	flagEval, _ := filepath.EvalSymlinks(flagDir)
	gotEval, _ := filepath.EvalSymlinks(got)
	if flagEval != gotEval {
		t.Errorf("flag should win — got %q, want %q", gotEval, flagEval)
	}
}

func TestResolveRepoRoot_NonExistentPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := resolveRepoRoot(dir)
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestResolveRepoRoot_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(notDir, []byte("hi"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := resolveRepoRoot(notDir)
	if err == nil {
		t.Fatal("expected error for non-directory path")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}
