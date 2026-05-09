package copilot

import (
	"testing"
)

func TestDefaultChitinDir(t *testing.T) {
	dir, err := defaultChitinDir()
	if err != nil {
		t.Fatalf("defaultChitinDir: %v", err)
	}
	if dir == "" {
		t.Error("expected non-empty dir")
	}
}

func TestDefaultChitinDir_HomeEmpty(t *testing.T) {
	// The empty home check is after os.UserHomeDir succeeds,
	// but we can't set HOME="" and have UserHomeDir succeed
	// simultaneously. This test documents the edge case.
	// On most systems, HOME=→ UserHomeDir returns ("", error),
	// so the empty-home branch is a belt-and-suspenders check.
	t.Skip("cannot simulate os.UserHomeDir returning (\"\", nil)")
}

func TestDefaultChitinDir_HomeUnset(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := defaultChitinDir()
	if err == nil {
		t.Error("expected error when HOME is unset")
	}
}