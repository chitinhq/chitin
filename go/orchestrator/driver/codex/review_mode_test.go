package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestInvoke_ReviewMode_ArgvIncludesSkipGitRepoCheck covers spec 110 US1 +
// FR-001: when the codex driver is invoked in review mode, the subprocess
// argv MUST include --skip-git-repo-check so codex's git-trust safety check
// doesn't refuse to run inside the worker worktree (which is never on the
// operator's pre-trusted directory list).
//
// Guards the 2026-05-24 PR #1007 dialectic failure where codex exited in
// 132ms with "Not inside a trusted directory and --skip-git-repo-check was
// not specified."
//
// Review-mode discriminator: SpecID="094" / TaskID="review" — the values
// activities/review/dispatch_machine_reviewer.go (line 126-127) sets on the
// WorkUnit it hands to a machine reviewer driver. claudecode's parallel
// test (driver/claudecode/review_mode_test.go) uses the same discriminator.
func TestInvoke_ReviewMode_ArgvIncludesSkipGitRepoCheck(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "codex")
	argvPath := filepath.Join(dir, "argv.bin")
	// Fake codex: null-delimit argv so a multi-line prompt doesn't get
	// confused with arg boundaries, then emit a minimally-valid
	// StructuredVerdict so review-mode post-processing (T002/T003) doesn't
	// fail the call on its own — keeps this test focused on the argv claim.
	cleanJSON := `{"verdict":"approve","concerns":[],"recommendations":[],"blockers":[]}`
	script := "#!/usr/bin/env bash\n" +
		"printf '%s\\0' \"$@\" > " + argvPath + "\n" +
		"cat <<'JSON'\n" + cleanJSON + "\nJSON\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-argv-001",
		SpecID:       "094",
		TaskID:       "review",
		WorktreePath: dir,
		Context:      `{"pr":{"repo":"chitinhq/chitin","number":1007}}`,
	}
	if _, err := d.Invoke(context.Background(), wu); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	argv := readArgv(t, argvPath)
	if !containsArg(argv, "--skip-git-repo-check") {
		t.Fatalf("review-mode argv missing --skip-git-repo-check; got %v", argv)
	}
}

// readArgv reads the null-delimited argv recorded by the fake binary and
// returns it as a string slice (trailing empty entry from the final NUL
// separator stripped).
func readArgv(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded argv at %s: %v", path, err)
	}
	parts := strings.Split(string(raw), "\x00")
	if n := len(parts); n > 0 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	return parts
}

func containsArg(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}
