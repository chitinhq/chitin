package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuild_SilentDropWithPRNumberJoinsLivePR(t *testing.T) {
	ts := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(map[string]any{
		"pr_number":        42,
		"work_unit_id":     "wu-118",
		"task_id":          "T008",
		"spec_ref":         "118-factory-dispatch-failed-reason-taxonomy",
		"deliverable_kind": "pr",
		"reason":           "gh_pr_create_failed",
	})
	chain := map[int][]EscalationEvent{
		42: {{
			EventType: "work_unit_completed_without_deliverable",
			Reason:    "silent_drop",
			PRNumber:  42,
			TaskID:    "T008",
			SpecRef:   "118-factory-dispatch-failed-reason-taxonomy",
			Ts:        ts,
			RunID:     "wu-118",
			Payload:   payload,
		}},
	}
	live := []LivePR{{
		Number:    42,
		Title:     "Recover dropped work unit",
		SpecRef:   "118-factory-dispatch-failed-reason-taxonomy",
		UpdatedAt: ts,
	}}

	got := Build(chain, live, ts)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if got[0].PRNumber != 42 || got[0].Reason != "silent_drop" {
		t.Fatalf("entry=%+v, want PR 42 silent_drop", got[0])
	}
	if got[0].TaskID != "T008" || got[0].SpecRef != "118-factory-dispatch-failed-reason-taxonomy" {
		t.Fatalf("silent-drop identity not preserved: %+v", got[0])
	}
	if got[0].Title != "Recover dropped work unit" {
		t.Fatalf("live PR fields not joined: %+v", got[0])
	}
}

func TestBuild_SilentDropWithoutPRUsesSpecTaskIdentity(t *testing.T) {
	ts := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(map[string]any{
		"work_unit_id":     "wu-118",
		"task_id":          "T010",
		"spec_ref":         "118-factory-dispatch-failed-reason-taxonomy",
		"deliverable_kind": "pr",
		"reason":           "no_changes_to_commit",
	})
	chain := map[int][]EscalationEvent{
		0: {{
			EventType: "work_unit_completed_without_deliverable",
			Reason:    "silent_drop",
			TaskID:    "T010",
			SpecRef:   "118-factory-dispatch-failed-reason-taxonomy",
			Ts:        ts,
			RunID:     "wu-118",
			Payload:   payload,
		}},
	}

	got := Build(chain, nil, ts)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if got[0].PRNumber != 0 || got[0].TaskID != "T010" ||
		got[0].SpecRef != "118-factory-dispatch-failed-reason-taxonomy" ||
		got[0].Reason != "silent_drop" {
		t.Fatalf("unexpected no-PR silent-drop entry: %+v", got[0])
	}
}
