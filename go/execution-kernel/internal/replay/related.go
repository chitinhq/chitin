package replay

import (
	"os"
	"path/filepath"
	"strings"
)

// FindRelatedSessions finds chain session IDs likely related to
// a given (entry_id, file_paths) hint. Used by the dispatcher to
// inject prior-session summaries into the next agent's prompt
// (memory-context primitive).
//
// MVP heuristic — substring match on session_id OR file-path
// overlap. Returns up to maxResults session IDs, most-recent
// first.
//
// Future improvement: vector similarity over session embeddings
// (Hindsight-shape retrieval). For now: cheap deterministic
// substring + path match.
func FindRelatedSessions(entryID string, filePaths []string, maxResults int) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(home, ".chitin", "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}

	type candidate struct {
		path  string
		mtime int64
		match int // higher = more confident match
	}
	var candidates []candidate
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		base := filepath.Base(p)
		base = strings.TrimPrefix(base, "events-")
		base = strings.TrimSuffix(base, ".jsonl")

		score := 0
		// entry_id substring match in session_id (weak signal but
		// useful since the swarm dispatches with stable session_ids
		// like swarm-<entry-id>-<timestamp>)
		if entryID != "" && strings.Contains(base, entryID) {
			score += 5
		}
		// File-path heuristic — read first ~50 events and check if
		// any action_target contains a declared file path
		if len(filePaths) > 0 {
			data, err := os.ReadFile(p)
			if err == nil {
				lines := strings.Split(string(data), "\n")
				if len(lines) > 100 {
					lines = lines[:100]
				}
				for _, line := range lines {
					for _, fp := range filePaths {
						if fp == "" {
							continue
						}
						if strings.Contains(line, fp) {
							score++
							break
						}
					}
					if score > 5 {
						break
					}
				}
			}
		}
		if score > 0 {
			candidates = append(candidates, candidate{
				path: p, mtime: st.ModTime().Unix(), match: score,
			})
		}
	}

	// Sort: highest match first, then most-recent
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0; j-- {
			a, b := candidates[j-1], candidates[j]
			if a.match < b.match || (a.match == b.match && a.mtime < b.mtime) {
				candidates[j-1], candidates[j] = b, a
			} else {
				break
			}
		}
	}

	// Cap + extract session_ids
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		base := filepath.Base(c.path)
		base = strings.TrimPrefix(base, "events-")
		base = strings.TrimSuffix(base, ".jsonl")
		out = append(out, base)
	}
	return out, nil
}
