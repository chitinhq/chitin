// spec: 085-operator-report-delivery
package report

import (
	"strings"
	"testing"
	"time"
)

// GatherDigest always produces exactly four sections, in the fixed order
// orchestration → kernel → driver activity → PRs, even when every telemetry
// source is empty or unreadable (FR-006, FR-009).
func TestGatherDigest_AlwaysFourSectionsInOrder(t *testing.T) {
	d := GatherDigest(DigestSources{
		ChitinDir:  t.TempDir(), // empty — no events, no decisions
		KernelBin:  "/nonexistent/kernel",
		RepoDir:    t.TempDir(),
		InstallLog: "/nonexistent/install-kernel.jsonl",
		Window:     24 * time.Hour,
	}, false)

	if len(d.Sections) != 4 {
		t.Fatalf("want exactly 4 sections, got %d", len(d.Sections))
	}
	wantTitles := []string{"Orchestration", "Kernel", "Driver activity", "PRs shipped"}
	for i, want := range wantTitles {
		if d.Sections[i].Title != want {
			t.Errorf("section %d title = %q, want %q", i, d.Sections[i].Title, want)
		}
	}
}

// An unavailable source yields a section marked unavailable — never a section
// dropped from the digest.
func TestGatherDigest_KernelSectionAlwaysAvailable(t *testing.T) {
	d := GatherDigest(DigestSources{
		ChitinDir: t.TempDir(), KernelBin: "/nonexistent", RepoDir: t.TempDir(),
		Window: 24 * time.Hour,
	}, false)
	// The kernel section's detectors self-degrade to `unknown`, so the section
	// is always Available — it carries an unknown verdict, not an absence.
	if !d.Sections[1].Available {
		t.Errorf("kernel section must always be available (detectors self-degrade)")
	}
}

// DigestMessage renders the scope (daily vs on-demand) into the heading and
// passes the four sections through to the renderer unchanged.
func TestDigestMessage(t *testing.T) {
	d := TelemetryDigest{
		TS:       time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC),
		Window:   24 * time.Hour,
		OnDemand: true,
		Sections: []Section{{Title: "Kernel", Available: true, Lines: []Line{{Text: "ok"}}}},
	}
	m := DigestMessage(d)
	if !strings.Contains(m.Heading, "on-demand") {
		t.Errorf("on-demand digest heading must say so, got %q", m.Heading)
	}
	if len(m.Sections) != 1 || m.Sections[0].Title != "Kernel" {
		t.Errorf("DigestMessage must pass sections through unchanged, got %+v", m.Sections)
	}

	d.OnDemand = false
	if got := DigestMessage(d); !strings.Contains(got.Heading, "daily") {
		t.Errorf("scheduled digest heading must say daily, got %q", got.Heading)
	}
}
