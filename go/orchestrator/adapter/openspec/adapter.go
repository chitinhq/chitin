// Package openspec is the OpenSpec adapter (spec 077, FR-006/FR-007). It
// compiles an OpenSpec change — `openspec/changes/<name>/` — into the same
// normalized Work-Unit DAG the spec-kit adapter produces, so two kits reach
// the scheduler through the one interface with zero scheduler change.
//
// OpenSpec is brownfield-oriented: a change directory holds a `proposal.md`,
// a `tasks.md`, and per-capability spec deltas under `specs/` whose sections
// are headed `## ADDED Requirements`, `## MODIFIED Requirements`, and
// `## REMOVED Requirements`. This adapter emits one DAG node per delta and
// preserves each delta's change-kind — ADDED / MODIFIED / REMOVED — as node
// metadata (FR-007), encoded into the node's TaskRef so the scheduler's audit
// trail carries it without the dag package needing an OpenSpec-specific
// field.
//
// Compilation is a pure, deterministic, side-effect-free transform — change
// files in, an in-memory DAG out — matching the spec-kit adapter's contract.
package openspec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// Kit is the stable kit name this adapter handles.
const Kit = "openspec"

// changesRel is the repo-relative directory OpenSpec keeps its changes under.
var changesRel = filepath.Join("openspec", "changes")

// Adapter is the OpenSpec adapter. It implements adapter.SpecKitAdapter. The
// zero value is ready to use.
type Adapter struct{}

// New returns an OpenSpec Adapter.
func New() *Adapter { return &Adapter{} }

// Kit returns the adapter's stable kit name.
func (*Adapter) Kit() string { return Kit }

