package copilot

import (
	"testing"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// ptr returns a pointer to the given string value.
func ptr(s string) *string { return &s }

func TestNormalize_Shell(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("ls /tmp"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActShellExec {
		t.Errorf("Type: got %q, want shell.exec", got.Type)
	}
	if got.Target != "ls /tmp" {
		t.Errorf("Target: got %q, want 'ls /tmp'", got.Target)
	}
	if got.Path != "/work" {
		t.Errorf("Path: got %q, want /work", got.Path)
	}
}

func TestNormalize_ShellNilCommandText(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: nil,
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActShellExec {
		t.Errorf("Type: got %q, want shell.exec (even with nil command)", got.Type)
	}
	// Nil command text must produce empty Target, not a panic.
	if got.Target != "" {
		t.Errorf("Target: got %q, want empty on nil command", got.Target)
	}
}

func TestNormalize_Write(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:     copilotsdk.PermissionRequestKindWrite,
		FileName: ptr("/src/main.go"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActFileWrite {
		t.Errorf("Type: got %q, want file.write", got.Type)
	}
	if got.Target != "/src/main.go" {
		t.Errorf("Target: got %q, want '/src/main.go'", got.Target)
	}
}

func TestNormalize_WriteNilFileName(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:     copilotsdk.PermissionRequestKindWrite,
		FileName: nil,
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActFileWrite {
		t.Errorf("Type: got %q, want file.write", got.Type)
	}
	if got.Target != "" {
		t.Errorf("Target: got %q, want empty on nil FileName", got.Target)
	}
}

func TestNormalize_Read(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKindRead,
		Path: ptr("/etc/config"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActFileRead {
		t.Errorf("Type: got %q, want file.read", got.Type)
	}
	if got.Target != "/etc/config" {
		t.Errorf("Target: got %q, want '/etc/config'", got.Target)
	}
}

func TestNormalize_ReadNilPath(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKindRead,
		Path: nil,
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActFileRead {
		t.Errorf("Type: got %q, want file.read", got.Type)
	}
	if got.Target != "" {
		t.Errorf("Target: got %q, want empty on nil Path", got.Target)
	}
}

func TestNormalize_MCP(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:       copilotsdk.PermissionRequestKindMcp,
		ServerName: ptr("my-server"),
		ToolName:   ptr("my-tool"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActMCPCall {
		t.Errorf("Type: got %q, want mcp.call", got.Type)
	}
	// Target should be "serverName/toolName" when both are present.
	if got.Target != "my-server/my-tool" {
		t.Errorf("Target: got %q, want 'my-server/my-tool'", got.Target)
	}
}

func TestNormalize_MCPNilFields(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKindMcp,
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActMCPCall {
		t.Errorf("Type: got %q, want mcp.call", got.Type)
	}
	// Nil server/tool should not panic; target may be empty or partial.
}

func TestNormalize_URL(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKindURL,
		URL:  ptr("https://api.example.com/v1"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActHTTPRequest {
		t.Errorf("Type: got %q, want http.request", got.Type)
	}
	if got.Target != "https://api.example.com/v1" {
		t.Errorf("Target: got %q, want 'https://api.example.com/v1'", got.Target)
	}
}

func TestNormalize_Memory(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:    copilotsdk.PermissionRequestKindMemory,
		Subject: ptr("user preferences"),
	}
	got := Normalize(req, "/work")
	if got.Type != "memory.access" {
		t.Errorf("Type: got %q, want memory.access", got.Type)
	}
}

func TestNormalize_CustomTool(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:     copilotsdk.PermissionRequestKindCustomTool,
		ToolName: ptr("my-custom-tool"),
	}
	got := Normalize(req, "/work")
	if got.Type != "tool.custom" {
		t.Errorf("Type: got %q, want tool.custom", got.Type)
	}
}

func TestNormalize_Hook(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:        copilotsdk.PermissionRequestKindHook,
		HookMessage: ptr("pre-commit hook needs approval"),
	}
	got := Normalize(req, "/work")
	if got.Type != "hook.invoke" {
		t.Errorf("Type: got %q, want hook.invoke", got.Type)
	}
}

func TestNormalize_UnknownKindIsFailClosed(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKind("this-kind-does-not-exist"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActUnknown {
		t.Errorf("Type: got %q, want unknown", got.Type)
	}
}

func TestNormalize_AllDocumentedKindsReturnSomething(t *testing.T) {
	// Invariant: every documented Kind produces a non-empty Type and Path==cwd.
	kinds := []copilotsdk.PermissionRequestKind{
		copilotsdk.PermissionRequestKindShell,
		copilotsdk.PermissionRequestKindWrite,
		copilotsdk.PermissionRequestKindRead,
		copilotsdk.PermissionRequestKindMcp,
		copilotsdk.PermissionRequestKindURL,
		copilotsdk.PermissionRequestKindMemory,
		copilotsdk.PermissionRequestKindCustomTool,
		copilotsdk.PermissionRequestKindHook,
	}
	for _, k := range kinds {
		t.Run(string(k), func(t *testing.T) {
			req := copilotsdk.PermissionRequest{Kind: k}
			got := Normalize(req, "/work")
			if got.Type == "" {
				t.Errorf("Kind %q produced empty Action.Type", k)
			}
			if got.Path != "/work" {
				t.Errorf("Kind %q: Path got %q, want /work", k, got.Path)
			}
		})
	}
}
