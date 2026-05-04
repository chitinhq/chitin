package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Stats — aggregate chain analytics keyed by an axis.
type Stats struct {
	Axis    string                 `json:"axis"`
	Buckets map[string]BucketStats `json:"buckets"`
	Total   int                    `json:"total_decisions"`
	Window  string                 `json:"window,omitempty"`
}

// BucketStats — per-axis-bucket counts.
type BucketStats struct {
	Decisions int `json:"decisions"`
	Allows    int `json:"allows"`
	Denies    int `json:"denies"`
	// SuccessRate is allows / decisions (0.0-1.0). NaN-safe: 0
	// when decisions=0.
	SuccessRate float64 `json:"success_rate"`
}

// SupportedAxes is the closed set of axis labels accepted by
// ComputeStats. Exposed so the CLI can validate `--by=...` up
// front and produce a useful error instead of silently returning
// an empty bucket map.
var SupportedAxes = []string{"tool_name", "action_type", "rule_id", "decision", "agent"}

// IsSupportedAxis reports whether a is a known axis label.
func IsSupportedAxis(a string) bool {
	for _, s := range SupportedAxes {
		if s == a {
			return true
		}
	}
	return false
}

// ComputeStats reads all chain JSONL files in ~/.chitin/ and
// aggregates decisions by axis. Axis must be one of SupportedAxes;
// an unknown axis returns an explicit error rather than silently
// returning empty results (which would mask CLI typos).
//
// Axis labels:
//   - tool_name      (which Claude Code tool)
//   - action_type    (which gov action)
//   - rule_id        (which policy rule)
//   - decision       (allow/deny — pivot for sanity-check)
//   - agent          (agent_instance_id)
func ComputeStats(axis string) (*Stats, error) {
	return ComputeStatsIn(axis, "")
}

// ComputeStatsIn is ComputeStats with an explicit chain dir. Empty
// chitinDir resolves to ~/.chitin (the default). The override
// path exists for tests + advanced operator scenarios; production
// CLI use should call ComputeStats.
func ComputeStatsIn(axis, chitinDir string) (*Stats, error) {
	if !IsSupportedAxis(axis) {
		return nil, fmt.Errorf("unsupported axis %q (want one of: %s)",
			axis, strings.Join(SupportedAxes, ", "))
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
	stats := &Stats{
		Axis:    axis,
		Buckets: make(map[string]BucketStats),
	}
	for _, p := range matches {
		// Note: we read the whole file and split on newlines for
		// simplicity. For typical chain logs this is well under 5MB
		// per file. Streaming via bufio.Scanner would help if a
		// long-running install accumulates >100MB per session,
		// which is not the current regime — measure before
		// switching.
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var ev map[string]interface{}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			etype, _ := ev["event_type"].(string)
			if etype != "decision" {
				continue
			}
			payload, _ := ev["payload"].(map[string]interface{})
			if payload == nil {
				continue
			}
			bucket := extractAxis(ev, payload, axis)
			if bucket == "" {
				continue
			}
			stats.Total++
			b := stats.Buckets[bucket]
			b.Decisions++
			if dec, _ := payload["decision"].(string); dec == "allow" {
				b.Allows++
			} else if dec == "deny" {
				b.Denies++
			}
			stats.Buckets[bucket] = b
		}
	}
	// Compute success rates
	for k, b := range stats.Buckets {
		if b.Decisions > 0 {
			b.SuccessRate = float64(b.Allows) / float64(b.Decisions)
		}
		stats.Buckets[k] = b
	}
	return stats, nil
}

func extractAxis(ev, payload map[string]interface{}, axis string) string {
	switch axis {
	case "tool_name":
		s, _ := payload["tool_name"].(string)
		return s
	case "action_type":
		s, _ := payload["action_type"].(string)
		return s
	case "rule_id":
		s, _ := payload["rule_id"].(string)
		return s
	case "decision":
		s, _ := payload["decision"].(string)
		return s
	case "agent":
		s, _ := ev["agent_instance_id"].(string)
		return s
	}
	return ""
}

// SortedBucketKeys returns bucket keys sorted by decision count
// descending (most-frequent first), with bucket-name lexicographic
// as the tie-breaker. The named tie-breaker matters because two
// runs over the same data must produce identical CLI output —
// without it, ties resolve via Go map iteration order, which is
// randomized.
func (s *Stats) SortedBucketKeys() []string {
	keys := make([]string, 0, len(s.Buckets))
	for k := range s.Buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		bi, bj := s.Buckets[keys[i]], s.Buckets[keys[j]]
		if bi.Decisions != bj.Decisions {
			return bi.Decisions > bj.Decisions
		}
		return keys[i] < keys[j]
	})
	return keys
}
