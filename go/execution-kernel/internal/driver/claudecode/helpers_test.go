package claudecode

import (
	"bytes"
	"testing"
)

func TestSetWarnSink(t *testing.T) {
	var buf bytes.Buffer
	SetWarnSink(&buf)
	// Trigger via stringField with non-string value
	m := map[string]any{"key": 42}
	stringField(m, "key")
	if buf.Len() == 0 {
		t.Error("expected warning output after SetWarnSink")
	}
}

func TestStringField(t *testing.T) {
	t.Run("nil map returns empty", func(t *testing.T) {
		if got := stringField(nil, "key"); got != "" {
			t.Errorf("expected empty for nil map, got %q", got)
		}
	})

	t.Run("missing key returns empty", func(t *testing.T) {
		m := map[string]any{"other": "val"}
		if got := stringField(m, "key"); got != "" {
			t.Errorf("expected empty for missing key, got %q", got)
		}
	})

	t.Run("string value returns it", func(t *testing.T) {
		m := map[string]any{"key": "hello"}
		if got := stringField(m, "key"); got != "hello" {
			t.Errorf("expected hello, got %q", got)
		}
	})

	t.Run("non-string value emits warning", func(t *testing.T) {
		m := map[string]any{"key": 42}
		var buf bytes.Buffer
		origWarn := warnSink
		warnSink = &buf
		defer func() { warnSink = origWarn }()

		if got := stringField(m, "key"); got != "" {
			t.Errorf("expected empty for non-string, got %q", got)
		}
		if buf.Len() == 0 {
			t.Error("expected warning output for wrong type")
		}
	})
}