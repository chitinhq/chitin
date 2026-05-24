// copilot_dispatch_test.go — spec 099 slice 1 + 2 tests for the
// --driver copilot path.
//
// Slice 1 verified: routing skeleton (flag parse, value validation,
// missing --repo, validates spec before branch, doesn't dial Temporal).
// Slice 2 verifies: actual `gh issue create` invocation + chain emit.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSchedule_UnknownDriverValue(t *testing.T) {
	// --driver value that's neither "" (default) nor "copilot" is
	// rejected as user error per contracts/cli-driver-flag.md.
	repo := fixtureRepo(t, "091-good", "- [ ] T001 [P] [US1] Implement the placeholder")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--driver", "foobar",
		"091",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "unknown driver") {
		t.Errorf("stderr should report unknown driver; got: %q", errBuf.String())
	}
}

func TestRunSchedule_DriverCopilot_MissingRepo(t *testing.T) {
	// --driver copilot requires --repo <owner/name>. Missing → user error.
	repo := fixtureRepo(t, "091-good", "- [ ] T001 [P] [US1] Implement the placeholder")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--driver", "copilot",
		"091",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "--repo") {
		t.Errorf("stderr should name the missing --repo flag; got: %q", errBuf.String())
	}
}

func TestRunSchedule_DriverCopilot_ValidatesSpecBeforeCopilotBranch(t *testing.T) {
	// Spec resolution + DAG validation must still run on the Copilot path —
	// catch obvious typos before consuming Copilot's slot (R7 in research.md).
	repo := fixtureRepo(t, "091-no-tasks", "")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--driver", "copilot",
		"--repo", "owner/name",
		"091",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "compile failed") {
		t.Errorf("Copilot path must still run spec compile; got stderr=%q", errBuf.String())
	}
}

func TestRunSchedule_DriverCopilot_ReachesCopilotBranchAfterValidation(t *testing.T) {
	// Happy path: valid spec, --driver copilot, --repo present, fake gh on
	// PATH returns success → exit 0, chain emit landed, Temporal NOT dialed.
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-good", tasksMd)
	fakeKernelForSchedule(t)
	installFakeGh(t, fakeGhOptions{
		IssueNumber: 42,
		IssueURL:    "https://github.com/owner/name/issues/42",
		ExitCode:    0,
	})
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		// Pointing Temporal at an impossible port would FAIL the local path
		// with exitRuntimeError. If we get exitSuccess here, the Copilot
		// branch did NOT dial Temporal — the routing skeleton works.
		"--temporal-host", "127.0.0.1:1",
		"--driver", "copilot",
		"--repo", "owner/name",
		"091",
	}, &out, &errBuf)
	if code != exitSuccess {
		t.Errorf("exit code = %d, want %d (success); stderr=%q", code, exitSuccess, errBuf.String())
	}
	if !strings.Contains(out.String(), "copilot dispatched") {
		t.Errorf("stdout should announce copilot dispatch; got: %q", out.String())
	}
	if strings.Contains(errBuf.String(), "Temporal unreachable") {
		t.Errorf("Copilot path must NOT dial Temporal; got: %q", errBuf.String())
	}
}

func TestRunSchedule_DefaultDriverStillDialsTemporal(t *testing.T) {
	// Regression: omitting --driver keeps the spec 097 path intact —
	// SchedulerWorkflow path dials Temporal and fails on unreachable.
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-good", tasksMd)
	fakeKernelForSchedule(t)
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--temporal-host", "127.0.0.1:1",
		"091",
	}, &out, &errBuf)
	if code != exitRuntimeError {
		t.Errorf("exit code = %d, want %d (runtime); stderr=%q", code, exitRuntimeError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Temporal unreachable") {
		t.Errorf("default path must dial Temporal; got: %q", errBuf.String())
	}
}

// --- Slice 2 tests: actual `gh issue create` + chain emit ---

func TestRunCopilotDispatch_HappyPath_PrintsIssueURL(t *testing.T) {
	fakeKernelForSchedule(t)
	installFakeGh(t, fakeGhOptions{
		IssueNumber: 42,
		IssueURL:    "https://github.com/owner/name/issues/42",
		ExitCode:    0,
	})
	var out, errBuf bytes.Buffer
	code := runCopilotDispatch(context.Background(), copilotDispatchInput{
		SpecRef: "099-github-native-dispatch",
		Repo:    "owner/name",
	}, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit code = %d, want %d; stderr=%q", code, exitSuccess, errBuf.String())
	}
	if !strings.Contains(out.String(), "https://github.com/owner/name/issues/42") {
		t.Errorf("stdout should include issue URL; got: %q", out.String())
	}
	if !strings.Contains(out.String(), "issue_number: 42") {
		t.Errorf("stdout should include issue number; got: %q", out.String())
	}
}

