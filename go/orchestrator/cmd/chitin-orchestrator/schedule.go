// schedule.go — `chitin-orchestrator schedule <spec-ref>` subcommand
// (spec 097 US1; FRs 001-005, 009-012).
//
// Flow:
//
//  1. Parse argv via Go's flag package, scoped to this subcommand.
//  2. Resolve --repo-root and --temporal-host (flag → env → default).
//  3. Resolve <spec-ref> via the three-tier resolver (specref.go).
//  4. Compile the spec via the spec-077 adapter
//     (speckit.New().CompileSpec). Fail user-error on compile error.
//  5. Pre-validate the DAG via ValidateForDispatch (validate.go). Fail
//     user-error if any node is needs_clarification or unroutable.
//  6. Dial Temporal (client.go). Fail runtime-error on unreachable.
//  7. ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{...}) with a
//     fresh UUID as both Temporal WorkflowID and SchedulerInput.RunID.
//  8. Emit scheduler_started chain event via emit.go (fail-soft).
//  9. Print success line to stdout; exit 0.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/workflows"
)

// cmdSchedule is the entrypoint dispatched from runMain. It delegates to
// runSchedule with the process's os.Stdout/Stderr so tests can wire fakes.
func cmdSchedule(args []string) int {
	return runSchedule(context.Background(), args, os.Stdout, os.Stderr)
}

// runSchedule is the testable form. Returns the exit code per FR-011.
func runSchedule(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("schedule", flag.ContinueOnError)
	fs.SetOutput(stderr)
	temporalHost := fs.String("temporal-host", "", "Temporal frontend host:port (default: $TEMPORAL_HOSTPORT or 127.0.0.1:7233)")
	repoRoot := fs.String("repo-root", "", "Chitin repo root (default: $CHITIN_REPO_ROOT or `git rev-parse --show-toplevel`)")
	targetRepo := fs.String("target-repo", "", "repo the dispatched work units operate on (default: --repo-root). The spec-077 adapter intentionally leaves Node.TargetRepo empty; the schedule subcommand fills it before ExecuteWorkflow.")
	baseRef := fs.String("base-ref", "main", "git ref each work unit's worktree is created from (default: main)")
	driver := fs.String("driver", "", "explicit driver routing (spec 099). Empty = local SchedulerWorkflow path. \"copilot\" = create GitHub issue + assign @copilot (requires --repo).")
	gitHubRepo := fs.String("repo", "", "GitHub repo slug owner/name (required for --driver copilot)")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator schedule <spec-ref> [--temporal-host host:port] [--repo-root path] [--target-repo path] [--base-ref ref] [--driver copilot --repo owner/name]")
	}

	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError already wrote the error + usage to stderr.
		return exitUserError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: exactly one positional argument required: <spec-ref>")
		fs.Usage()
		return exitUserError
	}
	specRefArg := fs.Arg(0)

	// Spec 099 — validate --driver value upfront. Empty = local path
	// (spec 097); "copilot" = GitHub issue dispatch (spec 099). Any other
	// value is rejected before spec resolution to fail fast.
	switch *driver {
	case "", "copilot":
		// ok
	default:
		fmt.Fprintf(stderr, "error: unknown driver: %q (supported: copilot, or omit for local SchedulerWorkflow)\n", *driver)
		return exitUserError
	}
	if *driver == "copilot" && *gitHubRepo == "" {
		fmt.Fprintln(stderr, "error: --driver copilot requires --repo <owner/name> (target GitHub repo for the issue)")
		return exitUserError
	}

	repo, err := resolveRepoRoot(*repoRoot)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	resolution, err := resolveSpecRef(repo, specRefArg)
	if err != nil {
		var sre *SpecRefError
		if errors.As(err, &sre) {
			renderSpecRefError(stderr, sre)
			return exitUserError
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	cs, err := speckit.New().CompileSpec(repo, resolution.SpecRef)
	if err != nil {
		fmt.Fprintf(stderr, "error: spec %s compile failed: %v\n", resolution.SpecRef, err)
		return exitUserError
	}
	if cs == nil || cs.DAG == nil {
		fmt.Fprintf(stderr, "error: spec %s compiled to nil DAG\n", resolution.SpecRef)
		return exitUserError
	}

	registry, err := buildRegistry("impl")
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot build driver registry: %v\n", err)
		return exitRuntimeError
	}

	verrs := ValidateForDispatch(ctx, cs.DAG, registry)
	if len(verrs) > 0 {
		renderValidationErrors(stderr, verrs)
		return exitUserError
	}

	// Spec 099 — Copilot branch. Spec resolution + DAG validation have
	// already run (R7 in research.md: catch operator typos before
	// consuming Copilot's slot). Skip Temporal dispatch and hand off to
	// the GitHub issue dispatcher. Slice 1 is the routing skeleton only;
	// slice 2 wires `gh issue create` + chain emit per contracts/cli-driver-flag.md.
	if *driver == "copilot" {
		return runCopilotDispatch(ctx, copilotDispatchInput{
			SpecRef: resolution.SpecRef,
			Repo:    *gitHubRepo,
		}, stdout, stderr)
	}

	c, host, err := dialTemporal(ctx, *temporalHost)
	if err != nil {
		fmt.Fprintf(stderr, "error: Temporal unreachable at %s — is the temporal-dev service running?\n", host)
		return exitRuntimeError
	}
	defer c.Close()

	// Populate Node.TargetRepo and Node.BaseRef. The spec-077 adapter
	// hardcodes these to "" (per adapter.go:326 comment "an input the
	// scheduler supplies, not the spec") because the same spec compiles
	// against any target repo; the orchestrator's CreateWorktree activity
	// then refuses an empty target_repo. The schedule subcommand is the
	// scheduler-side seam that fills both fields before ExecuteWorkflow.
	dispatchTargetRepo := *targetRepo
	if dispatchTargetRepo == "" {
		dispatchTargetRepo = repo
	}
	nodes := prepareNodesForDispatch(cs.DAG.Nodes(), dispatchTargetRepo, *baseRef)

	runID := uuid.NewString()
	in := workflows.SchedulerInput{
		RunID: runID,
		Nodes: nodes,
		Edges: cs.DAG.Edges(),
		Tick:  0,
	}
	startOpts := client.StartWorkflowOptions{
		ID:        runID,
		TaskQueue: TaskQueue,
	}
	if _, err := c.ExecuteWorkflow(ctx, startOpts, workflows.SchedulerWorkflow, in); err != nil {
		fmt.Fprintf(stderr, "error: ExecuteWorkflow failed: %v\n", err)
		return exitRuntimeError
	}

	capsRequired := collectCapabilities(cs.DAG)
	emitSchedulerStarted(ctx, SchedulerStartedPayload{
		SpecRef:              resolution.SpecRef,
		RunID:                runID,
		NodeCount:            cs.DAG.Len(),
		CapabilitiesRequired: capsRequired,
	}, stderr)

	fmt.Fprintf(stdout, "scheduled spec %s (%d nodes, %d capabilities required); run_id=%s\n",
		resolution.SpecRef, cs.DAG.Len(), len(capsRequired), runID)
	return exitSuccess
}

