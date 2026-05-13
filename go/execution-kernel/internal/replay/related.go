package replay

import (
	"encoding/json"
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
	return FindRelatedSessionsIn("", entryID, filePaths, maxResults)
}

// FindRelatedSessionsIn is FindRelatedSessions with an explicit chain
// directory. Empty chitinDir resolves to ~/.chitin.
func FindRelatedSessionsIn(chitinDir, entryID string, filePaths []string, maxResults int) ([]string, error) {
	if chitinDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		chitinDir = filepath.Join(home, ".chitin")
	}
	pattern := filepath.Join(chitinDir, "events-*.jsonl")
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

// FindRelatedSessionsByKind returns sessions containing an event whose
// event_type (or legacy top-level kind) matches kind, newest files first.
func FindRelatedSessionsByKind(chitinDir, kind string, maxResults int) ([]string, error) {
	if kind == "" {
		return nil, nil
	}
	if chitinDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		chitinDir = filepath.Join(home, ".chitin")
	}
	pattern := filepath.Join(chitinDir, "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	type candidate struct {
		sessionID string
		mtime     int64
	}
	var candidates []candidate
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		matched := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var ev map[string]interface{}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			eventType, _ := ev["event_type"].(string)
			legacyKind, _ := ev["kind"].(string)
			if eventType == kind || legacyKind == kind {
				matched = true
				break
			}
		}
		if matched {
			base := filepath.Base(p)
			base = strings.TrimPrefix(base, "events-")
			base = strings.TrimSuffix(base, ".jsonl")
			candidates = append(candidates, candidate{sessionID: base, mtime: st.ModTime().Unix()})
		}
	}

	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0; j-- {
			if candidates[j-1].mtime < candidates[j].mtime {
				candidates[j-1], candidates[j] = candidates[j], candidates[j-1]
			} else {
				break
			}
		}
	}
	if maxResults > 0 && len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.sessionID)
	}
	return out, nil
}
