# Contract: Queue Submission YAML Schema

**Consumer**: `chitin-orchestrator merge-queue submit <yaml-file>` CLI subcommand
**Producer**: operator (or future automation)
**Version**: `1`

This document defines the YAML schema the CLI accepts and the validation rules applied before a `MergeQueueWorkflow` is started.

---

## Schema (version 1)

```yaml
# Required: schema version. CLI rejects unknown versions.
version: 1

# Optional: human-readable label. Shown in OTLP telemetry and Discord
# notifications. Max 200 chars.
label: "Spec 092 follow-ups + spec 087-090 backlog"

# Required: policy-table version this submission is bound to.
# Must match the version compiled into the orchestrator binary.
# The CLI looks up the binary's version via `chitin-orchestrator policy-version`
# and rejects mismatch unless --override-policy-version is passed.
policy_table_version: "1.0.0"

# Required: ordered list of PRs to merge. Order is preserved; index 0 is
# attempted first. Min 1 entry, max 100 entries.
entries:
  - repo: chitinhq/chitin
    pr: 926
    # Optional: explicit class override. If set, must match the auto-
    # classified class OR strictly tighten it (governance is never
    # relaxable). Omit unless you intentionally need to escalate a class.
    expected_class: research-docs    # under v1.0.0 policy, docs/** auto-classifies as research-docs
    # Optional: indices (zero-based) of earlier entries this one depends on.
    # If any dependency is not merged (skipped, halted, aborted), this entry
    # is recorded as not-attempted with reason "dependency unmet".
    depends_on: []
    # Optional: free-form operator note. Max 500 chars. Surfaced in
    # telemetry and Discord. Use to capture intent ("merging research
    # report after constitution amendment per spec-092 plan").
    note: "Industry alignment research grounding §7."

  - repo: chitinhq/chitin
    pr: 927
    expected_class: spec-only
    depends_on: [0]   # waits for #926 to land first
    note: "No-driver-bypass invariant spec — depends on §7 ratification."

  - repo: chitinhq/chitin
    pr: 919
    note: "Spec 087 retire kanban substrate"

  - repo: chitinhq/chitin
    pr: 924
    expected_class: bookkeeping
    note: "Mark spec 068 tasks complete."
```

---

## Field reference

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `version` | int | yes | Must equal `1`. Other values rejected. |
| `label` | string | no | ≤ 200 chars. UTF-8. |
| `policy_table_version` | string | yes | Semver. Must match the orchestrator binary's compiled version unless `--override-policy-version` is passed on the CLI (and the override is recorded in telemetry). |
| `entries` | array | yes | Length 1–100. |
| `entries[].repo` | string | yes | Format `{owner}/{name}`. Both segments `[A-Za-z0-9._-]+`. |
| `entries[].pr` | int | yes | > 0. |
| `entries[].expected_class` | string | no | One of `governance`, `live-fix`, `spec-only`, `research-docs`, `impl`, `bookkeeping`. If set and the auto-classified class would be `governance`, the override must also be `governance` (no relaxation). |
| `entries[].depends_on` | array of int | no | Each value must be < the entry's own index (forward-only references). Empty array allowed (equivalent to omitting). |
| `entries[].note` | string | no | ≤ 500 chars. UTF-8. |

---

## CLI invocation

```bash
# Standard submission
chitin-orchestrator merge-queue submit ./queue.yaml

# Dry-run: validate YAML, classify each PR, print the policy class
# assignment per entry. Does NOT start a workflow.
chitin-orchestrator merge-queue submit --dry-run ./queue.yaml

# Override the policy table version check (recorded in telemetry)
chitin-orchestrator merge-queue submit --override-policy-version ./queue.yaml

# Output: prints the parent workflow ID on success, or a structured
# validation error on failure (exit code 2 for validation, 1 for
# Temporal connection issues, 0 for successful workflow start).
```

---

## Validation rules

The CLI performs ALL validation before calling `ExecuteWorkflow`. Validation errors are reported to stderr in this format:

