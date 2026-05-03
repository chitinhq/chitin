package replay

import (
	"encoding/json"
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

// ComputeStats reads all chain JSONL files in ~/.chitin/ and
// aggregates decisions by axis. Axis can be:
//   - tool_name      (which Claude Code tool)
//   - action_type    (which gov action)
//   - rule_id        (which policy rule)
//   - decision       (allow/deny — pivot for sanity-check)
//   - agent          (agent_instance_id)
func ComputeStats(axis string) (*Stats, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(home, ".chitin", "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	stats := &Stats{
		Axis:    axis,
		Buckets: make(map[string]BucketStats),
	}
	for _, p := range matches {
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
// descending (most-frequent first).
func (s *Stats) SortedBucketKeys() []string {
	keys := make([]string, 0, len(s.Buckets))
	for k := range s.Buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return s.Buckets[keys[i]].Decisions > s.Buckets[keys[j]].Decisions
	})
	return keys
}
