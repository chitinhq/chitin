package codex

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

func TestStringField_MissingKey(t *testing.T) {
	SetWarnSink(nil)
	if got := stringField(map[string]any{"a": "1"}, "b"); got != "" {
		t.Errorf("stringField missing = %q, want empty", got)
	}
}

func TestStringField_WrongType(t *testing.T) {
	SetWarnSink(nil)
	if got := stringField(map[string]any{"key": 42}, "key"); got != "" {
		t.Errorf("stringField wrong type = %q, want empty", got)
	}
}

func TestStringField_Present(t *testing.T) {
	SetWarnSink(nil)
	if got := stringField(map[string]any{"key": "value"}, "key"); got != "value" {
		t.Errorf("stringField present = %q, want 'value'", got)
	}
}