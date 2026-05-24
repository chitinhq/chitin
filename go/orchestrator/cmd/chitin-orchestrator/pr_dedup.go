// pr_dedup.go — spec 099 slice 4: chain-query dedup for the
// /webhook/pr route's copilot_pr_detected idempotency invariant
// (FR-008, SC-003).
//
// Approach (per research.md R3): scan ~/.chitin/events-*.jsonl
// read-only for an existing copilot_pr_detected with matching
// (repo, pr_number). This is constitutional under §1 — the kernel
// owns the WRITE path; READ-only scan from any side is fine.

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// hasPriorPRDetection reports whether the chain already contains a
// copilot_pr_detected event for the given (repo, pr_number). Used by
// the handlePR dedup gate before emitting / starting workflow per
// FR-008.
//
// Returns (false, nil) when no match is found OR when the chain dir
// doesn't exist yet (fresh operator host with zero events). Returns
// (false, err) only on unexpected IO errors — callers SHOULD log and
// proceed (fail-open: re-emitting a duplicate event is better than
// silently dropping a real detection).
//
// The chain dir is resolved via the same precedence the kernel uses:
//
//	1. $CHITIN_DIR if set (matches emitChainEvent's lookup)
//	2. $HOME/.chitin
//	3. ./.chitin as last-resort fallback
func hasPriorPRDetection(chainDir, repo string, prNumber int) (bool, error) {
	if chainDir == "" {
		chainDir = resolveChainDir()
	}
	pattern := filepath.Join(chainDir, "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return false, err
	}
	if len(matches) == 0 {
		return false, nil
	}
	for _, path := range matches {
		found, err := scanFileForDetection(path, repo, prNumber)
		if err != nil {
			// Tolerate per-file errors (e.g. concurrent rotation) but
			// surface the first non-NotExist err to the caller after
			// the loop so operators see it via the warn-on-emit path.
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// resolveChainDir mirrors emitChainEvent's resolution so the dedup
// scanner reads from the same place the emitter writes.
func resolveChainDir() string {
	if d := os.Getenv("CHITIN_DIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.chitin"
	}
	return ".chitin"
}

// scanFileForDetection walks one jsonl file looking for a
// copilot_pr_detected event matching (repo, pr_number). Streamed line
// reader; never loads the whole file into memory.
func scanFileForDetection(path, repo string, prNumber int) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// Allow up to 1 MiB per event line — kernel-side cap is similar.
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Cheap pre-filter to avoid JSON parse on every line.
		if !strings.Contains(string(line), "copilot_pr_detected") {
			continue
		}
		var ev struct {
			EventType string `json:"event_type"`
			Payload   struct {
				Repo     string `json:"repo"`
				PRNumber int    `json:"pr_number"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip malformed lines; per-line tolerant per spec 097 D8
		}
		if ev.EventType == "copilot_pr_detected" &&
			ev.Payload.Repo == repo &&
			ev.Payload.PRNumber == prNumber {
			return true, nil
		}
	}
	return false, sc.Err()
}
