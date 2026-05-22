// spec: 083-driver-governance-telemetry
package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// classifyStaleness is the pure core of kernel-staleness detection. An empty
// revision on either side must be unknown — never silently "current".
func TestClassifyStaleness(t *testing.T) {
	tests := []struct {
		name            string
		running, source string
		want            string
	}{
		{"equal revisions are current", "abc123def456", "abc123def456", StalenessCurrent},
		{"differing revisions are stale", "abc123def456", "999888777666", StalenessStale},
		{"empty running revision is unknown", "", "999888777666", StalenessUnknown},
		{"empty source revision is unknown", "abc123def456", "", StalenessUnknown},
		{"both empty is unknown", "", "", StalenessUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := classifyStaleness(tt.running, tt.source)
			if got != tt.want {
				t.Errorf("classifyStaleness(%q, %q) = %q, want %q", tt.running, tt.source, got, tt.want)
			}
		})
	}
}

// A stale verdict must name both revisions in its detail so the operator sees
// the delta without a second lookup.
func TestClassifyStaleness_StaleDetailNamesBothRevisions(t *testing.T) {
	_, detail := classifyStaleness("aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb")
	if !strings.Contains(detail, "aaaaaaaaaaaa") || !strings.Contains(detail, "bbbbbbbbbbbb") {
		t.Errorf("stale detail should name both short revisions, got %q", detail)
	}
}

// A binary path that cannot be read yields unknown — a health probe degrades,
// it never black-boxes or falsely reports current.
func TestGatherKernelStaleness_UnreadableBinaryIsUnknown(t *testing.T) {
	ks := GatherKernelStaleness(filepath.Join(t.TempDir(), "no-such-binary"), t.TempDir())
	if ks.Status != StalenessUnknown {
		t.Errorf("want status %q for unreadable binary, got %q", StalenessUnknown, ks.Status)
	}
	if ks.Detail == "" {
		t.Errorf("unknown status must carry a Detail explaining why")
	}
}

// classifyRedeploy maps an install-kernel.sh emit `kind` to a redeploy status.
func TestClassifyRedeploy(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"ok", RedeployOK},
		{"noop", RedeployOK},
		{"deferred", RedeployOK},
		{"warn", RedeployOK},
		{"fail", RedeployFailed},
		{"rollback", RedeployFailed},
		{"", RedeployUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got := classifyRedeploy(tt.kind); got != tt.want {
				t.Errorf("classifyRedeploy(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "install-kernel.jsonl")
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write log fixture: %v", err)
	}
	return p
}

// The redeploy status reflects the LAST log line — an earlier run does not mask
// a recent failure, and a recent success clears an earlier failure.
func TestGatherRedeployHealth_LastLineWins(t *testing.T) {
	p := writeLog(t,
		`{"ts":"2026-05-22T10:00:00Z","kind":"fail","msg":"build-failed"}`,
		`{"ts":"2026-05-22T10:15:00Z","kind":"ok","msg":"redeploy-success"}`,
	)
	rh := GatherRedeployHealth(p)
	if rh.Status != RedeployOK {
		t.Errorf("want %q (last line ok), got %q", RedeployOK, rh.Status)
	}
	if rh.LastKind != "ok" {
		t.Errorf("want LastKind ok, got %q", rh.LastKind)
	}
}

func TestGatherRedeployHealth_RollbackLineIsFailed(t *testing.T) {
	p := writeLog(t,
		`{"ts":"2026-05-22T10:00:00Z","kind":"ok","msg":"redeploy-success"}`,
		`{"ts":"2026-05-22T10:15:00Z","kind":"rollback","msg":"smoke-rollback-success"}`,
	)
	rh := GatherRedeployHealth(p)
	if rh.Status != RedeployFailed {
		t.Errorf("want %q (last line rollback), got %q", RedeployFailed, rh.Status)
	}
}

// Trailing blank lines must not mask the real last record.
func TestGatherRedeployHealth_IgnoresTrailingBlankLines(t *testing.T) {
	p := writeLog(t,
		`{"ts":"2026-05-22T10:15:00Z","kind":"fail","msg":"pull-conflict"}`,
		``,
		``,
	)
	rh := GatherRedeployHealth(p)
	if rh.Status != RedeployFailed {
		t.Errorf("want %q, got %q", RedeployFailed, rh.Status)
	}
}

// A missing log is unknown — the redeploy timer may not have run yet. It must
// never be reported as ok.
func TestGatherRedeployHealth_MissingLogIsUnknown(t *testing.T) {
	rh := GatherRedeployHealth(filepath.Join(t.TempDir(), "absent.jsonl"))
	if rh.Status != RedeployUnknown {
		t.Errorf("want %q for missing log, got %q", RedeployUnknown, rh.Status)
	}
}

func TestGatherRedeployHealth_EmptyLogIsUnknown(t *testing.T) {
	rh := GatherRedeployHealth(writeLog(t))
	if rh.Status != RedeployUnknown {
		t.Errorf("want %q for empty log, got %q", RedeployUnknown, rh.Status)
	}
}

// A corrupt last line is unknown — not a crash, and not a false ok.
func TestGatherRedeployHealth_UnparseableLastLineIsUnknown(t *testing.T) {
	rh := GatherRedeployHealth(writeLog(t, `{not json`))
	if rh.Status != RedeployUnknown {
		t.Errorf("want %q for unparseable line, got %q", RedeployUnknown, rh.Status)
	}
}

// The parsed timestamp is surfaced so the operator sees how recent the verdict is.
func TestGatherRedeployHealth_ParsesTimestamp(t *testing.T) {
	p := writeLog(t, `{"ts":"2026-05-22T10:15:00Z","kind":"ok","msg":"redeploy-success"}`)
	rh := GatherRedeployHealth(p)
	if rh.LastTs.IsZero() {
		t.Errorf("want a parsed LastTs, got zero")
	}
}
