# Architecture

Chitin is a small Go kernel with thin TypeScript adapters per surface. The kernel owns all side effects; everything else is read-only.

## System diagram

```
              Agent Surfaces (drivers)
   ┌────────────┐ ┌──────────────┐ ┌─────────────┐
   │ Claude Code│ │ Copilot CLI  │ │ openclaw    │
   │ PR #66 hook│ │ PR #51 SDK   │ │ acpx config │
   └─────┬──────┘ └──────┬───────┘ └──────┬──────┘
         │ tool call     │                │
         └───────────────┴────────┬───────┘
                                  ▼
                       ┌──────────────────────┐
                       │  gov.Gate.Evaluate   │  ←── chitin.yaml
                       │  (Go kernel; only    │      (closed-enum
                       │   side-effect layer) │       action vocab)
                       └──────────┬───────────┘
                                  │ Decision: allow / deny / guide
                  ┌───────────────┼───────────────┐
                  ▼               ▼               ▼
        ┌──────────────────┐ ┌──────────┐ ┌─────────────────┐
        │   event chain    │ │ gov.db   │ │ OTEL emit       │
        │  hash-linked,    │ │ envelope │ │ projection only │
        │  canonical       │ │ counters │ │ one-way bridge  │
        └────────┬─────────┘ └──────────┘ └─────────────────┘
                 │
                 ▼ on disk (kernel single-writer)
   .chitin/events-<run_id>.jsonl + flow_events.jsonl
   ~/.chitin/gov-decisions-YYYY-MM-DD.jsonl, gov.db
```

## Layers

| Layer | Path | Role | Side effects? |
|-------|------|------|---------------|
| Contracts | `libs/contracts/` | Zod event schema; Go types generated from it | No |
| Execution kernel | `go/execution-kernel/` | canon, normalize, emit, hook, gate, envelope | **Yes (only)** |
| Telemetry | `libs/telemetry/` | JSONL tailer, SQLite indexer, replay streamer | No |
| Adapters | `libs/adapters/<surface>/` | Thin per-surface forwarders | No |
| CLI | `apps/cli/` | Operator commands (`chitin run / events / replay`) | No |
| Souls | `souls/canonical/` + `souls/experimental/` | Cognitive lens definitions; `soul_id` populates `session_start.payload` | No |

The Go kernel exposes these subcommands:

```
chitin-kernel init                  # initialize a .chitin/ state dir
chitin-kernel emit                  # append a canonical event to the chain
chitin-kernel chain-info            # inspect chain state
chitin-kernel ingest-transcript     # post-hoc audit of a Claude Code transcript
chitin-kernel ingest-otel           # post-hoc audit of OTEL spans (legacy ingest path)
chitin-kernel sweep-transcripts     # batch transcript ingest
chitin-kernel install-hook          # wire a Claude Code PreToolUse hook
chitin-kernel uninstall-hook
chitin-kernel install / uninstall   # surface-aware install (`--surface claude-code --global`)
chitin-kernel health                # report on resolved .chitin/ state
chitin-kernel gate <evaluate|status|lockdown|reset>
chitin-kernel envelope <…>          # cost-gov v3 envelope (cross-process, sqlite WAL)
chitin-kernel drive copilot         # in-kernel Copilot CLI driver
```

## Layer Contracts v1

These are non-negotiable. New code, specs, and plans must respect them. See [`architecture/layer-contracts.md`](./architecture/layer-contracts.md) for the locked statement.

1. **Kernel Authority.** All execution passes through `gov.Gate.Evaluate`. The kernel is the only enforcement point.
2. **Driver Constraint.** The kernel exposes `AllowedDrivers(req)` as the feasible-driver set. Orchestrators consume it; they cannot derive their own or override it.
3. **Routing Scope.** Routing optimizes for capacity (latency, availability, hardware) within the allowed set. It cannot expand it.
4. **Aggregation Role.** The event chain is canonical; OTEL is a non-authoritative projection. Aggregation never affects live execution.

## Hard rule

> Only the Go execution kernel may perform side effects. TypeScript libraries and adapters are read-only against the filesystem except via the kernel binary.

**Why:** Forensic fidelity of captured events depends on a single write path. Multiple writers = non-deterministic replay + contract drift. Canonicalization and emission are co-located in Go so the shell-parse and JSONL-append cannot disagree.

## Two-driver pattern (open vs closed vendor)

Per-vendor integration shape varies by vendor posture; the `gov.Gate` API is shared.

- **Open vendors → in-process extension.** Vendor ships a documented extensibility API; chitin runs as a forked child via that API and gates tool calls via the vendor's own hook. Example: openclaw `acpx` config-override; Copilot CLI v2 (post-talk).
- **Closed vendors → wrapping orchestrator.** Vendor ships a closed binary; chitin spawns the agent as a child of a chitin-driven harness. Example: Copilot CLI v1 (`chitin-kernel drive copilot`); Claude Code likely fits here.

Classify the vendor first; let the classification pick the integration shape, not the other way around.

## On-disk layout

The kernel writes to a `.chitin/` state dir. Resolution order: `--chitin-dir` → repo-local `.chitin/` (walk up from cwd) → fallback `$HOME/.chitin/`. See [README — Where chitin writes data](../README.md#where-chitin-writes-data) for the file conventions and the [health runbook](./observations/runbooks/health.md) for diagnostics.
