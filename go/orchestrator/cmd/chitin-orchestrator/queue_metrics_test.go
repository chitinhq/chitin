package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedNow returns a deterministic "now" so per-day bucket boundaries are
// reproducible. 2026-05-25T17:00:00Z lands mid-afternoon UTC, so the
// "today" bucket has 17 hours of headroom for events.
func fixedNow() time.Time {
	return time.Date(2026, 5, 25, 17, 0, 0, 0, time.UTC)
}

// writeEvents writes a single events-*.jsonl file under dir containing
// one JSON line per event. Each event uses the v2 envelope shape the
// kernel emits (schema_version=2, event_type, ts, payload).
func writeEvents(t *testing.T, dir string, events []map[string]any) {
	t.Helper()
	path := filepath.Join(dir, "events-test.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
}

// makeEscalation builds an event map for the given type, timestamp, and
// PR number. Other envelope fields are populated with stand-in values
// matching the kernel's v2 envelope.
func makeEscalation(eventType string, ts time.Time, prNumber int) map[string]any {
	return map[string]any{
		"schema_version": "2",
		"event_type":     eventType,
		"run_id":         "test-run",
		"ts":             ts.UTC().Format(time.RFC3339Nano),
		"payload": map[string]any{
			"pr_number": prNumber,
			"reason":    "iteration_cap_hit",
		},
	}
}

func TestComputeQueueMetrics_BucketsAndMedian(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow()
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// Build a 7-day window. For days=7, buckets[0..6] cover calendar
	// dates today-6 .. today. Plant escalations at known days so the
	// per-day breakdown is predictable:
	//   today-6 (2026-05-19): 0
	//   today-5 (2026-05-20): PR 100, PR 101 -> size 2
	//   today-4 (2026-05-21): PR 102 -> size 1
	//   today-3 (2026-05-22): 0
	//   today-2 (2026-05-23): PR 103 -> size 1
	//   today-1 (2026-05-24): PR 104, PR 105, PR 106 -> size 3
	//   today   (2026-05-25): PR 107, PR 107 again -> size 1 (dedup)
	// Sorted sizes: [0, 0, 1, 1, 1, 2, 3]. Median (index 3) = 1.
	events := []map[string]any{
		makeEscalation("pr_iteration_escalated", todayMidnight.AddDate(0, 0, -5).Add(2*time.Hour), 100),
		makeEscalation("sibling_rebase_failed", todayMidnight.AddDate(0, 0, -5).Add(20*time.Hour), 101),
		makeEscalation("pr_iteration_escalated", todayMidnight.AddDate(0, 0, -4).Add(6*time.Hour), 102),
		makeEscalation("pr_iteration_escalated", todayMidnight.AddDate(0, 0, -2).Add(12*time.Hour), 103),
		makeEscalation("pr_iteration_escalated", todayMidnight.AddDate(0, 0, -1).Add(1*time.Hour), 104),
		makeEscalation("sibling_rebase_failed", todayMidnight.AddDate(0, 0, -1).Add(8*time.Hour), 105),
		makeEscalation("pr_iteration_escalated", todayMidnight.AddDate(0, 0, -1).Add(22*time.Hour), 106),
		makeEscalation("pr_iteration_escalated", todayMidnight.Add(3*time.Hour), 107),
		makeEscalation("sibling_rebase_failed", todayMidnight.Add(10*time.Hour), 107), // dup PR
		// Out-of-window noise — older than 7 days; must be ignored.
		makeEscalation("pr_iteration_escalated", todayMidnight.AddDate(0, 0, -10), 999),
		// Non-escalation event — must be ignored.
		{
			"schema_version": "2",
			"event_type":     "copilot_pr_detected",
			"ts":             todayMidnight.AddDate(0, 0, -1).Format(time.RFC3339Nano),
			"payload":        map[string]any{"pr_number": 200},
		},
	}
	writeEvents(t, dir, events)

	deps := queueMetricsDeps{
		chainDir:    dir,
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 17, nil },
	}

	r, err := computeQueueMetrics(context.Background(), deps, "chitinhq/chitin", 7, 0.60)
	if err != nil {
		t.Fatalf("computeQueueMetrics: %v", err)
	}

	wantSizes := []int{0, 2, 1, 0, 1, 3, 1}
	if len(r.PerDay) != len(wantSizes) {
		t.Fatalf("per_day len = %d, want %d (%+v)", len(r.PerDay), len(wantSizes), r.PerDay)
	}
	for i, d := range r.PerDay {
		if d.QueueSize != wantSizes[i] {
			t.Errorf("per_day[%d] (%s) size = %d, want %d", i, d.Date, d.QueueSize, wantSizes[i])
		}
	}

	if r.MedianQueueSize != 1 {
		t.Errorf("median = %d, want 1", r.MedianQueueSize)
	}
	if r.RawOpenPRs != 17 {
		t.Errorf("raw_open_prs = %d, want 17", r.RawOpenPRs)
	}
	// ratio = 1/17 ≈ 0.0588, reduction ≈ 0.941 — target met.
	wantRatio := 1.0 / 17.0
	if d := r.Ratio - wantRatio; d > 0.001 || d < -0.001 {
		t.Errorf("ratio = %f, want ≈ %f", r.Ratio, wantRatio)
	}
	if !r.TargetMet {
		t.Errorf("expected target_met=true (reduction %.3f vs target %.3f)", r.Reduction, r.TargetReduction)
	}

	// Today's bucket label is today's UTC date.
	if got := r.PerDay[len(r.PerDay)-1].Date; got != "2026-05-25" {
		t.Errorf("today bucket date = %s, want 2026-05-25", got)
	}
	if got := r.PerDay[0].Date; got != "2026-05-19" {
		t.Errorf("oldest bucket date = %s, want 2026-05-19", got)
	}
}

