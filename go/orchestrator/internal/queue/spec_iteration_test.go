// Package queue tests — spec 115 T017.
//
// These tests exist as a focused check on the T017 contribution: that
// `spec_iteration_escalated` chain events with the two new reason
// kinds (FR-010) land in the spec 114 operator queue alongside the
// existing code-PR escalations.
//
// The broader package tests (filter / scan / reason coverage) live on
// their respective spec 114 work-unit branches; this file only asserts
// the slice of behaviour T017 introduces, so its golden invariants are
// independent of T004 / T002 / T008 internal details and stay stable
// across their review cycles.
package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidReasons_IncludesSpec115Kinds(t *testing.T) {
	t.Parallel()
	want := map[string]bool{
		"design_judgement_required":   false,
		"lint_violation_unresolvable": false,
	}
	for _, r := range ValidReasons {
		if _, tracked := want[r]; tracked {
			want[r] = true
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("ValidReasons missing spec 115 FR-010 kind %q", k)
		}
	}
}

func TestValidateReason_AcceptsSpec115Kinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"design_judgement_required", "lint_violation_unresolvable"} {
		if err := ValidateReason(kind); err != nil {
			t.Errorf("ValidateReason(%q) = %v, want nil", kind, err)
		}
		if !IsValidReason(kind) {
			t.Errorf("IsValidReason(%q) = false, want true", kind)
		}
	}
}

func TestClassifyReason_SpecIterationEscalated(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		eventType string
		payload   string
		want      string
		ok        bool
	}{
		{"design_judgement", "spec_iteration_escalated", "design_judgement_required", "design_judgement_required", true},
		{"lint_unresolvable", "spec_iteration_escalated", "lint_violation_unresolvable", "lint_violation_unresolvable", true},
		{"shared_iteration_cap", "spec_iteration_escalated", "iteration_cap_hit", "iteration_cap_hit", true},
		{"shared_lease_lost", "spec_iteration_escalated", "lease_lost", "lease_lost", true},
		{"unknown_reason_skipped", "spec_iteration_escalated", "invented_kind", "", false},
		// `iteration_completed_with_skips` is NOT in spec 115's reason
		// set — spec 115 FR-010 lists 5 reasons, intentionally omitting
		// the "completed with skips" kind because the spec-iteration
		// loop doesn't have a "skip" disposition. Round-tripping it
		// through `spec_iteration_escalated` must therefore drop.
		{"spec_omits_skips", "spec_iteration_escalated", "iteration_completed_with_skips", "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := classifyReason(tc.eventType, tc.payload)
			if got != tc.want || ok != tc.ok {
				t.Errorf("classifyReason(%q, %q) = (%q, %v), want (%q, %v)",
					tc.eventType, tc.payload, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestFilter_SurfacesSpecIterationEscalations(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	prs := []LivePR{
		{Number: 401, Title: "spec/115/spec.md: add design-judgement classifier"},
		{Number: 402, Title: "spec/115/spec.md: invent a CLI subcommand"},
	}
	events := map[int][]EscalationEvent{
		401: {{
			EventType: "spec_iteration_escalated",
			Reason:    "design_judgement_required",
			PRNumber:  401,
			Ts:        now.Add(-30 * time.Minute),
		}},
		402: {{
			EventType: "spec_iteration_escalated",
			Reason:    "lint_violation_unresolvable",
			PRNumber:  402,
			Ts:        now.Add(-10 * time.Minute),
		}},
	}

	out := Filter(prs, events, now)
	if len(out) != 2 {
		t.Fatalf("Filter produced %d entries, want 2: %+v", len(out), out)
	}
	gotByReason := map[string]int{}
	for _, e := range out {
		gotByReason[e.Reason] = e.PR.Number
	}
	if gotByReason["design_judgement_required"] != 401 {
		t.Errorf("design_judgement_required entry = %d, want PR 401", gotByReason["design_judgement_required"])
	}
	if gotByReason["lint_violation_unresolvable"] != 402 {
		t.Errorf("lint_violation_unresolvable entry = %d, want PR 402", gotByReason["lint_violation_unresolvable"])
	}
}

func TestFilterByReason_NarrowsToSpecKinds(t *testing.T) {
	t.Parallel()
	entries := []QueueEntry{
		{PR: LivePR{Number: 401}, Reason: "design_judgement_required"},
		{PR: LivePR{Number: 402}, Reason: "lint_violation_unresolvable"},
		{PR: LivePR{Number: 403}, Reason: "iteration_cap_hit"},
	}
	got := FilterByReason(entries, "design_judgement_required")
	if len(got) != 1 || got[0].PR.Number != 401 {
		t.Errorf("FilterByReason(design_judgement_required) = %+v, want [{401, ...}]", got)
	}
	got = FilterByReason(entries, "lint_violation_unresolvable")
	if len(got) != 1 || got[0].PR.Number != 402 {
		t.Errorf("FilterByReason(lint_violation_unresolvable) = %+v, want [{402, ...}]", got)
	}
}

// TestScan_LoadsSpecIterationEvents asserts the end-to-end wiring:
// a JSONL chain row with event_type=spec_iteration_escalated is
// recognised by the byte pre-filter, parsed, classified, and indexed
// by PR number — the property that lets filter.go's two new rules
// actually fire on real chain data.
func TestScan_LoadsSpecIterationEvents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "events-test.jsonl")
	rows := []map[string]any{
		{
			"event_type": "spec_iteration_escalated",
			"ts":         "2026-05-26T11:30:00Z",
			"run_id":     "run-115-design",
			"payload":    map[string]any{"pr_number": 401, "reason": "design_judgement_required"},
		},
		{
			"event_type": "spec_iteration_escalated",
			"ts":         "2026-05-26T11:45:00Z",
			"run_id":     "run-115-lint",
			"payload":    map[string]any{"pr_number": 402, "reason": "lint_violation_unresolvable"},
		},
		// invented reason must be dropped per spec 115 FR-010 closed set
		{
			"event_type": "spec_iteration_escalated",
			"ts":         "2026-05-26T11:50:00Z",
			"run_id":     "run-115-bad",
			"payload":    map[string]any{"pr_number": 403, "reason": "invented_kind"},
		},
	}
	var sb strings.Builder
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal row: %v", err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	idx, err := Scan(dir, time.Time{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := idx[401]; len(got) != 1 || got[0].Reason != "design_judgement_required" {
		t.Errorf("idx[401] = %+v, want one design_judgement_required event", got)
	}
	if got := idx[402]; len(got) != 1 || got[0].Reason != "lint_violation_unresolvable" {
		t.Errorf("idx[402] = %+v, want one lint_violation_unresolvable event", got)
	}
	if got := idx[403]; len(got) != 0 {
		t.Errorf("idx[403] = %+v, want empty (invented reason dropped)", got)
	}
}
