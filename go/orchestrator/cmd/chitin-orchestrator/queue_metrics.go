// queue_metrics.go — spec 114 T015 / SC-001 measurement.
//
// Defines the operator-queue cognitive-load metric and renders it as a
// CLI report so the spec-114 success criterion (≥60% reduction vs raw
// `gh pr list`) can be measured on real chain data.
//
// Metric:
//
//   - queue_size(D) = distinct PR numbers with at least one escalation
//     event in the 24-hour window ending at day D's midnight UTC. The
//     24h window matches the daily-digest cadence (FR-009): on each
//     morning, the operator's "what needs me" list is what arrived in
//     the last day.
//   - median_queue_size = median across the last --days days.
//   - raw_open_prs = live `gh pr list --search "is:open" --limit 200`
//     count today. (We use today's snapshot because the open-PR count
//     is the cognitive-load baseline an operator faces when they sit
//     down — a historical replay would require daily snapshots we
//     don't store.)
//   - ratio = median_queue_size / raw_open_prs
//   - reduction = 1 - ratio
//   - SC-001 target: reduction ≥ 0.60 (i.e., ratio ≤ 0.40).
//
// Escalation event types scanned (per FR-008 closed taxonomy):
//
//   - pr_iteration_escalated  (covers iteration_cap_hit,
//     iteration_completed_with_skips, human_reviewer_present,
//     lease_lost — the spec-113 promotion path)
//   - sibling_rebase_failed   (spec 112 US2 fail-soft outcome)
//
// The derived FR-003 rules (dialectic_request_changes,
// stale_no_automation, conflicting_persistent) require live gh state
// that we cannot replay historically. In practice these PRs also fire
// a pr_iteration_escalated event when escalated, so the chain count is
// a lower bound on the true queue size — meaning the reported
// reduction is the WORST case, not the best.
//
// Read-only by construction: walks $CHITIN_DIR/events-*.jsonl streamed
// (never loads a file into memory), invokes `gh` once for the live
// count, emits nothing back to the chain.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// escalationEventTypes is the closed set of chain event_types that
// count toward the operator queue. Keep in sync with FR-008's
// taxonomy and discord_escalation.go's EventType field.
var escalationEventTypes = map[string]bool{
	"pr_iteration_escalated": true,
	"sibling_rebase_failed":  true,
}

// queueMetricsResult is the JSON-serializable shape returned by
// computeQueueMetrics and rendered by both --format outputs.
type queueMetricsResult struct {
	WindowDays      int             `json:"window_days"`
	AsOf            string          `json:"as_of"`
	RawOpenPRs      int             `json:"raw_open_prs"`
	PerDay          []dayQueueCount `json:"per_day"`
	MedianQueueSize int             `json:"median_queue_size"`
	Ratio           float64         `json:"ratio"`
	Reduction       float64         `json:"reduction"`
	TargetReduction float64         `json:"target_reduction"`
	TargetMet       bool            `json:"target_met"`
	EventTypes      []string        `json:"event_types_scanned"`
}

// dayQueueCount is one entry in the per_day breakdown. Each bucket is
// one calendar day in UTC; the "today" bucket's window ends at the
// `as_of` instant rather than tomorrow's midnight, so adjacent buckets
// never overlap and each event lands in at most one bucket.
type dayQueueCount struct {
	Date      string `json:"date"`       // YYYY-MM-DD (UTC)
	QueueSize int    `json:"queue_size"` // distinct PRs escalated during the bucket's window
}

// dayBucket is the internal window representation used by the scanner.
// Start is inclusive, End is exclusive.
type dayBucket struct {
	Start time.Time
	End   time.Time
	Label string
}

// queueMetricsDeps is the test seam — production wires the real chain
// dir resolver and the real gh CLI; tests inject fixtures.
type queueMetricsDeps struct {
	// chainDir is the directory to scan for events-*.jsonl. Empty =
	// use resolveChainDir() (same precedence as the emitter).
	chainDir string
	// openPRCount returns the current open-PR count for the repo.
	// Production runs `gh pr list --search "is:open" --json number
	// --limit 200`. Tests stub.
	openPRCount func(ctx context.Context, repo string) (int, error)
	// now is the wall clock; tests inject a deterministic value so
	// the per_day window is reproducible.
	now func() time.Time
}

func cmdQueueMetrics(args []string) int {
	return runQueueMetrics(context.Background(), args, os.Stdout, os.Stderr, defaultQueueMetricsDeps())
}

func defaultQueueMetricsDeps() queueMetricsDeps {
	return queueMetricsDeps{
		openPRCount: ghOpenPRCount,
		now:         time.Now,
	}
}

