package router

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSignalFunctionsAreDeterministicForFixedInputs(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	hook := HookInput{
		ToolName: "Bash",
		ToolInput: map[string]interface{}{
			"command": "rm -rf /tmp/out-of-scope",
		},
	}
	events := []ChainEvent{
		{
			Ts:        "2026-05-09T11:45:00Z",
			EventType: "intent",
			Payload: map[string]interface{}{
				"entry_id":   "entry-1",
				"task_class": "fix",
				"file_paths": []interface{}{"apps/cli/src/main.ts"},
			},
		},
		decisionEvent("2026-05-09T11:50:00Z", "Bash", "rm -rf /tmp/out-of-scope", "deny"),
		decisionEvent("2026-05-09T11:51:00Z", "Bash", "rm -rf /tmp/out-of-scope", "deny"),
		decisionEvent("2026-05-09T11:52:00Z", "Bash", "rm -rf /tmp/out-of-scope", "deny"),
		decisionEvent("2026-05-09T11:53:00Z", "Bash", "rm -rf /tmp/out-of-scope", "deny"),
		decisionEvent("2026-05-09T11:54:00Z", "Read", "README.md", "allow"),
	}
	thresholds := FlounderingThresholds{MaxLoopCount: 3, MaxStallSeconds: 600}

	firstBlast := ScoreBlastRadius(hook, 0.6)
	secondBlast := ScoreBlastRadius(hook, 0.6)
	if !reflect.DeepEqual(firstBlast, secondBlast) {
		t.Fatalf("blast radius not deterministic:\nfirst=%+v\nsecond=%+v", firstBlast, secondBlast)
	}

	firstFloundering := DetectFloundering(events, thresholds, now)
	secondFloundering := DetectFloundering(events, thresholds, now)
	if !reflect.DeepEqual(firstFloundering, secondFloundering) {
		t.Fatalf("floundering not deterministic:\nfirst=%+v\nsecond=%+v", firstFloundering, secondFloundering)
	}

	firstDrift := DetectDrift(hook, events, 0.6)
	secondDrift := DetectDrift(hook, events, 0.6)
	if !reflect.DeepEqual(firstDrift, secondDrift) {
		t.Fatalf("drift not deterministic:\nfirst=%+v\nsecond=%+v", firstDrift, secondDrift)
	}
}

func TestSignalFilesDoNotImportShellOrModelClients(t *testing.T) {
	signalFiles := []string{
		"blast_radius.go",
		"drift.go",
		"floundering.go",
		"route_for.go",
	}
	for _, name := range signalFiles {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(".", name))
			if err != nil {
				t.Fatalf("read source: %v", err)
			}
			file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
			if err != nil {
				t.Fatalf("parse source: %v", err)
			}
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				switch {
				case path == "os/exec":
					t.Fatalf("%s imports os/exec; signal path must not shell out", name)
				case path == "net/http":
					t.Fatalf("%s imports net/http; signal path must not call model APIs", name)
				case strings.Contains(path, "openai") || strings.Contains(path, "anthropic"):
					t.Fatalf("%s imports model client %q; signal path must stay pure-Go", name, path)
				}
			}
			text := string(src)
			for _, forbidden := range []string{"exec.Command", "CommandContext", "claude -p"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("%s contains %q; signal path must not spawn model or shell work", name, forbidden)
				}
			}
		})
	}
}

func decisionEvent(ts, tool, target, decision string) ChainEvent {
	return ChainEvent{
		Ts:        ts,
		EventType: "decision",
		Payload: map[string]interface{}{
			"tool_name":     tool,
			"action_target": target,
			"action_type":   "shell.exec",
			"decision":      decision,
		},
	}
}
