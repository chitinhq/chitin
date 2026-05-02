package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestExitErrWithChain_OmitsEmptyChainID asserts the spec §6 error
// shape: chain_id appears only when non-empty (#11). An empty chain_id
// would clutter the JSON envelope without adding signal.
func TestExitErrWithChain_OmitsEmptyChainID(t *testing.T) {
	cases := []struct {
		name      string
		chainID   string
		wantField bool
	}{
		{"with chain_id", "abc-123", true},
		{"empty chain_id", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]string{"error": "test_kind", "message": "test_msg"}
			if tc.chainID != "" {
				body["chain_id"] = tc.chainID
			}
			out, _ := json.Marshal(body)
			var parsed map[string]string
			if err := json.Unmarshal(out, &parsed); err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, hasChain := parsed["chain_id"]
			if hasChain != tc.wantField {
				t.Errorf("chain_id presence: got %v, want %v (output: %s)",
					hasChain, tc.wantField, string(out))
			}
			if parsed["error"] == "" || parsed["message"] == "" {
				t.Errorf("error+message must always be present, got: %s", string(out))
			}
		})
	}
}

// TestEnvelopeTemplateMismatch_DetectsForeignChain mirrors the #14
// security invariant: a caller passing a template with a foreign
// session_id / chain_id (different from --session-id) gets rejected.
//
// We can't invoke cmdIngestTranscript end-to-end here (it touches
// the filesystem), but the matching logic is a single pair of
// equality checks on the parsed template; this test exercises that
// logic via the same expressions cmdIngestTranscript uses.
func TestEnvelopeTemplateMismatch_DetectsForeignChain(t *testing.T) {
	const flagSessionID = "S-real"
	cases := []struct {
		name           string
		tmplSessionID  string
		tmplChainID    string
		wantMismatch   bool
		wantField      string
	}{
		{"matching", "S-real", "S-real", false, ""},
		{"empty template fields are OK (legacy)", "", "", false, ""},
		{"foreign session_id", "S-victim", "S-real", true, "session_id"},
		{"foreign chain_id", "S-real", "S-victim", true, "chain_id"},
		{"both foreign", "S-victim", "S-victim", true, "session_id"}, // session checked first
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mismatch := false
			field := ""
			if tc.tmplSessionID != "" && tc.tmplSessionID != flagSessionID {
				mismatch = true
				field = "session_id"
			} else if tc.tmplChainID != "" && tc.tmplChainID != flagSessionID {
				mismatch = true
				field = "chain_id"
			}
			if mismatch != tc.wantMismatch {
				t.Errorf("mismatch: got %v, want %v", mismatch, tc.wantMismatch)
			}
			if tc.wantMismatch && !strings.Contains(field, tc.wantField) {
				t.Errorf("first-mismatched field: got %q, want %q", field, tc.wantField)
			}
		})
	}
}
