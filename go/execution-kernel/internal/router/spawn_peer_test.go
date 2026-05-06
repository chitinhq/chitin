package router

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// stubSpawner records the most recent invocation and returns canned
// output. Used by tests so we never spawn a real subprocess.
type stubSpawner struct {
	stdout, stderr string
	exitCode       int
	err            error

	// Captured for assertions:
	gotName    string
	gotArgs    []string
	gotEnv     []string
	gotWorkDir string
	gotStdin   string

	// Optional: simulate a slow spawn (Sleep) so timeout tests fire.
	sleep time.Duration
}

func (s *stubSpawner) Run(ctx context.Context, name string, args []string, env []string, workDir string, stdin string) (string, string, int, error) {
	s.gotName, s.gotArgs, s.gotEnv, s.gotWorkDir, s.gotStdin = name, args, env, workDir, stdin
	if s.sleep > 0 {
		select {
		case <-time.After(s.sleep):
		case <-ctx.Done():
			return "", "", -1, ctx.Err()
		}
	}
	return s.stdout, s.stderr, s.exitCode, s.err
}

func sampleConfig(spawner Spawner) SpawnConfig {
	return SpawnConfig{
		Decision: RouteDecision{
			Rule:      RoutingRule{Name: "floundering-loop", Signal: "floundering", Route: "patch_quality"},
			Candidate: Candidate{Driver: "claude", Model: "claude-opus-4-7"},
			Rationale: "test",
		},
		Request: RouteRequest{
			Signal:           "floundering",
			Severity:         ">= 2 loops",
			WorkerWorkflowID: "swarm-test-12345",
		},
		PromptText:          "Worker is stuck. Please make progress.",
		SpawnTimeoutSeconds: 5,
		Spawner:             spawner,
	}
}

func TestSpawnPeer_Success(t *testing.T) {
	stub := &stubSpawner{stdout: "I made progress.", exitCode: 0}
	cfg := sampleConfig(stub)
	res, err := SpawnPeer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.Content != "I made progress." {
		t.Errorf("Content: got %q want %q", res.Content, "I made progress.")
	}
	if res.RawPeerStdout != "I made progress." {
		t.Errorf("RawPeerStdout not preserved")
	}
	if res.Provenance.PeerExitCode != 0 {
		t.Errorf("ExitCode: got %d want 0", res.Provenance.PeerExitCode)
	}
}

func TestSpawnPeer_FreshWorktreeUsed(t *testing.T) {
	stub := &stubSpawner{stdout: "ok", exitCode: 0}
	cfg := sampleConfig(stub)
	_, err := SpawnPeer(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stub.gotWorkDir, "chitin-peer-spawn-") {
		t.Errorf("workDir should be a chitin-peer-spawn-* tempdir; got %s", stub.gotWorkDir)
	}
	if stub.gotWorkDir == "" {
		t.Error("workDir empty — spawnPeer must always set a fresh worktree")
	}
}