func TestRunCopilotDispatch_GhBinaryArgvShape(t *testing.T) {
	// Verify the constructed gh argv matches contracts/cli-driver-flag.md:
	//   gh issue create --repo owner/name --title <T> --body <B>
	//                   --label chitin-dispatch --label driver:copilot
	//                   --assignee copilot
	fakeKernelForSchedule(t)
	_, argvPath := installFakeGh(t, fakeGhOptions{
		IssueNumber: 7,
		IssueURL:    "https://github.com/x/y/issues/7",
		ExitCode:    0,
		CaptureArgv: true,
	})
	var out, errBuf bytes.Buffer
	code := runCopilotDispatch(context.Background(), copilotDispatchInput{
		SpecRef: "099-github-native-dispatch",
		Repo:    "x/y",
	}, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit code = %d, want success; stderr=%q", code, errBuf.String())
	}
	argvBytes, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read captured argv: %v", err)
	}
	argv := string(argvBytes)
	wantArgs := []string{
		"issue", "create",
		"--repo", "x/y",
		"--title", "Run spec 099-github-native-dispatch",
		"--label", "chitin-dispatch",
		"--label", "driver:copilot",
		"--assignee", "copilot",
	}
	for _, want := range wantArgs {
		if !strings.Contains(argv, want) {
			t.Errorf("captured argv missing %q\nargv=%q", want, argv)
		}
	}
}

func TestRunCopilotDispatch_GhNotInstalled(t *testing.T) {
	// gh binary missing from PATH → user error with "gh not installed" message.
	fakeKernelForSchedule(t)
	t.Setenv("PATH", t.TempDir()) // PATH with nothing on it
	var out, errBuf bytes.Buffer
	code := runCopilotDispatch(context.Background(), copilotDispatchInput{
		SpecRef: "099-github-native-dispatch",
		Repo:    "owner/name",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "gh") {
		t.Errorf("stderr should mention gh binary; got: %q", errBuf.String())
	}
}

func TestRunCopilotDispatch_GhExitsNonZero(t *testing.T) {
	// gh returns non-zero (Copilot not assignable, network error, etc.).
	// Dispatch fails user-error per FR-004.
	fakeKernelForSchedule(t)
	installFakeGh(t, fakeGhOptions{
		ExitCode: 1,
		Stderr:   "GraphQL: copilot is not a valid assignee (assignee)",
	})
	var out, errBuf bytes.Buffer
	code := runCopilotDispatch(context.Background(), copilotDispatchInput{
		SpecRef: "099-github-native-dispatch",
		Repo:    "owner/name",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "copilot is not a valid assignee") {
		t.Errorf("stderr should propagate gh stderr; got: %q", errBuf.String())
	}
}

func TestRunCopilotDispatch_EmitsChainEvent(t *testing.T) {
	// Successful dispatch emits copilot_dispatched chain event per
	// contracts/chain-events.md Event 1.
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
	installFakeGh(t, fakeGhOptions{
		IssueNumber: 99,
		IssueURL:    "https://github.com/owner/name/issues/99",
		ExitCode:    0,
	})
	var out, errBuf bytes.Buffer
	code := runCopilotDispatch(context.Background(), copilotDispatchInput{
		SpecRef: "099-github-native-dispatch",
		Repo:    "owner/name",
	}, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit code = %d, want success; stderr=%q", code, errBuf.String())
	}
	captured, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(captured, &event); err != nil {
		t.Fatalf("event JSON malformed: %v\nraw=%q", err, string(captured))
	}
	if event["event_type"] != "copilot_dispatched" {
		t.Errorf("event_type = %v, want copilot_dispatched", event["event_type"])
	}
	payload, ok := event["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload not an object: %T", event["payload"])
	}
	if payload["repo"] != "owner/name" {
		t.Errorf("payload.repo = %v, want owner/name", payload["repo"])
	}
	if payload["spec_ref"] != "099-github-native-dispatch" {
		t.Errorf("payload.spec_ref = %v, want 099-github-native-dispatch", payload["spec_ref"])
	}
	if payload["issue_url"] != "https://github.com/owner/name/issues/99" {
		t.Errorf("payload.issue_url = %v, want issue URL", payload["issue_url"])
	}
	// issue_number arrives as float64 from JSON parse
	if n, _ := payload["issue_number"].(float64); n != 99 {
		t.Errorf("payload.issue_number = %v, want 99", payload["issue_number"])
	}
	if _, ok := payload["dispatched_at"].(string); !ok {
		t.Errorf("payload.dispatched_at = %v, want RFC3339 string", payload["dispatched_at"])
	}
}

// installFakeGh writes a fake `gh` binary onto a fresh directory placed
// FIRST on PATH for the test's duration. The fake reads the requested
// behavior from fakeGhOptions and prints the issue URL on stdout
// (matching gh's default `gh issue create` output shape).
//
// If CaptureArgv is true, the second return value is the path to a file
// where the fake binary appends its received argv (one arg per line).
type fakeGhOptions struct {
	IssueNumber int
	IssueURL    string
	ExitCode    int
	Stderr      string
	CaptureArgv bool
}

func installFakeGh(t *testing.T, opts fakeGhOptions) (binPath, argvPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "gh")
	argvPath = filepath.Join(dir, "argv.log")
	script := "#!/usr/bin/env bash\n"
	if opts.CaptureArgv {
		script += "for a in \"$@\"; do echo \"$a\" >> " + argvPath + "; done\n"
	}
	if opts.Stderr != "" {
		script += "echo " + shellQuote(opts.Stderr) + " >&2\n"
	}
	if opts.IssueURL != "" && opts.ExitCode == 0 {
		// Real gh issue create with default output prints the URL on its own line.
		script += "echo " + opts.IssueURL + "\n"
	}
	script += "exit " + itoa(opts.ExitCode) + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	// Prepend dir to PATH so this fake wins over any real gh on the box.
	existing := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+existing)
	return binPath, argvPath
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
