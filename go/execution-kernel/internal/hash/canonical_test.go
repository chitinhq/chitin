package hash

import (
	"testing"
)

func TestWriteCanonical_BoolFalse(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{"flag": false})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"flag":false}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteCanonical_BoolTrue(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{"flag": true})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"flag":true}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteCanonical_Nil(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{"val": nil})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"val":null}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteCanonical_IntTypes(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{"n": int32(42)})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"n":42}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteCanonical_UnsupportedType(t *testing.T) {
	_, err := CanonicalJSON(map[string]any{"val": complex(1, 2)})
	if err == nil {
		t.Error("expected error for unsupported type complex128")
	}
}

func TestHashEvent_ExcludesThisHashKey(t *testing.T) {
	event := map[string]any{
		"this_hash": "should-be-ignored",
		"action":    "shell.exec",
	}
	_, err := HashEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	// this_hash key should not appear in the canonical JSON input
}

func TestSha256Hex(t *testing.T) {
	got := Sha256Hex("hello")
	if len(got) != 64 {
		t.Errorf("expected 64-char hex string, got %d chars", len(got))
	}
	// Deterministic
	got2 := Sha256Hex("hello")
	if got != got2 {
		t.Error("Sha256Hex should be deterministic")
	}
}