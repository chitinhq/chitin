package speckit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// Kit is the stable kit name this adapter handles — the registry key and the
// value an operator passes to choose spec-kit explicitly.
const Kit = "speckit"

// Adapter is the GitHub spec-kit adapter (FR-004). It implements
// adapter.SpecKitAdapter: it detects a spec-kit repo by the presence of a
// spec-kit spec layout, and compiles a spec directory into a normalized
// Work-Unit DAG.
//
// The zero value is ready to use; New is provided for symmetry and so the
// registration site reads clearly.
type Adapter struct{}

// New returns a spec-kit Adapter.
func New() *Adapter { return &Adapter{} }

// Kit returns the adapter's stable kit name. It satisfies
// adapter.SpecKitAdapter.
func (*Adapter) Kit() string { return Kit }

// specsRoots is the ordered list of directory layouts a spec-kit repo may use
// for its specs. The canonical spec-kit location is `.specify/specs/`; chitin
// itself keeps its specs at the repo-root `specs/`. Detection and compilation
// try them in this order and use the first that exists.
var specsRoots = []string{
	filepath.Join(".specify", "specs"),
	"specs",
}

// Detect reports whether repoPath is a spec-kit repository. It is true when
// the repo has a `.specify/` directory, or a `specs/` directory that holds at
// least one `NNN-name/` spec directory containing a `tasks.md` — the
// recognizable spec-kit signature. It satisfies adapter.SpecKitAdapter.
//
// Detect reads only directory listings; a non-nil error means the repo path
// itself could not be read, not "kit absent".
func (*Adapter) Detect(repoPath string) (bool, error) {
	// The canonical `.specify/` marker is decisive on its own.
	if info, err := os.Stat(filepath.Join(repoPath, ".specify")); err == nil && info.IsDir() {
		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("speckit: probing .specify in %s: %w", repoPath, err)
	}
	// Otherwise accept a repo-root `specs/` that holds a real spec-kit spec.
	specsDir := filepath.Join(repoPath, "specs")
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("speckit: reading %s: %w", specsDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(specsDir, e.Name(), "tasks.md")); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// CompiledSpec is the full result of compiling a spec-kit spec: the
// normalized DAG plus the per-node Task Context keyed by node ID (FR-005).
// The DAG is the scheduler's contract; the contexts ride alongside so a
// driver can act on a node without re-reading the kit. They are kept in a
// sibling map rather than on dag.Node because the dag package (spec 076)
// owns Node's shape and this adapter must not redefine it.
type CompiledSpec struct {
	// DAG is the normalized Work-Unit DAG, acyclic, one node per task.
	DAG *dag.DAG
	// Contexts maps node ID to the task's extracted context.
	Contexts map[string]*adapter.TaskContext
}

// Compile turns the spec named by specRef in repoPath into a normalized
// Work-Unit DAG (FR-003, FR-004). It satisfies adapter.SpecKitAdapter; it
// delegates to CompileSpec and discards the contexts. Callers that need the
// Task Contexts call CompileSpec directly.
func (a *Adapter) Compile(repoPath, specRef string) (*dag.DAG, error) {
	if specRef == "" {
		return a.compileAll(repoPath)
	}
	cs, err := a.CompileSpec(repoPath, specRef)
	if err != nil {
		return nil, err
	}
	return cs.DAG, nil
}

// CompileSpec compiles one spec directory and returns the DAG together with
// the per-node Task Contexts. specRef names the spec directory — either the
// full `NNN-name` form or just the `NNN` numeric prefix; CompileSpec resolves
// the prefix to the unique matching directory.
//
// It is a pure, deterministic, side-effect-free transform: it reads the
// spec's tasks.md, plan.md, and spec.md and returns in-memory values. It
// fails — returning nil — on a malformed artifact (FR-010) or a dangling
// dependency reference (FR-011), never a partial result.
func (a *Adapter) CompileSpec(repoPath, specRef string) (*CompiledSpec, error) {
	specDir, relSpecDir, err := a.resolveSpecDir(repoPath, specRef)
	if err != nil {
		return nil, err
	}
	dirName := filepath.Base(specDir)
	return compileSpecDir(specDir, relSpecDir, dirName, deriveSpecNumber(dirName))
}

// compileAll compiles every spec directory the repo contains into one DAG —
// the empty-specRef behaviour of the SpecKitAdapter contract. Node IDs are
// namespaced by the full spec *directory name*, so two specs that share a
// numeric prefix (chitin has two `069-*` directories) never collide. It fails
// on the first spec that fails to compile.
func (a *Adapter) compileAll(repoPath string) (*dag.DAG, error) {
	root, err := a.resolveSpecsRoot(repoPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("speckit: reading specs root %s: %w", root, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // deterministic compile order

	merged := dag.New()
	for _, name := range names {
		specDir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(specDir, "tasks.md")); err != nil {
			continue // a spec directory with no tasks.md is not yet compilable
		}
		relSpecDir, relErr := filepath.Rel(repoPath, specDir)
		if relErr != nil {
			relSpecDir = specDir
		}
		cs, err := compileSpecDir(specDir, relSpecDir, name, deriveSpecNumber(name))
		if err != nil {
			// A directory whose tasks.md is not in spec-kit format is not a
			// spec-kit spec — skip it rather than aborting the whole-repo
			// compile. A genuinely malformed spec-kit tasks.md still fails.
			if errors.Is(err, ErrNotSpecKitTasks) {
				continue
			}
			return nil, err
		}
		if err := mergeInto(merged, cs.DAG); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// mergeInto copies every node and edge of src into dst. Node IDs carry their
// spec ref, so cross-spec collisions cannot occur in well-formed input; a
// genuine duplicate id is surfaced as an error rather than silently dropped.
func mergeInto(dst, src *dag.DAG) error {
	for _, n := range src.Nodes() {
		if err := dst.AddNode(n); err != nil {
			return fmt.Errorf("speckit: merging specs: %w", err)
		}
	}
	for _, e := range src.Edges() {
		if err := dst.AddEdge(e.From, e.To); err != nil {
			return fmt.Errorf("speckit: merging specs: %w", err)
		}
	}
	return nil
}

// resolveSpecsRoot returns the absolute path of the repo's specs root — the
// first of specsRoots that exists.
func (a *Adapter) resolveSpecsRoot(repoPath string) (string, error) {
	for _, rel := range specsRoots {
		root := filepath.Join(repoPath, rel)
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return root, nil
		}
	}
	return "", fmt.Errorf("speckit: no specs directory found in %s (looked for %v)", repoPath, specsRoots)
}

// resolveSpecDir resolves specRef to the absolute and repo-relative paths of
// a single spec directory. specRef may be the full `NNN-name` directory name
// or just the `NNN` prefix; a prefix that matches no directory, or more than
// one, is an error.
func (a *Adapter) resolveSpecDir(repoPath, specRef string) (absDir, relDir string, err error) {
	root, err := a.resolveSpecsRoot(repoPath)
	if err != nil {
		return "", "", err
	}
	// Exact directory name match first.
	exact := filepath.Join(root, specRef)
	if info, statErr := os.Stat(exact); statErr == nil && info.IsDir() {
		rel, _ := filepath.Rel(repoPath, exact)
		return exact, rel, nil
	}
	// Otherwise treat specRef as a numeric prefix.
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		return "", "", fmt.Errorf("speckit: reading specs root %s: %w", root, readErr)
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), specRef) {
			matches = append(matches, e.Name())
		}
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("speckit: no spec directory matching %q under %s", specRef, root)
	case 1:
		abs := filepath.Join(root, matches[0])
		rel, _ := filepath.Rel(repoPath, abs)
		return abs, rel, nil
	default:
		sort.Strings(matches)
		return "", "", fmt.Errorf("speckit: spec ref %q is ambiguous — matches %v", specRef, matches)
	}
}

