package router

import (
	"testing"
	"time"
)

func TestDetectFloundering_Empty(t *testing.T) {
	res := DetectFloundering(nil, FlounderingThresholds{MaxLoopCount: 3, MaxStallSeconds: 600}, time.Now())
	if res.Fired {
		t.Errorf("Fired=true on empty events; want false")
	}
	if res.Reason != "no-signals" {
		t.Errorf("reason=%q want no-signals", res.Reason)
	}
}

func TestDetectFloundering_Loop(t *testing.T) {
	ev := func(target string) ChainEvent {
		return ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"tool_name":     "Bash",
				"action_target": target,
				"decision":      "allow",
			},
		}
	}
	res := DetectFloundering(
		[]ChainEvent{ev("rm /tmp/x"), ev("rm /tmp/x"), ev("rm /tmp/x")},
		FlounderingThresholds{MaxLoopCount: 3, MaxStallSeconds: 600},
		mustParse("2026-05-03T20:00:30Z"),
	)
	if !res.Fired {
		t.Error("Fired=false; want true (loop detected)")
	}
	if res.Score != 1.0 {
		t.Errorf("score=%v want 1.0", res.Score)
	}
}

func TestDetectFloundering_VaryingTargets(t *testing.T) {
	ev := func(target string) ChainEvent {
		return ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"tool_name":     "Bash",
				"action_target": target,
				"decision":      "allow",
			},
		}
	}
	res := DetectFloundering(
		[]ChainEvent{ev("a"), ev("b"), ev("c")},
		FlounderingThresholds{MaxLoopCount: 3, MaxStallSeconds: 600},
		mustParse("2026-05-03T20:00:30Z"),
	)
	if res.Fired {
		t.Errorf("Fired=true on varying targets; want false (reason=%q)", res.Reason)
	}
}

func TestDetectFloundering_Stall(t *testing.T) {
	events := []ChainEvent{
		{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"tool_name":   "Read",
				"action_type": "file.read",
				"decision":    "allow",
			},
		},
	}
	// 700s after the only event; max_stall_seconds=600
	res := DetectFloundering(events,
		FlounderingThresholds{MaxLoopCount: 3, MaxStallSeconds: 600},
		mustParse("2026-05-03T20:11:40Z"),
	)
	if !res.Fired {
		t.Error("Fired=false; want true (stall detected)")
	}
	if res.Reason[:9] != "no-writes" {
		t.Errorf("reason=%q want prefix no-writes", res.Reason)
	}
}

func TestDetectFloundering_DenialCascade(t *testing.T) {
	denial := func(target string) ChainEvent {
		return ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"tool_name":     "Bash",
				"action_target": target,
				"decision":      "deny",
				"rule_id":       "no-rm-recursive",
			},
		}
	}
	allow := ChainEvent{
		Ts:        "2026-05-03T20:00:00Z",
		EventType: "decision",
		Payload: map[string]interface{}{
			"tool_name":     "Read",
			"action_target": "/tmp/x",
			"decision":      "allow",
		},
	}
	res := DetectFloundering(
		[]ChainEvent{allow, denial("c1"), denial("c2"), denial("c3"), denial("c4")},
		FlounderingThresholds{MaxLoopCount: 3, MaxStallSeconds: 600},
		mustParse("2026-05-03T20:00:30Z"),
	)
	if !res.Fired {
		t.Error("Fired=false; want true (denial cascade)")
	}
	if res.Reason[:15] != "denial-cascade:" {
		t.Errorf("reason=%q want prefix denial-cascade:", res.Reason)
	}
}

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