```text
ERROR: queue submission validation failed:
  entries[2]: dependency reference 3 must be < entry index 2
  entries[5].expected_class: cannot relax 'governance' (auto-classified) to 'impl'
  policy_table_version: submission specifies '0.9.0' but binary is '1.0.0' (use --override-policy-version to bypass)
```

Validation steps (in order, all must pass):

1. **Schema parse**: YAML must parse and conform to the schema above.
2. **Version check**: `version == 1`.
3. **Entry count**: 1–100.
4. **Repo format**: every `entries[].repo` matches `{owner}/{name}`.
5. **PR positivity**: every `entries[].pr > 0`.
6. **Duplicate detection**: no two entries share `(repo, pr)`.
7. **Dependency forward-only**: every `depends_on` index `< entry index`.
8. **Dependency existence**: every `depends_on` index `< total entries`.
9. **Class override validity**: if `expected_class` set, must be a valid class name.
10. **Policy version match**: unless `--override-policy-version` passed, must match binary version.
11. **Credential availability**: for each unique repo, `gh auth status -h <owner>` succeeds.
12. **Per-PR existence check**: `gh pr view <pr> -R <repo> --json state` returns OPEN.
13. **Auto-classify pre-check**: for each PR, fetch a `PRSnapshot` and run `Classify` (research R-POL). If `expected_class` is set and would relax `governance`, reject.

Steps 1–10 are pure CLI-side validation. Steps 11–13 require network calls to GitHub.

---

## Output format

On successful submission:

```text
Queue accepted.
  Submission ID: 01HF9P3R8X7M5K2N4Q6T8V1Y3W
  Parent workflow: merge-queue-01HF9P3R8X7M5K2N4Q6T8V1Y3W
  Policy table version: 1.0.0
  Entries: 4

Inspect:
  temporal workflow describe -w merge-queue-01HF9P3R8X7M5K2N4Q6T8V1Y3W
  temporal workflow show     -w merge-queue-01HF9P3R8X7M5K2N4Q6T8V1Y3W --follow
```

On dry-run:

```text
Dry run — no workflow started. Classification per entry:

  [0] chitinhq/chitin#926  → research-docs   (matches expected — docs/**, no gov trigger in v1.0.0)
  [1] chitinhq/chitin#927  → spec-only       (matches expected)
  [2] chitinhq/chitin#919  → spec-only       (no expected_class; auto-assigned)
  [3] chitinhq/chitin#924  → bookkeeping     (matches expected)

Validation: PASSED. Submit without --dry-run to start the workflow.
```

---

## Backwards compatibility

- The `version: 1` field exists so future schema changes can add `version: 2` without breaking existing operator scripts.
- New fields added in v2+ will be optional in v1 parsing or rejected with a clear "schema version 1 does not support field X" error.
- The `policy_table_version` field protects against running an old queue against a new policy (or vice versa). Bumping policy is intentionally a coordinated act.

---

## Example: the current 7-PR backlog (SC-001 test fixture)

```yaml
version: 1
label: "Spec 092/091/087-090 backlog — orchestrator's first real workload"
policy_table_version: "1.0.0"
entries:
  - repo: chitinhq/chitin
    pr: 926
    expected_class: research-docs
    note: "Industry alignment research grounding §7."

  - repo: chitinhq/chitin
    pr: 927
    expected_class: spec-only
    depends_on: [0]
    note: "No-driver-bypass invariant spec — depends on §7 ratification."

  - repo: chitinhq/chitin
    pr: 919
    expected_class: spec-only
    note: "Spec 087 — retire kanban substrate."

  - repo: chitinhq/chitin
    pr: 920
    expected_class: spec-only
    note: "Spec 088 — cull agent-bus mention listeners."

  - repo: chitinhq/chitin
    pr: 921
    expected_class: spec-only
    note: "Spec 089 — retire pre-v2 skills."

  - repo: chitinhq/chitin
    pr: 922
    expected_class: spec-only
    note: "Spec 090 — Discord channel-ingress for @clawta."

  - repo: chitinhq/chitin
    pr: 924
    expected_class: bookkeeping
    note: "Mark spec 068 tasks complete."
```