// deriveSpecNumber extracts the numeric spec ref from a `NNN-name` directory
// name — "077-spec-kit-adapter" → "077". When the directory name has no
// leading numeric segment the whole name is used, so the spec ref is always
// non-empty.
func deriveSpecNumber(dirName string) string {
	i := 0
	for i < len(dirName) && dirName[i] >= '0' && dirName[i] <= '9' {
		i++
	}
	if i == 0 {
		return dirName
	}
	return dirName[:i]
}

// compileSpecDir is the core spec→DAG transform for one spec directory. It
// reads tasks.md (required), plan.md and spec.md (optional framing), parses
// the tasks, derives edges, maps metadata, extracts context, and assembles
// the DAG.
//
// nodeNS is the node-ID namespace — the full spec directory name (e.g.
// "069-decommission-agent-bus-mini"). Two specs that share a numeric prefix
// must still produce DAG-unique node IDs, so the directory name, which is
// unique within a specs root, is the namespace. specRef is the numeric spec
// ref ("069") recorded on each Node.SpecRef and TaskContext.
func compileSpecDir(absSpecDir, relSpecDir, nodeNS, specRef string) (*CompiledSpec, error) {
	tasksRel := filepath.Join(relSpecDir, "tasks.md")
	tasksContent, err := readRequired(filepath.Join(absSpecDir, "tasks.md"), tasksRel)
	if err != nil {
		return nil, err
	}
	planContent := readOptional(filepath.Join(absSpecDir, "plan.md"))
	specContent := readOptional(filepath.Join(absSpecDir, "spec.md"))

	tasks, err := ParseTasks(tasksRel, tasksContent)
	if err != nil {
		return nil, err
	}

	edges, dangling := DeriveEdges(tasks)
	if len(dangling) > 0 {
		// FR-011: fail naming the missing target. Report the first dangling
		// reference in deterministic (from, target) order.
		sort.Slice(dangling, func(i, j int) bool {
			if dangling[i].From != dangling[j].From {
				return dangling[i].From < dangling[j].From
			}
			return dangling[i].MissingTarget < dangling[j].MissingTarget
		})
		d := dangling[0]
		d.File = tasksRel
		return nil, &d
	}

	d := dag.New()
	contexts := make(map[string]*adapter.TaskContext, len(tasks))

	for _, t := range tasks {
		nodeID := nodeNS + "/" + t.ID
		ctx := BuildContext(specRef, t, specContent, planContent)

		capTag, mapped := MapCapability(t)
		capability := string(capTag)
		if !mapped {
			// FR-014: an unmappable task is NEEDS CLARIFICATION, never an
			// invented tag. The marker doubles as the node capability so the
			// scheduler's router sees a non-taxonomy value and the human sees
			// the reason on the context.
			capability = adapter.NeedsClarification
			ctx.Clarifications = appendClarification(ctx.Clarifications, capabilityClarification)
		}
		// FR-009: a task that signals a dependency in prose but names no
		// resolvable task id has an ambiguous dependency — mark it NEEDS
		// CLARIFICATION rather than invent an edge or drop the hint.
		if HasAmbiguousDependency(t.Description) {
			ctx.Clarifications = appendClarification(ctx.Clarifications, dependencyClarification)
		}

		node := dag.Node{
			ID:               nodeID,
			SpecRef:          specRef,
			TaskRef:          t.ID,
			Capability:       capability,
			Priority:         DerivePriority(t),
			TargetRepo:       "",   // an input the scheduler supplies, not the spec
			BaseRef:          "",   // ditto
			WorktreeRequired: true, // every work unit runs in a fresh worktree
			Status:           dag.StatusPending,
		}
		if err := d.AddNode(node); err != nil {
			return nil, fmt.Errorf("speckit: spec %s: %w", specRef, err)
		}
		contexts[nodeID] = ctx
	}

	for _, e := range edges {
		from := nodeNS + "/" + e.from
		to := nodeNS + "/" + e.to
		if err := d.AddEdge(from, to); err != nil {
			return nil, fmt.Errorf("speckit: spec %s: %w", specRef, err)
		}
	}

	return &CompiledSpec{DAG: d, Contexts: contexts}, nil
}

// appendClarification appends reason to clars if not already present.
func appendClarification(clars []string, reason string) []string {
	for _, r := range clars {
		if r == reason {
			return clars
		}
	}
	out := append(clars, reason)
	sort.Strings(out)
	return out
}

// readRequired reads a required spec artifact. A missing or empty file is a
// malformed-artifact failure (FR-010): a spec with no tasks.md cannot be
// compiled. relPath labels the error.
func readRequired(absPath, relPath string) (string, error) {
	b, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &adapter.MalformedArtifactError{
				File: relPath, Line: 0, Reason: "required artifact is missing",
			}
		}
		return "", fmt.Errorf("speckit: reading %s: %w", relPath, err)
	}
	return string(b), nil
}

// readOptional reads an optional framing artifact, returning "" if it is
// absent or unreadable — plan.md and spec.md frame the work but are not
// required to compile it.
func readOptional(absPath string) string {
	b, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	return string(b)
}
