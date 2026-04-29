package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// runCLIWithHome runs the built chitin-kernel binary with CHITIN_HOME
// set to the given directory. Mirrors runCLI but extends env so envelope
// subcommands hit the test's sqlite db, not the operator's real one.
func runCLIWithHome(t *testing.T, home string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), testBinary, args...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "CHITIN_HOME="+home)
	stdout, err := cmd.Output()
	var stderr []byte
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = ee.Stderr
	}
	return string(stdout), string(stderr), cmd.ProcessState.ExitCode()
}

func envelopeCreate(t *testing.T, home string, calls, bytes int64) string {
	t.Helper()
	out, errOut, code := runCLIWithHome(t, home,
		"envelope", "create",
		"--calls", strconv.FormatInt(calls, 10),
		"--bytes", strconv.FormatInt(bytes, 10),
	)
	if code != 0 {
		t.Fatalf("envelope create exit=%d stderr=%s", code, errOut)
	}
	id := strings.TrimSpace(out)
	if len(id) != 26 {
		t.Fatalf("expected 26-char ULID, got %q (len=%d)", id, len(id))
	}
	return id
}

func TestEnvelope_CreateInspectListClose(t *testing.T) {
	home := t.TempDir()

	id := envelopeCreate(t, home, 100, 1024)

	// Inspect
	out, errOut, code := runCLIWithHome(t, home, "envelope", "inspect", id)
	if code != 0 {
		t.Fatalf("inspect exit=%d stderr=%s", code, errOut)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("parse inspect json: %v (out=%s)", err, out)
	}
	if st["id"] != id {
		t.Fatalf("inspect id mismatch: want %q got %v", id, st["id"])
	}
	limits := st["limits"].(map[string]any)
	if int64(limits["max_tool_calls"].(float64)) != 100 {
		t.Fatalf("max_tool_calls: want 100 got %v", limits["max_tool_calls"])
	}
	if st["closed_at"] != nil {
		t.Fatalf("closed_at should be empty/missing pre-close: %v", st["closed_at"])
	}

	// List should include the new envelope
	out, _, code = runCLIWithHome(t, home, "envelope", "list")
	if code != 0 {
		t.Fatalf("list exit=%d", code)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("parse list json: %v", err)
	}
	if len(arr) != 1 || arr[0]["id"] != id {
		t.Fatalf("list mismatch: %v", arr)
	}

	// Close (idempotent)
	if _, errOut, code := runCLIWithHome(t, home, "envelope", "close", id); code != 0 {
		t.Fatalf("close exit=%d stderr=%s", code, errOut)
	}
	if _, errOut, code := runCLIWithHome(t, home, "envelope", "close", id); code != 0 {
		t.Fatalf("close (idempotent re-call) exit=%d stderr=%s", code, errOut)
	}

	// Inspect after close: closed_at must be set
	out, _, _ = runCLIWithHome(t, home, "envelope", "inspect", id)
	_ = json.Unmarshal([]byte(out), &st)
	if st["closed_at"] == nil || st["closed_at"] == "" {
		t.Fatalf("closed_at should be set after close: %v", st["closed_at"])
	}
}

func TestEnvelope_NotFound(t *testing.T) {
	home := t.TempDir()
	cases := [][]string{
		{"envelope", "inspect", "FAKE-ID"},
		{"envelope", "grant", "FAKE-ID", "--calls=10"},
		{"envelope", "close", "FAKE-ID"},
		{"envelope", "use", "FAKE-ID"},
	}
	for _, args := range cases {
		_, errOut, code := runCLIWithHome(t, home, args...)
		if code == 0 {
			t.Fatalf("%v: expected non-zero exit, got 0", args)
		}
		if !strings.Contains(errOut, "envelope_not_found") {
			t.Fatalf("%v: expected envelope_not_found in stderr, got %s", args, errOut)
		}
	}
}

func TestEnvelope_CreateNegativeLimitsRejected(t *testing.T) {
	home := t.TempDir()
	cases := [][]string{
		{"envelope", "create", "--calls=-1"},
		{"envelope", "create", "--bytes=-1"},
		{"envelope", "create", "--usd=-0.1"},
	}
	for _, args := range cases {
		_, errOut, code := runCLIWithHome(t, home, args...)
		if code == 0 {
			t.Fatalf("%v: expected non-zero exit", args)
		}
		if !strings.Contains(errOut, "envelope_create_negative") {
			t.Fatalf("%v: expected envelope_create_negative in stderr, got %s", args, errOut)
		}
	}
}

func TestEnvelope_GrantReopensAndAudits(t *testing.T) {
	home := t.TempDir()
	id := envelopeCreate(t, home, 5, 0)

	// Close first
	if _, _, code := runCLIWithHome(t, home, "envelope", "close", id); code != 0 {
		t.Fatalf("close failed")
	}

	// Grant +calls — should reopen and bump cap
	if _, errOut, code := runCLIWithHome(t, home, "envelope", "grant", id, "--calls=10", "--reason=test-reopen"); code != 0 {
		t.Fatalf("grant exit=%d stderr=%s", code, errOut)
	}

	// Verify reopen + cap bump
	out, _, _ := runCLIWithHome(t, home, "envelope", "inspect", id)
	var st map[string]any
	_ = json.Unmarshal([]byte(out), &st)
	if v := st["closed_at"]; v != nil && v != "" {
		t.Fatalf("grant should have cleared closed_at, got %v", v)
	}
	limits := st["limits"].(map[string]any)
	if int64(limits["max_tool_calls"].(float64)) != 15 {
		t.Fatalf("max_tool_calls after grant: want 15 (5+10) got %v", limits["max_tool_calls"])
	}

	// Verify audit row was written
	date := time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(home, "gov-decisions-"+date+".jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"rule_id":"operator-grant"`) {
		t.Fatalf("audit log missing operator-grant row:\n%s", data)
	}
	if !strings.Contains(string(data), `"envelope_id":"`+id+`"`) {
		t.Fatalf("audit log missing envelope_id=%s:\n%s", id, data)
	}
	if !strings.Contains(string(data), "test-reopen") {
		t.Fatalf("audit log missing reason text 'test-reopen':\n%s", data)
	}
}

