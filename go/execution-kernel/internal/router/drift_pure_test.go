package router

import (
	"testing"
)

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate(short) = %q, want %q", got, "hello")
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("truncate(long) = %q, want %q", got, "hello…")
	}
	if got := truncate("exact", 5); got != "exact" {
		t.Errorf("truncate(exact) = %q, want %q", got, "exact")
	}
}