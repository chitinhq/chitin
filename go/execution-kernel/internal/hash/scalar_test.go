package hash

import (
	"testing"
)

func TestCanonicalJSON_Nil(t *testing.T) {
	got, err := CanonicalJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "null" {
		t.Errorf("got %q want \"null\"", got)
	}
}

func TestCanonicalJSON_Bool(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		got, err := CanonicalJSON(true)
		if err != nil {
			t.Fatal(err)
		}
		if got != "true" {
			t.Errorf("got %q want \"true\"", got)
		}
	})
	t.Run("false", func(t *testing.T) {
		got, err := CanonicalJSON(false)
		if err != nil {
			t.Fatal(err)
		}
		if got != "false" {
			t.Errorf("got %q want \"false\"", got)
		}
	})
}

func TestCanonicalJSON_Float64(t *testing.T) {
	got, err := CanonicalJSON(float64(3.14))
	if err != nil {
		t.Fatal(err)
	}
	if got != "3.14" {
		t.Errorf("got %q want \"3.14\"", got)
	}
}

func TestCanonicalJSON_IntTypes(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		got, err := CanonicalJSON(int(42))
		if err != nil {
			t.Fatal(err)
		}
		if got != "42" {
			t.Errorf("got %q want \"42\"", got)
		}
	})
	t.Run("int32", func(t *testing.T) {
		got, err := CanonicalJSON(int32(7))
		if err != nil {
			t.Fatal(err)
		}
		if got != "7" {
			t.Errorf("got %q want \"7\"", got)
		}
	})
	t.Run("int64", func(t *testing.T) {
		got, err := CanonicalJSON(int64(99))
		if err != nil {
			t.Fatal(err)
		}
		if got != "99" {
			t.Errorf("got %q want \"99\"", got)
		}
	})
}

func TestCanonicalJSON_StringEscaping(t *testing.T) {
	got, err := CanonicalJSON("hello\nworld")
	if err != nil {
		t.Fatal(err)
	}
	if got != `"hello\nworld"` {
		t.Errorf("got %q want quoted escaped string", got)
	}
}

func TestCanonicalJSON_EmptyCollections(t *testing.T) {
	t.Run("empty_slice", func(t *testing.T) {
		got, err := CanonicalJSON([]any{})
		if err != nil {
			t.Fatal(err)
		}
		if got != "[]" {
			t.Errorf("got %q want []", got)
		}
	})
	t.Run("empty_map", func(t *testing.T) {
		got, err := CanonicalJSON(map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		if got != "{}" {
			t.Errorf("got %q want {}", got)
		}
	})
}

func TestCanonicalJSON_UnsupportedType(t *testing.T) {
	_, err := CanonicalJSON(struct{}{})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestCanonicalJSON_ComplexNested(t *testing.T) {
	input := map[string]any{
		"events": []any{
			map[string]any{"type": "tool_call", "seq": float64(1)},
			map[string]any{"type": "session_end", "seq": float64(2)},
		},
	}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"events":[{"seq":1,"type":"tool_call"},{"seq":2,"type":"session_end"}]}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestHashEvent_Deterministic(t *testing.T) {
	e1 := map[string]any{"action": "bash_run", "target": "/tmp/x"}
	e2 := map[string]any{"target": "/tmp/x", "action": "bash_run"}
	h1, err := HashEvent(e1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashEvent(e2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("key order should not affect hash: %q vs %q", h1, h2)
	}
}

func TestHashEvent_ErrorPropagation(t *testing.T) {
	_, err := HashEvent(map[string]any{"bad": struct{}{}})
	if err == nil {
		t.Fatal("expected error from unsupported type in HashEvent")
	}
}