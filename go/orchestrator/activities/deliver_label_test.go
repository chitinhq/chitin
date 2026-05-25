package activities

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestApplyPRLabel_CreatesLabelBeforeAdding is the regression test for the
// 2026-05-25 finding (after PR #1038 merged): a chitin-authored PR was
// opened with the new `sched/run/<uuid>` label intended for spec 112 US2,
// but `gh pr edit --add-label <name>` does NOT auto-create the label —
// the call failed with `'sched/run/...' not found` and every redispatched
// PR landed unlabeled, breaking US2's sibling-rebase lister.
//
// This test installs a fake `gh` binary, runs applyPRLabel, then asserts
// that the FIRST invocation was `gh label create <label> --force ...`
// and only AFTER that did `gh pr edit --add-label <label>` fire.
func TestApplyPRLabel_CreatesLabelBeforeAdding(t *testing.T) {
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.log")
	installFakeGhForLabelTest(t, dir, argvLog, 0, "")

	_, err := applyPRLabel(
		context.Background(),
		t.TempDir(),
		"https://github.com/chitinhq/chitin/pull/9999",
		"sched/run/test-run-id",
	)
	if err != nil {
		t.Fatalf("applyPRLabel returned error: %v", err)
	}

	calls := readGhCalls(t, argvLog)
	if len(calls) != 2 {
		t.Fatalf("expected exactly 2 gh invocations, got %d: %v", len(calls), calls)
	}
	if !isLabelCreateCall(calls[0], "sched/run/test-run-id") {
		t.Errorf("first gh call should be `gh label create sched/run/test-run-id --force ...`, got %v", calls[0])
	}
	if !isPREditAddLabelCall(calls[1], "sched/run/test-run-id") {
		t.Errorf("second gh call should be `gh pr edit <url> --add-label sched/run/test-run-id`, got %v", calls[1])
	}
}

// TestApplyPRLabel_LabelCreateFailureFailsHelper asserts the error path: if
// the create call fails the helper returns an error rather than silently
// trying the add (which would surface the same unlabeled-PR symptom).
func TestApplyPRLabel_LabelCreateFailureFailsHelper(t *testing.T) {
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.log")
	// Make EVERY gh call fail with a "label not found"-like stderr. The
	// create call is the FIRST invocation so it fails first.
	installFakeGhForLabelTest(t, dir, argvLog, 1, "rate limited or whatever")

	_, err := applyPRLabel(
		context.Background(),
		t.TempDir(),
		"https://github.com/chitinhq/chitin/pull/9999",
		"sched/run/will-fail",
	)
	if err == nil {
		t.Fatal("expected applyPRLabel to return error on label-create failure")
	}
	if !strings.Contains(err.Error(), "ensure label") {
		t.Errorf("expected error to name `ensure label` step, got %q", err.Error())
	}

	calls := readGhCalls(t, argvLog)
	// The add MUST NOT have fired — only the create attempt.
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 gh invocation (create only), got %d: %v", len(calls), calls)
	}
}

// TestChitinLabelDescription asserts the per-prefix description mapping —
// sched/run/* gets the spec 112 US2 tracking text; other labels get a
// generic chitin-authored description.
func TestChitinLabelDescription(t *testing.T) {
	cases := []struct {
		label string
		want  string
	}{
		{"sched/run/abc-123", "Chitin scheduler run abc-123 (spec 112 US2 sibling tracking)"},
		{"other/label", "Applied by the Chitin orchestrator"},
	}
	for _, c := range cases {
		got := chitinLabelDescription(c.label)
		if got != c.want {
			t.Errorf("chitinLabelDescription(%q) = %q, want %q", c.label, got, c.want)
		}
	}
}

// --- helpers ---------------------------------------------------------------

// installFakeGhForLabelTest writes a fake `gh` binary into dir, prepends
// dir to PATH, and captures every invocation's argv into argvLog (one line
// per call, args TAB-separated). The fake exits with exitCode after writing
// stderr text.
func installFakeGhForLabelTest(t *testing.T, dir, argvLog string, exitCode int, stderrText string) {
	t.Helper()
	binPath := filepath.Join(dir, "gh")
	script := "#!/usr/bin/env bash\n"
	// One TSV line per invocation — separator unambiguous against any label name.
	script += `printf '%s\n' "$(IFS=$'\t'; echo "$*")" >> ` + argvLog + "\n"
	if stderrText != "" {
		script += "echo " + shellQuoteForLabelTest(stderrText) + " >&2\n"
	}
	script += "exit " + itoaForLabelTest(exitCode) + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	existing := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+existing)
}

// readGhCalls returns each recorded invocation as a slice of args (split on
// TAB — the separator the fake gh used in installFakeGhForLabelTest).
func readGhCalls(t *testing.T, argvLog string) [][]string {
	t.Helper()
	data, err := os.ReadFile(argvLog)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read argv log: %v", err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		calls = append(calls, strings.Split(line, "\t"))
	}
	return calls
}

// isLabelCreateCall returns true iff argv is `gh label create <name>`
// followed by --force (in any position).
func isLabelCreateCall(argv []string, name string) bool {
	if len(argv) < 3 {
		return false
	}
	if argv[0] != "label" || argv[1] != "create" || argv[2] != name {
		return false
	}
	for _, a := range argv[3:] {
		if a == "--force" {
			return true
		}
	}
	return false
}

// isPREditAddLabelCall returns true iff argv is `gh pr edit <url> --add-label <name>`.
func isPREditAddLabelCall(argv []string, name string) bool {
	if len(argv) < 5 {
		return false
	}
	if argv[0] != "pr" || argv[1] != "edit" {
		return false
	}
	for i, a := range argv {
		if a == "--add-label" && i+1 < len(argv) && argv[i+1] == name {
			return true
		}
	}
	return false
}

// itoaForLabelTest is a tiny strconv replacement to avoid the import — only
// used by the fake gh script construction.
func itoaForLabelTest(n int) string {
	if n == 0 {
		return "0"
	}
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return neg + string(b)
}

// shellQuoteForLabelTest wraps s in single quotes with embedded quotes escaped —
// matches the copilot_dispatch_test.go convention.
func shellQuoteForLabelTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
