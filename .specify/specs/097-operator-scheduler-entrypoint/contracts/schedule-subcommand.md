# Contract: `chitin-orchestrator schedule <spec-ref>`

**Spec**: 097 | **FRs**: 001, 002, 003, 004, 005, 009, 010, 011, 012

## Synopsis

```text
chitin-orchestrator schedule <spec-ref> [--temporal-host <host:port>] [--repo-root <path>]
```

## Arguments

| Position / Flag | Type | Required | Default | Description |
|---|---|---|---|---|
| `<spec-ref>` | string | yes | — | The spec to schedule. Forms: exact directory name (`096-operator-session-state-surface`), numeric prefix (`096`), or slug-only (`operator-session-state-surface`). |
| `--temporal-host` | string | no | `$TEMPORAL_HOSTPORT` ?? `127.0.0.1:7233` | Temporal frontend host:port. |
| `--repo-root` | string | no | `$CHITIN_REPO_ROOT` ?? `$(git rev-parse --show-toplevel)` | Chitin repo root from which `.specify/specs/` is read. |

Unknown flags MUST cause an exit-1 error with usage to stderr. Operators must NOT discover semantics by trial-and-error.

## Behavior

1. Resolve `<spec-ref>` per D9 (exact → numeric prefix → slug). Exit 1 with operator-readable error on miss or ambiguity.
2. Compile via `speckit.New().CompileSpec(repoRoot, specRef)`. Exit 1 on compile error (missing `tasks.md`, malformed, unresolvable depends-on).
3. Validate the compiled DAG via `validate.ValidateForDispatch(dag, registry)`. Exit 1 on any validation error; render the full list to stderr (do not stop at the first).
4. Generate a fresh UUID for the run.
5. Call `client.ExecuteWorkflow(ctx, StartWorkflowOptions{ID: uuid, TaskQueue: <existing constant>}, "SchedulerWorkflow", SchedulerInput{RunID: uuid, Nodes, Edges, Tick: 0})`. Exit 2 on Temporal unreachable or any other Temporal SDK error.
6. Emit `scheduler_started` chain event via `chitin-kernel emit -event-json -`. Emit failures log a warning to stderr but do NOT change the exit code (D8).
7. Print success line to stdout in the form: `scheduled spec <ref> (<N> nodes, <M> capabilities required); run_id=<uuid>`.
8. Exit 0.

## Exit codes

- **0** — workflow scheduled (regardless of chain-emit success).
- **1** — user error: bad ref, ambiguous ref, missing `tasks.md` / `spec.md`, malformed `tasks.md`, DAG validation failure (NeedsClarification or unroutable capabilities).
- **2** — runtime error: Temporal unreachable, repo-root resolution failure, IO error reading the spec directory.

## stderr messages (stable strings — operators may grep)

| Condition | Message |
|---|---|
| No spec matches the ref | `error: no spec matching ref "<ref>"`<br>followed by `available specs:` and a sorted list |
| Ref is ambiguous | `error: ref "<ref>" is ambiguous — matched <N> specs:`<br>followed by sorted list |
| `tasks.md` missing | `error: spec <ref> has no tasks.md — run /speckit-tasks first` |
| `tasks.md` malformed | `error: spec <ref> tasks.md compile failed: <underlying error>` |
| DAG has NeedsClarification | `error: DAG validation failed — N node(s) have unclassified capability:`<br>followed by `  - <node_id>: <task description>` per node |
| DAG has unroutable | `error: DAG validation failed — N node(s) require capability not declared by any registered driver:`<br>followed by `  - <node_id>: capability '<tag>' — register a driver that declares it, or amend tasks.md` |
| Temporal unreachable | `error: Temporal unreachable at <host:port> — is the temporal-dev service running?` |
| Chain emit failed (warn only) | `warning: chain emit failed: <error> — schedule succeeded; the audit chain lost this entry` |

## Side effects

- Starts a Temporal workflow (one per successful invocation).
- Emits one `scheduler_started` chain event (best-effort; failure does not roll back the workflow start).
- Writes nothing to `.specify/`, `~/.chitin/gov.db`, or any other on-disk state.

## Non-behaviors (negative contract)

- MUST NOT consult the kanban or any decommissioned coordination surface (FR-012).
- MUST NOT register workflows or activities; that's the worker-host path. Subcommand mode is client-only.
- MUST NOT modify `tasks.md`, `spec.md`, or any other spec artifact.
- MUST NOT silently accept malformed input — always exit non-zero with a stable stderr message.

## Operator examples

```bash
# Schedule by numeric prefix:
chitin-orchestrator schedule 096

# Schedule by slug:
chitin-orchestrator schedule operator-session-state-surface

# Schedule by full directory name:
chitin-orchestrator schedule 096-operator-session-state-surface

# Schedule against a non-default Temporal (e.g., for testing):
chitin-orchestrator schedule 096 --temporal-host 127.0.0.1:17233

# Capture the run_id for later status / cancel:
RUN_ID=$(chitin-orchestrator schedule 096 | awk '{ for (i=1;i<=NF;i++) if ($i ~ /^run_id=/) { sub(/^run_id=/,"",$i); print $i } }')
```
