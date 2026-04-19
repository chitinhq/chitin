package hash

import (
	"encoding/json"
	"testing"
)

func TestCanonicalJSON_SortsKeys(t *testing.T) {
	input := map[string]any{"b": 1, "a": 2}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":2,"b":1}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCanonicalJSON_NoWhitespace(t *testing.T) {
	input := map[string]any{"a": []any{1.0, 2.0, 3.0}}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[1,2,3]}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestCanonicalJSON_NestedSort(t *testing.T) {
	input := map[string]any{"z": map[string]any{"b": 1, "a": 2}}
	got, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"z":{"a":2,"b":1}}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSha256Hex_Empty(t *testing.T) {
	got := Sha256Hex("")
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSha256Hex_ABC(t *testing.T) {
	got := Sha256Hex("abc")
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestHashEvent_ExcludesThisHash(t *testing.T) {
	e1 := map[string]any{"a": 1.0, "b": 2.0, "this_hash": "xyz"}
	e2 := map[string]any{"a": 1.0, "b": 2.0, "this_hash": "abc"}
	h1, _ := HashEvent(e1)
	h2, _ := HashEvent(e2)
	if h1 != h2 {
		t.Errorf("expected equal hashes when only this_hash differs: %q vs %q", h1, h2)
	}
}

func TestCanonicalJSON_EventGolden(t *testing.T) {
	event := map[string]any{
		"schema_version":  "2",
		"run_id":          "550e8400-e29b-41d4-a716-446655440000",
		"session_id":      "550e8400-e29b-41d4-a716-446655440001",
		"surface":         "claude-code",
		"seq":             0.0,
		"event_type":      "session_start",
		"chain_type":      "session",
		"parent_chain_id": nil,
		"prev_hash":       nil,
		"labels":          map[string]any{},
	}
	got, err := CanonicalJSON(event)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"chain_type":"session","event_type":"session_start","labels":{},"parent_chain_id":null,"prev_hash":null,"run_id":"550e8400-e29b-41d4-a716-446655440000","schema_version":"2","seq":0,"session_id":"550e8400-e29b-41d4-a716-446655440001","surface":"claude-code"}`
	if got != want {
		t.Errorf("golden canonical JSON mismatch:\ngot:  %s\nwant: %s", got, want)
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(got), &rt); err != nil {
		t.Fatalf("canonical JSON did not parse: %v", err)
	}
}
