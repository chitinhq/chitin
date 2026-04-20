// Package health gathers dogfooding health metrics.
package health

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Report is the shape `chitin health` presents.
type Report struct {
	WindowStart        time.Time      `json:"window_start"`
	DirExists          bool           `json:"dir_exists"`
	EventsByWindow     map[string]int `json:"events_by_window"`
	EventsTotal        int            `json:"events_total"`
	HookFailureCount   int            `json:"hook_failure_count"`
	SchemaDriftCount   int            `json:"schema_drift_count"`
	OrphanedChains     int            `json:"orphaned_chains"`
	LatestEventTs      time.Time      `json:"latest_event_ts,omitempty"`
	ClockSkewSuspected bool           `json:"clock_skew_suspected"`
	// FailedFiles records jsonl files whose scan failed with a non-ErrNotExist
	// open error or a scanner error. Each entry is "<path>: <error message>".
	// The remaining files are still scanned — one bad file must not black-box
	// the health signal for the rest.
	FailedFiles []string `json:"failed_files,omitempty"`
}

// clockSkewFutureTolerance bounds how far ahead of wall-clock an event ts may
// be before we flag it. 1h absorbs NTP jitter + cross-box clock drift without
// swallowing real skew (NTP resync across DST, resumed laptop, container
// with bad epoch).
const clockSkewFutureTolerance = 1 * time.Hour

// Gather scans a single .chitin directory and produces a Report for the
// window ending now and lasting `window` duration.
func Gather(chitinDir string, window time.Duration) (Report, error) {
	now := time.Now().UTC()
	r := Report{
		WindowStart:    now.Add(-window),
		EventsByWindow: map[string]int{},
	}

	entries, err := os.ReadDir(chitinDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return r, nil
		}
		return r, fmt.Errorf("read .chitin dir: %w", err)
	}
	r.DirExists = true

	skewThreshold := now.Add(clockSkewFutureTolerance)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		path := filepath.Join(chitinDir, name)
		if err := scanJSONL(path, &r, now, skewThreshold); err != nil {
			r.FailedFiles = append(r.FailedFiles, fmt.Sprintf("%s: %s", path, err))
			continue
		}
	}

	errLog := filepath.Join(chitinDir, "kernel-errors.log")
	if err := scanErrorLog(errLog, &r); err != nil {
		r.FailedFiles = append(r.FailedFiles, fmt.Sprintf("%s: %s", errLog, err))
	}
	return r, nil
}

// Invariant: an event counts toward EventsTotal/EventsByWindow iff it parses,
// has schema_version == "2", has a non-empty surface, and has ts inside the
// window [WindowStart, now]. Any other shape is schema drift (bumped exactly
// once per line) and does not count as a real event.
//
// Two separate rules govern future-stamped events:
//   - Any event with t > now is excluded from EventsTotal / EventsByWindow
//     (the window is half-open on the future side).
//   - Only events with t > skewThreshold (now + clockSkewFutureTolerance)
//     also set ClockSkewSuspected. Events between now and skewThreshold are
//     silently excluded without flagging — they're within NTP jitter range.
//
// now and skewThreshold are pinned per Gather call so behavior is
// deterministic within a single scan.
func scanJSONL(path string, r *Report, now, skewThreshold time.Time) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open jsonl %q: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var ev struct {
			TS      string `json:"ts"`
			Surface string `json:"surface"`
			Schema  string `json:"schema_version"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			r.SchemaDriftCount++
			continue
		}
		if ev.Schema != "2" {
			r.SchemaDriftCount++
			continue
		}
		if ev.Surface == "" {
			r.SchemaDriftCount++
			continue
		}
		t, err := time.Parse(time.RFC3339, ev.TS)
		if err != nil {
			r.SchemaDriftCount++
			continue
		}
		if t.After(r.LatestEventTs) {
			r.LatestEventTs = t
		}
		if t.After(skewThreshold) {
			r.ClockSkewSuspected = true
		}
		if t.Before(r.WindowStart) || t.After(now) {
			continue
		}
		r.EventsTotal++
		r.EventsByWindow[ev.Surface]++
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan jsonl %q: %w", path, err)
	}
	return nil
}

func scanErrorLog(path string, r *Report) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open error log %q: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		r.HookFailureCount++
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan error log %q: %w", path, err)
	}
	return nil
}
