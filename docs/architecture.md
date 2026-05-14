# Architecture

Chitin is a small Go kernel with thin TypeScript adapters per surface. The kernel owns all side effects; everything else is read-only.

## System diagram

```
                          Agent Surfaces (drivers)
   ┌──────────┐ ┌────────┐ ┌──────────┐ ┌────────┐ ┌──────────┐ ┌─────────┐
   │ Claude   │ │ Codex  │ │ Gemini   │ │Copilot │ │ openclaw │ │ MCP     │
   │ Code     │ │ CLI    │ │ CLI      │ │ CLI    │ │ (local-*)│ │ servers │
   │ PreToolU │ │ PreToo │ │ BeforeT  │ │ SDK    │ │ before_  │ │ via     │
   │ hook     │ │ hook   │ │ hook     │ │ wrap   │ │tool_call │ │ above   │
   └────┬─────┘ └───┬────┘ └────┬─────┘ └───┬────┘ └────┬─────┘ └────┬────┘
        │           │           │           │           │            │
        └───────────┴───────────┴────┬──────┴───────────┴────────────┘
                                     │ tool call
                                     ▼
                        ┌──────────────────────────┐
                        │  gov.Gate.Evaluate       │ ←── chitin.yaml
                        │  (Go kernel; only        │     (closed-enum
                        │   side-effect layer)     │      action vocab)
                        └──────────┬───────────────┘
                                   │ Decision: allow / deny / guide
                  ┌────────────────┼────────────────┐
                  ▼                ▼                ▼
        ┌──────────────────┐ ┌──────────┐ ┌─────────────────┐
        │   event chain    │ │ gov.db   │ │ OTEL emit       │
        │  hash-linked,    │ │ envelope │ │ projection only │
        │  canonical       │ │ counters │ │ one-way bridge  │
        └────────┬─────────┘ └──────────┘ └─────────────────┘
                 │
                 ▼ on disk (kernel single-writer)
   ~/.chitin/events-<run_id>.jsonl
   ~/.chitin/gov-decisions-YYYY-MM-DD.jsonl
   ~/.chitin/gov.db (envelope counters + budgets)
   ~/.chitin/usage/<driver>.json (universal usage feed; PR #269)
```

**Per-vendor integration matrix.** Same kernel, different integration points:

| Driver | Integration | Wire shape | Real-time gating? |
|---|---|---|---|
| `claude-code-headless` | PreToolUse hook (settings.json) | claudecode.HookInput | yes |
| `codex` | PreToolUse hook (~/.codex/config.toml) | codex.HookInput (byte-compatible w/ Claude Code; hooks/list RPC) | yes |
| `gemini` | BeforeTool hook (~/.gemini/settings.json) | gemini.HookInput (byte-compatible w/ Claude Code) | yes |
| `hermes` | `pre_tool_call` hook (~/.hermes/config.yaml) | hermes.HookInput (byte-compatible w/ Claude Code) | yes |
| `copilot` | wrapping orchestrator (`chitin-kernel drive copilot`) | Copilot SDK PermissionRequest | yes |
| `openclaw` (local-qwen/-glm/-deepseek + clawta orchestrator) | openclaw `before_tool_call` plugin (`apps/openclaw-plugin-governance/`) | openclaw plugin context | yes |
| MCP tools | flow through whichever agent dispatched | parent driver's hook | yes (via parent) |

