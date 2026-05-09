package gemini

import (
	"bytes"
	"testing"
)

func TestSetWarnSink(t *testing.T) {
	var buf bytes.Buffer
	SetWarnSink(&buf)

	got := stringField(map[string]any{"key": 42}, "key")
	if got != "" {
		t.Errorf("stringField wrong type = %q, want empty", got)
	}
	if buf.Len() == 0 {
		t.Error("expected warning when warnSink is set and key has wrong type")
	}
	SetWarnSink(nil)
}

func TestStringField_NilMap(t *testing.T) {
	SetWarnSink(nil)
	if got := stringField(nil, "key"); got != "" {
		t.Errorf("stringField(nil) = %q, want empty", got)
	}
}

func TestStringField_Branches(t *testing.T) {
	SetWarnSink(nil)
	m := map[string]any{"key": "value"}

	if got := stringField(m, "key"); got != "value" {
		t.Errorf("present key = %q, want 'value'", got)
	}
	if got := stringField(m, "absent"); got != "" {
		t.Errorf("absent key = %q, want empty", got)
	}
	if got := stringField(map[string]any{"key": 42}, "key"); got != "" {
		t.Errorf("wrong type = %q, want empty", got)
	}
}

func TestFirstStringInList(t *testing.T) {
	SetWarnSink(nil)
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{"missing key", map[string]any{}, "key", ""},
		{"not a slice", map[string]any{"key": "str"}, "key", ""},
		{"empty slice", map[string]any{"key": []any{}}, "key", ""},
		{"first non-string", map[string]any{"key": []any{42}}, "key", ""},
		{"first is string", map[string]any{"key": []any{"hello", "world"}}, "key", "hello"},
	}
	for _, tc := range tests {
		got := firstStringInList(tc.m, tc.key)
		if got != tc.want {
			t.Errorf("firstStringInList(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}