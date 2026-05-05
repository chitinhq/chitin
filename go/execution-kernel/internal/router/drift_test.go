package router

import "testing"

func TestDetectDrift_NoIntent(t *testing.T) {
	res := DetectDrift(HookInput{ToolName: "Edit", ToolInput: map[string]interface{}{"file_path": "/tmp/x.txt"}}, nil, 0.5)
	if res.Fired {
		t.Errorf("Fired=true with no intent; want false")
	}
	if res.Reason != "no-intent-recorded" {
		t.Errorf("reason=%q want no-intent-recorded", res.Reason)
	}
}

func TestDetectDrift_InScope(t *testing.T) {
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "foo",
			"task_class": "refactor",
			"file_paths": []interface{}{"apps/runner/src/dispatcher.ts"},
		},
	}
	res := DetectDrift(
		HookInput{ToolName: "Edit", ToolInput: map[string]interface{}{"file_path": "apps/runner/src/dispatcher.ts"}},
		[]ChainEvent{intent},
		0.5,
	)
	if res.Fired {
		t.Errorf("Fired=true on in-scope edit; want false (reason=%q)", res.Reason)
	}
}

func TestDetectDrift_OutOfScope(t *testing.T) {
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "foo",
			"task_class": "refactor",
			"file_paths": []interface{}{"apps/runner/src/dispatcher.ts"},
		},
	}
	res := DetectDrift(
		HookInput{ToolName: "Edit", ToolInput: map[string]interface{}{"file_path": "/etc/passwd"}},
		[]ChainEvent{intent},
		0.5,
	)
	if !res.Fired {
		t.Errorf("Fired=false on out-of-scope edit; want true (reason=%q score=%v)", res.Reason, res.Score)
	}
}

func TestDetectDrift_OutOfScopeHighBlast(t *testing.T) {
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "foo",
			"task_class": "refactor",
			"file_paths": []interface{}{"apps/runner/src/dispatcher.ts"},
		},
	}
	res := DetectDrift(
		HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "rm -rf /home/user/important"}},
		[]ChainEvent{intent},
		0.5,
	)
	if !res.Fired {
		t.Errorf("Fired=false on out-of-scope rm-rf; want true")
	}
	if res.Score < 0.7 {
		t.Errorf("score=%v on out-of-scope rm-rf; want >= 0.8", res.Score)
	}
}

func TestPathOverlap(t *testing.T) {
	cases := []struct {
		proposed string
		declared []string
		want     bool
	}{
		{"apps/foo/bar.ts", []string{"apps/foo/"}, true},
		{"apps/foo/bar.ts", []string{"apps/foo/bar.ts"}, true},
		{"apps/foo/", []string{"apps/foo/bar.ts"}, true},
		{"apps/foo/bar.ts", []string{"apps/baz/"}, false},
		{"", []string{"apps/foo/"}, false},
		{"apps/foo/bar.ts", nil, false},
	}
	for _, c := range cases {
		got := pathOverlap(c.proposed, c.declared)
		if got != c.want {
			t.Errorf("pathOverlap(%q, %v) = %v; want %v", c.proposed, c.declared, got, c.want)
		}
	}
}
