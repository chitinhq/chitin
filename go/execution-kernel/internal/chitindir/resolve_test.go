package chitindir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_FindsExistingChitinDirInParent(t *testing.T) {
	root := t.TempDir()
	chitin := filepath.Join(root, ".chitin")
	if err := os.MkdirAll(chitin, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(nested, root)
	if err != nil {
		t.Fatal(err)
	}
	if got != chitin {
		t.Fatalf("want %s, got %s", chitin, got)
	}
}

func TestResolve_OrphanFallbackWhenNoChitinDirFound(t *testing.T) {
	cwd := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	got, err := Resolve(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeHome, ".chitin")
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("orphan dir not created: %v", err)
	}
}

func TestResolve_StopsAtWorkspaceBoundary(t *testing.T) {
	boundary := t.TempDir()
	outside := filepath.Dir(boundary)
	// .chitin exists ABOVE the boundary — must not be found.
	if err := os.MkdirAll(filepath.Join(outside, ".chitin"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(boundary, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	got, err := Resolve(nested, boundary)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeHome, ".chitin")
	if got != want {
		t.Fatalf("expected orphan fallback at %s, got %s", want, got)
	}
}
