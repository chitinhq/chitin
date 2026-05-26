package claudecodeglm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCardDeclaresGLMContract(t *testing.T) {
	d := New(WithModel("glm-test"))
	card := d.Card()

	if d.ID() != "claudecode-glm" {
		t.Fatalf("ID() = %q, want claudecode-glm", d.ID())
	}
	if card.DriverID != d.ID() {
		t.Fatalf("card DriverID = %q, want %q", card.DriverID, d.ID())
	}
	if card.AgentRuntime != "claude-code" {
		t.Fatalf("runtime = %q, want claude-code", card.AgentRuntime)
	}
	if card.Model != "glm-test" {
		t.Fatalf("model = %q, want glm-test", card.Model)
	}
	if card.Tier != driver.TierLocal {
		t.Fatalf("tier = %s, want local", card.Tier)
	}
	if card.CostClass != driver.CostZero {
		t.Fatalf("cost = %s, want zero/free", card.CostClass)
	}
	for _, cap := range []driver.Capability{driver.CapCodeImplement, driver.CapSpecImplement} {
		if !card.HasCapability(cap) {
			t.Fatalf("card missing capability %q", cap)
		}
	}
	for _, cap := range []driver.Capability{driver.CapCodeReview, driver.CapSpecAuthor} {
		if card.HasCapability(cap) {
			t.Fatalf("card unexpectedly declares capability %q", cap)
		}
	}
	if card.Constraints.NetworkRequired {
		t.Fatalf("NetworkRequired = true, want false")
	}
	if !card.Constraints.WorktreeRequired {
		t.Fatalf("WorktreeRequired = false, want true")
	}
	if card.Constraints.MaxContextTokens != defaultContextTokens {
		t.Fatalf("MaxContextTokens = %d, want %d", card.Constraints.MaxContextTokens, defaultContextTokens)
	}
}

func TestNewReadsEnvironmentOverrides(t *testing.T) {
	t.Setenv(modelEnv, "qwen-local")
	t.Setenv(contextEnv, "65536")
	t.Setenv(ollamaBinEnv, "/bin/ollama-test")

	d := New()
	card := d.Card()
	if card.Model != "qwen-local" {
		t.Fatalf("model = %q, want env override", card.Model)
	}
	if card.Constraints.MaxContextTokens != 65536 {
		t.Fatalf("context = %d, want env override", card.Constraints.MaxContextTokens)
	}
	if d.ollamaBin != "/bin/ollama-test" {
		t.Fatalf("ollamaBin = %q, want env override", d.ollamaBin)
	}
}

func TestReady(t *testing.T) {
	dir := t.TempDir()
	ollama := fakeExecutable(t, dir, "ollama")
	claude := fakeExecutable(t, dir, "claude")
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)

	okClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != tagsURL {
			t.Fatalf("probe URL = %q, want %q", req.URL.String(), tagsURL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"models":[{"name":"glm-5.1"}]}`)),
			Header:     make(http.Header),
		}, nil
	})}

	cases := []struct {
		name      string
		driver    *Driver
		wantReady bool
		want      string
	}{
		{
			name:      "ready",
			driver:    New(WithOllamaBin(ollama), WithClaudeBin(claude), WithHTTPClient(okClient)),
			wantReady: true,
		},
		{
			name:   "ollama binary missing",
			driver: New(WithOllamaBin("definitely-not-ollama"), WithClaudeBin(claude), WithHTTPClient(okClient)),
			want:   "ollama binary",
		},
		{
			name: "daemon down",
			driver: New(WithOllamaBin(ollama), WithClaudeBin(claude), WithHTTPClient(&http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return nil, errors.New("connection refused")
				}),
			})),
			want: "ollama daemon not reachable",
		},
		{
			name: "model missing",
			driver: New(WithOllamaBin(ollama), WithClaudeBin(claude), WithHTTPClient(&http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"models":[{"name":"qwen"}]}`)),
						Header:     make(http.Header),
					}, nil
				}),
			})),
			want: "model glm-5.1 not present in ollama",
		},
		{
			name:   "claude missing",
			driver: New(WithOllamaBin(ollama), WithClaudeBin("definitely-not-claude"), WithHTTPClient(okClient)),
			want:   "Claude Code runtime",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ready, reason := tc.driver.Ready(context.Background())
			if ready != tc.wantReady {
				t.Fatalf("Ready() = %v, want %v; reason=%q", ready, tc.wantReady, reason)
			}
			if tc.want != "" && !strings.Contains(reason, tc.want) {
				t.Fatalf("Ready() reason = %q, want containing %q", reason, tc.want)
			}
		})
	}
}

func TestInvokeBuildsOllamaLaunchClaudeArgv(t *testing.T) {
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.log")
	ollama := filepath.Join(dir, "ollama")
	script := "#!/usr/bin/env bash\n" +
		"for a in \"$@\"; do echo \"$a\" >> " + argvPath + "; done\n" +
		"exit 0\n"
	if err := os.WriteFile(ollama, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ollama: %v", err)
	}

	d := New(WithOllamaBin(ollama), WithModel("glm-test"))
	wu := driver.WorkUnit{
		ID:           "wu-120",
		SpecID:       "120",
		Context:      "implement everything",
		WorktreePath: dir,
	}
	res, err := d.Invoke(context.Background(), wu)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.Status != driver.StatusSucceeded {
		t.Fatalf("status = %s, want succeeded: %+v", res.Status, res)
	}
	body, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(body)), "\n")
	wantPrefix := []string{"launch", "claude", "--model", "glm-test", "--", "--dangerously-skip-permissions", "-p"}
	if len(got) < len(wantPrefix)+1 {
		t.Fatalf("argv too short: %q", got)
	}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Fatalf("argv[%d] = %q, want %q; argv=%q", i, got[i], want, got)
		}
	}
	argvText := string(body)
	if !strings.Contains(argvText, "Chitin work unit: wu-120") {
		t.Fatalf("prompt missing work unit header: %q", argvText)
	}
	if !strings.Contains(argvText, "implement everything") {
		t.Fatalf("prompt missing context: %q", argvText)
	}
}

func fakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	return path
}
