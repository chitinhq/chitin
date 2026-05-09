package plugins

import (
	"bytes"
	"strings"
	"testing"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello…"},
		{"", 3, ""},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.n)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}

func TestWarn_NilWriter(t *testing.T) {
	// Should not panic with nil writer
	warn(nil, "test-plugin", "test message")
}

func TestWarn_WritesJSON(t *testing.T) {
	var buf bytes.Buffer
	warn(&buf, "my-plugin", "something happened")
	output := buf.String()
	if !strings.Contains(output, "my-plugin") {
		t.Errorf("expected plugin name in output, got: %s", output)
	}
	if !strings.Contains(output, "something happened") {
		t.Errorf("expected message in output, got: %s", output)
	}
	if !strings.Contains(output, "warn") {
		t.Errorf("expected level in output, got: %s", output)
	}
}

func TestResolveHome(t *testing.T) {
	home, err := resolveHome()
	if err != nil {
		// In a test environment, HOME should be resolvable
		t.Logf("resolveHome() returned error: %v (may be OK in restricted env)", err)
	} else {
		if home == "" || home == "/" {
			t.Errorf("resolveHome() = %q, expected non-empty, non-root path", home)
		}
	}
}

func TestResolveHome_Unresolvable(t *testing.T) {
	t.Setenv("HOME", "/")
	_, err := resolveHome()
	if err == nil {
		t.Error("expected error when HOME is /")
	}
}