func renderSpecRefError(stderr io.Writer, sre *SpecRefError) {
	switch sre.Kind {
	case "not-found":
		fmt.Fprintf(stderr, "error: no spec matching ref %q\n", sre.Ref)
		if len(sre.Candidates) > 0 {
			fmt.Fprintln(stderr, "available specs:")
			for _, c := range sre.Candidates {
				fmt.Fprintf(stderr, "  %s\n", c)
			}
		}
	case "ambiguous":
		fmt.Fprintf(stderr, "error: ref %q is ambiguous — matched %d specs:\n", sre.Ref, len(sre.Candidates))
		for _, c := range sre.Candidates {
			fmt.Fprintf(stderr, "  %s\n", c)
		}
	default:
		fmt.Fprintf(stderr, "error: spec ref resolution: %s\n", sre.Error())
	}
}

func renderValidationErrors(stderr io.Writer, errs []ValidationError) {
	byKind := map[string][]ValidationError{}
	for _, e := range errs {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	if needs := byKind["needs_clarification"]; len(needs) > 0 {
		fmt.Fprintf(stderr, "error: DAG validation failed — %d node(s) have unclassified capability:\n", len(needs))
		for _, e := range needs {
			fmt.Fprintf(stderr, "  - %s: %s\n", e.NodeID, e.Detail)
		}
	}
	if unr := byKind["unroutable"]; len(unr) > 0 {
		fmt.Fprintf(stderr, "error: DAG validation failed — %d node(s) require capability not declared by any registered driver:\n", len(unr))
		for _, e := range unr {
			fmt.Fprintf(stderr, "  - %s: capability %q — %s\n", e.NodeID, e.Capability, e.Detail)
		}
	}
	if miss := byKind["missing_capability"]; len(miss) > 0 {
		fmt.Fprintf(stderr, "error: DAG validation failed — %d node(s) missing capability:\n", len(miss))
		for _, e := range miss {
			fmt.Fprintf(stderr, "  - %s: %s\n", e.NodeID, e.Detail)
		}
	}
}

// prepareNodesForDispatch returns a copy of nodes with TargetRepo and
// BaseRef populated on every node. The spec-077 adapter intentionally
// leaves both fields empty (adapter.go:326 — "an input the scheduler
// supplies, not the spec") so the same compiled DAG can dispatch against
// any target repo; the schedule subcommand is the scheduler-side seam
// that fills both fields before ExecuteWorkflow. Without this step the
// orchestrator's CreateWorktree activity refuses every node with
// "target repo must not be empty," and every real-spec dispatch fails.
//
// Pure function: does not mutate the input slice or its elements.
// targetRepo is the absolute path to the operator's chitin repo; baseRef
// is the git ref each work unit's worktree is checked out from
// (default "main" via the --base-ref flag).
func prepareNodesForDispatch(nodes []dag.Node, targetRepo, baseRef string) []dag.Node {
	out := make([]dag.Node, len(nodes))
	for i, n := range nodes {
		n.TargetRepo = targetRepo
		n.BaseRef = baseRef
		out[i] = n
	}
	return out
}

// collectCapabilities returns the sorted, deduplicated capability tags
// across every agent node in the DAG, for the scheduler_started chain
// event's CapabilitiesRequired field (per data-model.md Entity 6).
func collectCapabilities(d *dag.DAG) []string {
	seen := map[string]struct{}{}
	for _, n := range d.Nodes() {
		if n.Kind == dag.NodeKindDeterministic {
			continue
		}
		if n.Capability == "" {
			continue
		}
		seen[n.Capability] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
