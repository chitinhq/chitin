package ingest

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"dir/file", "dir_file"},
		{"dir\\file", "dir_file"},
		{"host:port", "host_port"},
		{"mix/path\\here:there", "mix_path_here_there"},
		{"", ""},
		{"no-special", "no-special"},
		{"a.b.c", "a.b.c"},
	}
	for _, tc := range tests {
		got := sanitizeFilename(tc.input)
		if got != tc.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}