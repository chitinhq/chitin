package driver_test

import (
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/codex"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/copilot"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/gemini"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/hermes"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	copilotsdk "github.com/github/copilot-sdk/go"
)

func TestShellExecPayloadsNormalizeConsistentlyAcrossDrivers(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		wantType   gov.ActionType
		wantTarget string
	}{
		{
			name:       "ordinary shell",
			command:    "ls -la",
			wantType:   gov.ActShellExec,
			wantTarget: "ls -la",
		},
		{
			name:       "recursive delete",
			command:    "rm -rf go/",
			wantType:   gov.ActFileRecursiveDelete,
			wantTarget: "rm -rf go/",
		},
		{
			name:       "force push",
			command:    "git push --force origin main",
			wantType:   gov.ActGitForcePush,
			wantTarget: "git push --force origin main",
		},
		{
			name:       "infra destroy",
			command:    "terraform destroy",
			wantType:   gov.ActInfraDestroy,
			wantTarget: "terraform destroy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, got := range normalizeShellAcrossDrivers(t, tc.command) {
				if got.action.Type != tc.wantType {
					t.Errorf("%s: Type=%q want %q", got.driver, got.action.Type, tc.wantType)
				}
				if got.action.Target != tc.wantTarget {
					t.Errorf("%s: Target=%q want %q", got.driver, got.action.Target, tc.wantTarget)
				}
				if got.action.Path != "/work" {
					t.Errorf("%s: Path=%q want /work", got.driver, got.action.Path)
				}
			}
		})
	}
}

func TestUnknownToolPayloadsFailClosedAcrossHookDrivers(t *testing.T) {
	cases := []struct {
		driver string
		action gov.Action
	}{
		{"claudecode", mustClaude(t, "FutureUnreleasedTool", map[string]any{"x": 1})},
		{"codex", mustCodex(t, "future_codex_tool", map[string]any{"x": 1})},
		{"gemini", mustGemini(t, "future_unreleased_gemini_tool", map[string]any{"x": 1})},
		{"hermes", mustHermes(t, "image_generate", map[string]any{"prompt": "x"})},
		{"copilot", copilot.Normalize(copilotsdk.PermissionRequest{Kind: copilotsdk.PermissionRequestKind("future-kind")}, "/work")},
	}

	for _, tc := range cases {
		if tc.action.Type != gov.ActUnknown {
			t.Errorf("%s: Type=%q want %q", tc.driver, tc.action.Type, gov.ActUnknown)
		}
	}
}

type driverAction struct {
	driver string
	action gov.Action
}

func normalizeShellAcrossDrivers(t *testing.T, command string) []driverAction {
	t.Helper()

	return []driverAction{
		{"claudecode", mustClaude(t, "Bash", map[string]any{"command": command})},
		{"codex", mustCodex(t, "Bash", map[string]any{"command": command})},
		{"gemini", mustGemini(t, "run_shell_command", map[string]any{"command": command})},
		{"hermes", mustHermes(t, "terminal", map[string]any{"command": command})},
		{"copilot", copilot.Normalize(copilotsdk.PermissionRequest{
			Kind:            copilotsdk.PermissionRequestKindShell,
			FullCommandText: ptr(command),
		}, "/work")},
	}
}

func mustClaude(t *testing.T, tool string, input map[string]any) gov.Action {
	t.Helper()
	a, err := claudecode.Normalize(claudecode.HookInput{ToolName: tool, ToolInput: input, Cwd: "/work"})
	if err != nil {
		t.Fatalf("claudecode %s: %v", tool, err)
	}
	return a
}

func mustCodex(t *testing.T, tool string, input map[string]any) gov.Action {
	t.Helper()
	a, err := codex.Normalize(codex.HookInput{ToolName: tool, ToolInput: input, Cwd: "/work"})
	if err != nil {
		t.Fatalf("codex %s: %v", tool, err)
	}
	return a
}

func mustGemini(t *testing.T, tool string, input map[string]any) gov.Action {
	t.Helper()
	a, err := gemini.Normalize(gemini.HookInput{ToolName: tool, ToolInput: input, Cwd: "/work"})
	if err != nil {
		t.Fatalf("gemini %s: %v", tool, err)
	}
	return a
}

func mustHermes(t *testing.T, tool string, input map[string]any) gov.Action {
	t.Helper()
	a, err := hermes.Normalize(hermes.HookInput{ToolName: tool, ToolInput: input, Cwd: "/work"})
	if err != nil {
		t.Fatalf("hermes %s: %v", tool, err)
	}
	return a
}

func ptr(s string) *string {
	return &s
}
