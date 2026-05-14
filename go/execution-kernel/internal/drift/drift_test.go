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

func TestEvaluate_EmptyIntentScopeDoesNotDetect(t *testing.T) {
	events := []Event{
		{EventType: "intent", Payload: map[string]interface{}{
			"entry_id": "t1", "task_class": "fix", "file_paths": []interface{}{},
		}},
	}
	got := Evaluate(Observation{ToolName: "Edit", TargetPath: "docs/README.md"}, events, DefaultConfig())
	if got.Detected {
		t.Fatal("Detected=true want false for empty intent scope")
	}
	if got.Reason != "no-intent-recorded" {
		t.Fatalf("Reason=%q want no-intent-recorded", got.Reason)
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

func TestEvaluate_MaxTurnsCapsTurnScore(t *testing.T) {
	events := []Event{
		{EventType: "intent", Payload: map[string]interface{}{
			"entry_id": "t1", "task_class": "fix", "file_paths": []interface{}{"apps/cli/src/**"},
		}},
	}
	for i := 0; i < 10; i++ {
		events = append(events, Event{
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":      "allow",
				"action_type":   "file.write",
				"action_target": "apps/cli/src/main.ts",
			},
		})
	}
	got := Evaluate(Observation{ToolName: "Edit", TargetPath: "apps/cli/src/other.ts"}, events, Config{MaxTurns: 2})
	if got.Score != 0.4 {
		t.Fatalf("Score=%.2f want 0.40 with max turn score capped", got.Score)
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

func TestInScope_ErrorMalformedGlobReturnsFalse(t *testing.T) {
	got := InScope("apps/cli/src/main.ts", []string{"apps/cli/src/["})
	if got {
		t.Fatal("InScope=true want false for malformed glob")
	}
}

func TestInScope_RelativePatternMatchesAbsoluteTarget(t *testing.T) {
	// Intent file_paths are repo-relative; a tool target can be absolute.
	// A plain (non-glob) relative pattern must still match the absolute
	// path by path-suffix, otherwise scope checks miss every absolute path.
	if !InScope("/repo/apps/cli/src/main.ts", []string{"apps/cli/src/main.ts"}) {
		t.Fatal("InScope=false; relative pattern should match absolute target by path-suffix")
	}
	if !InScope("apps/cli/src/main.ts", []string{"apps/cli/src/main.ts"}) {
		t.Fatal("InScope=false; exact relative match should hold")
	}
	if InScope("/repo/docs/readme.md", []string{"apps/cli/src/main.ts"}) {
		t.Fatal("InScope=true; unrelated path must not match")
	}
}
