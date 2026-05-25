---
spec_id: 117
title: File-overlap edge inference for shared support files — broaden spec 112 US1 to catch parallel-create collisions
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 112
related:
  - 114
  - 115
  - 118
---

# Spec 117 — File-overlap edge inference for shared support files

## Why

Spec 112 US1 (file-overlap edges between parallel siblings) successfully
prevents parallel tasks from clobbering each other's edits to the SAME
backticked file. The inference reads each parallel task's description,
extracts its backticked file scope (set of basenames), and serializes
any pair whose scopes overlap.

Empirically validated by the spec 114 + spec 115 re-dispatch on
2026-05-25: the inference correctly serialized tasks that named the
same file. But three of spec 114's parallel tasks (T005 `format_table.go`,
T006 `format_md.go`, T007 `format_json.go`) each NAMED a different file
in their description — so the inference saw no overlap — yet each
driver independently created `internal/queue/types.go` to declare the
shared `Entry` struct the three renderers all needed. Same pattern hit
spec 115: T005 (`l03_task_fr.go`) and T006 (`l04_events.go`) each
created `internal/speclint/violation.go` because each rule needed a
shared `Violation` type. The conflict was invisible until merge time.

The pattern is robust: **multiple parallel tasks in the same Go package
(same directory) usually have implicit shared types/constants.** When
the driver realises this mid-task, it creates the support file
locally. Each branch gets one, but the three branches each get a
DIFFERENT version (different field tags, different comment placements,
sometimes different names). At merge time the duplicate type
declarations fail to compile — caught by neither the per-PR CI (each
branch builds in isolation) nor the inference (which only saw the
named files).

The fix: **broaden the file-overlap inference to add an implicit edge
between any two parallel tasks whose named file scopes resolve to the
same directory.** Same-directory parallel tasks get serialised even
when no specific file collision is named. Tasks in different
directories continue to parallelize freely. The cost is some false
positives (two tasks that genuinely could have run in parallel get
serialised); the win is closing the "they each created the shared
support file" failure class that has now hit two specs in a row.

This composes naturally:
  - Spec 112 US1's `fileScopes()` already extracts backticked paths
  - The basename-based overlap check stays — same-basename overlap
    (the original case) is still detected
  - The new check adds a SECOND overlap criterion: two parallel
    tasks whose scope sets resolve to the same directory get an
    implicit edge

## User stories

### US1 (P1) — Same-directory parallel tasks get an implicit overlap edge

> As the spec author, when I write two `[P]` tasks that each name a
> file in `go/orchestrator/internal/queue/`, the inference adds an
> implicit overlap edge between them — they execute sequentially even
> if neither names the other's file. No "drivers each independently
> created `types.go`" failure on the next dispatch.

**Independent test:** Schedule a synthetic spec with two parallel
tasks T01 and T02, each naming a different file in `pkg/foo/`.
Inspect the compiled DAG: T02 must depend on T01 (the order they
appear in tasks.md). Schedule a parallel pair where the files are in
different directories (`pkg/foo/x.go` vs `pkg/bar/y.go`): no implicit
edge. Verify the spec 114 re-dispatch (T005/T006/T007 each naming a
file in `internal/queue/`) would now serialise.

### US2 (P2) — The new edge surfaces with a distinct EdgeReason

> As the operator debugging "why did the scheduler serialise these two
> tasks I marked `[P]`", the compiled DAG shows the edge's reason as
> `same_directory_overlap` (not the existing `file_overlap`), so I
> can distinguish "you literally named the same file" from "you
> named different files in the same package."

**Independent test:** Compile a spec containing one pair caught by
the same-basename rule and one pair caught by the new same-directory
rule. The scheduler status output (or chain event payload) attributes
each edge to its specific reason.

## Functional requirements

- **FR-001** The file-overlap inference function (currently
  `deriveFileOverlapEdges` in `go/orchestrator/adapter/speckit/edges.go`)
  MUST add an implicit edge from task B to task A when both:
  - A and B are both `[P]`-marked
  - A's `fileScopes` and B's `fileScopes` are both non-empty
  - At least one path in A's scope resolves to the same parent
    directory (`filepath.Dir`) as at least one path in B's scope
  - A's `fileScopes` and B's `fileScopes` have NO basename overlap
    (so the new rule doesn't double-count cases the existing rule
    already caught)

- **FR-002** The new edge MUST carry a distinct EdgeReason value
  `same_directory_overlap` so operators distinguish it from the
  existing `file_overlap` reason. Both reasons surface in the
  scheduler's chain emission and the compiled-DAG status output.

