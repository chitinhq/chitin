package report

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/health"
)

// DigestSources is the I/O configuration for gathering the four digest
// sections (spec 085 US2). Each section gatherer degrades to an unavailable
// Section on a source failure — it never aborts the digest (FR-009).
type DigestSources struct {
	ChitinDir   string        // .chitin event + gov-decisions dir
	KernelBin   string        // installed kernel binary — staleness source
	RepoDir     string        // chitin source repo — staleness source
	InstallLog  string        // install-kernel.jsonl — redeploy-health source
	Window      time.Duration // the period the digest summarises
	ConsoleBase string        // chitin-console base URL; empty disables links
}

// consoleURL joins the console base with a route, or returns "" when no base
// is configured (the digest then renders the line without a link).
func consoleURL(base, route string) string {
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + route
}

// detailSuffix renders an optional parenthetical detail.
func detailSuffix(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}

// windowHours converts the digest window to whole hours, floored at 1 so a
// sub-hour window still queries a valid range.
func windowHours(d time.Duration) int {
	h := int(d.Hours())
	if h < 1 {
		return 1
	}
	return h
}

// orchestrationSection summarises event-pipeline activity from `chitin health`.
func orchestrationSection(s DigestSources) Section {
	sec := Section{Title: "Orchestration"}
	rep, err := health.Gather(s.ChitinDir, s.Window)
	if err != nil {
		sec.Unavailable = err.Error()
		return sec
	}
	sec.Available = true
	sec.Lines = append(sec.Lines, Line{
		Text: fmt.Sprintf("%d events across %d surface(s) in the last %s",
			rep.EventsTotal, len(rep.EventsByWindow), s.Window),
		Link: consoleURL(s.ConsoleBase, "/overview"),
	})
	// HookFailureCount comes from kernel-errors.log and is a real signal.
	// SchemaDriftCount is deliberately NOT surfaced: health.Gather scans every
	// *.jsonl in the state dir, so it counts gov-decisions and other non-event
	// logs as "drift" — a number dominated by noise, not by real drift.
	if rep.HookFailureCount > 0 {
		sec.Lines = append(sec.Lines, Line{
			Text: fmt.Sprintf("%d hook failure(s) in the window", rep.HookFailureCount),
		})
	}
	if rep.ClockSkewSuspected {
		sec.Lines = append(sec.Lines, Line{Text: "⚠ clock skew suspected in event timestamps"})
	}
	return sec
}

// kernelSection reports kernel staleness and the last redeploy outcome —
// always available, since the underlying detectors self-degrade to `unknown`.
func kernelSection(s DigestSources) Section {
	ks := health.GatherKernelStaleness(s.KernelBin, s.RepoDir)
	rh := health.GatherRedeployHealth(s.InstallLog)
	return Section{
		Title:     "Kernel",
		Available: true,
		Lines: []Line{
			{Text: fmt.Sprintf("staleness: %s%s", ks.Status, detailSuffix(ks.Detail))},
			{Text: fmt.Sprintf("last redeploy: %s%s", rh.Status, detailSuffix(rh.LastMsg))},
		},
	}
}

// driverAgg is the per-driver decision tally — the pure grouping output.
type driverAgg struct {
	Driver  string
	Total   int
	Allowed int
}

// groupDecisionsByDriver tallies decisions per attributed driver, preferring
// the Driver field, falling back to Agent, then to "(unattributed)". The
// result is sorted by driver name so two runs over the same input render
// identically (a deterministic tie-break, per the Knuth lens).
func groupDecisionsByDriver(decs []gov.Decision) []driverAgg {
	by := map[string]*driverAgg{}
	for _, d := range decs {
		key := d.Driver
		if key == "" {
			key = d.Agent
		}
		if key == "" {
			key = "(unattributed)"
		}
		a := by[key]
		if a == nil {
			a = &driverAgg{Driver: key}
			by[key] = a
		}
		a.Total++
		if d.Allowed {
			a.Allowed++
		}
	}
	out := make([]driverAgg, 0, len(by))
	for _, a := range by {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Driver < out[j].Driver })
	return out
}