Codex, gemini, and hermes all speak the Claude Code PreToolUse wire format byte-for-byte; only the per-tool name set differs (`Bash`/`apply_patch` for codex, `run_shell_command`/`edit`/`replace` for gemini, hermes-specific names for hermes). The `chitin-router-hook` shim — a compiled Go binary at `bin/chitin-router-hook` (source: `go/execution-kernel/cmd/chitin-router-hook/router_hook.go`) — is shared across all four PreToolUse-class drivers; `internal/driver/<vendor>/normalize.go` does the per-vendor translation. The shim stamps `CHITIN_DRIVER` from its `--agent=<cli>` flag (deferring to a caller-set value if one is exported) so every chain event carries an unambiguous driver identity.

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
chitin-kernel chain replay          # re-evaluate a session against current policy (kernel + heuristic layers)
chitin-kernel chain summarize       # compact markdown summary suitable for next-agent prompt injection
chitin-kernel chain related         # find sessions touching the same entry/files
chitin-kernel chain stats           # aggregate decisions by tool/action/rule/decision/agent
chitin-kernel chain recommend-tier  # data-driven starting-tier per action_type
chitin-kernel chain snapshot        # immutable hash-linked session export (sigstore-shape)
chitin-kernel ingest-transcript     # post-hoc audit of a Claude Code transcript
chitin-kernel ingest-otel           # post-hoc audit of OTEL spans (legacy ingest path)
chitin-kernel sweep-transcripts     # batch transcript ingest
chitin-kernel install-hook          # wire a Claude Code PreToolUse hook
chitin-kernel uninstall-hook
chitin-kernel install / uninstall   # surface-aware install (`--surface claude-code --global`)
chitin-kernel health                # report on resolved .chitin/ state
chitin-kernel gate <evaluate|status|lockdown|reset>
chitin-kernel router evaluate       # router pipeline (kernel verdict -> pure-Go signals/plugin checks -> advisory telemetry)
chitin-kernel simulate              # what-if a single hook input without executing
chitin-kernel envelope <…>          # cost-gov v3 envelope (cross-process, sqlite WAL)
chitin-kernel drive copilot         # in-kernel Copilot CLI driver
```

Operator-side scripts (under `scripts/`):

```
chitin-status                 # dashboard: timers + chain + router + spend + tier-recs
chitin-budget                 # per-driver $-spend + universal usage feeds (codex 5h/weekly, etc)
chitin-router-hook            # compiled Go binary (bin/chitin-router-hook); all PreToolUse-class hooks point at it; stamps CHITIN_DRIVER
install-kernel.sh             # idempotent rebuild/install + per-vendor hook refresh
install-gemini-hook.sh        # writes hooks block into ~/.gemini/settings.json
install-codex-hook.sh         # writes [features] codex_hooks=true + [[hooks.PreToolUse]] into ~/.codex/config.toml
```

## Layer Contracts v1

These are non-negotiable. New code, specs, and plans must respect them. See [`architecture/layer-contracts.md`](./architecture/layer-contracts.md) for the locked statement.

1. **Kernel Authority.** All execution passes through `gov.Gate.Evaluate`. The kernel is the only enforcement point.
2. **Driver Constraint.** `allowed_drivers` is a typed, schema-validated field on `ExecutionRequest` (non-empty, closed enum). Orchestrators pick within it; they cannot expand it. Active kernel-side narrowing (`chitin-kernel task validate`) is deferred per the 2026-04-30 addendum; downstream enforcement at every leaf hook by `gov.Gate.Evaluate` is the load-bearing guarantee.
3. **Routing Scope.** Routing optimizes for capacity (latency, availability, hardware) within the allowed set. It cannot expand it.
4. **Aggregation Role.** The event chain is canonical; OTEL is a non-authoritative projection. Aggregation never affects live execution.

## Hard rule

> Only the Go execution kernel may perform side effects. TypeScript libraries and adapters are read-only against the filesystem except via the kernel binary.

**Why:** Forensic fidelity of captured events depends on a single write path. Multiple writers = non-deterministic replay + contract drift. Canonicalization and emission are co-located in Go so the shell-parse and JSONL-append cannot disagree.

## Vendor integration patterns (open vs closed vendor)

Per-vendor integration shape varies by vendor posture; the `gov.Gate` API is shared.

- **Open vendors with native hook surface → in-process extension.** Vendor ships a PreToolUse-style hook config; chitin's router-hook binary is wired in via the vendor's config, kernel does normalization. Examples: Claude Code (`~/.claude/settings.json` PreToolUse), Codex (`~/.codex/config.toml` [features] codex_hooks=true + [[hooks.PreToolUse]]), Gemini (`~/.gemini/settings.json` BeforeTool — same wire shape, different event name), Hermes (`~/.hermes/config.yaml` `pre_tool_call`).
- **Open vendors with plugin runtime → openclaw plugin.** openclaw's `before_tool_call` plugin path via `apps/openclaw-plugin-governance/`. Examples: local-qwen / local-glm / local-glm-flash / local-deepseek, plus the clawta orchestrator agent itself.
- **Closed vendors → wrapping orchestrator.** Vendor ships a closed binary; chitin spawns the agent as a child of a chitin-driven harness. Example: Copilot CLI (`chitin-kernel drive copilot`).

Codex, gemini, and hermes all ship native PreToolUse-class hook systems that are byte-compatible with Claude Code's wire shape — gemini's `gemini hooks migrate --from-claude` is explicit about the equivalence. That's why all four slot into the same `chitin-router-hook` pipeline; the only per-vendor work is the tool-name normalizer in `internal/driver/<vendor>/normalize.go`.

Classify the vendor first; let the classification pick the integration shape, not the other way around.

## On-disk layout

The kernel writes to a `.chitin/` state dir. Resolution order: `--chitin-dir` → repo-local `.chitin/` (walk up from cwd) → fallback `$HOME/.chitin/`. See [README — Where chitin writes data](../README.md#where-chitin-writes-data) for the file conventions and the [health runbook](./runbooks/health.md) for diagnostics.
