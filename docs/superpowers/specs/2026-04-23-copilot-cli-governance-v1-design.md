# Copilot CLI Governance v1 — Design

**Date:** 2026-04-23
**Status:** Design. Ready for user review, then handoff to writing-plans for an execution plan.
**Forcing function:** Tech talk "Copilot CLI Without Fear: Adding Safety Guardrails to AI-Generated Terminal Commands," Session 2, 19:00, 2026-05-07 — **13 days from this spec.** 60-minute live-demo session demonstrating chitin governing Copilot CLI in real-world DevOps scenarios.
**Parent evidence:** `docs/superpowers/specs/2026-04-25-copilot-sdk-spike-findings.md` (2-day feasibility spike, all 4 rungs cleared, verdict **go / Option Y viable**).
**Parent governance:** `docs/superpowers/specs/2026-04-22-chitin-governance-v1-design.md` (PR #45 — agent-agnostic gov kernel + hermes plugin as reference).

## Preamble

Two decisions already established this session gate the shape of this spec and are treated as settled context, not requirements to re-justify:

- **Hermes is killed; chitin is governance around openclaw + Claude Code + Copilot CLI.** Chitin is no longer in the tick-loop business (see `memory/project_hermes_killed_chitin_as_governance.md`).
- **Option Y is the integration pattern for Copilot CLI** — in-kernel SDK-embedded, not an external plugin. The spike established this as architecturally clean and empirically viable.

What remains is the v1 build: a single Go package, a single `chitin-kernel` subcommand, two existing-file edits, and two new policy rules. Demo-ready by 2026-05-07. The spec below assumes the spike's architectural findings as pre-decided (Copilot SDK `v0.2.2` pinned; `UseLoggedInUser: true` auth; `OnPermissionRequest` as the typed synchronous intercept; 8-value `Kind` enum maps 1:1 to chitin `ActionType`; refusal via non-nil error return).

The posture on denial is **guide-mode + escalation lockdown (D + C from brainstorming)**: most denials carry chitin's `Reason` + `Suggestion` + `CorrectedCommand` back to the model via the refusal error string so the model pivots to a safe alternative; the existing `gov.Counter` escalation ladder (Normal → Elevated → High → Lockdown, from PR #45) triggers session-wide termination after N same-fingerprint denials. This reuses the governance primitive chitin already ships — Option Y is a *new driver pattern*, not new policy semantics.

## One-sentence invariant

Every tool call the Copilot CLI model proposes flows through chitin's governance gate in-process via `OnPermissionRequest`, is normalized to a canonical `Action`, evaluated against the existing `chitin.yaml` policy (with two new rules added for demo scenarios), and is either allowed, guided-denied (reason + suggestion returned to the model), or terminal-denied (lockdown triggered by the existing escalation counter) — with every decision written to `~/.chitin/gov-decisions-<date>.jsonl` tagged with `agent: "copilot-cli"`.

## Scope

### In scope

- **New package `go/execution-kernel/internal/driver/copilot/`** with five source files (client, normalize, handler, driver, tests) and `testdata/` for recorded JSON-RPC fixtures.
- **New subcommand `chitin-kernel drive copilot [prompt]`** with flags `--cwd`, `--interactive`, `--preflight`.
- **`Normalize(PermissionRequest) Action`** — one table-driven switch on the SDK's 8-value `Kind` enum (`shell`, `write`, `read`, `mcp`, `url`, `memory`, `custom-tool`, `hook`). Unknown Kind → `Action{Type: "unknown"}` (fail-closed per policy default-deny).
- **Library-direct gov call** — `OnPermissionRequest` handler calls `gov.Evaluate()` as a Go function, not via subprocess. This is the in-kernel architectural advantage Option Y buys over Option X.
- **Guide-mode denial encoding** — when `Decision.Allowed == false`, the refusal error string is formatted as `chitin: <reason> | suggest: <suggestion> | try: <corrected_command>` so the model sees *why* and can pivot.
- **Escalation ladder integration** — lockdown state (from `gov.Counter`) causes the handler to return a lockdown-class error; the driver detects it, prints a closing audit summary, and terminates the session cleanly with exit 0 (lockdown is correct operation, not error).
- **Two new policy rules** in `chitin.yaml`: `no-terraform-destroy` (new action type `infra.destroy`) and `no-curl-pipe-bash` (new regex on `shell.exec`).
- **Two new action types** in `gov/normalize.go`: `infra.destroy` with terraform/kubectl detection, and a new regex pattern in existing `shell.exec` for `curl ... | bash`-shaped commands.
- **Decision-log schema extension** — add `Agent string` field to `Decision` struct and the JSONL output shape (soft blocker #1 from the spike findings).
- **Gate binary path** — `exec.LookPath("chitin-kernel")` at client startup; fail-fast with install instructions if missing (soft blocker #2 from the spike findings). Applies to any downstream consumer; the in-kernel driver itself does not shell out.
- **Five demo scenarios** covered by existing + new rules: `git push --force`, `rm -rf /var/log/*`, `terraform destroy`, `curl ... | bash`, and an escalation-ladder demo exercising lockdown.
- **Pre-flight subcommand** (`--preflight`) that validates binary presence, auth, policy load, decision-log path, and `gov.db` availability. Used before every rehearsal and on talk day.
- **Test suite** per §Testing.

### Out of scope

- **OpenClaw governance plugin and Claude Code governance plugin.** Separate specs, separate v1s, post-talk timeline. The structural pattern for these is `chitin-kernel gate evaluate` via subprocess (like hermes plugin), different from this spec's library-direct integration.
- **Hermes retirement code removal.** Deferred until after the talk. Hermes code stays dormant (no new work dispatched); its `pre_tool_call` plugin path remains functional until formally retired in a separate cleanup spec.
- **Swarm coordinator** (cross-driver scheduler, backpressure). Deferred.
- **Verifier / post-action reality-check.** PR #45's v2 roadmap item. Deferred — the intercept is before execution, so hallucinated-success reports aren't a class of risk the Copilot driver adds.
- **Drift detection.** PR #45 v2 roadmap item. Deferred.
- **`chitin-kernel ingest-policy`** — folding decision-log entries into the chitin event chain. Phase 0 monitoring-parity workstream. Deferred until after the talk.
- **Expansion of policy vocabulary to agentguard's full 26 invariants.** Two new rules only, demo-driven.
- **Cross-platform polish.** Linux-only. `exec.LookPath` is portable-enough; macOS and Windows smoke tests are post-talk work.
- **Production-grade SDK version upgrade protocol.** v1 pins `v0.2.2`; upgrades are manual and deferred.
- **Non-shell Kinds beyond the five demo scenarios.** `write`, `read`, `mcp`, `url`, `memory`, `custom-tool`, `hook` all get normalizer entries and fall through to default-allow in the baseline. Specific per-Kind demo rules are post-v1 unless a scenario demands otherwise.
- **Readybench or bench-devs content.** Chitin is OSS; content-boundary rule applies (see `memory/feedback_chitin_oss_boundary.md`).

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  chitin-kernel drive copilot [prompt]  (Go, single process)         │
│                                                                     │
│    ┌─────────────────┐       ┌────────────────────────┐             │
│    │  Cobra dispatch │─────▶ │  driver.Run(ctx, prompt)│            │
│    │ drive_copilot.go│       │     driver.go          │             │
│    └─────────────────┘       └──────────┬─────────────┘             │
│                                         │                           │
│                                         │ load policy + construct   │
│                                         ▼ gov.Gate in-process       │
│                              ┌────────────────────────┐             │
│                              │ gov.Gate {policy,      │             │
│                              │   counter, logDir}     │             │
│                              │ (same instance, lib-   │             │
│                              │  direct — no fork)     │             │
│                              └──────────┬─────────────┘             │
│                                         │                           │
│                                         ▼                           │
│                              ┌────────────────────────┐             │
│                              │  copilot.Client        │             │
│                              │  wraps SDK v0.2.2      │             │
│                              │  client.go             │             │
│                              └──────────┬─────────────┘             │
│                                         │                           │
│                                         ▼ spawns                    │
│                              ┌────────────────────────┐             │
│                              │  copilot CLI subprocess│             │
│                              │  (via exec.LookPath)   │             │
│                              └──────────┬─────────────┘             │
│                                         │ JSON-RPC                  │
│                                         ▼ pipe                      │
│                              ┌────────────────────────┐             │
│                              │ session.On(handler)    │             │
│                              │ OnPermissionRequest(   │             │
│                              │   req, inv) (res, err) │             │
│                              │ handler.go             │             │
│                              └──────────┬─────────────┘             │
│                                         │                           │
│                              Normalize(req) → Action                │
│                              gate.Evaluate(action,                  │
│                                "copilot-cli") → Decision            │
│                                         │                           │
│              ┌──────────────────────────┼──────────────────────────┐│
│              ▼ Allowed                  ▼ Denied/guide             ▼│
│              │                          │                  Lockdown │
│              │ return (Approved, nil)   │ return (Denied,          │
│              │                          │   error(format(Decision)) │
│              │                          │                          │
│              └──────────┬───────────────┴────────────┬─────────────┘│
│                         │                            │              │
│                         ▼                            ▼              │
│                 SDK allows                   SDK refuses, model     │
│                 tool exec                    sees error, rephrases  │
│                                              (or lockdown → driver  │
│                                              terminates session)    │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
              Every Decision appended to:
              ~/.chitin/gov-decisions-<date>.jsonl
              with new field: {"agent": "copilot-cli", ...}
```

### Why library-direct, not subprocess

The `OnPermissionRequest` handler and `gov.Gate` live in the same Go process. Calling `gate.Evaluate()` as a function (not `exec("chitin-kernel", "gate", "evaluate", ...)`):

- Saves ~5–15ms per decision (fork/exec + JSON marshal round-trip).
- Preserves typed Go access to `Decision`, `Counter`, and `Policy`.
- Eliminates the "chitin-kernel not on PATH" failure mode entirely for the in-kernel driver (only external-plugin drivers like hermes need the binary).
- Matches the "Go kernel owns all side effects" architectural rule (see `memory/project_architectural_rules.md`).

External-plugin drivers (openclaw, claude-code) WILL use the subprocess path via `chitin-kernel gate evaluate`. The uniformity pitch is real but doesn't apply here — the Copilot SDK is our Go library, so we use the gov package as our Go library.

### Escalation: reuses the existing ladder

`gov.Counter` (from PR #45, in `gov/escalation.go`) already tracks per-(agent, action-fingerprint) denial counts and exposes `IsLocked(agent) bool`. The Copilot driver passes `agent="copilot-cli"` on every `gate.Evaluate()` call. The existing ladder thresholds (Normal <3 / Elevated 3-6 / High 7-9 / Lockdown ≥10) apply unchanged. On lockdown:

- `handler.go` returns an error tagged as lockdown (e.g., `&LockdownError{Agent: "copilot-cli"}` — a sentinel type the driver recognizes).
- `driver.go` catches it, prints an audit summary (turns, decisions made, denied fingerprints, escalation state), flushes the decision log, closes the session, and exits 0.
- `chitin-kernel gate reset --agent=copilot-cli` (existing PR #45 subcommand) clears state.

## Components

### New files in `go/execution-kernel/internal/driver/copilot/`

| File | Responsibility | Rough size |
|---|---|---|
| `client.go` | SDK client wrapper. `NewClient(opts)` resolves `copilot` binary via `exec.LookPath`, sets `UseLoggedInUser: true`, pins SDK import to `v0.2.2`. Exposes `Client.Start(ctx)`, `Client.CreateSession(cfg)`, `Client.Close()`. Passes `session.On` + `OnPermissionRequest` through. | ~80 lines |
| `normalize.go` | `Normalize(req copilot.PermissionRequest) gov.Action`. Table-driven switch on `req.Kind`: `shell` → `Action{Type: "shell.exec", Target: req.CommandText}`; `write` → `Action{Type: "file.write", Target: req.Path}`; `read` → `Action{Type: "file.read", Target: req.Path}`; `mcp/url/memory/custom-tool/hook` → appropriate types; default/unknown → `Action{Type: "unknown"}`. Sets `Action.Path = cwd` for all. | ~70 lines |
| `handler.go` | `Handler` struct holds `*gov.Gate`, agent id `"copilot-cli"`, and a `*LockdownNotifier` channel. Implements `OnPermissionRequest(ctx, req, inv) (Result, error)`. Calls `Normalize`, calls `gate.Evaluate`, formats Decision per §Guide-mode encoding, returns appropriate `(Result, error)`. Emits lockdown sentinel when `counter.IsLocked`. | ~120 lines |
| `driver.go` | `Run(ctx, prompt string, opts RunOpts) error` — top-level entry. Wires client + handler, starts session, sends prompt, awaits completion or lockdown-notifier signal, prints summary. Handles `--interactive` via a REPL loop that re-uses the same session. Also exposes `Preflight(opts) error` for `--preflight` flag. | ~150 lines |
| `*_test.go` | Unit tests per file — see §Testing. | ~400 lines total |
| `testdata/` | Redacted JSON-RPC fixtures salvaged from the spike's `captured-stream.jsonl` + two new fixtures for terraform + curl-pipe-bash scenarios. | ~4 fixture files |

### Guide-mode encoding format

The refusal error string is the only channel by which the model sees *why* its action was denied. Format:

```
chitin: <Decision.Reason> | suggest: <Decision.Suggestion> | try: <Decision.CorrectedCommand>
```

If Suggestion or CorrectedCommand is empty, the corresponding segment is omitted:

```
chitin: <Decision.Reason>
chitin: <Decision.Reason> | suggest: <Decision.Suggestion>
chitin: <Decision.Reason> | try: <Decision.CorrectedCommand>
```

Always prefix with `chitin:` so the model sees a consistent sentinel and the audience understands the source in demo output. Always a single line — multi-line error strings get flattened through the SDK's error channel and look noisy.

For lockdown, the error string takes a distinct form the driver parses (with the sentinel `chitin-lockdown:`):

```
chitin-lockdown: agent=copilot-cli denials=10 threshold=10 — session terminated. Reset with: chitin-kernel gate reset --agent=copilot-cli
```

### New CLI subcommand in `go/execution-kernel/cmd/chitin-kernel/`

`drive_copilot.go` — Cobra subcommand: `chitin-kernel drive copilot [prompt]`. Flags:

- `--cwd <path>` (default `.`) — policy scope; passed to `gov.LoadWithInheritance(cwd)`
- `--interactive` — REPL mode; repeated prompts against one session until `/quit` or EOF
- `--preflight` — run all startup validations, print report, exit without starting a session
- `--verbose` — log every Decision JSON to stderr (useful for demos; default off so stdout stays clean)

Exit codes:
- 0: session completed normally OR lockdown triggered cleanly
- 1: runtime error (network, subprocess crash, timeout)
- 2: startup error (binary not found, auth missing, policy load failed, `--preflight` failure)

### Modified files

#### `go/execution-kernel/internal/gov/decision.go`

Add:

```go
type Decision struct {
    // ... existing fields ...
    Agent string `json:"agent,omitempty"` // NEW
}
```

`Gate.Evaluate(action Action, agent string) Decision` already takes `agent` as a parameter but the current implementation drops it before writing the JSONL line. Fix: thread `agent` into the `Decision` construction and into `WriteLog` so every log line carries `"agent": "<name>"`. Existing consumers of the log ignore unknown fields (JSONL is forgiving); additive change; no schema version bump needed.

#### `go/execution-kernel/internal/gov/normalize.go`

Add:

1. **New ActionType constant:** `ActInfraDestroy = "infra.destroy"`.
2. **Shell-command post-normalize pattern detection:** after the existing `shell.exec` normalization, inspect the target string:
   - If it matches `^terraform\s+destroy\b` (possibly with `-auto-approve`), re-tag as `Action{Type: "infra.destroy", Target: target, Params: {"tool": "terraform"}}`.
   - If it matches `^kubectl\s+delete\s+(ns|namespace)\b`, re-tag as `Action{Type: "infra.destroy", Target: target, Params: {"tool": "kubectl"}}`. (Not required for v1 demos but lands in v1 since the action type exists anyway; keeps policy compact.)
   - If it matches `\bcurl\b[^|]*\|\s*(bash|sh)\b`, keep as `Action{Type: "shell.exec", ...}` but attach `Params: {"shape": "curl-pipe-bash"}` so the policy rule can match on the shape without needing a new type.

3. **Existing `no-destructive-rm` regex** covers `rm -rf` — no change needed.

#### `chitin.yaml` (repo root)

Add two rules:

```yaml
- id: no-terraform-destroy
  action: infra.destroy
  effect: deny
  reason: "terraform destroy removes live infrastructure"
  suggestion: "Use `terraform plan` first; if destroy is intended, it requires a human-approved path (not an agent action)"
  correctedCommand: "terraform plan"

- id: no-curl-pipe-bash
  action: shell.exec
  effect: deny
  target_regex: '\bcurl\b[^|]*\|\s*(bash|sh)\b'
  reason: "curl-pipe-bash executes untrusted remote code"
  suggestion: "Download the script first (`curl -fsSLo /tmp/x.sh <url>`), inspect, and run explicitly (`bash /tmp/x.sh`) if safe"
  correctedCommand: "curl -fsSLo /tmp/script.sh <url> && less /tmp/script.sh"
```

The existing `no-destructive-rm`, `no-force-push`, `no-protected-push`, and `no-env-file-write` rules cover the other demo risk classes. Baseline policy strictness stays monotonic (child policies can't weaken these rules; enforced by `gov.LoadWithInheritance`).

## Data flow

### Flow A — Allow path (demo: `ls /tmp`)

```
Operator: chitin-kernel drive copilot "list /tmp"
  → chitin-kernel: load policy, construct Gate
  → driver.Run → client.Start (spawns copilot CLI) → session.On(handler)
  → session.Send("list /tmp")
  → model fires bash tool call: {command: "ls /tmp"}
  → OnPermissionRequest({Kind: "shell", CommandText: "ls /tmp"}, inv)
    → Normalize → Action{Type: "shell.exec", Target: "ls /tmp", Path: cwd}
    → gate.Evaluate(action, "copilot-cli")
      → policy match: "default-allow-shell" → Decision{Allowed: true, ...}
      → counter.Record(allow) → level unchanged
      → decision log line appended with agent=copilot-cli
    → return (PermissionRequestResult{Kind: Approved}, nil)
  → SDK allows tool execution
  → ls runs, output returned to model
  → model composes final assistant reply
  → session completes
```

### Flow B — Guide-mode deny (demo: `rm -rf /var/log/*`)

```
session.Send("clean up /var/log")
  → model fires bash tool call: {command: "rm -rf /var/log/*"}
  → OnPermissionRequest({Kind: "shell", CommandText: "rm -rf /var/log/*"}, inv)
    → Normalize → Action{Type: "shell.exec", Target: "rm -rf /var/log/*", ...}
    → gate.Evaluate
      → policy match: "no-destructive-rm" → Decision{Allowed: false, Mode: "guide",
        Reason: "Recursive delete is blocked — use targeted file operations",
        Suggestion: "Use `git rm <specific-file>` or `rm <file>` for individual files",
        CorrectedCommand: "git rm <specific-files>"}
      → counter.Record(deny, fp=SHA256("copilot-cli|shell.exec|rm-rf-*"))
        → level: Normal (1/10)
      → decision log line appended
    → return (PermissionRequestResult{Kind: Denied},
             error("chitin: Recursive delete is blocked — use targeted file operations | suggest: Use `git rm <specific-file>`... | try: git rm <specific-files>"))
  → SDK surfaces error text to Copilot CLI → CLI surfaces to model
  → model fires a second assistant.turn_start (retry/rephrase)
  → model proposes: `find /var/log -name "*.log" -mtime +30 -delete` (safer alternative)
  → OnPermissionRequest fires again with the new command
  → Normalize + Evaluate + policy match: default-allow-shell → Allowed: true
  → tool runs, model completes
```

This is the money demo — on stage the audience sees: "model tried dangerous thing → blocked with reason → model understood → pivoted to safe alternative." The narrative is *the guard rail guides, doesn't just block.*

### Flow C — Escalation lockdown (demo: closing beat)

```
Prompt designed to trigger repeated destructive attempts
  → Turn 1: model tries rm -rf /X → denied, counter=1 (Normal)
  → Turn 2: model tries rm -rf /Y → denied, counter=2 (Normal)
  → Turn 3: model tries rm -rf /Z → denied, counter=3 (Elevated)
    → guide message now includes: "3/10 before lockdown"
  → ... turns 4-9 similar ...
  → Turn 10: model tries rm -rf /P → denied, counter=10 (Lockdown)
    → handler returns &LockdownError{Agent: "copilot-cli", Count: 10}
  → driver.Run catches LockdownError
    → prints audit summary:
        Session terminated: copilot-cli in lockdown
        Turns: 11  Decisions: 10 (10 denied, 0 allowed)
        Denied fingerprints: shell.exec|rm-rf-* (10×)
        Reset with: chitin-kernel gate reset --agent=copilot-cli
    → flushes decision log
    → closes session cleanly
    → exit 0 (lockdown is correct operation)
```

### Flow D — Startup failure (preflight semantics)

```
Operator: chitin-kernel drive copilot --preflight
  → step 1: exec.LookPath("copilot") → if fails, print install instructions, exit 2
  → step 2: shell out `gh auth status` → if not authenticated, print `gh auth login` guidance, exit 2
  → step 3: load policy via gov.LoadWithInheritance(cwd) → if parse fails, print file:line:col, exit 2
  → step 4: confirm ~/.chitin/ exists and writable → if not, create or error, exit 2
  → step 5: open ~/.chitin/gov.db → if not SQLite-valid, error, exit 2
  → print "preflight OK: <details>", exit 0
```

`--preflight` is invoked as the first step of every demo rehearsal and the morning of the talk. Cheap, fast, catches config rot.

### Flow E — Signal handling (mid-session SIGINT)

```
During session, operator hits Ctrl+C
  → driver.go: context cancels
  → session gracefully closes (SDK handles the SIGINT in its subprocess model)
  → handler flushes any pending decision log line
  → print "interrupted — <turns-completed> turns, <decisions-logged> decisions"
  → exit 0 (clean interruption, not failure)
```

## Error handling

| Failure | Behavior | Exit |
|---|---|---|
| `exec.LookPath("copilot")` err | Print install instructions (`npm install -g openclaw@latest` if we end up reusing openclaw's install path, OR the `gh` extension install path — confirm on day 1 of build) | 2 |
| `gh auth status` not authenticated | Print `gh auth login --scopes copilot` guidance | 2 |
| SDK session fails to start | Print error with SDK version + Copilot CLI version | 1 |
| `gov.LoadWithInheritance` fails (YAML parse, monotonic-strictness violation) | Print file:line:col + exact rule id | 2 |
| `~/.chitin/` not writable | Print path + `chmod` or `mkdir -p` guidance | 2 |
| Gate Evaluate internal error | Fail-closed: return `Decision{Allowed: false, Reason: "gate_internal_error: <err>"}`; log to stderr; continue session | session continues; decision-level error only |
| Unknown `PermissionRequest.Kind` | `Normalize` returns `Action{Type: "unknown"}`; policy default-denies; logged | session continues |
| Decision log ENOSPC | stderr warn; keep returning correct Decision to SDK (correctness > log completeness) | session continues |
| Lockdown triggered | Driver catches LockdownError, prints summary, terminates session | 0 |
| SIGINT mid-session | Clean close, flush log | 0 |
| Copilot subprocess crash | SDK error surfaces; driver prints subprocess status + exit code | 1 |
| Per-turn timeout (default 120s, configurable) | Cancel session, print timeout reason | 1 |
| Live-integration test network flake | Retry once; if still failing, mark test skipped (CI-only)  | n/a |

### Kill switches (inherited from PR #45, unchanged)

- **Soft:** `chitin.yaml` top-level `mode: monitor` — all denials become log-only. Useful for tuning new demo rules without blocking real work during rehearsal.
- **Hard:** `chitin.yaml` with single rule `{action: "*", effect: deny}` — everything denied. Useful as a "stage-panic" toggle if something goes wrong mid-demo.
- **Per-agent lockdown:** `chitin-kernel gate lockdown --agent=copilot-cli`.
- **Clear:** `chitin-kernel gate reset --agent=copilot-cli`.

## Testing

### Unit tests

| File | What it covers |
|---|---|
| `normalize_test.go` | One test per Kind value (8 kinds × allow + reject case). One test for unknown-Kind → `Action{Type: "unknown"}`. One test per demo scenario command (5): verifies the command text normalizes to the correct Action shape that the policy will match. |
| `handler_test.go` | Mock `*gov.Gate` returning synthetic Decisions. Verify callback returns `(Approved, nil)` for Allowed=true. Verify callback returns `(Denied, error(format(decision)))` for guide-mode deny, including error-string format exactness. Verify lockdown path returns `&LockdownError{...}`. |
| `client_test.go` | `exec.LookPath` failure path (temporarily rename `copilot` on PATH, restore after). Auth-missing path (use a test fixture env that fakes `gh auth status` fail). No live SDK call. |
| `driver_test.go` | Run-to-completion via recorded fixtures (injected into a fake client). Run-to-lockdown via injected escalation counter. Preflight-flag path with all 5 validation subcases (each validation success + failure). `--interactive` path with 3-prompt fixture and `/quit` terminator. |
| `gov/decision_test.go` | Verify `Agent` field lands in JSONL output; existing tests remain green. |
| `gov/normalize_test.go` | Verify `infra.destroy` detection for `terraform destroy` and `kubectl delete ns`; verify `shell.exec` with `curl-pipe-bash` shape on the three regex variants (`|bash`, `| bash`, `|sh`); verify non-matching commands don't accidentally match. |

### Integration tests

- `integration_test.go` — replay the spike's `captured-stream.jsonl` through a mock client feeding events to the real handler; verify gate calls are correct and every decision log entry matches the expected shape. No network, no Copilot binary needed; runs in CI.
- **Live integration (not CI, manual):** `make drive-copilot-live` — runs one actual Copilot session on the operator's box exercising one allow + one block (same fixtures as Rung 4 of the spike); confirms real end-to-end still works. Must pass before each demo rehearsal.

### Demo scenario tests

One test per demo scenario (5 total). Each verifies:

1. The command pattern for that scenario normalizes correctly.
2. The policy rule for it evaluates to deny (or allow, for the rare positive-path scenario).
3. The guide-mode error string format is exactly what the audience will see on stage (so the demo output is a tested property, not a hope).

### Escalation test

Fire 10 same-fingerprint denials against a real `gov.Counter` (with a temp SQLite DB under `$TMPDIR`); verify lockdown state at count=10; verify subsequent `OnPermissionRequest` returns lockdown error regardless of Kind; verify `gov gate reset --agent=copilot-cli` clears it; verify the next request post-reset evaluates normally.

### Pre-flight rehearsal routine (not a test — a checklist)

Run `chitin-kernel drive copilot --preflight` before every demo rehearsal. On talk day: run on laptop the morning of, then again ~30 min before session start. Output is a single-line "preflight OK" or a concrete failure message; fast fail beats subtle mid-demo surprise.

## Self-review

### Placeholder scan

No `TBD` / `TODO` / `fill in details`. The `<release-path>` placeholder for copilot install instructions in §Error handling is flagged with "confirm on day 1 of build" — that's a deliberate pointer for the implementer, not a hand-wave; the actual path will be one of three known options (gh extension / npm / direct binary download) depending on what's current at build time.

### Internal consistency

- Guide-mode encoding format is consistent across §Components and Flow B.
- Lockdown sentinel (`chitin-lockdown:`) is consistent across §Components, Flow C, and Error handling.
- Agent id (`"copilot-cli"`) is consistent across §Architecture, Flow A-E, and testing.
- SDK version (`v0.2.2`) pinned once in §Scope and referenced consistently.
- Escalation thresholds (3/7/10) are the PR #45 values, unchanged — consistent with the existing ladder.
- Decision-log agent field is additive (not a schema bump) — consistent across §Components and §Data flow.

### Scope check

Single plan, single driver, two policy rules, two new action-type behaviors, one schema extension. No multi-subsystem leak: no hermes code changes, no openclaw/claude-code plugin work, no chain-ingest work, no drift/verifier work. All post-talk work explicitly deferred with concrete follow-on pointers.

### Ambiguity check

- "Guide-mode denial" defined precisely: `Decision.Allowed == false` AND `Decision.Mode == "guide"` AND `Decision.Reason != ""`. The error string is a deterministic format.
- "Lockdown" defined precisely: `gov.Counter.IsLocked("copilot-cli") == true`; handler returns a sentinel error type; driver recognizes the type. Not a string-parse contract.
- "Library-direct" vs "subprocess-gov" defined with specific boundary: Copilot driver uses library; external-plugin drivers use subprocess. Documented.
- "Agent id" is always literal string `"copilot-cli"` for this driver; not configurable in v1. Post-v1 could accept a `--agent-suffix` flag for multi-session parallel runs.
- "Session-wide lockdown termination" means the specific process running `chitin-kernel drive copilot` exits cleanly with exit 0. Lockdown state persists in `~/.chitin/gov.db` across processes until reset, which is the intended behavior (a lockdown that's forgotten on process exit is not a lockdown).

### Out-of-scope leak check

- No hermes code modifications — the hermes plugin path remains functional until a separate retirement spec.
- No openclaw or claude-code plugin work — those are separate post-talk specs.
- No decision-log ingest into event chain (`chitin-kernel ingest-policy` is deferred).
- No drift detection, no verifier.
- No new invariants beyond the two demo rules.
- No Readybench / bench-devs content.

### Dependencies

All in place or available:

- Go 1.25 (current chitin toolchain).
- `github.com/github/copilot-sdk/go v0.2.2` — pinned; proven in the spike.
- `chitin-kernel` binary builds cleanly at `6ceb74b` + merged main.
- `gov` package (normalize, policy, gate, counter, decision, inherit) — all shipped in PR #45.
- `gh` CLI on operator's box with Copilot seat — confirmed during spike Rung 1.
- `copilot` CLI v1.0.35 (or current) at `/home/red/.vite-plus/bin/copilot` or equivalent resolvable PATH.

## Execution handoff

Next action: invoke `superpowers:writing-plans` to produce a task-by-task 12-day implementation plan + 1-day rehearsal plan. Target plan file: `docs/superpowers/plans/2026-04-23-copilot-cli-governance-v1.md`. The plan should:

- Sequence tasks so demo-worthy output is visible by Day 5 (the earliest "something on screen" milestone reduces demo-day risk).
- Mark which tasks are candidates for Copilot-CLI-driven implementation vs Claude-subagent-driven implementation (the dogfood directive — Copilot building its own guardrails is the talk's narrative hook).
- Include rehearsal-specific tasks (preflight check, timing practice, fallback-path rehearsal) as discrete items, not afterthoughts.
- Define a mid-build checkpoint (end of Day 6 or 7) where the build is paused for a full live-demo run against all 5 scenarios — if any scenario falters, that's the moment to pivot before the last-week crunch.
