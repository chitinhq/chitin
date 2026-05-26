package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var autoMergeTerminal = map[string]string{
	"auto_merge_succeeded":  "succeeded",
	"auto_merge_failed":     "failed",
	"auto_merge_ci_failed":  "ci_failed",
	"auto_merge_conflict":   "conflict",
	"auto_merge_ci_timeout": "ci_timeout",
	"auto_merge_canceled":   "canceled",
}

type autoMergeChainEvent struct {
	Ts        string         `json:"ts"`
	EventType string         `json:"event_type"`
	RunID     string         `json:"run_id"`
	Payload   map[string]any `json:"payload"`
}

func findAutoMergeByTrigger(chainDir, deliveryID string) (string, bool, error) {
	events, err := readAutoMergeEvents(chainDir, "", 0)
	if err != nil {
		return "", false, err
	}
	var runID string
	for _, ev := range events {
		if ev.EventType == "auto_merge_triggered" && stringField(ev.Payload, "trigger_event_id") == deliveryID {
			runID = ev.RunID
		}
	}
	if runID == "" {
		return "", false, nil
	}
	outcome := "canceled"
	for _, ev := range events {
		if ev.RunID == runID {
			if v, ok := autoMergeTerminal[ev.EventType]; ok {
				outcome = v
			}
		}
	}
	return outcome, true, nil
}

func findLatestRunningAutoMerge(chainDir, repo string, prNumber int) (string, bool, error) {
	events, err := readAutoMergeEvents(chainDir, repo, prNumber)
	if err != nil {
		return "", false, err
	}
	running := map[string]bool{}
	var latestID string
	var latest time.Time
	for _, ev := range events {
		if ev.EventType == "auto_merge_triggered" {
			running[ev.RunID] = true
			ts, _ := time.Parse(time.RFC3339Nano, ev.Ts)
			if latestID == "" || ts.After(latest) {
				latestID, latest = ev.RunID, ts
			}
		}
		if _, ok := autoMergeTerminal[ev.EventType]; ok {
			running[ev.RunID] = false
		}
	}
	if latestID == "" || !running[latestID] {
		return "", false, nil
	}
	return latestID, true, nil
}

func readAutoMergeEvents(chainDir, repo string, prNumber int) ([]autoMergeChainEvent, error) {
	if chainDir == "" {
		chainDir = resolveChainDir()
	}
	matches, err := filepath.Glob(filepath.Join(chainDir, "events-*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []autoMergeChainEvent
	for _, path := range matches {
		events, err := readAutoMergeEventsFile(path, repo, prNumber)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		out = append(out, events...)
	}
	return out, nil
}

func readAutoMergeEventsFile(path, repo string, prNumber int) ([]autoMergeChainEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []autoMergeChainEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), "auto_merge_") {
			continue
		}
		var ev autoMergeChainEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if !strings.HasPrefix(ev.EventType, "auto_merge_") {
			continue
		}
		if repo != "" && stringField(ev.Payload, "repo") != repo {
			continue
		}
		if prNumber > 0 && intField(ev.Payload, "pr_number") != prNumber {
			continue
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func intField(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
