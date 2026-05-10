package plugins

import (
	"bytes"
	"testing"
)

func TestResolveHome(t *testing.T) {
	// Normal case: HOME is set
	home, err := resolveHome()
	if err != nil {
		t.Fatalf("resolveHome() error: %v", err)
	}
	if home == "" {
		t.Error("resolveHome() returned empty string")
	}
}

func TestResolveHome_EmptyHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := resolveHome()
	if err == nil {
		t.Error("expected error for empty HOME")
	}
}

func TestResolveHome_RootHome(t *testing.T) {
	t.Setenv("HOME", "/")
	_, err := resolveHome()
	if err == nil {
		t.Error("expected error for HOME=/ (root)")
	}
}

func TestWarn_NilWriter(t *testing.T) {
	// Should not panic with nil writer
	warn(nil, "test-plugin", "test message")
}

func TestWarn_WithWriter(t *testing.T) {
	var buf bytes.Buffer
	warn(&buf, "my-plugin", "sandbox violation")
	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("warn")) {
		t.Errorf("expected warn level in output, got %q", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("my-plugin")) {
		t.Errorf("expected plugin name in output, got %q", output)
	}
}

func TestTruncate(t *testing.T) {
	// Short string — no truncation
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate(short) = %q, want %q", got, "hello")
	}
	// Exact length
	if got := truncate("12345", 5); got != "12345" {
		t.Errorf("truncate(exact) = %q, want %q", got, "12345")
	}
	// Over length
	if got := truncate("abcdefghij", 5); got != "abcde…" {
		t.Errorf("truncate(long) = %q, want %q", got, "abcde…")
	}
}

func TestHashFile_MissingPath(t *testing.T) {
	// Non-existent file
	_, err := HashFile("/nonexistent/path/to/plugin")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}