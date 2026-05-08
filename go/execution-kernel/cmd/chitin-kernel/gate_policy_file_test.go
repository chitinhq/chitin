package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCLIWithEnv mirrors runCLI from main_test.go but appends the given
// env entries (KEY=value strings) to os.Environ() before exec — so we
// can validate env-var-driven flag defaults like --policy-file.
//
// Test isolation: CHITIN_HOME defaults to a per-test temp dir so the
// kernel uses a fresh gov.db (mirrors runCLI's isolation contract). If
// the caller passes their own CHITIN_HOME via env, it overrides the
// default — the last KEY=VALUE in cmd.Env wins, per Go's exec docs.
func runCLIWithEnv(t *testing.T, wd string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), testBinary, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(), "CHITIN_HOME="+t.TempDir())
	cmd.Env = append(cmd.Env, env...)
	stdout, err := cmd.Output()
	var stderr []byte
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = ee.Stderr
	}
	return string(stdout), string(stderr), cmd.ProcessState.ExitCode()
}

// TestCLI_GateEvaluate_PolicyFileFlag verifies the --policy-file flag (and
// the CHITIN_POLICY_FILE env fallback) load policy from an explicit path
// instead of walking up from --cwd.
//
// Why this matters: the chitin-governance hermes plugin shells out to
// chitin-kernel from whatever cwd hermes runs from — typically a per-task
// worktree of a non-chitin repo, or /tmp. Without --policy-file the
// kernel's cwd-walk-upward returns no_policy_found and the plugin's
// lenient default lets every tool call through. Manual test on
// 2026-05-06 confirmed: from /tmp every dangerous call (write to
// /etc/hostname, sudo tee /etc/hostname, write to ~/.ssh) returned
// block=False even with the protected-system-path-* rules in place.
func TestCLI_GateEvaluate_PolicyFileFlag(t *testing.T) {
	// Stand up a minimal policy in one temp dir; cd into a different
	// temp dir that has no chitin.yaml in its ancestor chain.
	policyDir := t.TempDir()
	policyPath := filepath.Join(policyDir, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(`
id: test-policy
mode: enforce
invariantModes:
  protected-system-path-write: enforce
rules:
  - id: protected-system-path-write
    action: file.write
    effect: deny
    path_under: ["/etc/"]
    reason: "system paths are protected"
  - id: default-allow-file-write
    action: file.write
    effect: allow
    reason: "writes allowed by default"
`), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	noPolicyDir := t.TempDir() // no chitin.yaml here or upward (t.TempDir lives under TMPDIR with no ancestor chitin.yaml)

	cases := []struct {
		name      string
		path      string
		wantAllow bool
		wantRule  string
	}{
		{"system path denied via explicit policy", "/etc/hostname", false, "protected-system-path-write"},
		{"safe path allowed via explicit policy", "/tmp/scratch.txt", true, "default-allow-file-write"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argsJSON, _ := json.Marshal(map[string]string{"path": tc.path})
			stdout, stderr, _ := runCLI(t, noPolicyDir,
				"gate", "evaluate",
				"--tool", "write_file",
				"--args-json", string(argsJSON),
				"--agent", "test-agent",
				"--cwd", noPolicyDir,
				"--policy-file", policyPath,
			)
			if stdout == "" {
				t.Fatalf("empty stdout (stderr=%q)", stderr)
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(stdout), &out); err != nil {
				t.Fatalf("unmarshal stdout %q: %v", stdout, err)
			}
			allowed, _ := out["allowed"].(bool)
			ruleID, _ := out["rule_id"].(string)
			if allowed != tc.wantAllow {
				t.Errorf("allowed = %v, want %v (rule_id=%q)", allowed, tc.wantAllow, ruleID)
			}
			if ruleID != tc.wantRule {
				t.Errorf("rule_id = %q, want %q", ruleID, tc.wantRule)
			}
		})
	}
}

// TestCLI_GateEvaluate_PolicyFileEnvFallback proves the CHITIN_POLICY_FILE
// env var is honored when --policy-file is not passed. This is the path
// the hermes plugin uses to set CHITIN_POLICY_FILE in the spawned-process
// env.
func TestCLI_GateEvaluate_PolicyFileEnvFallback(t *testing.T) {
	policyDir := t.TempDir()
	policyPath := filepath.Join(policyDir, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(`
id: test-policy-env
mode: enforce
rules:
  - id: deny-everything
    action: file.write
    effect: deny
    reason: "blanket deny via env-fallback policy"
`), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	noPolicyDir := t.TempDir()

	// We can't use runCLI here because it doesn't accept env overrides.
	// Inline a small subprocess instead.
	stdout, _, code := runCLIWithEnv(t, noPolicyDir,
		[]string{"CHITIN_POLICY_FILE=" + policyPath},
		"gate", "evaluate",
		"--tool", "write_file",
		"--args-json", `{"path":"/tmp/anything.txt"}`,
		"--agent", "test-agent",
		"--cwd", noPolicyDir,
	)
	if code == 0 {
		t.Errorf("env-fallback policy should deny, got allow (stdout=%s)", stdout)
	}
	if !strings.Contains(stdout, "deny-everything") {
		t.Errorf("expected env-loaded policy's rule_id=deny-everything in stdout, got: %s", stdout)
	}
}