// driversSection groups recent governance decisions by attributed driver.
func driversSection(s DigestSources) Section {
	sec := Section{Title: "Driver activity"}
	decs, err := gov.ReadRecent(gov.ReadRecentArgs{
		Dir:         s.ChitinDir,
		WindowHours: windowHours(s.Window),
		Limit:       5000,
	})
	if err != nil {
		sec.Unavailable = err.Error()
		return sec
	}
	sec.Available = true
	groups := groupDecisionsByDriver(decs)
	if len(groups) == 0 {
		sec.Lines = []Line{{Text: "no governance decisions in the window"}}
		return sec
	}
	for _, g := range groups {
		sec.Lines = append(sec.Lines, Line{
			Text: fmt.Sprintf("%s — %d decisions (%d allowed, %d denied)",
				g.Driver, g.Total, g.Allowed, g.Total-g.Allowed),
			Link: consoleURL(s.ConsoleBase, "/tickets?assignee="+g.Driver),
		})
	}
	return sec
}

// ghPR is the subset of `gh pr list --json` output the digest uses.
type ghPR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	HeadRefName string `json:"headRefName"`
	State       string `json:"state"`
}

// driverFromBranch extracts the driver from a worker branch name. The
// convention is `agent/<driver>-<slug>` (legacy `swarm/<driver>-<slug>`); a
// branch in neither shape yields "" — the PR is then grouped as unattributed.
func driverFromBranch(branch string) string {
	for _, prefix := range []string{"agent/", "swarm/"} {
		if rest, ok := strings.CutPrefix(branch, prefix); ok {
			if i := strings.IndexByte(rest, '-'); i > 0 {
				return rest[:i]
			}
			return rest
		}
	}
	return ""
}

// prsSection lists the PRs shipped in the window, attributed to drivers by the
// `agent/<driver>-<slug>` branch convention.
func prsSection(s DigestSources) Section {
	sec := Section{Title: "PRs shipped"}
	since := time.Now().Add(-s.Window).UTC().Format("2006-01-02")
	out, err := exec.Command("gh", "pr", "list",
		"--state", "all",
		"--search", "created:>="+since,
		"--limit", "100",
		"--json", "number,title,headRefName,state",
	).Output()
	if err != nil {
		sec.Unavailable = "gh pr list failed: " + err.Error()
		return sec
	}
	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		sec.Unavailable = "gh pr list output unparseable"
		return sec
	}
	sec.Available = true
	if len(prs) == 0 {
		sec.Lines = []Line{{Text: fmt.Sprintf("no PRs opened since %s", since)}}
		return sec
	}
	byDriver := map[string][]ghPR{}
	for _, pr := range prs {
		d := driverFromBranch(pr.HeadRefName)
		if d == "" {
			d = "(unattributed)"
		}
		byDriver[d] = append(byDriver[d], pr)
	}
	drivers := make([]string, 0, len(byDriver))
	for d := range byDriver {
		drivers = append(drivers, d)
	}
	sort.Strings(drivers)
	for _, d := range drivers {
		list := byDriver[d]
		sec.Lines = append(sec.Lines, Line{
			Text: fmt.Sprintf("%s — %d PR(s): %s", d, len(list), prTitles(list)),
		})
	}
	return sec
}

// prTitles renders a compact "#NN title" list, capped so one busy driver does
// not blow the digest's length budget — depth is the console's job.
func prTitles(prs []ghPR) string {
	const cap = 5
	parts := make([]string, 0, cap)
	for i, pr := range prs {
		if i == cap {
			parts = append(parts, fmt.Sprintf("+%d more", len(prs)-cap))
			break
		}
		parts = append(parts, fmt.Sprintf("#%d", pr.Number))
	}
	return strings.Join(parts, ", ")
}
