package verdict

import (
	"errors"
	"testing"
)

func TestParseStructured_HappyPath(t *testing.T) {
	raw := []byte(`{"verdict":"approve","concerns":[],"recommendations":[],"blockers":[]}`)
	v, err := ParseStructured(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v.Verdict != Approve {
		t.Errorf("verdict = %q, want approve", v.Verdict)
	}
}

func TestParseStructured_Empty(t *testing.T) {
	_, err := ParseStructured(nil)
	if err == nil {
		t.Fatal("empty input: want error")
	}
	if !errors.Is(err, ErrMalformedJSON) {
		t.Errorf("err = %v, want wrapped ErrMalformedJSON", err)
	}
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Kind != "malformed_json" {
		t.Errorf("ParseError.Kind mismatch: %+v", pe)
	}
}

func TestParseStructured_BadJSON(t *testing.T) {
	_, err := ParseStructured([]byte("{verdict: approve"))
	if !errors.Is(err, ErrMalformedJSON) {
		t.Errorf("bad json: err = %v, want ErrMalformedJSON", err)
	}
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Kind != "malformed_json" {
		t.Errorf("ParseError.Kind = %q, want malformed_json", pe.Kind)
	}
}

func TestParseStructured_BadShape(t *testing.T) {
	// approve verdict with non-empty blockers — fails FR-014 invariant #1
	raw := []byte(`{"verdict":"approve","blockers":["should not be here"]}`)
	_, err := ParseStructured(raw)
	if !errors.Is(err, ErrMalformedShape) {
		t.Errorf("bad shape: err = %v, want ErrMalformedShape", err)
	}
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Kind != "malformed_shape" {
		t.Errorf("ParseError.Kind = %q, want malformed_shape", pe.Kind)
	}
}

func TestParseStructured_UnknownEnum(t *testing.T) {
	raw := []byte(`{"verdict":"maybe-approve"}`)
	_, err := ParseStructured(raw)
	if !errors.Is(err, ErrMalformedShape) {
		t.Errorf("unknown enum: err = %v, want ErrMalformedShape (Validate wraps it)", err)
	}
}

func TestParseError_Error(t *testing.T) {
	e := &ParseError{Kind: "malformed_json", Detail: "x"}
	if got := e.Error(); got != "malformed_json: x" {
		t.Errorf("Error() = %q, want \"malformed_json: x\"", got)
	}
}
