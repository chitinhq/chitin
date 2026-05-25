package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestInvoke_NonReviewMode_OmitsSkipGitRepoCheck is the spec 110 FR-008 / US3
// regression test. Non-review-mode codex invocations MUST NOT pass
// `--skip-git-repo-check`: the flag is mandatory for review-mode dispatch
// (spec 110 FR-001, because the review worktree isn't pre-trusted by the
// codex CLI), but on a local-driver implementation work unit the worktree
// IS the contract we want codex to enforce. Bypassing the git-trust check
// there would silently disable a real safety boundary (SC-003).
func TestInvoke_NonReviewMode_OmitsSkipGitRepoCheck(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "codex")
	argvPath := filepath.Join(dir, "argv.log")
	// Fake codex binary: dump argv (one per line) to a file, exit 0 so the
	// driver treats the invocation as successful and we can assert on the
	// captured argv without an error-path interfering. argvPath is single-
	// quoted so a TMPDIR containing spaces or shell metacharacters doesn't
	// break the redirection; printf '%s\n' is used instead of `echo` so
	// args starting with `-` are not interpreted as flags.
	script := "#!/usr/bin/env bash\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + argvPath + "'; done\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-impl-non-review-001",
		SpecID:       "999",
		TaskID:       "T001",
		WorktreePath: dir,
		Context:      "implement the feature described in spec 999",
	}
	if _, err := d.Invoke(context.Background(), wu); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read captured argv: %v", err)
	}
	if strings.Contains(string(argv), "--skip-git-repo-check") {
		t.Errorf("non-review-mode argv unexpectedly contains --skip-git-repo-check; "+
			"spec 110 FR-002 requires this flag stay scoped to review-mode dispatch only\nargv=%q", string(argv))
	}
}
