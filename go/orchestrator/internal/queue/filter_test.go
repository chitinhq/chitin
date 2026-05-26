package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildSilentDropWithPRJoinsLivePR(t *testing.T) {
	ts := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(map[string]any{
		"pr_number":        123,
		"spec_ref":         "118-test",
		"task_id":          "T008",
		"work_unit_id":     "wu-118-T008",
		"deliverable_kind": "pr",
		"reason":           "gh_pr_create_failed",
	})
	entries := Build(map[int][]EscalationEvent{
		123: {{
			EventType: "work_unit_completed_without_deliverable",
			Reason:    "silent_drop",
			PRNumber:  123,
			SpecRef:   "118-test",
			TaskID:    "T008",
			Ts:        ts,
			RunID:     "run-123",
			Payload:   payload,
		}},
	}, []LivePR{{
		Number:    123,
		Title:     "Delivery PR",
		SpecRef:   "live-spec",
		UpdatedAt: ts,
	}}, ts)
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1", len(entries))
	}
	e := entries[0]
	if e.PRNumber != 123 || e.Reason != "silent_drop" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.TaskID != "T008" || e.SpecRef != "118-test" {
		t.Fatalf("silent-drop payload identity should win; got spec=%q task=%q", e.SpecRef, e.TaskID)
	}
	if e.Title != "Delivery PR" {
		t.Fatalf("title=%q", e.Title)
	}
}

func TestBuildSilentDropWithoutPRUsesSpecTaskIdentity(t *testing.T) {
	ts := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(map[string]any{
		"spec_ref":         "118-test",
		"task_id":          "T009",
		"work_unit_id":     "wu-118-T009",
		"deliverable_kind": "pr",
		"reason":           "activity_declined_without_failure",
	})
	entries := Build(map[int][]EscalationEvent{
		-1: {{
			EventType: "work_unit_completed_without_deliverable",
			Reason:    "silent_drop",
			PRNumber:  0,
			SpecRef:   "118-test",
			TaskID:    "T009",
			Ts:        ts,
			RunID:     "wu-118-T009",
			Payload:   payload,
		}},
	}, nil, ts)
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1", len(entries))
	}
	e := entries[0]
	if e.PRNumber != 0 || e.SpecRef != "118-test" || e.TaskID != "T009" || e.Reason != "silent_drop" {
		t.Fatalf("unexpected no-PR entry: %+v", e)
	}
}
