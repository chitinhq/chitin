package drift

import "testing"

func TestEvaluate_NoIntent(t *testing.T) {
	got := Evaluate(Observation{ToolName: "Edit", TargetPath: "apps/cli/src/main.ts"}, nil, DefaultConfig())
	if got.HasIntent {
		t.Fatal("HasIntent=true want false")
	}
	if got.Reason != "no-intent-recorded" {
		t.Fatalf("Reason=%q want no-intent-recorded", got.Reason)
	}
}

func TestEvaluate_InScope(t *testing.T) {
	events := []Event{
		{EventType: "intent", Payload: map[string]interface{}{
			"entry_id": "t1", "task_class": "fix", "file_paths": []interface{}{"apps/cli/src/**"},
		}},
	}
	got := Evaluate(Observation{ToolName: "Edit", TargetPath: "apps/cli/src/main.ts"}, events, DefaultConfig())
	if got.Detected {
		t.Fatal("Detected=true want false")
	}
	if !got.InScope {
		t.Fatal("InScope=false want true")
	}
}

func TestEvaluate_DemoteThenKill(t *testing.T) {
	events := []Event{
		{EventType: "intent", Payload: map[string]interface{}{
			"entry_id": "t1", "task_class": "fix", "file_paths": []interface{}{"apps/cli/src/**"},
		}},
	}
	demote := Evaluate(Observation{ToolName: "Edit", TargetPath: "docs/README.md"}, events, DefaultConfig())
	if demote.Action != ActionDemote {
		t.Fatalf("Action=%q want demote (score=%.2f)", demote.Action, demote.Score)
	}

	events = append(events, Event{
		EventType: "decision",
		Payload: map[string]interface{}{
			"decision":      "allow",
			"action_type":   "file.write",
			"action_target": "docs/README.md",
		},
	})
	kill := Evaluate(Observation{ToolName: "Edit", TargetPath: "docs/ops.md"}, events, DefaultConfig())
	if kill.Action != ActionKill {
		t.Fatalf("Action=%q want kill (score=%.2f)", kill.Action, kill.Score)
	}
}

func TestInScopeSupportsRecursiveGlobsAndAbsolutePaths(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/repo/src/foo.go", true},
		{"C:/repo/src/foo.go", true},
		{"docs/readme.md", false},
	}
	for _, tc := range cases {
		got := InScope(tc.path, []string{"src/**"})
		if got != tc.want {
			t.Fatalf("InScope(%q)= %v want %v", tc.path, got, tc.want)
		}
	}
}
