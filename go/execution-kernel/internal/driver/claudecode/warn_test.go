package claudecode

import (
	"bytes"
	"testing"
)

func TestSetWarnSink(t *testing.T) {
	var buf bytes.Buffer
	SetWarnSink(&buf)

	// Trigger a warning via stringField with wrong type
	got := stringField(map[string]any{"key": 42}, "key")
	if got != "" {
		t.Errorf("stringField with non-string value should return empty, got %q", got)
	}
	if buf.Len() == 0 {
		t.Error("expected warning output when warnSink is set and key has wrong type")
	}
	SetWarnSink(nil)
}

func TestStringField_NilMap(t *testing.T) {
	// Reset warnSink to avoid side effects
	SetWarnSink(nil)

	got := stringField(nil, "any_key")
	if got != "" {
		t.Errorf("stringField(nil, key) = %q, want empty", got)
	}
}

func TestStringField_MissingKey(t *testing.T) {
	SetWarnSink(nil)
	m := map[string]any{"present": "value"}

	got := stringField(m, "absent")
	if got != "" {
		t.Errorf("stringField missing key = %q, want empty", got)
	}
}

func TestStringField_WrongType(t *testing.T) {
	SetWarnSink(nil)
	m := map[string]any{"key": 42}

	got := stringField(m, "key")
	if got != "" {
		t.Errorf("stringField wrong type = %q, want empty", got)
	}
}

func TestStringField_Present(t *testing.T) {
	SetWarnSink(nil)
	m := map[string]any{"key": "hello"}

	got := stringField(m, "key")
	if got != "hello" {
		t.Errorf("stringField present = %q, want 'hello'", got)
	}
}