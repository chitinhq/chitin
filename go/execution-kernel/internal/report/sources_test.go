// spec: 085-operator-report-delivery
package report

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// driverFromBranch extracts the driver from the agent/<driver>-<slug> (and
// legacy swarm/<driver>-<slug>) worker-branch convention.
func TestDriverFromBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"agent/codex-fix-telemetry", "codex"},
		{"agent/claude-084-schema-drift", "claude"},
		{"swarm/hermes-x", "hermes"},
		{"agent/copilot", "copilot"}, // no slug — whole rest is the driver
		{"main", ""},
		{"feat/085-operator-report-delivery", ""},
		{"agent/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			if got := driverFromBranch(tt.branch); got != tt.want {
				t.Errorf("driverFromBranch(%q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}

// groupDecisionsByDriver tallies per driver, prefers Driver over Agent, falls
// back to "(unattributed)", and returns a name-sorted (deterministic) result.
func TestGroupDecisionsByDriver(t *testing.T) {
	decs := []gov.Decision{
		{Driver: "codex", Allowed: true},
		{Driver: "codex", Allowed: false},
		{Agent: "hermes", Allowed: true}, // no Driver — Agent fallback
		{Allowed: true},                  // neither — unattributed
	}
	groups := groupDecisionsByDriver(decs)

	if len(groups) != 3 {
		t.Fatalf("want 3 driver groups, got %d: %+v", len(groups), groups)
	}
	// Sorted by name: "(unattributed)" < "codex" < "hermes".
	if groups[0].Driver != "(unattributed)" || groups[1].Driver != "codex" || groups[2].Driver != "hermes" {
		t.Errorf("groups not name-sorted: %+v", groups)
	}
	codex := groups[1]
	if codex.Total != 2 || codex.Allowed != 1 {
		t.Errorf("codex tally = total %d allowed %d, want total 2 allowed 1", codex.Total, codex.Allowed)
	}
}

func TestGroupDecisionsByDriver_Empty(t *testing.T) {
	if got := groupDecisionsByDriver(nil); len(got) != 0 {
		t.Errorf("empty input should yield no groups, got %+v", got)
	}
}

// consoleURL joins a base and route, and yields "" when no base is set so the
// digest renders the line without a (broken) link.
func TestConsoleURL(t *testing.T) {
	if got := consoleURL("http://c:4280/", "/overview"); got != "http://c:4280/overview" {
		t.Errorf("consoleURL trailing-slash join = %q", got)
	}
	if got := consoleURL("", "/overview"); got != "" {
		t.Errorf("consoleURL with no base must be empty, got %q", got)
	}
}

// prTitles caps the per-driver PR list so one busy driver cannot blow the
// digest length budget.
func TestPRTitles_CapsLongLists(t *testing.T) {
	var prs []ghPR
	for i := 0; i < 9; i++ {
		prs = append(prs, ghPR{Number: i})
	}
	got := prTitles(prs)
	if !strings.Contains(got, "+4 more") {
		t.Errorf("a 9-PR list should be capped with a +N more marker, got %q", got)
	}
}
