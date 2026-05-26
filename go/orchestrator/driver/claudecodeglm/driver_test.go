package claudecodeglm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

func TestCardDeclaresClaudeCodeGLMContract(t *testing.T) {
	d := New(WithModel("glm-test"))
	card := d.Card()

	if d.ID() != "claudecode-glm" {
		t.Fatalf("ID() = %q, want claudecode-glm", d.ID())
	}
	if card.DriverID != d.ID() {
		t.Fatalf("card DriverID = %q, want %q", card.DriverID, d.ID())
	}
	if card.AgentRuntime != "claude-code" {
		t.Errorf("AgentRuntime = %q, want claude-code", card.AgentRuntime)
	}
	if card.Model != "glm-test" {
		t.Errorf("Model = %q, want glm-test", card.Model)
	}
	if card.Tier != driver.TierLocal {
		t.Errorf("Tier = %s, want local", card.Tier)
	}
	if card.CostClass != driver.CostZero {
		t.Errorf("CostClass = %s, want zero/free", card.CostClass)
	}
	for _, cap := range []driver.Capability{driver.CapCodeImplement, driver.CapSpecImplement} {
		if !card.HasCapability(cap) {
			t.Errorf("card missing capability %q", cap)
		}
	}
	for _, cap := range []driver.Capability{driver.CapCodeReview, driver.CapSpecAuthor} {
		if card.HasCapability(cap) {
			t.Errorf("card unexpectedly declares capability %q", cap)
		}
	}
	if card.Constraints.NetworkRequired {
		t.Error("NetworkRequired = true, want local-only false")
	}
	if !card.Constraints.WorktreeRequired {
		t.Error("WorktreeRequired = false, want true")
	}
	if card.Constraints.MaxContextTokens != 32768 {
		t.Errorf("MaxContextTokens = %d, want 32768", card.Constraints.MaxContextTokens)
	}
}

func TestCardHonorsEnvOverrides(t *testing.T) {
	t.Setenv(modelEnv, "env-glm")
	t.Setenv(contextEnv, "65536")
	card := New().Card()
	if card.Model != "env-glm" {
		t.Errorf("Model = %q, want env-glm", card.Model)
	}
	if card.Constraints.MaxContextTokens != 65536 {
		t.Errorf("MaxContextTokens = %d, want 65536", card.Constraints.MaxContextTokens)
	}
}

func TestReady(t *testing.T) {
	dir := t.TempDir()
	ollama := writeShim(t, dir, "ollama", "exit 0\n")
	claude := writeShim(t, dir, "claude", "exit 0\n")

	tagsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("probe path = %q, want /api/tags", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[{"name":"glm-5.1:latest","model":"glm-5.1"}]}`))
	}))
	t.Cleanup(tagsServer.Close)

	cases := []struct {
		name      string
		driver    *Driver
		wantReady bool
		wantText  string
	}{
		{
			name:      "ready",
			driver:    New(WithOllamaCommand(ollama), WithClaudeCommand(claude), WithBaseURL(tagsServer.URL)),
			wantReady: true,
		},
		{
			name:      "ollama binary missing",
			driver:    New(WithOllamaCommand(filepath.Join(dir, "missing-ollama")), WithClaudeCommand(claude), WithBaseURL(tagsServer.URL)),
			wantReady: false,
			wantText:  "ollama binary",
		},
		{
			name:      "ollama daemon down",
			driver:    New(WithOllamaCommand(ollama), WithClaudeCommand(claude), WithBaseURL("http://127.0.0.1:1")),
			wantReady: false,
			wantText:  "ollama daemon not reachable",
		},
		{
			name: "model missing",
			driver: New(
				WithOllamaCommand(ollama),
				WithClaudeCommand(claude),
				WithBaseURL(serverReturning(t, `{"models":[{"name":"qwen3-coder:latest"}]}`).URL),
			),
			wantReady: false,
			wantText:  "model glm-5.1 not present",
		},
		{
			name:      "claude missing",
			driver:    New(WithOllamaCommand(ollama), WithClaudeCommand(filepath.Join(dir, "missing-claude")), WithBaseURL(tagsServer.URL)),
			wantReady: false,
			wantText:  "claude CLI binary",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ready, reason := tc.driver.Ready(context.Background())
			if ready != tc.wantReady {
				t.Fatalf("Ready() ready = %v, want %v; reason=%q", ready, tc.wantReady, reason)
			}
			if tc.wantText != "" && !strings.Contains(reason, tc.wantText) {
				t.Fatalf("Ready() reason = %q, want containing %q", reason, tc.wantText)
			}
			if tc.wantReady && reason != "" {
				t.Fatalf("Ready() reason on success = %q, want empty", reason)
			}
		})
	}
}

func TestInvokeBuildsOllamaLaunchClaudeArgv(t *testing.T) {
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.log")
	ollama := writeShim(t, dir, "ollama", "for a in \"$@\"; do echo \"$a\" >> "+argvPath+"; done\nexit 0\n")
	d := New(WithOllamaCommand(ollama), WithModel("glm-test"))

	res, err := d.Invoke(context.Background(), driver.WorkUnit{
		ID:           "wu-test",
		SpecID:       "120",
		Context:      "Implement the thing.",
		WorktreePath: dir,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Status != driver.StatusSucceeded {
		t.Fatalf("status = %s, want succeeded; explanation=%q", res.Status, res.Explanation)
	}
	body, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(body)), "\n")
	wantPrefix := []string{"launch", "claude", "--model", "glm-test", "--", "--dangerously-skip-permissions", "-p"}
	if len(argv) < len(wantPrefix)+1 {
		t.Fatalf("argv too short: %q", string(body))
	}
	for i, want := range wantPrefix {
		if argv[i] != want {
			t.Fatalf("argv[%d] = %q, want %q; argv=%v", i, argv[i], want, argv)
		}
	}
	prompt := strings.Join(argv[len(wantPrefix):], "\n")
	if !strings.Contains(prompt, "Chitin work unit: wu-test") {
		t.Errorf("prompt arg missing work unit header: %q", prompt)
	}
	if !strings.Contains(prompt, "Implement the thing.") {
		t.Errorf("prompt arg missing context: %q", prompt)
	}
}

func TestInvokeExplainsOldOllamaLaunchFailure(t *testing.T) {
	dir := t.TempDir()
	ollama := writeShim(t, dir, "ollama", "echo 'unknown command \"launch\" for \"ollama\"' >&2\nexit 1\n")
	d := New(WithOllamaCommand(ollama))

	res, err := d.Invoke(context.Background(), driver.WorkUnit{ID: "wu-test", WorktreePath: dir})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Status != driver.StatusFailed {
		t.Fatalf("status = %s, want failed", res.Status)
	}
	if !strings.Contains(res.Explanation, "ollama v0.21+ required") {
		t.Fatalf("explanation = %q, want v0.21+ guidance", res.Explanation)
	}
}

func writeShim(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body), 0o755); err != nil {
		t.Fatalf("write shim %s: %v", name, err)
	}
	return path
}

func serverReturning(t *testing.T, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}
