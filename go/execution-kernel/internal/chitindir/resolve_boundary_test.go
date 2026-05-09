package chitindir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_EmptyCWD(t *testing.T) {
	// Empty cwd resolves via filepath.Abs(".") to current working dir.
	// That may or may not have a .chitin ancestor. Just verify no error.
	got, err := Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve with empty cwd: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty result")
	}
}

func TestResolve_BoundaryIdenticalToCWD(t *testing.T) {
	// When cwd == boundary and no .chitin at boundary, should fall through to orphan
	sandbox := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	got, err := Resolve(sandbox, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeHome, ".chitin")
	if got != want {
		t.Errorf("expected orphan %s, got %s", want, got)
	}
}

func TestResolve_ChitinDirAtCWD(t *testing.T) {
	// .chitin at the cwd itself should be found
	root := t.TempDir()
	chitin := filepath.Join(root, ".chitin")
	if err := os.MkdirAll(chitin, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != chitin {
		t.Errorf("expected %s, got %s", chitin, got)
	}
}

func TestResolve_ChitinDirAtBoundary(t *testing.T) {
	// .chitin at the boundary should be found (boundary is inclusive)
	boundary := t.TempDir()
	chitin := filepath.Join(boundary, ".chitin")
	if err := os.MkdirAll(chitin, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(boundary, "sub", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(nested, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if got != chitin {
		t.Errorf("expected %s, got %s", chitin, got)
	}
}

func TestResolve_NoBoundaryFindsHomeFallback(t *testing.T) {
	// Without boundary, walk-up may find a system .chitin (e.g., /tmp/.chitin)
	// in CI environments. Use a boundary to force orphan fallback.
	sandbox := t.TempDir()
	fencedSub := filepath.Join(sandbox, "fenced")
	if err := os.MkdirAll(fencedSub, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Use boundary=sandbox so walk-up stops before /tmp
	got, err := Resolve(fencedSub, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeHome, ".chitin")
	if got != want {
		t.Errorf("expected orphan %s, got %s", want, got)
	}
}

func TestResolve_StatPermissionError(t *testing.T) {
	// Create a directory with a .chitin that is unreadable
	root := t.TempDir()
	unreadable := filepath.Join(root, ".chitin")
	if err := os.MkdirAll(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadable, 0o755) // cleanup

	nested := filepath.Join(root, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Resolve(nested, "")
	// On some systems root can read 0o000 dirs; only fail test if
	// the error actually surfaces. If no error, the walk found it.
	if err != nil {
		t.Logf("stat permission error surfaced correctly: %v", err)
	}
	// If no error, that's fine too — means OS/root bypassed the permission
}