func TestSpawnPeer_RecursiveEscalationGuard(t *testing.T) {
	stub := &stubSpawner{stdout: "ok", exitCode: 0}
	cfg := sampleConfig(stub)
	_, err := SpawnPeer(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range stub.gotEnv {
		if e == "CHITIN_NO_ESCALATE=1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("CHITIN_NO_ESCALATE=1 must always be in spawn env (recursive guard)")
	}
}

func TestSpawnPeer_PromptPipedToStdin(t *testing.T) {
	stub := &stubSpawner{stdout: "ok", exitCode: 0}
	cfg := sampleConfig(stub)
	cfg.PromptText = "specific worker prompt content"
	_, err := SpawnPeer(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if stub.gotStdin != "specific worker prompt content" {
		t.Errorf("stdin: got %q want %q", stub.gotStdin, "specific worker prompt content")
	}
}

func TestSpawnPeer_ProvenancePopulated(t *testing.T) {
	stub := &stubSpawner{stdout: "ok", exitCode: 0}
	cfg := sampleConfig(stub)
	res, err := SpawnPeer(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := res.Provenance
	if p.EscalationID == "" || !strings.HasPrefix(p.EscalationID, "esc-") {
		t.Errorf("EscalationID: got %q want esc-*", p.EscalationID)
	}
	if p.WorkerWorkflowID != "swarm-test-12345" {
		t.Errorf("WorkerWorkflowID: got %q", p.WorkerWorkflowID)
	}
	if p.TriggerSignal != "floundering" {
		t.Errorf("TriggerSignal: got %q", p.TriggerSignal)
	}
	if p.Severity != ">= 2 loops" {
		t.Errorf("Severity: got %q", p.Severity)
	}
	if p.Route != "patch_quality" {
		t.Errorf("Route: got %q", p.Route)
	}
	if p.Candidate.Driver != "claude" || p.Candidate.Model != "claude-opus-4-7" {
		t.Errorf("Candidate: got %v", p.Candidate)
	}
	if p.SpawnedAt.IsZero() {
		t.Error("SpawnedAt not set")
	}
	if p.WorktreePath == "" {
		t.Error("WorktreePath not recorded for audit")
	}
	if p.DurationMs < 0 {
		t.Errorf("DurationMs negative: %d", p.DurationMs)
	}
}

func TestSpawnPeer_UnsupportedDriver(t *testing.T) {
	stub := &stubSpawner{}
	cfg := sampleConfig(stub)
	cfg.Decision.Candidate.Driver = "made-up-driver"
	_, err := SpawnPeer(context.Background(), cfg)
	if !errors.Is(err, ErrUnsupportedDriver) {
		t.Errorf("expected ErrUnsupportedDriver; got %v", err)
	}
}

func TestSpawnPeer_TimeoutDistinctFromFailure(t *testing.T) {
	stub := &stubSpawner{sleep: 200 * time.Millisecond}
	cfg := sampleConfig(stub)
	cfg.SpawnTimeoutSeconds = 0  // 0 → default 60, but we want a real test
	// Rebuild with a tiny ctx instead so we can test timeout behavior:
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := SpawnPeer(ctx, cfg)
	if !errors.Is(err, ErrSpawnTimeout) && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("expected timeout-class error; got %v", err)
	}
}

func TestSpawnPeer_ErrorOnFailedSpawn(t *testing.T) {
	stub := &stubSpawner{
		stdout:   "",
		stderr:   "boom",
		exitCode: 1,
		err:      errors.New("simulated spawn failure"),
	}
	cfg := sampleConfig(stub)
	_, err := SpawnPeer(context.Background(), cfg)
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("expected ErrSpawnFailed; got %v", err)
	}
}

func TestSpawnTemplate_KnownDrivers(t *testing.T) {
	for _, driver := range []string{"claude", "copilot", "codex", "gemini"} {
		tmpl, ok := spawnTemplate(driver)
		if !ok {
			t.Errorf("driver %q should have a template", driver)
			continue
		}
		if tmpl.Command == "" {
			t.Errorf("driver %q template has empty Command", driver)
		}
		args := tmpl.ArgsFor("test-model")
		if len(args) == 0 {
			t.Errorf("driver %q template returned no args", driver)
		}
	}
}

func TestSpawnTemplate_UnknownDriver(t *testing.T) {
	if _, ok := spawnTemplate("nonexistent"); ok {
		t.Error("unknown driver should not return template")
	}
}

func TestSpawnTemplate_CopilotUsesNonInteractiveHelper(t *testing.T) {
	// Regression: 2026-05-06 in-gate test caught that the copilot
	// template used `gh copilot suggest -t shell` (interactive — exits
	// 1 in headless context, no output). Fix: route to scripts/peer-
	// copilot-chat.sh which hits the Copilot Chat completions API
	// directly. Lock the new shape so the regression can't return.
	tmpl, ok := spawnTemplate("copilot")
	if !ok {
		t.Fatal("copilot template missing")
	}
	if !strings.HasSuffix(tmpl.Command, "/scripts/peer-copilot-chat.sh") {
		t.Errorf("copilot Command should resolve to peer-copilot-chat.sh; got %q", tmpl.Command)
	}
	args := tmpl.ArgsFor("gpt-4.1")
	if len(args) != 1 || args[0] != "gpt-4.1" {
		t.Errorf("copilot args should be [model]; got %v", args)
	}
	for _, bad := range []string{"suggest", "-t", "shell"} {
		for _, a := range args {
			if a == bad {
				t.Errorf("copilot args still contains interactive-form arg %q (regression)", bad)
			}
		}
	}
}

func TestCopilotChatHelperPath_HonorsEnv(t *testing.T) {
	orig, hadOrig := os.LookupEnv("CHITIN_REPO")
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("CHITIN_REPO", orig)
		} else {
			os.Unsetenv("CHITIN_REPO")
		}
	})
	os.Setenv("CHITIN_REPO", "/custom/path/to/chitin")
	got := copilotChatHelperPath()
	want := "/custom/path/to/chitin/scripts/peer-copilot-chat.sh"
	if got != want {
		t.Errorf("CHITIN_REPO override: got %q want %q", got, want)
	}
}

func TestCopilotChatHelperPath_DefaultUnderHome(t *testing.T) {
	orig, hadOrig := os.LookupEnv("CHITIN_REPO")
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("CHITIN_REPO", orig)
		} else {
			os.Unsetenv("CHITIN_REPO")
		}
	})
	os.Unsetenv("CHITIN_REPO")
	got := copilotChatHelperPath()
	if !strings.HasSuffix(got, "/workspace/chitin/scripts/peer-copilot-chat.sh") {
		t.Errorf("default path should end with workspace/chitin/scripts/peer-copilot-chat.sh; got %q", got)
	}
}

func TestEscalationID_Unique(t *testing.T) {
	a := newEscalationID()
	b := newEscalationID()
	if a == b {
		t.Errorf("EscalationIDs should be unique; got %q twice", a)
	}
	if !strings.HasPrefix(a, "esc-") {
		t.Errorf("EscalationID format: got %q want esc-*", a)
	}
}

func TestSpawnPeer_DefaultTimeoutApplied(t *testing.T) {
	// When SpawnTimeoutSeconds <= 0, default 60s is used. We assert
	// indirectly: a 0-value timeout should NOT fire on a fast spawn.
	stub := &stubSpawner{stdout: "ok", exitCode: 0, sleep: 10 * time.Millisecond}
	cfg := sampleConfig(stub)
	cfg.SpawnTimeoutSeconds = 0
	_, err := SpawnPeer(context.Background(), cfg)
	if err != nil {
		t.Errorf("default timeout should be generous; got %v", err)
	}
}
