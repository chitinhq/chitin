// spec: 085-operator-report-delivery
package report

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/health"
)

// kernelComponent: current+ok is healthy; stale or failed redeploy is
// degraded; an unknown on either side is unknown — never healthy on absence.
func TestKernelComponent(t *testing.T) {
	tests := []struct {
		name      string
		stale     string
		redeploy  string
		wantState ComponentState
	}{
		{"current and ok", health.StalenessCurrent, health.RedeployOK, StateHealthy},
		{"stale kernel", health.StalenessStale, health.RedeployOK, StateDegraded},
		{"failed redeploy", health.StalenessCurrent, health.RedeployFailed, StateDegraded},
		{"unknown staleness", health.StalenessUnknown, health.RedeployOK, StateUnknown},
		{"unknown redeploy", health.StalenessCurrent, health.RedeployUnknown, StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := kernelComponent(
				health.KernelStaleness{Status: tt.stale},
				health.RedeployHealth{Status: tt.redeploy},
			)
			if c.State != tt.wantState {
				t.Errorf("kernelComponent(%s,%s) state = %q, want %q", tt.stale, tt.redeploy, c.State, tt.wantState)
			}
			if c.Name != "kernel" {
				t.Errorf("component name = %q, want kernel", c.Name)
			}
		})
	}
}

// systemctlComponent keys off the printed word, not the exit code (is-active
// exits non-zero for inactive/failed units). An unrecognised word is unknown.
func TestSystemctlComponent(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		runErr    error
		wantState ComponentState
	}{
		{"active", "active\n", nil, StateHealthy},
		{"inactive despite exit error", "inactive\n", errors.New("exit status 3"), StateDegraded},
		{"failed despite exit error", "failed\n", errors.New("exit status 3"), StateDegraded},
		{"no output and a run error", "", errors.New("systemctl not found"), StateUnknown},
		{"unrecognised word", "weird\n", nil, StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := systemctlComponent("gateway", tt.output, tt.runErr)
			if c.State != tt.wantState {
				t.Errorf("systemctlComponent(%q) state = %q, want %q", tt.output, c.State, tt.wantState)
			}
		})
	}
}

// agentsComponent: at least one active surface is healthy; zero is degraded.
func TestAgentsComponent(t *testing.T) {
	if c := agentsComponent([]string{"hermes", "clawta"}); c.State != StateHealthy {
		t.Errorf("two active surfaces should be healthy, got %q", c.State)
	}
	if c := agentsComponent(nil); c.State != StateDegraded {
		t.Errorf("zero active surfaces should be degraded, got %q", c.State)
	}
}

// HeartbeatMessage renders every component, and surfaces missed reports as
// their own section only when there are any.
func TestHeartbeatMessage(t *testing.T) {
	hb := Heartbeat{
		Components: []ComponentStatus{
			{Name: "kernel", State: StateHealthy, Detail: "ok"},
			{Name: "gateway", State: StateHealthy, Detail: "active"},
		},
	}
	m := HeartbeatMessage(hb)
	out := Render(m, DefaultMaxLen)
	if !strings.Contains(out, "kernel: healthy") || !strings.Contains(out, "gateway: healthy") {
		t.Errorf("message must render every component, got %q", out)
	}
	if strings.Contains(out, "Missed reports") {
		t.Errorf("no missed-reports section expected when there are none, got %q", out)
	}

	hb.MissedReports = []string{"digest at 2026-05-22T10:00:00Z: discord 503"}
	out = Render(HeartbeatMessage(hb), DefaultMaxLen)
	if !strings.Contains(out, "Missed reports") || !strings.Contains(out, "discord 503") {
		t.Errorf("missed reports must be surfaced, got %q", out)
	}
}

// missedReports returns failures recorded after the last successful delivery;
// an earlier failure that was followed by a success is not re-reported.
func TestMissedReports(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "operator-report.jsonl")
	lines := []string{
		`{"ts":"2026-05-22T08:00:00Z","kind":"heartbeat","outcome":"failed","detail":"old failure"}`,
		`{"ts":"2026-05-22T09:00:00Z","kind":"heartbeat","outcome":"delivered","detail":"ok"}`,
		`{"ts":"2026-05-22T10:00:00Z","kind":"digest","outcome":"failed","detail":"discord 503"}`,
	}
	if err := os.WriteFile(log, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	missed := missedReports(log)
	if len(missed) != 1 {
		t.Fatalf("want 1 missed report (only the one after the last success), got %d: %v", len(missed), missed)
	}
	if !strings.Contains(missed[0], "discord 503") {
		t.Errorf("missed report should name the recent failure, got %q", missed[0])
	}
}

// A missing delivery log means nothing was missed — never a crash.
func TestMissedReports_MissingLogIsEmpty(t *testing.T) {
	if got := missedReports(filepath.Join(t.TempDir(), "absent.jsonl")); got != nil {
		t.Errorf("missing log should yield no missed reports, got %v", got)
	}
}
