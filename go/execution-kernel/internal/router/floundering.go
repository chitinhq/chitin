package router

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ChainEvent — minimal shape for floundering analysis. Mirrors the
// kernel's full event.Event but only the fields we need.
type ChainEvent struct {
	Ts        string                 `json:"ts"`
	EventType string                 `json:"event_type"`
	Payload   map[string]interface{} `json:"payload"`
}

// FlounderingThresholds holds the operator-tunable cutoffs.
type FlounderingThresholds struct {
	MaxLoopCount    int
	MaxStallSeconds int
}

// ReadChainEvents loads recent chain events for a session from
// ~/.chitin/events-<session_id>.jsonl. Returns empty slice (not
// nil, not error) on missing file — matches the TS behavior.
func ReadChainEvents(sessionID string) []ChainEvent {
	if sessionID == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(home, ".chitin", fmt.Sprintf("events-%s.jsonl", sessionID))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var events []ChainEvent
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev ChainEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// skip malformed lines
			continue
		}
		events = append(events, ev)
	}
	return events
}

// detectLoop returns (true, reason) if the most recent N tool calls
// share both tool_name AND non-empty action_target. Same-tool with
// no target is too loose to flag as a loop.
func detectLoop(events []ChainEvent, maxLoopCount int) (bool, string) {
	if len(events) < maxLoopCount {
		return false, ""
	}
	// Filter for decision events with tool_name AND non-empty
	// action_target — see TS counterpart for rationale.
	var recent []ChainEvent
	for _, ev := range events {
		if ev.EventType != "decision" {
			continue
		}
		toolName, _ := ev.Payload["tool_name"].(string)
		target, _ := ev.Payload["action_target"].(string)
		if toolName == "" || target == "" {
			continue
		}
		recent = append(recent, ev)
	}
	if len(recent) < maxLoopCount {
		return false, ""
	}
	recent = recent[len(recent)-maxLoopCount:]
	sig := func(e ChainEvent) string {
		t, _ := e.Payload["tool_name"].(string)
		tg, _ := e.Payload["action_target"].(string)
		return fmt.Sprintf("%s|%s", t, tg)
	}
	first := sig(recent[0])
	for _, e := range recent {
		if sig(e) != first {
			return false, ""
		}
	}
	short := first
	if len(short) > 80 {
		short = short[:80]
	}
	return true, fmt.Sprintf("looping-tool-call:%s-x%d", short, maxLoopCount)
}

// detectStall returns (true, reason) if no write-shape decisions
// have been seen in the last maxStallSeconds.
func detectStall(events []ChainEvent, maxStallSeconds int, now time.Time) (bool, string) {
	var writeEvents []ChainEvent
	for _, ev := range events {
		if ev.EventType != "decision" {
			continue
		}
		dec, _ := ev.Payload["decision"].(string)
		if dec != "allow" {
			continue
		}
		actionType, _ := ev.Payload["action_type"].(string)
		if actionType == "file.write" || actionType == "git.commit" || actionType == "git.push" {
			writeEvents = append(writeEvents, ev)
		}
	}
	if len(writeEvents) == 0 {
		// No writes ever — only flag if the session has been going long enough
		if len(events) == 0 {
			return false, ""
		}
		firstTs, err := time.Parse(time.RFC3339, events[0].Ts)
		if err != nil {
			return false, ""
		}
		elapsed := int(now.Sub(firstTs).Seconds())
		if elapsed > maxStallSeconds {
			return true, fmt.Sprintf("no-writes-in-%ds", elapsed)
		}
		return false, ""
	}
	lastWriteTs, err := time.Parse(time.RFC3339, writeEvents[len(writeEvents)-1].Ts)
	if err != nil {
		return false, ""
	}
	elapsed := int(now.Sub(lastWriteTs).Seconds())
	if elapsed > maxStallSeconds {
		return true, fmt.Sprintf("no-writes-since-%ds-ago", elapsed)
	}
	return false, ""
}

// detectDenialCascade returns (true, reason) if 4+ of the last 5
// decisions were denied — sign of confusion or floundering.
func detectDenialCascade(events []ChainEvent) (bool, string) {
	var recent []ChainEvent
	for _, ev := range events {
		if ev.EventType == "decision" {
			recent = append(recent, ev)
		}
	}
	if len(recent) < 5 {
		return false, ""
	}
	recent = recent[len(recent)-5:]
	denials := 0
	for _, e := range recent {
		dec, _ := e.Payload["decision"].(string)
		if dec == "deny" {
			denials++
		}
	}
	if denials >= 4 {
		return true, fmt.Sprintf("denial-cascade:%d-of-last-5", denials)
	}
	return false, ""
}

// DetectFloundering combines the three signals (loop, stall,
// denial-cascade) and returns the FIRST signal that fires.
// Priority: loop > stall > denial-cascade.
func DetectFloundering(events []ChainEvent, thresholds FlounderingThresholds, now time.Time) HeuristicScore {
	if fired, reason := detectLoop(events, thresholds.MaxLoopCount); fired {
		return HeuristicScore{Score: 1.0, Fired: true, Reason: reason}
	}
	if fired, reason := detectStall(events, thresholds.MaxStallSeconds, now); fired {
		return HeuristicScore{Score: 0.85, Fired: true, Reason: reason}
	}
	if fired, reason := detectDenialCascade(events); fired {
		return HeuristicScore{Score: 0.9, Fired: true, Reason: reason}
	}
	return HeuristicScore{Score: 0.0, Fired: false, Reason: "no-signals"}
}
