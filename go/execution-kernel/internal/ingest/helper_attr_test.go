package ingest

import (
	"testing"
)

func TestIsAllZero(t *testing.T) {
	if !isAllZero(nil) {
		t.Error("nil slice should be all-zero")
	}
	if !isAllZero([]byte{0, 0, 0}) {
		t.Error("all-zero slice should return true")
	}
	if isAllZero([]byte{0, 1, 0}) {
		t.Error("slice with non-zero should return false")
	}
	if isAllZero([]byte{255}) {
		t.Error("non-zero byte should return false")
	}
}

func TestGetResourceStringAttr_Nil(t *testing.T) {
	// nil resource → empty string
	got := getResourceStringAttr(nil, "service.name")
	if got != "" {
		t.Errorf("expected empty string for nil resource, got %q", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"a/b/c", "a_b_c"},
		{"file.txt", "file.txt"},
		{"", ""},
		{"path\\to\\file", "path_to_file"},
		{"C:\\Users", "C__Users"},
	}
	for _, c := range cases {
		got := sanitizeFilename(c.input)
		if got != c.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}