- **FR-003** The directory comparison MUST normalise paths the same
  way `fileScopes` normalises basenames — strip `./` prefix, no
  trailing slash, no symlink resolution. A path with no directory
  component (e.g. just `README.md`) resolves to `"."` and any two
  such tasks DO get the implicit edge (the repo root is conservatively
  treated as a shared package).

- **FR-004** Tasks that name a directory but no specific file (e.g.
  ``"in `internal/queue/`"``) are out of scope for this spec — the
  fileScopes parser doesn't recognise them today. A follow-up spec
  may extend the parser.

- **FR-005** When a parallel task names files in MULTIPLE directories
  (e.g. one file in `internal/queue/` and one in `cmd/foo/`), the
  task contributes to the directory set for both. Two such tasks
  overlap if their directory sets intersect.

- **FR-006** The inference MUST remain pure — same input tasks list
  produces the same edge list, no IO. The existing edge-ordering
  contract (`fileOverlapEdgesAt` returns edges in dependent-task file
  order) extends to the new edges: when both `file_overlap` and
  `same_directory_overlap` edges apply to the same pair, the
  `file_overlap` edge wins (the more specific reason).

- **FR-007** The change MUST be backwards-compatible with every
  existing spec — re-running every shipped spec's edge inference on
  current main MUST produce a superset of the existing edges (no
  edges removed; some new ones added). A spec that PR-merged cleanly
  under the old inference stays clean under the new.

## Success criteria

- **SC-001** Re-run the spec 114 + spec 115 edge inference on their
  current tasks.md: every "would have collided on types.go /
  violation.go" pair gets an explicit edge. Measured by comparing
  the pre- and post-spec-117 DAG output.

- **SC-002** Run the inference on the prior 10 shipped specs
  (089-098, 100-116). No spec regresses in dispatchability: the
  newly-added implicit edges either (a) match an explicit `depends_on`
  the spec already declares, or (b) serialise tasks that did in fact
  run sequentially in their original dispatch (so the parallelism
  loss is theoretical, not observed).

- **SC-003** A unit test fixture covering every FR ships in
  `edges_test.go`. The fixture set is the canonical contract for
  "did the inference do the right thing on the spec-117 cases."

## Scope

In:
  - `go/orchestrator/adapter/speckit/edges.go` — new check in
    `deriveFileOverlapEdges`
  - `go/orchestrator/adapter/speckit/edges_test.go` — coverage for
    the new check
  - The EdgeReason enum (in the same file or adjacent) — new
    `same_directory_overlap` value
  - Chain event payload — the EdgeReason new value flows through
    existing emission paths without code change

Out:
  - The directory-mention case (FR-004) — different spec
  - Tasks naming no file at all — already handled by existing
    inference (they get NO file-overlap edges; the user can still
    force sequencing via explicit `[depends on T0NN]`)
  - Cross-package edge inference (e.g. `cmd/x.go` and `internal/x.go`
    sharing a type via a third package) — out of scope; the
    transitive case is hard and rare

## Edge cases

  - **Path with no directory**: `README.md` resolves to `"."`. Two
    parallel tasks each editing a top-level `README.md` and `CLAUDE.md`
    BOTH resolve to `"."` and get the new edge. Acceptable: top-level
    file edits are often serial in practice.
  - **Path with leading `./`**: `./go/orchestrator/foo.go` and
    `go/orchestrator/bar.go` normalise to the same directory after
    `filepath.Clean`. The new edge fires.
  - **Path with `..`**: Treated as opaque — fileScopes already
    extracts basenames literally. The new directory check uses
    `filepath.Dir` on the raw extracted path, no `..` resolution.
    Anyone writing `../foo.go` in a task description is asking for
    surprising behaviour; out of scope.
  - **Multiple files same dir in ONE task**: A single task naming
    `pkg/a.go` and `pkg/b.go` has the directory set `{"pkg"}`. Pairs
    with that task on directory overlap as expected.
  - **Empty `fileScopes` on one side**: If task A has no backticked
    paths (e.g. a "write docs" task with no file reference), the
    rule does not fire. The pair remains parallel. This matches the
    existing `file_overlap` behaviour (no scope = no inference).

## Composability

  - **Spec 112 US1** declares the original `file_overlap` rule; this
    spec extends the same function with a sibling rule. Both rules
    use the same `fileScopes()` extractor.
  - **Spec 115 L08 (potential follow-up)** could add a spec-lint rule
    that flags specs with implicit `same_directory_overlap` edges and
    suggests the spec author either accept the serialisation or
    extract the shared file into a dedicated sequential setup task.
  - **Spec 118** (factory_dispatch_failed reason taxonomy) is the
    other half of the "spec 114/115 silent-drop diagnosis" — this
    spec prevents the file-overlap class; spec 118 makes the silent-
    drop class observable.
