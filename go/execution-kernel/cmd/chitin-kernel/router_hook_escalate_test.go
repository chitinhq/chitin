package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// Each test sets up a temp cwd with a chitin-routes.yaml of its
// choosing, then calls tryInGateSpawn directly and asserts on the
// mutation of `composed` plus the telemetry written to errOut.

func writeRoutesYAML(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "chitin-routes.yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func samplePayload() claudecode.HookInput {
	return claudecode.HookInput{
		HookEventName: "PreToolUse",
		ToolName:      "Edit",
		ToolInput:     map[string]interface{}{"file_path": "/tmp/foo.go", "old_string": "x", "new_string": "y"},
		Cwd:           "/tmp",
		SessionID:     "session-123",
	}
}

func sampleAdvice() *router.AdvisorResponse {
	return &router.AdvisorResponse{
		Nudge:    "you're going in circles",
		Verdict:  "takeover",
		Escalate: true,
	}
}

func sampleOutcome() router.HeuristicOutcome {
	return router.HeuristicOutcome{
		Floundering: &router.HeuristicScore{
			Fired:  true,
			Reason: "loop_count=3",
		},
		AnyFired: true,
	}
}

func TestTryInGateSpawn_PolicyDisabled(t *testing.T) {
	dir := t.TempDir()
	writeRoutesYAML(t, dir, `version: 1
enabled: false
routes:
  patch_quality:
    - {driver: claude, model: claude-opus-4-7}
`)
	composed := map[string]interface{}{"decision": "block", "reason": "original"}
	var errOut bytes.Buffer
	tryInGateSpawn(&bytes.Buffer{}, &errOut, &composed, samplePayload(), sampleAdvice(), sampleOutcome(), dir)
	if composed["reason"] != "original" {
		t.Errorf("disabled policy should NOT mutate reason; got %v", composed["reason"])
	}
	if strings.Contains(errOut.String(), "peer_escalation") {
		t.Error("disabled policy should not emit peer_escalation telemetry")
	}
}

func TestTryInGateSpawn_NoMatchingRule(t *testing.T) {
	dir := t.TempDir()
	writeRoutesYAML(t, dir, `version: 1
enabled: true
rules:
  - name: blast-only
    signal: blast_radius
    route: patch_quality
routes:
  patch_quality:
    - {driver: claude, model: claude-opus-4-7}
`)
	composed := map[string]interface{}{"decision": "block", "reason": "original"}
	var errOut bytes.Buffer
	tryInGateSpawn(&bytes.Buffer{}, &errOut, &composed, samplePayload(), sampleAdvice(), sampleOutcome(), dir)
	// outcome is floundering — only blast_radius rule defined → no match
	if composed["reason"] != "original" {
		t.Errorf("no rule match should leave reason intact; got %v", composed["reason"])
	}
	if !strings.Contains(errOut.String(), "in_gate_spawn_no_route") {
		t.Errorf("expected 'in_gate_spawn_no_route' warning; got %q", errOut.String())
	}
}

func TestTryInGateSpawn_NoPolicyFile(t *testing.T) {
	dir := t.TempDir()
	// No chitin-routes.yaml at all → DefaultRoutesPolicy (disabled)
	composed := map[string]interface{}{"decision": "block", "reason": "original"}
	var errOut bytes.Buffer
	tryInGateSpawn(&bytes.Buffer{}, &errOut, &composed, samplePayload(), sampleAdvice(), sampleOutcome(), dir)
	if composed["reason"] != "original" {
		t.Errorf("no policy file → silent skip; got %v", composed["reason"])
	}
}

func TestTryInGateSpawn_SignalSelectionPriority(t *testing.T) {
	// When multiple heuristics fire, floundering > blast_radius > advisor_takeover.
	tests := []struct {
		name    string
		outcome router.HeuristicOutcome
		want    string
	}{
		{
			name:    "floundering wins over nothing",
			outcome: router.HeuristicOutcome{Floundering: &router.HeuristicScore{Fired: true, Reason: "x"}},
			want:    "floundering",
		},
		{
			name:    "blast_radius wins when no floundering",
			outcome: router.HeuristicOutcome{BlastRadius: &router.HeuristicScore{Fired: true, Reason: "y"}},
			want:    "blast_radius",
		},
		{
			name: "floundering wins over blast_radius",
			outcome: router.HeuristicOutcome{
				Floundering: &router.HeuristicScore{Fired: true, Reason: "x"},
				BlastRadius: &router.HeuristicScore{Fired: true, Reason: "y"},
			},
			want: "floundering",
		},
		{
			name:    "neither fired → advisor_takeover signal",
			outcome: router.HeuristicOutcome{},
			want:    "advisor_takeover",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Define rules for ALL signals so we can confirm WHICH one routed.
			writeRoutesYAML(t, dir, `version: 1
enabled: true
rules:
  - {name: f, signal: floundering, route: patch_quality}
  - {name: b, signal: blast_radius, route: patch_quality}
  - {name: a, signal: advisor_takeover, route: patch_quality}
routes:
  patch_quality:
    - {driver: nonexistent-driver, model: x}
`)
			composed := map[string]interface{}{"decision": "block", "reason": "original"}
			var errOut bytes.Buffer
			tryInGateSpawn(&bytes.Buffer{}, &errOut, &composed, samplePayload(), sampleAdvice(), tc.outcome, dir)
			// Spawn will fail (driver unsupported) — that's fine, we're
			// asserting on the SIGNAL the routeFor saw, captured in the
			// in_gate_spawn_peer_failed warning OR no_route.
			out := errOut.String()
			if !strings.Contains(out, `"signal":"`+tc.want+`"`) && !strings.Contains(out, `signal=`+tc.want) {
				// The peer_failed path doesn't include signal; check the
				// rule name as proxy (a/f/b matches signal a/f/b).
				want := map[string]string{"floundering": "f", "blast_radius": "b", "advisor_takeover": "a"}[tc.want]
				if !strings.Contains(out, `"escalation":"`+want+`"`) {
					t.Errorf("expected signal %q or rule %q in telemetry; got: %s", tc.want, want, out)
				}
			}
		})
	}
}

func TestToolInputSummary(t *testing.T) {
	t.Run("nil → (none)", func(t *testing.T) {
		if s := toolInputSummary(nil); s != "(none)" {
			t.Errorf("got %q want (none)", s)
		}
	})
	t.Run("small map → JSON", func(t *testing.T) {
		s := toolInputSummary(map[string]interface{}{"a": "b"})
		if !strings.Contains(s, `"a":"b"`) {
			t.Errorf("got %q", s)
		}
	})
	t.Run("oversized → truncated", func(t *testing.T) {
		big := strings.Repeat("X", 5000)
		s := toolInputSummary(map[string]interface{}{"big": big})
		if !strings.HasSuffix(s, "(truncated)") {
			t.Errorf("expected truncation marker; got tail %q", s[len(s)-30:])
		}
		if len(s) > 4050 {
			t.Errorf("truncated string too long: %d", len(s))
		}
	})
}
