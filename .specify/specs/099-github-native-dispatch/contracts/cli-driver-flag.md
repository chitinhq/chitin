# Contract — `chitin-orchestrator schedule --driver <id>`

## Surface

Extends the existing `chitin-orchestrator schedule <spec-ref>` subcommand (spec 097).

```bash
chitin-orchestrator schedule <spec-ref> [--driver copilot] [--repo <owner/name>] [--repo-root <path>] [--temporal-host <addr>]
```

## Behavior

| `--driver` value | Behavior |
|---|---|
| (omitted) | Existing spec 097 path: resolve spec, compile DAG, validate, dial Temporal, start `SchedulerWorkflow`. |
| `copilot` | New path: resolve spec, compile DAG, validate (steps 3–5 from spec 097), then SKIP Temporal dispatch. Instead: call `gh issue create` against `--repo`, assign `@copilot`, apply labels, emit `copilot_dispatched`, print URL. Exit 0. |
| (other) | Reserved. For now: exit 2 with `error: unknown driver: <value>`. Future: SelectDriver capability match. |

## Required flags on the Copilot path

| Flag | Required? | Default |
|---|---|---|
| `--repo` | **Yes** on `--driver copilot` (target repo for the issue) | none |
| `--driver` | (the trigger) | none |
| `--repo-root` | Optional (for spec resolution) | `$PWD` |

`--temporal-host` is ignored on the Copilot path.

## Exit codes (extension to spec 097's table)

| Code | Condition |
|---|---|
| 0 | Issue created, `copilot_dispatched` emitted. |
| 1 | User error: missing `--repo`, spec resolution failed, DAG validation failed, `gh` not on PATH, `gh` returned non-zero (Copilot not assignable on the repo, network error, etc.). Stderr has the reason. |
| 2 | Programmer error: invalid `--driver` value, internal panic. |

## stdout shape (success)

```text
copilot dispatched: https://github.com/<repo>/issues/<NUMBER>
  spec_ref: <NNN-name>
  issue_number: <NUMBER>
```

## stderr shape (failure)

```text
chitin-orchestrator: <stage>: <error message>
```

Stage names: `flag-parse`, `spec-resolve`, `dag-validate`, `gh-exec`, `chain-emit`.

## Chain side effects

On success: exactly one `copilot_dispatched` chain event (see `contracts/chain-events.md`).
On failure: zero chain events (the dispatch is atomic — either the issue+event both land, or neither).

## Constitutional gates touched

- §1 (kernel-side-effect boundary): `gh issue create` is a subprocess; routes through kernel PreToolUse hook for exec(2) gating.
- §7 (orchestrator-as-implementation-gate): this CLI invocation IS the orchestrator-intaked work-unit.
