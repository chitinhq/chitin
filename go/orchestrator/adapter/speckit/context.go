package speckit

import (
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
)

// excerptMaxLines caps how many lines of a located spec/plan section are
// carried onto a node. The excerpt is framing context for a driver, not the
// whole document; a bounded excerpt keeps the in-memory DAG compact and the
// compile output deterministic in size.
const excerptMaxLines = 40

// BuildContext extracts the per-node Task Context for one spec-kit task
// (FR-005). It pulls FR references and file paths from the task line — via
// adapter.NewTaskContext — and attaches the framing excerpts located in the
// spec's spec.md and plan.md so a driver can act without re-reading the kit.
//
// specRef is the spec the task belongs to (e.g. "077"); specMD and planMD
// are the full text of the spec's spec.md and plan.md (empty string when the
// file is absent — both are optional framing, not required artifacts).
//
// The spec excerpt is the user-story section matching the task's first `[USn]`
// story tag when one is present; otherwise the spec's Requirements section.
// The plan excerpt is the plan's Summary section. Both are best-effort: an
// excerpt that cannot be located is left empty, never guessed.
func BuildContext(specRef string, t Task, specMD, planMD string) *adapter.TaskContext {
	ctx := adapter.NewTaskContext(specRef, t.ID, t.Description)
	specExcerpt := locateSpecExcerpt(specMD, t.Stories)
	planExcerpt := sectionExcerpt(planMD, "## Summary")
	ctx.AttachExcerpts(specExcerpt, planExcerpt)
	return ctx
}

// locateSpecExcerpt returns the spec.md excerpt that frames a task: the
// User-Story section for the task's first story tag when it has one,
// otherwise the Requirements section. An empty result means no framing
// section could be located.
func locateSpecExcerpt(specMD string, stories []string) string {
	if len(stories) > 0 {
		if ex := userStoryExcerpt(specMD, stories[0]); ex != "" {
			return ex
		}
	}
	return sectionExcerpt(specMD, "## Requirements")
}

// userStoryExcerpt locates the "### User Story N …" heading for the given
// story tag (e.g. "US1" → story number 1) and returns its section text.
func userStoryExcerpt(specMD, storyTag string) string {
	num := strings.TrimPrefix(strings.ToUpper(storyTag), "US")
	if num == storyTag || num == "" {
		return "" // not a USn-shaped tag
	}
	return headingExcerpt(specMD, "### User Story "+num)
}

// sectionExcerpt returns the text under the heading line that exactly equals
// heading, up to the next heading of the same-or-shallower level. An empty
// result means the heading was not found.
func sectionExcerpt(doc, heading string) string {
	return headingExcerpt(doc, heading)
}

// headingExcerpt scans doc for the first line that begins with headingPrefix
// and returns the section that follows it — the heading line plus its body —
// stopping at the next heading of the same or a shallower Markdown level, and
// truncated to excerptMaxLines. The match is a prefix match so "### User
// Story 1" finds "### User Story 1 - Compile a spec-kit repo …".
func headingExcerpt(doc, headingPrefix string) string {
	if doc == "" {
		return ""
	}
	lines := strings.Split(doc, "\n")
	level := headingLevel(headingPrefix)
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, headingPrefix) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	out := []string{lines[start]}
	for i := start + 1; i < len(lines) && len(out) < excerptMaxLines; i++ {
		if l := headingLevel(lines[i]); l > 0 && l <= level {
			break
		}
		out = append(out, lines[i])
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n ")
}

// headingLevel returns the Markdown heading level of a line (1 for "# ", 2
// for "## ", …) or 0 if the line is not a heading.
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n > 0 && n < len(line) && line[n] == ' ' {
		return n
	}
	return 0
}
