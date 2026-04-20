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
	WindowStart      time.Time      `json:"window_start"`
	EventsByWindow   map[string]int `json:"events_by_window"`
	EventsTotal      int            `json:"events_total"`
	HookFailureCount int            `json:"hook_failure_count"`
	SchemaDriftCount int            `json:"schema_drift_count"`
	OrphanedChains   int            `json:"orphaned_chains"`
}

// Gather scans a single .chitin directory and produces a Report for the
// window ending now and lasting `window` duration.
func Gather(chitinDir string, window time.Duration) (Report, error) {
	r := Report{
		WindowStart:    time.Now().Add(-window).UTC(),
		EventsByWindow: map[string]int{},
	}

	entries, err := os.ReadDir(chitinDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, fmt.Errorf("read .chitin dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if err := scanJSONL(filepath.Join(chitinDir, name), &r); err != nil {
			return r, err
		}
	}

	errLog := filepath.Join(chitinDir, "kernel-errors.log")
	if err := scanErrorLog(errLog, &r); err != nil {
		return r, err
	}
	return r, nil
}

func scanJSONL(path string, r *Report) error {
	f, err := os.Open(path)
	if err != nil {
		return nil // missing jsonl is fine
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24) // allow long lines
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
		if ev.Schema != "" && ev.Schema != "2" {
			r.SchemaDriftCount++
		}
		t, err := time.Parse(time.RFC3339, ev.TS)
		if err != nil {
			continue
		}
		if t.Before(r.WindowStart) {
			continue
		}
		r.EventsTotal++
		r.EventsByWindow[ev.Surface]++
	}
	return sc.Err()
}

func scanErrorLog(path string, r *Report) error {
	f, err := os.Open(path)
	if err != nil {
		return nil // missing is fine
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
	return sc.Err()
}
