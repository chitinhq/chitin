package adapter

import (
	"regexp"
	"sort"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TaskContext is the per-node payload an adapter extracts from a kit so a
// driver can act on a work unit without re-reading the kit's files (FR-005).
// It is purely descriptive — references and excerpts — and carries no
// scheduling state. The DAG node it accompanies holds the routing inputs;
// this holds the human-and-driver-facing context.
//
// A TaskContext is built deterministically: every slice it exposes is sorted
// or in source order, never in map-iteration order, so the same spec always
// yields the same context (compilation determinism — plan.md Constraints).
type TaskContext struct {
	// SpecRef is the source spec the task derives from — e.g. "077".
	SpecRef string
	// TaskRef is the task id within the spec — e.g. "T009".
	TaskRef string
	// Description is the task's one-line description, taxonomy/story markers
	// stripped, as written in the kit artifact.
	Description string
	// FRRefs is the sorted, de-duplicated set of functional-requirement ids
	// (e.g. "FR-004") the task line cites — the spec sections the work
	// implements.
	FRRefs []string
	// FilePaths is the sorted, de-duplicated set of repo file paths the task
	// line names — the files the work touches.
	FilePaths []string
	// SpecExcerpt is the excerpt of the spec's spec.md that frames this work —
	// the user story or requirement block, empty when none could be located.
	SpecExcerpt string
	// PlanExcerpt is the excerpt of the spec's plan.md that frames this work,
	// empty when none could be located.
	PlanExcerpt string
	// Clarifications lists every reason this task could not be fully resolved
	// — an unmappable capability, an ambiguous dependency. A non-empty slice
	// means the node carries the NeedsClarification marker (FR-009, FR-014).
	// It is sorted for determinism.
	Clarifications []string
}

// NeedsClarification reports whether this context recorded any unresolved
// ambiguity. A node built from such a context MUST carry the
// NeedsClarification marker rather than an invented edge or tag.
func (c *TaskContext) NeedsClarification() bool {
	return len(c.Clarifications) > 0
}

// addClarification records an ambiguity reason, keeping the slice sorted and
// de-duplicated so the context stays deterministic.
func (c *TaskContext) addClarification(reason string) {
	for _, r := range c.Clarifications {
		if r == reason {
			return
		}
	}
	c.Clarifications = append(c.Clarifications, reason)
	sort.Strings(c.Clarifications)
}

var (
	// frRefRe matches a functional-requirement citation: FR-001, FR-014, …
	frRefRe = regexp.MustCompile(`\bFR-\d{3}\b`)
	// filePathRe matches a backtick-quoted path that ends in a code or doc
	// file extension — the convention chitin's tasks.md uses to name files.
	filePathRe = regexp.MustCompile("`([A-Za-z0-9][A-Za-z0-9_./-]*\\.(?:go|md|ts|tsx|js|json|yaml|yml|sh|py|rs))`")
)

// ExtractFRRefs returns the sorted, de-duplicated FR ids cited in text.
func ExtractFRRefs(text string) []string {
	return sortedUnique(frRefRe.FindAllString(text, -1))
}

// ExtractFilePaths returns the sorted, de-duplicated file paths named in
// backticks in text.
func ExtractFilePaths(text string) []string {
	var paths []string
	for _, m := range filePathRe.FindAllStringSubmatch(text, -1) {
		paths = append(paths, m[1])
	}
	return sortedUnique(paths)
}

// sortedUnique returns in a sorted, de-duplicated copy of in.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// capabilityKeyword pairs a closed-taxonomy capability with the lowercase
// description substrings that map to it. The keyword set is intentionally
// conservative: a task whose description matches no keyword set, or matches
// more than one capability, is left ambiguous and marked NeedsClarification
// (FR-014) rather than being given a guessed tag.
type capabilityKeyword struct {
	cap      driver.Capability
	keywords []string
}

// capabilityKeywords is the ordered keyword table. Order does not affect the
// result — every entry is checked and a match count is kept — but a stable
// table keeps the mapping auditable.
var capabilityKeywords = []capabilityKeyword{
	{driver.CapTestAuthor, []string{"unit-test", "unit test", "test suite", "table-driven test", "fixture test", "write tests", "author test", "add a test", "add the", "_test.go"}},
	{driver.CapCodeReview, []string{"review the code", "code review", "review a code"}},
	{driver.CapCodeRefactor, []string{"refactor", "restructure"}},
	{driver.CapResearchWeb, []string{"research against the web", "research the open web", "web research"}},
	{driver.CapResearchX, []string{"research against x", "research on x"}},
	{driver.CapDocsWrite, []string{"documentation", "write docs", "runbook", "author the doc", "update the doc"}},
	{driver.CapSpecAuthor, []string{"author a spec", "write a spec", "author the spec", "spec.md", "plan.md", "tasks.md"}},
	{driver.CapBulkCodegen, []string{"scaffold", "boilerplate", "bulk", "generate in bulk", "codegen"}},
	{driver.CapCodeImplement, []string{"implement", "define the", "wire ", "add the import", "create the", "build the"}},
}

// MapCapability maps a task description to exactly one closed-taxonomy
// capability tag (FR-014). It returns the matched tag and ok=true only when
// the description matches the keyword set of precisely one capability. When
// it matches none, or matches more than one, it returns ok=false — the
// caller MUST then mark the node NeedsClarification rather than invent a tag.
//
// The returned capability, when ok, is guaranteed to satisfy
// driver.IsKnownCapability.
func MapCapability(description string) (driver.Capability, bool) {
	lower := strings.ToLower(description)
	matched := make(map[driver.Capability]struct{})
	for _, ck := range capabilityKeywords {
		for _, kw := range ck.keywords {
			if strings.Contains(lower, kw) {
				matched[ck.cap] = struct{}{}
				break
			}
		}
	}
	if len(matched) != 1 {
		return "", false
	}
	for c := range matched {
		// Defence in depth: only return tags the closed taxonomy admits.
		if !driver.IsKnownCapability(string(c)) {
			return "", false
		}
		return c, true
	}
	return "", false
}

// NewTaskContext builds a TaskContext from a task's identifying fields and
// raw description line, extracting FR references and file paths from the
// description. Spec/plan excerpts are attached separately by AttachExcerpts
// once the framing artifacts have been located. The returned context records
// no clarifications yet; callers add them as ambiguities are discovered.
func NewTaskContext(specRef, taskRef, description string) *TaskContext {
	return &TaskContext{
		SpecRef:     specRef,
		TaskRef:     taskRef,
		Description: strings.TrimSpace(description),
		FRRefs:      ExtractFRRefs(description),
		FilePaths:   ExtractFilePaths(description),
	}
}

// AttachExcerpts records the spec and plan framing text on the context. It is
// a setter kept separate from NewTaskContext so an adapter can locate the
// relevant excerpt with kit-specific logic (a user-story block, a phase
// section) and the core context type stays kit-agnostic.
func (c *TaskContext) AttachExcerpts(specExcerpt, planExcerpt string) {
	c.SpecExcerpt = strings.TrimSpace(specExcerpt)
	c.PlanExcerpt = strings.TrimSpace(planExcerpt)
}