func TestComputeQueueMetrics_TargetNotMet(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow()
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// 4 distinct PRs per day for 7 days. Median = 4. Open count = 5.
	// Ratio = 0.8, reduction = 0.2 — fails the 60% target.
	var events []map[string]any
	pr := 100
	for day := 0; day < 7; day++ {
		for k := 0; k < 4; k++ {
			ts := todayMidnight.AddDate(0, 0, -(6 - day)).Add(time.Duration(k) * time.Hour)
			events = append(events, makeEscalation("pr_iteration_escalated", ts, pr))
			pr++
		}
	}
	writeEvents(t, dir, events)

	deps := queueMetricsDeps{
		chainDir:    dir,
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 5, nil },
	}

	r, err := computeQueueMetrics(context.Background(), deps, "chitinhq/chitin", 7, 0.60)
	if err != nil {
		t.Fatalf("computeQueueMetrics: %v", err)
	}
	if r.MedianQueueSize != 4 {
		t.Errorf("median = %d, want 4", r.MedianQueueSize)
	}
	if r.TargetMet {
		t.Errorf("target should NOT be met (median=%d / open=%d = ratio %.3f)", r.MedianQueueSize, r.RawOpenPRs, r.Ratio)
	}
}

func TestComputeQueueMetrics_EmptyChainDir(t *testing.T) {
	dir := t.TempDir() // no events-*.jsonl
	deps := queueMetricsDeps{
		chainDir:    dir,
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 10, nil },
	}
	r, err := computeQueueMetrics(context.Background(), deps, "chitinhq/chitin", 7, 0.60)
	if err != nil {
		t.Fatalf("computeQueueMetrics: %v", err)
	}
	if r.MedianQueueSize != 0 {
		t.Errorf("median = %d, want 0 (no events)", r.MedianQueueSize)
	}
	if !r.TargetMet {
		t.Errorf("target should be met with zero queue (ratio=0)")
	}
	if r.Reduction != 1.0 {
		t.Errorf("reduction should be 1.0 with zero queue, got %f", r.Reduction)
	}
}

