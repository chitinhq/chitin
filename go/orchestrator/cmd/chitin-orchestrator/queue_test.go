package main

// queue_test.go — hermetic end-to-end test for the spec 114 operator
// escalation surface (T011).
//
// The test exercises the full queue subcommand stack through a single
// invocation of runQueue:
//
//   1. fixture chain events written to a temp $CHITIN_DIR cover the five
//      reason kinds that derive from chain rows (iteration_cap_hit,
//      iteration_completed_with_skips, human_reviewer_present, lease_lost,
//      sibling_rebase_failed);
//   2. a fake `gh` binary on PATH returns canned `gh pr list` + `gh pr view`
//      payloads that drive the three reason kinds that derive from live PR
//      state (dialectic_request_changes, stale_no_automation,
//      conflicting_persistent) plus the PRs the chain events join against;
//   3. runQueue is invoked with --format json so the test asserts on
//      structured output without coupling to the table/markdown layout
//      of T005/T006.
//
// The full FR-008 closed taxonomy is covered: one PR per reason kind plus
// a "should be hidden" PR (clean, in-flight) that the filter MUST exclude.
//
// Dependencies: this test depends on the queue subcommand wiring landing
// from T001 (runQueue), the chain scanner T002, the live-PR adapter T003,
// the FR-003 filter T004, the FR-008 --reason validation T008, and the
// JSON renderer T007. The test is intentionally written ahead of merge so
// it can pin the across-all-reasons contract the implementation tasks must
// satisfy.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// queueTestNow is the wall-clock anchor every fixture event/PR is built
// against. Pinning it makes the test deterministic against the FR-003
// thresholds (stale_no_automation = > 24h; conflicting_persistent = > 1h).
var queueTestNow = time.Date(2026, 5, 25, 17, 0, 0, 0, time.UTC)

// queueExpect bundles the asserted contract for one PR fixture: the PR
// number we expect in the JSON output and the canonical FR-008 reason
// kind the filter should attach to it.
type queueExpect struct {
	PR     int
	Reason string
}