// TestEnvelope_AtomicUseRace asserts the spec invariant: under concurrent
// `envelope use <id>` calls, the final ~/.chitin/current-envelope content
// equals exactly one of the input IDs and is internally well-formed.
//
// rename(2) on POSIX is atomic for src+dst on the same filesystem, which
// is what writeCurrentEnvelope relies on. Verifying that here means: kick
// off N concurrent subprocess writers and one or more concurrent readers,
// then assert (a) final state ∈ {ids[0]..ids[N-1]}, (b) every observed
// read mid-race was either empty (file not yet existing) or one of the
// valid IDs — never a partial/torn line.
func TestEnvelope_AtomicUseRace(t *testing.T) {
	home := t.TempDir()
	const N = 8

	// Pre-create N envelopes so each `use` resolves a real id.
	ids := make([]string, N)
	for i := 0; i < N; i++ {
		ids[i] = envelopeCreate(t, home, 1, 1)
	}
	validIDs := map[string]struct{}{}
	for _, id := range ids {
		validIDs[id] = struct{}{}
	}

	// Concurrent reader: spin reading current-envelope until done. Records
	// every distinct value observed.
	target := filepath.Join(home, "current-envelope")
	stop := make(chan struct{})
	var observedMu sync.Mutex
	observed := map[string]struct{}{}
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(target)
			if err != nil {
				continue
			}
			s := strings.TrimSpace(string(b))
			observedMu.Lock()
			observed[s] = struct{}{}
			observedMu.Unlock()
		}
	}()

	// Launch N writers in parallel.
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errOut, code := runCLIWithHome(t, home, "envelope", "use", ids[i])
			if code != 0 {
				t.Errorf("use %s: exit=%d stderr=%s", ids[i], code, errOut)
			}
		}()
	}
	wg.Wait()
	close(stop)

	// Final state must be one of the IDs, never partial/empty.
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read final current-envelope: %v", err)
	}
	final := strings.TrimSpace(string(b))
	if _, ok := validIDs[final]; !ok {
		t.Fatalf("final content %q is not one of the valid IDs %v", final, ids)
	}

	// Every observed mid-race state must be a valid ID (or empty if the
	// reader saw EOF before any writer landed). Anything else means a
	// torn/partial read leaked through.
	observedMu.Lock()
	defer observedMu.Unlock()
	for s := range observed {
		if s == "" {
			continue
		}
		if _, ok := validIDs[s]; !ok {
			t.Fatalf("observed torn/partial read: %q not in valid-id set", s)
		}
	}
}

func TestEnvelopeTail_FormatRow(t *testing.T) {
	// Allow row with full payload — verify column layout is stable and
	// the ALLOW verdict lands on the right.
	got := formatRow(auditRow{
		Allowed:      true,
		Mode:         "enforce",
		RuleID:       "allow-read",
		Agent:        "claude-code",
		ActionType:   "file.read",
		ActionTarget: "/some/path/that/is/short.go",
		Ts:           "2026-04-29T15:01:02Z",
		Tier:         "T0",
		CostUSD:      0.0001,
	})
	for _, want := range []string{
		"2026-04-29T15:01:02Z", "claude-code", "T0", "$0.0001",
		"file.read", "/some/path/that/is/short.go", "ALLOW",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatRow row missing %q in: %q", want, got)
		}
	}

	// Deny row with no envelope/agent — verify empty-field placeholders
	// render and DENY appears.
	got = formatRow(auditRow{
		Allowed:      false,
		ActionType:   "git.push",
		ActionTarget: "main",
		Ts:           "2026-04-29T15:01:02Z",
	})
	if !strings.Contains(got, "DENY") {
		t.Fatalf("expected DENY in formatRow output, got %q", got)
	}
	if !strings.Contains(got, "  -  ") {
		t.Fatalf("expected dash placeholder for missing tier/agent, got %q", got)
	}

	// Long target should be truncated with an ellipsis so the column
	// layout stays scannable.
	long := strings.Repeat("a", 200)
	got = formatRow(auditRow{Allowed: true, ActionType: "file.read", ActionTarget: long, Ts: "t"})
	if !strings.Contains(got, "...") {
		t.Fatalf("expected long target to be truncated with '...', got %q", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1000, "1.0KB"},
		{2_300_000, "2.3MB"},
		{1_500_000_000, "1.5GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Fatalf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEnvelope_UseSetsResolverFile(t *testing.T) {
	home := t.TempDir()
	id := envelopeCreate(t, home, 5, 0)
	if _, errOut, code := runCLIWithHome(t, home, "envelope", "use", id); code != 0 {
		t.Fatalf("use exit=%d stderr=%s", code, errOut)
	}
	// gate_hook resolveEnvelope reads <chitinDir>/current-envelope.
	// Verify exact content + trailing newline (kubectl-style convention).
	b, err := os.ReadFile(filepath.Join(home, "current-envelope"))
	if err != nil {
		t.Fatalf("read current-envelope: %v", err)
	}
	if string(b) != id+"\n" {
		t.Fatalf("current-envelope content: want %q got %q", id+"\n", string(b))
	}
}
