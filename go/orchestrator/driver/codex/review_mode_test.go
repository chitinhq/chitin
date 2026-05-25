package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestReviewArgvFor_IncludesSkipGitRepoCheck covers spec 110 FR-001: the
// review-mode argv builder MUST emit --skip-git-repo-check so codex exec runs
// inside the worker worktree without tripping the CLI's trusted-directory
// safety check (which otherwise fails the subprocess in ~130ms before any
// model call).
//
// Guards the 2026-05-24 PR #1007 dialectic where codex exited with
// "Not inside a trusted directory and --skip-git-repo-check was not
// specified."
func TestReviewArgvFor_IncludesSkipGitRepoCheck(t *testing.T) {
	// SpecID="094" / TaskID="review" are NOT the spec under test (spec 110)
	// — they're the WorkUnit discriminator that activities/review/dispatch_
	// machine_reviewer.go sets for any review invocation. The reviewArgvFor
	// builder (the spec-110 surface) doesn't read SpecID directly; the
	// fixture mirrors what a live review dispatch would carry so any future
	// reader sees a realistic WorkUnit shape.
	wu := driver.WorkUnit{
		ID:           "wu-review-argv-001",
		SpecID:       "094",
		TaskID:       "review",
		WorktreePath: "/tmp/wt",
		Context:      `{"pr":{"repo":"chitinhq/chitin","number":1007}}`,
	}
	argv := reviewArgvFor(wu, "gpt-5.x-codex")

	if !containsArg(argv, "--skip-git-repo-check") {
		t.Fatalf("review-mode argv missing --skip-git-repo-check; got %v", argv)
	}
	if argv[0] != "exec" {
		t.Fatalf("argv[0] = %q, want \"exec\"", argv[0])
	}
}

// TestInvoke_NonReviewMode_OmitsSkipGitRepoCheck covers spec 110 FR-002: the
// non-review (implementation) codepath MUST NOT pass --skip-git-repo-check
// — the git-trust check is the expected safety behaviour on local-driver
// implementation work, and only review-mode legitimately needs to bypass it.
//
// The default Driver.Invoke path (no review-mode discriminator wiring yet)
// is the non-review codepath; this test pins the IFF invariant from the
// other side so a future change that accidentally adds the flag to the
// inline argv in driver.go gets caught.
func TestInvoke_NonReviewMode_OmitsSkipGitRepoCheck(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "codex")
	argvPath := filepath.Join(dir, "argv.bin")
	// Null-delimit argv so a multi-line prompt doesn't blur arg boundaries.
	// argvPath is single-quoted in the script body so a TMPDIR containing
	// spaces or shell metacharacters (CI sandboxes sometimes mount under
	// such paths) does not break the redirection.
	script := "#!/usr/bin/env bash\n" +
		"printf '%s\\0' \"$@\" > '" + argvPath + "'\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-impl-001",
		SpecID:       "110",
		TaskID:       "T999",
		WorktreePath: dir,
		Context:      "implement a thing",
	}
	if _, err := d.Invoke(context.Background(), wu); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	argv := readArgv(t, argvPath)
	if containsArg(argv, "--skip-git-repo-check") {
		t.Fatalf("non-review-mode argv unexpectedly contains --skip-git-repo-check; got %v", argv)
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

// TestInvoke_ReviewMode_CleanJSONEmitsValidatedVerdict covers spec 110 T006
// (parity with spec 109 US1's claudecode equivalent): when the codex
// review-mode invocation receives a clean StructuredVerdict JSON document on
// stdout, the driver emits StatusSucceeded with the validated verdict body in
// Result.Explanation — the field spec 094's DispatchMachineReviewer activity
// parses via verdict.ParseStructured.
//
// Guards the 2026-05-24 dialectic dogfood failure where codex returned either
// a trust-check error (FR-001) or prose (FR-005). With T001-T003 in place the
// clean CLI response is propagated to the activity in the spec-compliant
// shape (FR-006).
//
// Discriminator: SpecID="094" / TaskID="review" mirrors what the activity
// dispatcher sets (activities/review/dispatch_machine_reviewer.go) — same
// shape used by the claudecode review-mode test.
func TestInvoke_ReviewMode_CleanJSONEmitsValidatedVerdict(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "codex")
	// Canonical enum spelling is hyphenated ("approve-with-comments"); see
	// activities/review/verdict/verdict.go. The body satisfies the FR-014
	// invariant for approve-with-comments (empty blockers + at least one
	// concern or recommendation).
	cleanJSON := `{"verdict":"approve-with-comments","concerns":["nit: name shadowing on line 42"],"recommendations":["extract a helper for the duplicated branch"],"blockers":[]}`
	script := "#!/usr/bin/env bash\n" +
		"cat <<'JSON'\n" + cleanJSON + "\nJSON\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "wu-review-clean-001",
		SpecID:       "094",
		TaskID:       "review",
		WorktreePath: dir,
		Context:      `{"pr":{"repo":"chitinhq/chitin","number":1007}}`,
	}
	res, err := d.Invoke(context.Background(), wu)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Status != driver.StatusSucceeded {
		t.Fatalf("status = %s, want StatusSucceeded; explanation=%q", res.Status, res.Explanation)
	}

	var got verdict.StructuredVerdict
	if err := json.Unmarshal([]byte(res.Explanation), &got); err != nil {
		t.Fatalf("Result.Explanation is not parseable as StructuredVerdict JSON: %v\nexplanation=%q", err, res.Explanation)
	}
	if err := verdict.Validate(got); err != nil {
		t.Fatalf("Result.Explanation failed verdict.Validate: %v\nexplanation=%q", err, res.Explanation)
	}
	if got.Verdict != verdict.ApproveWithComments {
		t.Errorf("verdict = %q, want %q", got.Verdict, verdict.ApproveWithComments)
	}
	if len(got.Concerns) != 1 || got.Concerns[0] == "" {
		t.Errorf("concerns = %v, want one non-empty entry", got.Concerns)
	}
	if len(got.Blockers) != 0 {
		t.Errorf("blockers = %v, want empty for approve-with-comments", got.Blockers)
	}
}