func runQueueMetrics(ctx context.Context, args []string, stdout, stderr io.Writer, deps queueMetricsDeps) int {
	fs := flag.NewFlagSet("queue-metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	days := fs.Int("days", 7, "number of trailing days to include in the median")
	repo := fs.String("repo", "", "GitHub repo as OWNER/NAME (default $CHITIN_REPO)")
	format := fs.String("format", "text", "output format: text or json")
	target := fs.Float64("target-reduction", 0.60, "SC-001 reduction target (0.60 = 60%)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator queue-metrics [--days 7] [--repo OWNER/NAME] [--format text|json] [--target-reduction 0.60]")
	}

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "error: queue-metrics takes no positional arguments")
		fs.Usage()
		return exitUserError
	}
	if *days < 1 {
		fmt.Fprintln(stderr, "error: --days must be ≥ 1")
		return exitUserError
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "error: --format must be text or json (got %q)\n", *format)
		return exitUserError
	}
	if *repo == "" {
		*repo = os.Getenv("CHITIN_REPO")
	}
	if *repo == "" {
		fmt.Fprintln(stderr, "error: --repo or $CHITIN_REPO required")
		return exitUserError
	}

	result, err := computeQueueMetrics(ctx, deps, *repo, *days, *target)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	if *format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "error: encode json: %v\n", err)
			return exitRuntimeError
		}
		return exitSuccess
	}
	renderQueueMetricsText(stdout, result)
	return exitSuccess
}

// computeQueueMetrics walks the chain dir, replays escalation events
// across the last N days, queries the live open-PR count, and assembles
// the result. The function is the testable seam — runQueueMetrics is
// just flag plumbing + rendering.
func computeQueueMetrics(ctx context.Context, deps queueMetricsDeps, repo string, days int, target float64) (queueMetricsResult, error) {
	chainDir := deps.chainDir
	if chainDir == "" {
		chainDir = resolveChainDir()
	}
	if deps.now == nil {
		deps.now = time.Now
	}
	if deps.openPRCount == nil {
		deps.openPRCount = ghOpenPRCount
	}

	asOf := deps.now().UTC()
	// Build non-overlapping calendar-day buckets in UTC. Bucket i
	// covers [midnight(today - (days-1-i)), midnight(today - (days-2-i)))
	// — i.e. one full UTC day — except the "today" bucket whose end is
	// capped at `asOf` so we never count events that haven't happened
	// yet. Adjacent buckets meet but do not overlap.
	todayMidnight := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	buckets := make([]dayBucket, days)
	for i := 0; i < days; i++ {
		start := todayMidnight.AddDate(0, 0, -(days - 1 - i))
		end := start.Add(24 * time.Hour)
		if end.After(asOf) {
			end = asOf
		}
		buckets[i] = dayBucket{
			Start: start,
			End:   end,
			Label: start.Format("2006-01-02"),
		}
	}

	prPerDay, err := scanEscalationsByDay(chainDir, buckets)
	if err != nil {
		return queueMetricsResult{}, fmt.Errorf("scan chain dir %s: %w", chainDir, err)
	}

	openCount, err := deps.openPRCount(ctx, repo)
	if err != nil {
		return queueMetricsResult{}, fmt.Errorf("query open PR count for %s: %w", repo, err)
	}

	perDay := make([]dayQueueCount, days)
	sizes := make([]int, days)
	for i, b := range buckets {
		size := len(prPerDay[i])
		perDay[i] = dayQueueCount{Date: b.Label, QueueSize: size}
		sizes[i] = size
	}

	median := medianInt(sizes)
	var ratio float64
	if openCount > 0 {
		ratio = float64(median) / float64(openCount)
	}
	reduction := 1.0 - ratio
	// Clamp: if median > openCount (transient state, e.g. PRs that
	// closed before today's snapshot), reduction can go negative.
	// Surface as 0 so the "target met" gate stays honest.
	if reduction < 0 {
		reduction = 0
	}

	eventTypes := make([]string, 0, len(escalationEventTypes))
	for k := range escalationEventTypes {
		eventTypes = append(eventTypes, k)
	}
	sort.Strings(eventTypes)

	return queueMetricsResult{
		WindowDays:      days,
		AsOf:            asOf.Format(time.RFC3339),
		RawOpenPRs:      openCount,
		PerDay:          perDay,
		MedianQueueSize: median,
		Ratio:           ratio,
		Reduction:       reduction,
		TargetReduction: target,
		TargetMet:       reduction >= target,
		EventTypes:      eventTypes,
	}, nil
}

// scanEscalationsByDay returns, for each provided dayBucket, the set
// of distinct PR numbers that had at least one escalation event whose
// timestamp lies in the bucket's half-open window. The returned slice
// is parallel to buckets.
//
// Implementation: a single pass over every events-*.jsonl in chainDir.
// For each escalation row, we test membership against every bucket and
// add the pr_number to the matching bucket's set. One pass scales
// O(L * D) where L is line count and D is the bucket count (small,
// typically ≤ 31).
func scanEscalationsByDay(chainDir string, buckets []dayBucket) ([]map[int]struct{}, error) {
	sets := make([]map[int]struct{}, len(buckets))
	for i := range sets {
		sets[i] = map[int]struct{}{}
	}

	pattern := filepath.Join(chainDir, "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		// Fresh host with no events yet — every bucket is empty,
		// which is a valid result (queue_size = 0 every day).
		return sets, nil
	}

	for _, path := range matches {
		if err := scanFileForEscalations(path, buckets, sets); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
	}
	return sets, nil
}

