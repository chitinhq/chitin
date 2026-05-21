// Package speckit is the GitHub spec-kit adapter (spec 077, FR-004). It
// compiles a spec-kit spec directory — `.specify/specs/NNN-name/` (or the
// `specs/NNN-name/` layout chitin itself uses) — into the normalized
// Work-Unit DAG the scheduler consumes. One DAG node per `tasks.md` task;
// dependency edges from the task ordering and `[P]` parallel markers;
// capability and priority carried from task metadata.
//
// Compilation is a pure, deterministic, side-effect-free transform: it reads
// the spec's `tasks.md`, `plan.md`, and `spec.md` and returns an in-memory
// DAG. It takes no wall clock, opens no network connection, and writes no
// file. Where a task's dependency or capability is left ambiguous by the
// artifacts, the adapter emits the adapter.NeedsClarification marker rather
// than inventing one (FR-009, FR-014).
package speckit

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
)

// ErrNotSpecKitTasks is returned by ParseTasks when a non-empty tasks.md
// contains no `- [ ] TNNN …` task lines at all — the file is simply not a
// spec-kit task list (an older `## Task N:` layout, or another kit's file),
// as opposed to a malformed spec-kit one. It is distinct from
// *adapter.MalformedArtifactError so callers can tell the two apart: a
// single-spec Compile surfaces it as "not a spec-kit spec", while a
// whole-repo compileAll skips such a directory rather than aborting the
// batch. Callers detect it with errors.Is.
var ErrNotSpecKitTasks = errors.New("speckit: tasks.md is not in spec-kit `- [ ] TNNN` format")

// Task is one entry parsed from a spec-kit `tasks.md`. It captures everything
// the spec-kit task line and its enclosing phase header declare; edge
// derivation, metadata mapping, and context extraction all read from it.
type Task struct {
	// ID is the task identifier — e.g. "T009". Unique within a tasks.md.
	ID string
	// Num is the numeric form of ID, for deterministic ordering.
	Num int
	// Phase is the "## Phase N" header the task sits under. 0 means the task
	// appeared before any phase header.
	Phase int
	// PhaseSeq is the 1-based ordinal of the phase in file order — distinct
	// from Phase because phase headers need not be numbered 1,2,3… It is what
	// priority derivation uses, so an unnumbered or oddly-numbered phase still
	// orders correctly.
	PhaseSeq int
	// Parallel is true if the task line carried the leading `[P]` marker.
	Parallel bool
	// Stories is the set of `[USn]` story tags on the task line, in file
	// order — e.g. ["US1"]. Empty for setup/foundational tasks.
	Stories []string
	// Description is the task line with the leading `[P]` and `[USn]` markers
	// stripped — the human-readable task text.
	Description string
	// RawLine is the full task line as written, markers included — kept for
	// context extraction and precise error messages.
	RawLine string
	// LineNo is the 1-based line number of the task in tasks.md — for
	// MalformedArtifactError locations.
	LineNo int
}

var (
	// phaseRe matches a phase header: "## Phase 2: Foundational …". The
	// number is optional so an unnumbered "## Phase: Polish" still registers.
	phaseRe = regexp.MustCompile(`^##\s+Phase\s*(\d*)\b`)
	// taskRe matches a checkbox task line: "- [ ] T009 …" / "- [x] T009 …".
	// Group 1 is the zero-padded task number, group 2 the remainder.
	taskRe = regexp.MustCompile(`^- \[[ xX]\]\s+T(\d+)\s+(.*)$`)
	// markerRe matches a single leading bracket marker — `[P]` or `[US1]` —
	// at the start of a (possibly already-trimmed) remainder string.
	markerRe = regexp.MustCompile(`^\[([A-Za-z0-9]+)\]\s*`)
)

// ParseTasks parses the text of a spec-kit `tasks.md` and returns its tasks
// in file order. tasksPath is the repo-relative path of the file, used only
// to label any *adapter.MalformedArtifactError.
//
// ParseTasks fails — returning a nil slice — in two distinguishable ways:
//
//   - *adapter.MalformedArtifactError when the file IS a spec-kit task list
//     but is broken (FR-010): an empty file, a duplicate task id, an
//     unparseable task number — a partial DAG must never result.
//   - ErrNotSpecKitTasks when the non-empty file contains no `- [ ] TNNN`
//     lines at all — it is not a spec-kit task list (e.g. an older
//     `## Task N:` layout). compileAll skips such a directory; a single-spec
//     Compile surfaces it as "not a spec-kit spec".
//
// It never returns a partial task list.
func ParseTasks(tasksPath, content string) ([]Task, error) {
	if strings.TrimSpace(content) == "" {
		return nil, &adapter.MalformedArtifactError{
			File: tasksPath, Line: 0, Reason: "tasks.md is empty",
		}
	}

	var tasks []Task
	seen := make(map[string]int) // task id -> first line it was seen on
	phase, phaseSeq := 0, 0
	inPhase := false

	for i, line := range strings.Split(content, "\n") {
		lineNo := i + 1

		if m := phaseRe.FindStringSubmatch(line); m != nil {
			if m[1] != "" {
				phase, _ = strconv.Atoi(m[1])
			} else {
				phase = 0
			}
			phaseSeq++
			inPhase = true
			continue
		}

		m := taskRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			// Defensive: taskRe's group 1 is \d+, so Atoi cannot realistically
			// fail — but a malformed artifact must never slip through silently.
			return nil, &adapter.MalformedArtifactError{
				File: tasksPath, Line: lineNo,
				Reason: fmt.Sprintf("unparseable task number %q", m[1]),
			}
		}
		id := "T" + m[1]
		if prev, dup := seen[id]; dup {
			return nil, &adapter.MalformedArtifactError{
				File: tasksPath, Line: lineNo,
				Reason: fmt.Sprintf("duplicate task id %s (first defined on line %d)", id, prev),
			}
		}
		seen[id] = lineNo

		parallel, stories, desc := stripMarkers(m[2])
		seq := phaseSeq
		if !inPhase {
			seq = 0
		}
		tasks = append(tasks, Task{
			ID:          id,
			Num:         num,
			Phase:       phase,
			PhaseSeq:    seq,
			Parallel:    parallel,
			Stories:     stories,
			Description: desc,
			RawLine:     strings.TrimSpace(line),
			LineNo:      lineNo,
		})
	}

	if len(tasks) == 0 {
		// Non-empty, but no spec-kit task lines — not a malformed spec-kit
		// file, just not one. Surface the distinguishable sentinel.
		return nil, ErrNotSpecKitTasks
	}
	return tasks, nil
}

// stripMarkers peels the leading `[P]` and `[USn]` bracket markers off a task
// line remainder, returning the parallel flag, the story tags in order, and
// the remaining description. A `[P]` in any leading position sets parallel;
// any other bracket token is treated as a story tag. Stripping stops at the
// first non-bracket token so a description that itself contains brackets is
// left intact.
func stripMarkers(rest string) (parallel bool, stories []string, desc string) {
	rest = strings.TrimSpace(rest)
	for {
		m := markerRe.FindStringSubmatch(rest)
		if m == nil {
			break
		}
		token := m[1]
		if token == "P" {
			parallel = true
		} else {
			stories = append(stories, token)
		}
		rest = rest[len(m[0]):]
	}
	return parallel, stories, strings.TrimSpace(rest)
}