// Detect reports whether repoPath is an OpenSpec repository — true when it
// has an `openspec/` directory. It satisfies adapter.SpecKitAdapter.
func (*Adapter) Detect(repoPath string) (bool, error) {
	info, err := os.Stat(filepath.Join(repoPath, "openspec"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("openspec: probing openspec/ in %s: %w", repoPath, err)
	}
	return info.IsDir(), nil
}

// Compile turns the OpenSpec change named by specRef into a normalized
// Work-Unit DAG (FR-003, FR-006). specRef is the change directory name under
// `openspec/changes/`; an empty specRef compiles every change in the repo.
// It satisfies adapter.SpecKitAdapter.
func (a *Adapter) Compile(repoPath, specRef string) (*dag.DAG, error) {
	if specRef == "" {
		return a.compileAll(repoPath)
	}
	cs, err := a.CompileChange(repoPath, specRef)
	if err != nil {
		return nil, err
	}
	return cs.DAG, nil
}

// CompiledChange is the result of compiling one OpenSpec change: the DAG plus
// the per-node Task Contexts (FR-005), keyed by node ID.
type CompiledChange struct {
	DAG      *dag.DAG
	Contexts map[string]*adapter.TaskContext
}

// CompileChange compiles one OpenSpec change directory and returns the DAG
// with its Task Contexts. It is a pure, deterministic, side-effect-free
// transform; it fails — returning nil — on a malformed artifact (FR-010),
// never a partial result.
func (a *Adapter) CompileChange(repoPath, changeName string) (*CompiledChange, error) {
	changeDir := filepath.Join(repoPath, changesRel, changeName)
	info, err := os.Stat(changeDir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("openspec: no change directory %q under %s", changeName, changesRel)
	}
	relChangeDir := filepath.Join(changesRel, changeName)
	return compileChangeDir(changeDir, relChangeDir, changeName)
}

// compileAll compiles every change under openspec/changes/ into one DAG.
func (a *Adapter) compileAll(repoPath string) (*dag.DAG, error) {
	root := filepath.Join(repoPath, changesRel)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("openspec: reading %s: %w", root, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && e.Name() != "archive" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	merged := dag.New()
	for _, name := range names {
		cs, err := compileChangeDir(
			filepath.Join(root, name), filepath.Join(changesRel, name), name)
		if err != nil {
			return nil, err
		}
		for _, n := range cs.DAG.Nodes() {
			if err := merged.AddNode(n); err != nil {
				return nil, fmt.Errorf("openspec: merging changes: %w", err)
			}
		}
		for _, e := range cs.DAG.Edges() {
			if err := merged.AddEdge(e.From, e.To); err != nil {
				return nil, fmt.Errorf("openspec: merging changes: %w", err)
			}
		}
	}
	return merged, nil
}

// compileChangeDir is the core transform for one OpenSpec change directory.
// It reads proposal.md (optional framing) and every `specs/**/spec.md` delta
// file, parses the ADDED/MODIFIED/REMOVED requirement sections, and emits one
// node per delta with its change-kind preserved.
func compileChangeDir(absDir, relDir, changeName string) (*CompiledChange, error) {
	proposal := readOptional(filepath.Join(absDir, "proposal.md"))

	deltaFiles, err := findDeltaFiles(absDir)
	if err != nil {
		return nil, err
	}
	if len(deltaFiles) == 0 {
		return nil, &adapter.MalformedArtifactError{
			File: relDir, Line: 0,
			Reason: "OpenSpec change has no spec delta files under specs/",
		}
	}

	d := dag.New()
	contexts := make(map[string]*adapter.TaskContext)
	var ordered []delta

	for _, df := range deltaFiles {
		rel := filepath.Join(relDir, df.rel)
		content, readErr := os.ReadFile(df.abs)
		if readErr != nil {
			return nil, fmt.Errorf("openspec: reading %s: %w", rel, readErr)
		}
		deltas, parseErr := parseDeltas(rel, string(content))
		if parseErr != nil {
			return nil, parseErr
		}
		ordered = append(ordered, deltas...)
	}
	if len(ordered) == 0 {
		return nil, &adapter.MalformedArtifactError{
			File: relDir, Line: 0,
			Reason: "OpenSpec change declares no ADDED/MODIFIED/REMOVED requirements",
		}
	}

	// Stable, deterministic ordering: by capability area, then change-kind,
	// then requirement title. This is the tie-breaker that makes two compiles
	// of the same change produce byte-identical DAGs.
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].area != ordered[j].area {
			return ordered[i].area < ordered[j].area
		}
		if ordered[i].kind != ordered[j].kind {
			return ordered[i].kind < ordered[j].kind
		}
		return ordered[i].title < ordered[j].title
	})

	for idx, dl := range ordered {
		nodeID := fmt.Sprintf("%s/D%03d", changeName, idx+1)
		// FR-007: preserve the change-kind as node metadata. dag.Node has no
		// OpenSpec-specific field, so the change-kind rides in TaskRef as
		// "<kind>:<area>" — visible in the scheduler's audit trail without a
		// dag-package change.
		taskRef := dl.kind + ":" + dl.area

		ctx := adapter.NewTaskContext(changeName, taskRef, dl.title)
		ctx.AttachExcerpts(proposal, "")

		capTag, mapped := adapter.MapCapability(dl.title + " " + dl.body)
		capability := string(capTag)
		if !mapped {
			capability = adapter.NeedsClarification
			ctx.Clarifications = append(ctx.Clarifications,
				"capability could not be mapped to the closed taxonomy from the OpenSpec delta")
			sort.Strings(ctx.Clarifications)
		}

		node := dag.Node{
			ID:               nodeID,
			SpecRef:          changeName,
			TaskRef:          taskRef,
			Capability:       capability,
			Priority:         changeKindPriority(dl.kind),
			WorktreeRequired: true,
			Status:           dag.StatusPending,
		}
		if err := d.AddNode(node); err != nil {
			return nil, fmt.Errorf("openspec: change %s: %w", changeName, err)
		}
		contexts[nodeID] = ctx
	}

	return &CompiledChange{DAG: d, Contexts: contexts}, nil
}

// changeKindPriority orders the runnable frontier by change-kind: REMOVED
// before MODIFIED before ADDED, so a brownfield change tears down before it
// rebuilds. The values are declared, not heuristic.
func changeKindPriority(kind string) int {
	switch kind {
	case "REMOVED":
		return 300
	case "MODIFIED":
		return 200
	default: // ADDED
		return 100
	}
}

// readOptional reads an optional framing file, returning "" when absent.
func readOptional(absPath string) string {
	b, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	return string(b)
}

// deltaFile pairs an absolute and repo-relative delta file path.
type deltaFile struct {
	abs string
	rel string
}

// findDeltaFiles returns every spec delta file under the change's specs/
// directory, sorted by relative path for deterministic compile order.
func findDeltaFiles(changeDir string) ([]deltaFile, error) {
	specsDir := filepath.Join(changeDir, "specs")
	var out []deltaFile
	err := filepath.WalkDir(specsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(changeDir, path)
		if relErr != nil {
			rel = path
		}
		out = append(out, deltaFile{abs: path, rel: rel})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("openspec: scanning %s: %w", specsDir, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}