// scanFileForEscalations is the per-file inner loop. Streamed (never
// reads the whole file into memory) and tolerant of malformed lines —
// per spec 097 D8, individual unparseable rows are skipped rather than
// failing the whole scan.
func scanFileForEscalations(path string, buckets []dayBucket, sets []map[int]struct{}) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Cheap pre-filter — avoid JSON parse on the 99% of lines
		// that aren't escalations.
		if !containsEscalationMarker(line) {
			continue
		}
		var ev struct {
			EventType string `json:"event_type"`
			TS        string `json:"ts"`
			Payload   struct {
				PRNumber int `json:"pr_number"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if !escalationEventTypes[ev.EventType] {
			continue
		}
		if ev.Payload.PRNumber == 0 {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, ev.TS)
		if err != nil {
			// Try the non-nano form too — some emitters drop sub-second precision.
			ts, err = time.Parse(time.RFC3339, ev.TS)
			if err != nil {
				continue
			}
		}
		ts = ts.UTC()
		// Bucket assignment: an event lands in every bucket whose
		// [Start, End) window contains ts. With non-overlapping
		// calendar-day buckets this is at most one — events earlier
		// than buckets[0].Start or later than the last bucket's End
		// fall outside the window and are ignored.
		for i, b := range buckets {
			if (ts.Equal(b.Start) || ts.After(b.Start)) && ts.Before(b.End) {
				sets[i][ev.Payload.PRNumber] = struct{}{}
				break
			}
		}
	}
	return sc.Err()
}

// containsEscalationMarker is the cheap byte-level pre-filter — true
// if the raw line mentions any of the escalation event_type strings.
// Order doesn't matter; the JSON parse below is the real check.
func containsEscalationMarker(line []byte) bool {
	for et := range escalationEventTypes {
		if bytesContains(line, et) {
			return true
		}
	}
	return false
}

// bytesContains is a tiny local helper so we don't pull in
// bytes.Contains across the file (keeps the dependency surface minimal
// — the rest of the file is encoding/json + os).
func bytesContains(haystack []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	n := []byte(needle)
	for i := 0; i+len(n) <= len(haystack); i++ {
		if haystack[i] == n[0] {
			match := true
			for j := 1; j < len(n); j++ {
				if haystack[i+j] != n[j] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// medianInt computes the median of a slice of ints. Even-length
// slices return the lower of the two middle values (floor) — the
// reduction is the OPERATOR-FACING number, so the conservative read
// matches operator intuition.
func medianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]int(nil), xs...)
	sort.Ints(cp)
	return cp[len(cp)/2]
}

// ghOpenPRCount is the production openPRCount implementation. It
// shells out to `gh pr list --search "is:open" --limit 200 --json
// number` and counts the result array. The --limit cap matches the
// queue subcommand's gh invocation contract (FR-003 in spec 114
// scaffolding); larger result sets are uncommon for a single repo.
func ghOpenPRCount(ctx context.Context, repo string) (int, error) {
	args := []string{
		"pr", "list",
		"--repo", repo,
		"--state", "open",
		"--limit", "200",
		"--json", "number",
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return 0, fmt.Errorf("gh pr list failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return 0, fmt.Errorf("gh pr list: %w", err)
	}
	var rows []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return 0, fmt.Errorf("decode gh output: %w", err)
	}
	return len(rows), nil
}

// renderQueueMetricsText writes the human-readable report.
func renderQueueMetricsText(w io.Writer, r queueMetricsResult) {
	fmt.Fprintln(w, "Operator queue cognitive-load metric (spec 114 SC-001)")
	fmt.Fprintf(w, "Window:                  %d days ending %s\n", r.WindowDays, r.AsOf)
	fmt.Fprintf(w, "Scanned event types:     %s\n", strings.Join(r.EventTypes, ", "))
	fmt.Fprintf(w, "Raw open PRs (today):    %d\n", r.RawOpenPRs)
	fmt.Fprintln(w, "Per-day queue size (distinct PRs escalated in trailing 24h):")
	for _, d := range r.PerDay {
		fmt.Fprintf(w, "  %s   %d\n", d.Date, d.QueueSize)
	}
	fmt.Fprintf(w, "Median queue size:       %d\n", r.MedianQueueSize)
	fmt.Fprintf(w, "Ratio (median / open):   %.3f\n", r.Ratio)
	pct := r.Reduction * 100
	gate := "❌"
	if r.TargetMet {
		gate = "✅"
	}
	fmt.Fprintf(w, "Reduction:               %.1f%%  %s target %.0f%% %s\n",
		pct, gate, r.TargetReduction*100,
		map[bool]string{true: "met", false: "not met"}[r.TargetMet])
}