func TestComputeQueueMetrics_MalformedLinesTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events-malformed.jsonl")
	// Mix of: valid escalation, malformed JSON, escalation with bad
	// timestamp, escalation with PR 0, valid escalation. Only the two
	// valid escalations should land in the today bucket.
	// One hour before `now` so the event falls inside the today bucket's
	// half-open window [midnight, now).
	ts := fixedNow().Add(-1 * time.Hour).Format(time.RFC3339Nano)
	body := strings.Join([]string{
		`{"schema_version":"2","event_type":"pr_iteration_escalated","ts":"` + ts + `","payload":{"pr_number":100,"reason":"iteration_cap_hit"}}`,
		`{NOT JSON pr_iteration_escalated}`,
		`{"schema_version":"2","event_type":"pr_iteration_escalated","ts":"not-a-time","payload":{"pr_number":101}}`,
		`{"schema_version":"2","event_type":"pr_iteration_escalated","ts":"` + ts + `","payload":{"pr_number":0}}`,
		`{"schema_version":"2","event_type":"pr_iteration_escalated","ts":"` + ts + `","payload":{"pr_number":102,"reason":"lease_lost"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write malformed jsonl: %v", err)
	}

	deps := queueMetricsDeps{
		chainDir:    dir,
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 10, nil },
	}
	r, err := computeQueueMetrics(context.Background(), deps, "chitinhq/chitin", 7, 0.60)
	if err != nil {
		t.Fatalf("computeQueueMetrics: %v", err)
	}
	todayBucket := r.PerDay[len(r.PerDay)-1]
	if todayBucket.QueueSize != 2 {
		t.Errorf("today bucket size = %d, want 2 (PR 100, PR 102); per_day=%+v", todayBucket.QueueSize, r.PerDay)
	}
}

func TestRunQueueMetrics_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	writeEvents(t, dir, []map[string]any{
		makeEscalation("pr_iteration_escalated", fixedNow().Add(-1*time.Hour), 100),
	})

	deps := queueMetricsDeps{
		chainDir:    dir,
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 10, nil },
	}
	var out, errBuf bytes.Buffer
	code := runQueueMetrics(context.Background(),
		[]string{"--repo", "chitinhq/chitin", "--format", "json", "--days", "3"},
		&out, &errBuf, deps)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitSuccess, errBuf.String())
	}

	var got queueMetricsResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v\noutput:\n%s", err, out.String())
	}
	if got.WindowDays != 3 {
		t.Errorf("window_days = %d, want 3", got.WindowDays)
	}
	if len(got.PerDay) != 3 {
		t.Errorf("per_day len = %d, want 3", len(got.PerDay))
	}
	if got.RawOpenPRs != 10 {
		t.Errorf("raw_open_prs = %d, want 10", got.RawOpenPRs)
	}
	if got.PerDay[len(got.PerDay)-1].QueueSize != 1 {
		t.Errorf("today bucket size = %d, want 1", got.PerDay[len(got.PerDay)-1].QueueSize)
	}
	if len(got.EventTypes) == 0 {
		t.Errorf("event_types_scanned should be non-empty")
	}
}

func TestRunQueueMetrics_TextFormatRendersGate(t *testing.T) {
	dir := t.TempDir()
	writeEvents(t, dir, []map[string]any{
		makeEscalation("pr_iteration_escalated", fixedNow().Add(-1*time.Hour), 100),
	})
	deps := queueMetricsDeps{
		chainDir:    dir,
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 10, nil },
	}
	var out, errBuf bytes.Buffer
	code := runQueueMetrics(context.Background(),
		[]string{"--repo", "chitinhq/chitin"}, &out, &errBuf, deps)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want %d; stderr=%q", code, exitSuccess, errBuf.String())
	}
	s := out.String()
	wantSubs := []string{
		"Operator queue cognitive-load metric (spec 114 SC-001)",
		"Window:",
		"Raw open PRs (today):    10",
		"Median queue size:",
		"Reduction:",
	}
	for _, sub := range wantSubs {
		if !strings.Contains(s, sub) {
			t.Errorf("text output missing %q\nfull output:\n%s", sub, s)
		}
	}
}

func TestRunQueueMetrics_FlagErrors(t *testing.T) {
	deps := queueMetricsDeps{
		chainDir:    t.TempDir(),
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 0, nil },
	}
	cases := []struct {
		name string
		args []string
	}{
		{"missing repo", []string{}},
		{"positional arg", []string{"--repo", "x/y", "extra"}},
		{"bad days", []string{"--repo", "x/y", "--days", "0"}},
		{"bad format", []string{"--repo", "x/y", "--format", "yaml"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := runQueueMetrics(context.Background(), tc.args, &out, &errBuf, deps)
			if code != exitUserError {
				t.Errorf("exit = %d, want %d; stderr=%q", code, exitUserError, errBuf.String())
			}
		})
	}
}

func TestRunQueueMetrics_OpenPRCountError(t *testing.T) {
	deps := queueMetricsDeps{
		chainDir:    t.TempDir(),
		now:         fixedNow,
		openPRCount: func(_ context.Context, _ string) (int, error) { return 0, fmt.Errorf("gh unreachable") },
	}
	var out, errBuf bytes.Buffer
	code := runQueueMetrics(context.Background(),
		[]string{"--repo", "x/y"}, &out, &errBuf, deps)
	if code != exitRuntimeError {
		t.Errorf("exit = %d, want %d", code, exitRuntimeError)
	}
	if !strings.Contains(errBuf.String(), "gh unreachable") {
		t.Errorf("stderr should mention error; got %q", errBuf.String())
	}
}

func TestMedianInt(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{nil, 0},
		{[]int{5}, 5},
		{[]int{3, 1, 2}, 2},
		{[]int{0, 0, 1, 1, 1, 2, 3}, 1},
		{[]int{1, 2, 3, 4}, 3}, // even: returns upper of two middles (index 2)
	}
	for _, tc := range cases {
		if got := medianInt(tc.in); got != tc.want {
			t.Errorf("medianInt(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
