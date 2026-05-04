package replay

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRun_NoEvents(t *testing.T) {
	_, err := Run(context.Background(), "nonexistent-session", "/tmp")
	if err == nil {
		t.Error("expected error for missing session; got nil")
	}
}

func TestFindMostRecentSession_Empty(t *testing.T) {
	tmp := t.TempDir()
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	_, err := FindMostRecentSession()
	if err == nil {
		t.Error("expected error when no chain files; got nil")
	}
}

func TestFindMostRecentSession_PickNewest(t *testing.T) {
	tmp := t.TempDir()
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	older := filepath.Join(chitinDir, "events-aaa.jsonl")
	newer := filepath.Join(chitinDir, "events-bbb.jsonl")
	if err := os.WriteFile(older, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pastTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(older, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}
	got, err := FindMostRecentSession()
	if err != nil {
		t.Fatal(err)
	}
	if got != "bbb" {
		t.Errorf("FindMostRecentSession=%q want bbb", got)
	}
}

func TestWriteHumanReport_NoDiffs(t *testing.T) {
	r := &Result{
		SessionID:   "test-session",
		TotalEvents: 5,
		Decisions:   3,
		Summary:     Summary{UnchangedDecisions: 3},
	}
	var buf strings.Builder
	WriteHumanReport(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "test-session") {
		t.Errorf("output missing session id: %q", out)
	}
	if !strings.Contains(out, "No diffs") {
		t.Errorf("output missing 'No diffs' message: %q", out)
	}
}

// TestRun_KernelDenyReplay verifies that gov.Policy deny rules are
// re-evaluated against the recorded action_type+action_target. We
// craft a session whose recorded event was originally allowed, then
// run replay against a chitin.yaml that contains a deny rule
// matching that action — the diff must surface as a kernel-layer
// "now denied".
func TestRun_KernelDenyReplay(t *testing.T) {
	tmp := t.TempDir()
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	sessionID := "kernel-deny-test"
	chainPath := filepath.Join(chitinDir, "events-"+sessionID+".jsonl")
	chain := `{"ts":"2026-05-03T10:00:00Z","event_type":"decision","payload":{"tool_name":"Bash","action_type":"shell.exec","action_target":"echo hello","decision":"allow","rule_id":"default-allow-shell"}}` + "\n"
	if err := os.WriteFile(chainPath, []byte(chain), 0o644); err != nil {
		t.Fatal(err)
	}

	policyDir := t.TempDir()
	policyYAML := `version: 1
mode: enforce
rules:
  - id: deny-all-shell-now
    effect: deny
    action: shell.exec
    reason: post-incident shell lockdown
`
	if err := os.WriteFile(filepath.Join(policyDir, "chitin.yaml"), []byte(policyYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Run(context.Background(), sessionID, policyDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.GovRuleCount == 0 {
		t.Fatalf("expected gov policy to load (rules>0); got 0 — chitin.yaml not found at %q?", policyDir)
	}
	if r.Summary.NowDenied != 1 {
		t.Fatalf("expected 1 NowDenied, got %d (diffs=%+v)", r.Summary.NowDenied, r.Diffs)
	}
	if len(r.Diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(r.Diffs))
	}
	d := r.Diffs[0]
	if d.Layer != "kernel" {
		t.Errorf("expected Layer=kernel, got %q", d.Layer)
	}
	if d.ReplayedRule != "deny-all-shell-now" {
		t.Errorf("expected ReplayedRule=deny-all-shell-now, got %q", d.ReplayedRule)
	}
	if d.OriginalAllow == d.ReplayedAllow {
		t.Errorf("expected allow→deny flip, got orig=%v replayed=%v", d.OriginalAllow, d.ReplayedAllow)
	}
}

// TestRun_MalformedPolicySurfaces ensures a chitin.yaml that
// EXISTS but fails to parse/validate is reported as an error
// rather than silently falling back to heuristic-only replay.
// Silent fall-open here would hide a broken policy — exactly the
// trap operators run replay to catch.
func TestRun_MalformedPolicySurfaces(t *testing.T) {
	tmp := t.TempDir()
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	sessionID := "malformed-policy-test"
	chainPath := filepath.Join(chitinDir, "events-"+sessionID+".jsonl")
	chain := `{"ts":"2026-05-03T10:00:00Z","event_type":"decision","payload":{"tool_name":"shell.exec","action_type":"shell.exec","action_target":"x","decision":"allow","rule_id":"x"}}` + "\n"
	if err := os.WriteFile(chainPath, []byte(chain), 0o644); err != nil {
		t.Fatal(err)
	}

	policyDir := t.TempDir()
	// Invalid YAML: indentation error → parse failure.
	bad := []byte("version: 1\nrules:\n  - id: oops\n  effect: deny\n")
	if err := os.WriteFile(filepath.Join(policyDir, "chitin.yaml"), bad, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), sessionID, policyDir)
	if err == nil {
		t.Fatal("expected error for malformed chitin.yaml; got nil — silent fall-open hides the broken policy")
	}
	if !strings.Contains(err.Error(), "failed to load policy") {
		t.Errorf("err=%q want to mention 'failed to load policy'", err.Error())
	}
}

// TestRun_NoPolicyFallsOpen ensures a missing chitin.yaml at
// policyCwd is non-fatal — we still run heuristic-only replay, and
// the report flags GovRuleCount=0 so the operator notices.
func TestRun_NoPolicyFallsOpen(t *testing.T) {
	tmp := t.TempDir()
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	sessionID := "no-policy-test"
	chainPath := filepath.Join(chitinDir, "events-"+sessionID+".jsonl")
	chain := `{"ts":"2026-05-03T10:00:00Z","event_type":"decision","payload":{"tool_name":"Read","action_type":"file.read","action_target":"/tmp/x","decision":"allow","rule_id":"default-allow"}}` + "\n"
	if err := os.WriteFile(chainPath, []byte(chain), 0o644); err != nil {
		t.Fatal(err)
	}

	emptyDir := t.TempDir()
	r, err := Run(context.Background(), sessionID, emptyDir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.GovRuleCount != 0 {
		t.Errorf("expected GovRuleCount=0 with no chitin.yaml, got %d", r.GovRuleCount)
	}
}

func TestWriteHumanReport_WithDiffs(t *testing.T) {
	r := &Result{
		SessionID:   "diff-session",
		TotalEvents: 2,
		Decisions:   2,
		Diffs: []DecisionDiff{
			{
				Ts:             "2026-05-03T10:00:00Z",
				ToolName:       "Bash",
				ActionTarget:   "rm -rf /tmp/foo",
				OriginalRule:   "default-allow-shell",
				OriginalAllow:  true,
				ReplayedAllow:  false,
				ReplayedReason: "blast-radius:recursive-delete",
			},
		},
		Summary: Summary{UnchangedDecisions: 1, NowDenied: 1},
	}
	var buf strings.Builder
	WriteHumanReport(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "NOW DENIED") {
		t.Errorf("output missing NOW DENIED label: %q", out)
	}
	if !strings.Contains(out, "blast-radius:recursive-delete") {
		t.Errorf("output missing replay reason: %q", out)
	}
}