// queueJSONEntry mirrors the FR-007 JSON shape (T007). Field names match
// internal/queue.Entry's json tags; this duplicate decode target lets the
// test parse output without importing the queue package directly (the
// runQueue path constructs Entry values itself).
type queueJSONEntry struct {
	PRNumber int    `json:"pr_number"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
	SpecRef  string `json:"spec_ref,omitempty"`
}

// allFR008Reasons is the FR-008 closed taxonomy, in the order this test
// surfaces them. Keeping it explicit catches drift if the spec extends
// the set without the test learning about it.
var allFR008Reasons = []string{
	"iteration_cap_hit",
	"iteration_completed_with_skips",
	"human_reviewer_present",
	"lease_lost",
	"sibling_rebase_failed",
	"dialectic_request_changes",
	"stale_no_automation",
	"conflicting_persistent",
}

// TestRunQueue_HermeticAcrossAllReasonKinds is the T011 contract test:
// after fixturing chain events for the chain-derived reasons and stubbing
// gh for the live-state-derived reasons, runQueue --format json must
// return exactly the set of escalated PRs with the canonical reason kind
// for each — and must NOT include the clean in-flight PR.
func TestRunQueue_HermeticAcrossAllReasonKinds(t *testing.T) {
	chainDir := t.TempDir()
	writeQueueFixtureChain(t, chainDir, queueTestNow)

	ghBin := writeFakeGHForQueue(t, queueTestNow)
	t.Setenv("PATH", filepath.Dir(ghBin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Point the queue at the fixture chain dir. The scanner (T002) reads
	// $CHITIN_DIR via queue.ResolveChainDir; the same env var has been the
	// kernel's chain-store anchor since the 2026-05-23 emit refactor.
	t.Setenv("CHITIN_DIR", chainDir)
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")

	stdout, stderr, code := runQueueCapture(t, []string{
		"--format", "json",
		"--repo", "chitinhq/chitin",
		"--since", "168h",
	}, queueTestNow)
	if code != exitSuccess {
		t.Fatalf("runQueue exit = %d (want %d); stderr=%q", code, exitSuccess, stderr)
	}

	got, err := decodeQueueJSON(stdout)
	if err != nil {
		t.Fatalf("decode runQueue stdout: %v; raw=%q", err, stdout)
	}

	want := []queueExpect{
		{PR: 9001, Reason: "iteration_cap_hit"},
		{PR: 9002, Reason: "iteration_completed_with_skips"},
		{PR: 9003, Reason: "human_reviewer_present"},
		{PR: 9004, Reason: "lease_lost"},
		{PR: 9005, Reason: "sibling_rebase_failed"},
		{PR: 9006, Reason: "dialectic_request_changes"},
		{PR: 9007, Reason: "stale_no_automation"},
		{PR: 9008, Reason: "conflicting_persistent"},
	}
	assertQueueSetEqual(t, want, got)

	// The clean in-flight PR (#9999) carries chitin-iterating/active and a
	// recent automated commit; FR-004 says it MUST be hidden.
	for _, e := range got {
		if e.PRNumber == 9999 {
			t.Errorf("PR #9999 should be hidden (clean in-flight) but appeared as %q", e.Reason)
		}
	}

	// Sanity: every FR-008 reason kind appears in the output at least once.
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.Reason] = true
	}
	for _, r := range allFR008Reasons {
		if !seen[r] {
			t.Errorf("FR-008 reason %q missing from queue output", r)
		}
	}
}

// TestRunQueue_ReasonFilter_NarrowsToSingleKind pins the FR-008 --reason
// drill-down contract (T008). With the same fixture set, asking for one
// reason returns only PRs with that reason.
func TestRunQueue_ReasonFilter_NarrowsToSingleKind(t *testing.T) {
	chainDir := t.TempDir()
	writeQueueFixtureChain(t, chainDir, queueTestNow)
	ghBin := writeFakeGHForQueue(t, queueTestNow)
	t.Setenv("PATH", filepath.Dir(ghBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CHITIN_DIR", chainDir)
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")

	for _, reason := range allFR008Reasons {
		reason := reason
		t.Run(reason, func(t *testing.T) {
			stdout, stderr, code := runQueueCapture(t, []string{
				"--format", "json",
				"--repo", "chitinhq/chitin",
				"--since", "168h",
				"--reason", reason,
			}, queueTestNow)
			if code != exitSuccess {
				t.Fatalf("runQueue exit = %d (want %d); stderr=%q", code, exitSuccess, stderr)
			}
			got, err := decodeQueueJSON(stdout)
			if err != nil {
				t.Fatalf("decode: %v; raw=%q", err, stdout)
			}
			if len(got) == 0 {
				t.Fatalf("--reason %s returned no rows; want at least 1", reason)
			}
			for _, e := range got {
				if e.Reason != reason {
					t.Errorf("--reason %s leaked row: pr=#%d reason=%q", reason, e.PRNumber, e.Reason)
				}
			}
		})
	}
}

// TestRunQueue_UnknownReason_RejectsWithHelpfulError pins the FR-008 edge
// case: unknown reason kinds must error with the closed-taxonomy list.
func TestRunQueue_UnknownReason_RejectsWithHelpfulError(t *testing.T) {
	chainDir := t.TempDir()
	writeQueueFixtureChain(t, chainDir, queueTestNow)
	ghBin := writeFakeGHForQueue(t, queueTestNow)
	t.Setenv("PATH", filepath.Dir(ghBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CHITIN_DIR", chainDir)
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")

	_, stderr, code := runQueueCapture(t, []string{
		"--format", "json",
		"--reason", "not-a-real-reason",
	})
	if code != exitUserError {
		t.Fatalf("exit = %d; want %d (user error) for unknown reason; stderr=%q", code, exitUserError, stderr)
	}
	if !strings.Contains(stderr, "not-a-real-reason") {
		t.Errorf("stderr should name the bad reason kind; got: %q", stderr)
	}
	// FR-008 closed taxonomy: the error must also surface at least one
	// valid reason so an operator can self-correct without consulting docs.
	sawValid := false
	for _, r := range allFR008Reasons {
		if strings.Contains(stderr, r) {
			sawValid = true
			break
		}
	}
	if !sawValid {
		t.Errorf("stderr should list at least one valid FR-008 reason kind to guide the operator; got: %q", stderr)
	}
}

func TestRunQueue_PublicEntryPointEmptyQueue(t *testing.T) {
	chainDir := t.TempDir()
	ghBin := writeFakeGHForEmptyQueue(t)
	t.Setenv("PATH", filepath.Dir(ghBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CHITIN_DIR", chainDir)
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")

	stdout, stderr, code := runQueueCapture(t, []string{
		"--repo", "chitinhq/chitin",
	})
	if code != exitSuccess {
		t.Fatalf("runQueue exit = %d (want %d); stderr=%q", code, exitSuccess, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q; want empty", stderr)
	}
	if stdout != "✅ no PRs need attention\n" {
		t.Fatalf("stdout = %q; want empty queue message", stdout)
	}
}

// runQueueCapture invokes the queue subcommand with stdout/stderr buffered
// so the test can assert on both surfaces without spawning a process.
func runQueueCapture(t *testing.T, args []string, now ...time.Time) (stdout, stderr string, code int) {
	t.Helper()
	var so, se strings.Builder
	if len(now) > 0 {
		code = runQueueWithNow(context.Background(), args, now[0], &so, &se)
	} else {
		code = runQueue(context.Background(), args, &so, &se)
	}
	return so.String(), se.String(), code
}

// decodeQueueJSON parses the FR-007 JSON output. The format is one
// object per PR — either a top-level array or NDJSON; the test accepts
// both shapes so T007's renderer choice doesn't constrain us.
func decodeQueueJSON(raw string) ([]queueJSONEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var entries []queueJSONEntry
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, fmt.Errorf("array decode: %w", err)
		}
		return entries, nil
	}
	// NDJSON: one Entry per line.
	var entries []queueJSONEntry
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e queueJSONEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("ndjson line %d: %w", i+1, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// assertQueueSetEqual compares two queue-entry sets ignoring order. Spec
// 114 doesn't fix output order (sort key is the renderer's choice), so
// the contract is set equality on (PR, Reason) pairs.
func assertQueueSetEqual(t *testing.T, want []queueExpect, got []queueJSONEntry) {
	t.Helper()

	type pair struct {
		PR     int
		Reason string
	}
	wantSet := map[pair]bool{}
	for _, w := range want {
		wantSet[pair{w.PR, w.Reason}] = true
	}
	gotSet := map[pair]bool{}
	gotCounts := map[pair]int{}
	for _, g := range got {
		p := pair{g.PRNumber, g.Reason}
		gotSet[p] = true
		gotCounts[p]++
	}

	// FR-008 contract: exactly one row per (PR, reason). Surface duplicates
	// before the set-membership diff so they don't get masked by it.
	var dupes []pair
	for p, n := range gotCounts {
		if n > 1 {
			dupes = append(dupes, p)
		}
	}
	if len(dupes) > 0 {
		sort.Slice(dupes, func(i, j int) bool {
			if dupes[i].PR != dupes[j].PR {
				return dupes[i].PR < dupes[j].PR
			}
			return dupes[i].Reason < dupes[j].Reason
		})
		t.Errorf("queue emitted duplicate (PR, reason) rows: %+v (counts=%+v)", dupes, gotCounts)
	}

	var missing, extra []pair
	for p := range wantSet {
		if !gotSet[p] {
			missing = append(missing, p)
		}
	}
	for p := range gotSet {
		if !wantSet[p] {
			extra = append(extra, p)
		}
	}
	sortPairs := func(ps []pair) {
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].PR != ps[j].PR {
				return ps[i].PR < ps[j].PR
			}
			return ps[i].Reason < ps[j].Reason
		})
	}
	sortPairs(missing)
	sortPairs(extra)

	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("queue set mismatch:\n  missing: %+v\n  extra:   %+v\n  got:     %+v", missing, extra, got)
	}
}

// writeQueueFixtureChain seeds chainDir with events-*.jsonl files
// covering the five chain-derived FR-008 reasons. The events match the
// shape spec 113 FR-011 emits and the spec 112 US2 sibling_rebase_failed
// event-type from the chain ledger.
func writeQueueFixtureChain(t *testing.T, chainDir string, now time.Time) {
	t.Helper()

	// Events sit 30 minutes inside the default 168h --since window.
	ts := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)

	piEscalated := func(pr int, reason, runID string) map[string]any {
		return map[string]any{
			"event_type": "pr_iteration_escalated",
			"run_id":     runID,
			"ts":         ts,
			"payload": map[string]any{
				"pr_number":        pr,
				"reason":           reason,
				"rounds_attempted": 3,
				"last_review_id":   "RV_T011_" + reason,
			},
		}
	}
	siblingRebaseFailed := func(pr int, runID string) map[string]any {
		return map[string]any{
			"event_type": "sibling_rebase_failed",
			"run_id":     runID,
			"ts":         ts,
			"payload": map[string]any{
				"pr_number":      pr,
				"conflict_files": []string{"go.mod"},
			},
		}
	}

	rows := []map[string]any{
		piEscalated(9001, "iteration_cap_hit", "run-icap"),
		piEscalated(9002, "iteration_completed_with_skips", "run-skip"),
		piEscalated(9003, "human_reviewer_present", "run-hum"),
		piEscalated(9004, "lease_lost", "run-lease"),
		siblingRebaseFailed(9005, "run-reb"),
	}

	path := filepath.Join(chainDir, "events-t011-fixture.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode row: %v", err)
		}
	}
}

// writeFakeGHForQueue installs a fake `gh` binary on PATH that returns
// canned `gh pr list` and `gh pr view` payloads. The list payload pins
// the eight escalated PRs (#9001–#9008) and the one clean PR (#9999); the
// view payload returns commit history that drives the
// last-automated-commit decoration FetchLive depends on for the
// stale_no_automation rule.
//
// The script dispatches on `gh pr list` vs `gh pr view` by sniffing argv.
// All other invocations exit 1 so an accidental gh subcommand surfaces
// as a test failure rather than a silent zero-row queue.
func writeFakeGHForQueue(t *testing.T, now time.Time) (binPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "gh")

	listJSON := buildFakeGHListJSON(t, now)
	listPath := filepath.Join(dir, "pr-list.json")
	if err := os.WriteFile(listPath, []byte(listJSON), 0o644); err != nil {
		t.Fatalf("write pr-list fixture: %v", err)
	}

	// gh pr view <number> --json commits: one response per PR. The only
	// PR for which the chitin orchestrator identity has commits recently
	// is the clean in-flight PR (#9999); #9007 deliberately has NO
	// orchestrator commits in the last 24h so it surfaces as
	// stale_no_automation.
	viewDir := filepath.Join(dir, "pr-view")
	if err := os.MkdirAll(viewDir, 0o755); err != nil {
		t.Fatalf("mkdir pr-view: %v", err)
	}
	for prNumber, commitsJSON := range buildFakeGHViewPayloads(now) {
		p := filepath.Join(viewDir, fmt.Sprintf("%d.json", prNumber))
		if err := os.WriteFile(p, []byte(commitsJSON), 0o644); err != nil {
			t.Fatalf("write pr-view fixture: %v", err)
		}
	}
	defaultViewPath := filepath.Join(viewDir, "default.json")
	if err := os.WriteFile(defaultViewPath, []byte(`{"commits":[]}`), 0o644); err != nil {
		t.Fatalf("write default pr-view: %v", err)
	}

	// Paths are shellQuote'd so a TMPDIR containing spaces or shell
	// metacharacters can't break the fake gh dispatcher.
	script := "#!/usr/bin/env bash\n" +
		"set -e\n" +
		"sub=\"$1\"; act=\"$2\"; shift 2 || true\n" +
		"if [[ \"$sub\" == \"pr\" && \"$act\" == \"list\" ]]; then\n" +
		"  cat " + shellQuote(listPath) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [[ \"$sub\" == \"pr\" && \"$act\" == \"view\" ]]; then\n" +
		"  prnum=\"$1\"\n" +
		"  payload=" + shellQuote(viewDir) + "/\"${prnum}.json\"\n" +
		"  if [[ -f \"$payload\" ]]; then\n" +
		"    cat \"$payload\"\n" +
		"  else\n" +
		"    cat " + shellQuote(defaultViewPath) + "\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"fake gh: unsupported invocation: $sub $act $*\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	return binPath
}

func writeFakeGHForEmptyQueue(t *testing.T) (binPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "gh")
	script := "#!/usr/bin/env bash\n" +
		"set -e\n" +
		"sub=\"$1\"; act=\"$2\"; shift 2 || true\n" +
		"if [[ \"$sub\" == \"pr\" && \"$act\" == \"list\" ]]; then\n" +
		"  printf '[]\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [[ \"$sub\" == \"pr\" && \"$act\" == \"view\" ]]; then\n" +
		"  printf '{\"commits\":[]}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"fake gh: unsupported invocation: $sub $act $*\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	return binPath
}

// buildFakeGHListJSON constructs the `gh pr list --json ...` payload. The
// field set matches the T003 invocation:
//
//	number, title, headRefName, labels, mergeable, updatedAt, reviews
//
// Each fixture PR is sized to a specific FR-003 rule:
//
//   - #9001..#9005 are joined via chain events (their gh-level state is
//     "iterating" — only the chain event escalates them).
//   - #9006 carries a non-bot review state REQUEST_CHANGES (FR-003
//     dialectic_request_changes — the dialectic verdict surface).
//   - #9007 is > 24h old with no orchestrator commit (FR-003
//     stale_no_automation).
//   - #9008 has mergeable=CONFLICTING and an UpdatedAt > 1h ago
//     (FR-003 conflicting_persistent — transient sub-1h conflicts are
//     filtered out per the spec).
//   - #9999 is the clean in-flight PR: chitin-iterating/active label,
//     fresh updatedAt, no escalation chain event. FR-004 hides it.
func buildFakeGHListJSON(t *testing.T, now time.Time) string {
	t.Helper()
	mk := func(num int, title, head string, labels []string, mergeable string, updatedAgo time.Duration, reviewState, reviewer string) map[string]any {
		labelObjs := make([]map[string]any, 0, len(labels))
		for _, l := range labels {
			labelObjs = append(labelObjs, map[string]any{"name": l})
		}
		var reviews []map[string]any
		if reviewState != "" {
			reviews = append(reviews, map[string]any{
				"state":       reviewState,
				"submittedAt": now.Add(-2 * time.Hour).Format(time.RFC3339),
				"author":      map[string]any{"login": reviewer},
			})
		}
		return map[string]any{
			"number":      num,
			"title":       title,
			"headRefName": head,
			"labels":      labelObjs,
			"mergeable":   mergeable,
			"updatedAt":   now.Add(-updatedAgo).Format(time.RFC3339),
			"reviews":     reviews,
		}
	}

	prs := []map[string]any{
		mk(9001, "iteration_cap_hit PR", "chitin/wu/114-operator-escalation-surface-t001-a", []string{"sched/run/run-icap"}, "MERGEABLE", 2*time.Hour, "", ""),
		mk(9002, "skips PR", "chitin/wu/114-operator-escalation-surface-t002-b", []string{"sched/run/run-skip"}, "MERGEABLE", 2*time.Hour, "", ""),
		mk(9003, "human reviewer PR", "chitin/wu/114-operator-escalation-surface-t003-c", []string{"sched/run/run-hum"}, "MERGEABLE", 2*time.Hour, "COMMENTED", "human-operator"),
		mk(9004, "lease lost PR", "chitin/wu/114-operator-escalation-surface-t004-d", []string{"sched/run/run-lease"}, "MERGEABLE", 2*time.Hour, "", ""),
		mk(9005, "sibling rebase failed PR", "chitin/wu/112-sibling-rebase-t005-e", []string{"sched/run/run-reb"}, "MERGEABLE", 2*time.Hour, "", ""),
		mk(9006, "dialectic request changes PR", "chitin/wu/094-dialectic-t006-f", []string{"sched/run/run-dialectic"}, "MERGEABLE", 2*time.Hour, "CHANGES_REQUESTED", "human-reviewer"),
		mk(9007, "stale no-automation PR", "chitin/wu/100-stale-t007-g", []string{"sched/run/run-stale"}, "MERGEABLE", 48*time.Hour, "COMMENTED", "human-operator"),
		mk(9008, "persistent conflict PR", "chitin/wu/099-conflict-t008-h", []string{"sched/run/run-conf"}, "CONFLICTING", 3*time.Hour, "", ""),
		// Clean in-flight PR — MUST be hidden per FR-004.
		mk(9999, "clean in-flight PR", "chitin/wu/113-iterating-t099-z", []string{"sched/run/run-clean", "chitin-iterating/active"}, "MERGEABLE", 10*time.Minute, "", ""),
	}

	b, err := json.MarshalIndent(prs, "", "  ")
	if err != nil {
		t.Fatalf("marshal fake gh pr list: %v", err)
	}
	return string(b)
}

// buildFakeGHViewPayloads returns the per-PR `gh pr view --json commits`
// payloads. The orchestrator identity (orchestrator@chitin.local) drives
// the "last automated action" decoration; PRs whose payload includes a
// recent commit at that identity are NOT stale, while #9007's empty
// commit set surfaces it as stale_no_automation.
func buildFakeGHViewPayloads(now time.Time) map[int]string {
	orchestratorCommit := func(authoredAgo time.Duration) string {
		ts := now.Add(-authoredAgo).Format(time.RFC3339)
		return fmt.Sprintf(`{
  "commits": [
    {
      "committedDate": %q,
      "authoredDate": %q,
      "authors": [
        {"email": "orchestrator@chitin.local", "login": "chitin-orchestrator", "name": "Chitin Orchestrator"}
      ]
    }
  ]
}`, ts, ts)
	}

	humanCommit := func(authoredAgo time.Duration) string {
		ts := now.Add(-authoredAgo).Format(time.RFC3339)
		return fmt.Sprintf(`{
  "commits": [
    {
      "committedDate": %q,
      "authoredDate": %q,
      "authors": [
        {"email": "jared@example.com", "login": "jpleva91", "name": "Jared Pleva"}
      ]
    }
  ]
}`, ts, ts)
	}

	return map[int]string{
		// Most fixture PRs have a recent orchestrator commit so they don't
		// trip stale_no_automation incidentally.
		9001: orchestratorCommit(1 * time.Hour),
		9002: orchestratorCommit(1 * time.Hour),
		9003: orchestratorCommit(1 * time.Hour),
		9004: orchestratorCommit(1 * time.Hour),
		9005: orchestratorCommit(1 * time.Hour),
		9006: orchestratorCommit(1 * time.Hour),
		// #9007 has ONLY human commits — no orchestrator activity → stale.
		9007: humanCommit(30 * time.Hour),
		9008: orchestratorCommit(1 * time.Hour),
		9999: orchestratorCommit(5 * time.Minute),
	}
}
