package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// TestWriteRouterTelemetry_StableSchema pins the JSONL telemetry
// shape emitted by writeRouterTelemetry. Downstream consumers
// (analysis lib, operator dashboards) parse this via a fixed key
// set; any rename or reorder breaks them silently.
//
// The escalate field was removed alongside the in-gate LLM advisor
// in the audit Tier 6 cull (2026-05-08); chain consumers stamp any
// escalation intent themselves when they read the heuristic signals
// off the gov-decisions log.
func TestWriteRouterTelemetry_StableSchema(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		kernelDeny bool
	}{
		{"heuristic-fired-allow", "heuristic-fired", false},
		{"heuristic-fired-deny", "heuristic-fired", true},
		{"pre-action-block", "pre-action-block", false},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		writeRouterTelemetry(&buf, tc.kind, router.HeuristicOutcome{}, KernelVerdict{Denied: tc.kernelDeny})
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &parsed); err != nil {
			t.Errorf("%s: telemetry not valid JSON: %v (raw: %q)", tc.name, err, buf.String())
			continue
		}
		if parsed["msg"] != tc.kind {
			t.Errorf("%s: msg=%v want %v", tc.name, parsed["msg"], tc.kind)
		}
		if parsed["component"] != "router-hook" {
			t.Errorf("%s: component=%v want router-hook", tc.name, parsed["component"])
		}
		if parsed["kernel_denied"] != tc.kernelDeny {
			t.Errorf("%s: kernel_denied=%v want %v", tc.name, parsed["kernel_denied"], tc.kernelDeny)
		}
		// The escalate field MUST be absent — its presence would
		// signal that the in-gate advisor path crept back in.
		if _, present := parsed["escalate"]; present {
			t.Errorf("%s: escalate field present (should have been removed in audit Tier 6 cull); raw=%q",
				tc.name, buf.String())
		}
	}
}

func TestWriteRouterTelemetry_IncludesKernelDenialDetails(t *testing.T) {
	var buf bytes.Buffer
	writeRouterTelemetry(&buf, "heuristic-fired", router.HeuristicOutcome{}, KernelVerdict{
		Denied: true,
		RuleID: "worktree-required",
		Reason: "worktree-required: cwd is the primary checkout",
	})
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &parsed); err != nil {
		t.Fatalf("telemetry not valid JSON: %v (raw: %q)", err, buf.String())
	}
	if parsed["kernel_rule_id"] != "worktree-required" {
		t.Fatalf("kernel_rule_id=%v want worktree-required", parsed["kernel_rule_id"])
	}
	if !strings.Contains(parsed["kernel_reason"].(string), "primary checkout") {
		t.Fatalf("kernel_reason missing denial reason: %v", parsed["kernel_reason"])
	}
}

func TestParseKernelVerdictFromBlockJSON(t *testing.T) {
	got := parseKernelVerdict([]byte(`{"decision":"block","reason":"chitin: no-rm: no rm","rule_id":"no-rm"}`), claudecode.ExitBlock)
	if !got.Denied {
		t.Fatal("Denied=false want true")
	}
	if got.RuleID != "no-rm" {
		t.Fatalf("RuleID=%q want no-rm", got.RuleID)
	}
	if !strings.Contains(got.Reason, "no rm") {
		t.Fatalf("Reason=%q missing original reason", got.Reason)
	}
}

func TestEvalRouterHookStdin_PreservesKernelDenyInStdoutAndTelemetry(t *testing.T) {
	env := setupHookEnv(t, baselinePolicy+`
router:
  enabled: true
  heuristics:
    blast_radius:
      enabled: true
      threshold: 0.1
    floundering:
      enabled: false
`)
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "rm -rf go/"},
		"cwd":        env.cwd,
		"session_id": "router-deny-test",
	})
	var out, errOut bytes.Buffer
	code := evalRouterHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, true)
	if code != claudecode.ExitBlock {
		t.Fatalf("code=%d want block; stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	var blocked map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &blocked); err != nil {
		t.Fatalf("stdout not valid block JSON: %v\n%s", err, out.String())
	}
	if blocked["rule_id"] != "no-rm" {
		t.Fatalf("stdout rule_id=%q want no-rm", blocked["rule_id"])
	}
	if !strings.Contains(blocked["reason"], "no-rm") || !strings.Contains(blocked["reason"], "no rm -rf") {
		t.Fatalf("stdout reason should preserve kernel rule and reason, got %q", blocked["reason"])
	}

	var telemetry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(errOut.Bytes()), &telemetry); err != nil {
		t.Fatalf("stderr telemetry not valid JSON: %v\n%s", err, errOut.String())
	}
	if telemetry["kernel_denied"] != true {
		t.Fatalf("kernel_denied=%v want true", telemetry["kernel_denied"])
	}
	if telemetry["kernel_rule_id"] != "no-rm" {
		t.Fatalf("kernel_rule_id=%v want no-rm", telemetry["kernel_rule_id"])
	}
	if !strings.Contains(telemetry["kernel_reason"].(string), "no rm -rf") {
		t.Fatalf("kernel_reason missing policy reason: %v", telemetry["kernel_reason"])
	}
}

