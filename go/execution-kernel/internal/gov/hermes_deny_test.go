package gov

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGate_HermesDeniesFrontierSpawn verifies the chitin.yaml
// `hermes-no-frontier-spawn` rule: when CHITIN_DRIVER=hermes, direct
// shell.exec of `codex`, `claude`, or `gemini` denies; shell.exec of
// `clawta` allows. Regression for the 2026-05-11 deny-rule lockdown.
func TestGate_HermesDeniesFrontierSpawn(t *testing.T) {
	policy, err := LoadPolicyFile(filepath.Join("testdata", "hermes-no-frontier-spawn.yaml"))
	if err != nil {
		t.Fatalf("LoadPolicyFile: %v", err)
	}

	cases := []struct {
		name       string
		driver     string
		target     string
		wantAllow  bool
		wantRuleID string
	}{
		{"hermes-codex-direct-denied", "hermes", "codex --message hi", false, "hermes-no-frontier-spawn"},
		{"hermes-claude-direct-denied", "hermes", "claude -p 'do thing'", false, "hermes-no-frontier-spawn"},
		{"hermes-gemini-direct-denied", "hermes", "gemini -p 'do thing'", false, "hermes-no-frontier-spawn"},
		{"hermes-pathed-codex-denied", "hermes", "/usr/local/bin/codex --yolo", false, "hermes-no-frontier-spawn"},
		{"hermes-clawta-allowed", "hermes", "clawta --text 'dispatch'", true, "allow-clawta-and-other-shell"},
		{"hermes-ls-allowed", "hermes", "ls -la", true, "allow-clawta-and-other-shell"},
		{"codex-codex-allowed", "codex", "codex --message hi", true, "allow-clawta-and-other-shell"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &Gate{Policy: policy}
			g.Fingerprint = FingerprintContext{Driver: tc.driver}
			d := g.Evaluate(Action{Type: ActShellExec, Target: tc.target}, "test-agent", nil)
			if d.Allowed != tc.wantAllow {
				t.Errorf("Allowed: got %v want %v (decision=%+v)", d.Allowed, tc.wantAllow, d)
			}
			if !strings.Contains(d.RuleID, tc.wantRuleID) {
				t.Errorf("RuleID: got %q want contains %q", d.RuleID, tc.wantRuleID)
			}
		})
	}
}