func TestEvalRouterHookStdin_DriftKillBlocksAndEmitsEvents(t *testing.T) {
	sessionID := "drift-kill"
	env := setupHookEnv(t, baselinePolicy+`
router:
  enabled: true
  heuristics:
    drift:
      enabled: true
      warn_threshold: 0.3
      halt_threshold: 0.6
      max_turns: 8
`)
	intentFile := filepath.Join(env.chitin, "events-"+sessionID+".jsonl")
	intentLine := `{"ts":"2026-05-14T00:00:00Z","event_type":"intent","payload":{"entry_id":"t1","task_class":"fix","file_paths":["apps/cli/src/**"]}}
{"ts":"2026-05-14T00:00:01Z","event_type":"decision","payload":{"decision":"allow","action_type":"file.write","action_target":"docs/README.md"}}`
	if err := os.WriteFile(intentFile, []byte(intentLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": "docs/ops.md"},
		"cwd":        env.cwd,
		"session_id": sessionID,
	})
	var out, errOut bytes.Buffer
	code := evalRouterHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false)
	if code != claudecode.ExitBlock {
		t.Fatalf("code=%d want block; stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "drift-kill") {
		t.Fatalf("stdout=%q want drift-kill reason", out.String())
	}
	data, err := os.ReadFile(filepath.Join(env.chitin, "events-"+sessionID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"event_type":"drift"`) < 2 {
		t.Fatalf("expected detection+action drift events, got:\n%s", string(data))
	}
}

func TestEvalRouterHookStdin_DriftDemoteStampsRoutingDecision(t *testing.T) {
	sessionID := "drift-demote"
	env := setupHookEnv(t, baselinePolicy+`
router:
  enabled: true
  heuristics:
    drift:
      enabled: true
      warn_threshold: 0.3
      halt_threshold: 0.9
      max_turns: 8
`)
	intentFile := filepath.Join(env.chitin, "events-"+sessionID+".jsonl")
	intentLine := `{"ts":"2026-05-14T00:00:00Z","event_type":"intent","payload":{"entry_id":"t1","task_class":"fix","file_paths":["apps/cli/src/**"]}}`
	if err := os.WriteFile(intentFile, []byte(intentLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.cwd, "chitin-routes.yaml"), []byte(`version: 1
enabled: true
rules:
  - name: drift-high
    signal: drift
    severity: "score>=0.3"
    route: reasoning_depth
routes:
  reasoning_depth:
    - driver: copilot
      model: gpt-5.5
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": "docs/README.md"},
		"cwd":        env.cwd,
		"session_id": sessionID,
	})
	var out, errOut bytes.Buffer
	code := evalRouterHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false)
	if code != 0 {
		t.Fatalf("code=%d want allow; stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	decisionLog, err := os.ReadFile(filepath.Join(env.chitin, "gov-decisions-"+time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decisionLog), `"routing_decision":"drift.demote:`) {
		t.Fatalf("expected drift demote routing decision, got:\n%s", string(decisionLog))
	}
}

// TestEvalRouterHookStdin_DriftDisabledSkipsDetection covers the Copilot
// finding: the drift heuristic must honor `router.heuristics.drift.enabled:
// false` — the same off-task scenario that drift-kill blocks on must NOT
// kill and must NOT emit drift events when drift is disabled.
func TestEvalRouterHookStdin_DriftDisabledSkipsDetection(t *testing.T) {
	sessionID := "drift-off"
	env := setupHookEnv(t, baselinePolicy+`
router:
  enabled: true
  heuristics:
    drift:
      enabled: false
`)
	intentFile := filepath.Join(env.chitin, "events-"+sessionID+".jsonl")
	intentLine := `{"ts":"2026-05-14T00:00:00Z","event_type":"intent","payload":{"entry_id":"t1","task_class":"fix","file_paths":["apps/cli/src/**"]}}
{"ts":"2026-05-14T00:00:01Z","event_type":"decision","payload":{"decision":"allow","action_type":"file.write","action_target":"docs/README.md"}}`
	if err := os.WriteFile(intentFile, []byte(intentLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": "docs/ops.md"},
		"cwd":        env.cwd,
		"session_id": sessionID,
	})
	var out, errOut bytes.Buffer
	code := evalRouterHookStdin(bytes.NewReader(body), &out, &errOut, "claude-code", "", "", false, false)
	if code == claudecode.ExitBlock {
		t.Fatalf("code=block; drift disabled must not kill. stdout=%q", out.String())
	}
	data, _ := os.ReadFile(intentFile)
	if strings.Contains(string(data), `"event_type":"drift"`) {
		t.Fatalf("drift disabled but drift events emitted:\n%s", string(data))
	}
}

// TestHasNonZeroSignal_BoundaryCases pins the predicate that decides
// whether the router stamps a heuristic-signal row. The predicate
// drives chain bloat: too eager and every read-only tool call writes
// a stamping row; too lax and sub-threshold training signal is lost.
func TestHasNonZeroSignal_BoundaryCases(t *testing.T) {
	cases := []struct {
		name    string
		blast   *router.HeuristicScore
		flound  *router.HeuristicScore
		drift   router.HeuristicScore
		wantHit bool
	}{
		{"all-zero", nil, nil, router.HeuristicScore{Score: 0}, false},
		{"blast-non-zero-sub-threshold", &router.HeuristicScore{Score: 0.1, Fired: false}, nil, router.HeuristicScore{}, true},
		{"floundering-non-zero", nil, &router.HeuristicScore{Score: 0.5}, router.HeuristicScore{}, true},
		{"drift-non-zero", nil, nil, router.HeuristicScore{Score: 0.3}, true},
	}
	for _, tc := range cases {
		o := router.HeuristicOutcome{BlastRadius: tc.blast, Floundering: tc.flound}
		got := hasNonZeroSignal(o, tc.drift)
		if got != tc.wantHit {
			t.Errorf("%s: hasNonZeroSignal=%v want %v", tc.name, got, tc.wantHit)
		}
	}
}
