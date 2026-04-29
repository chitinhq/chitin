# Cost-Governance Kernel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the cost-governance kernel slice — cross-process budget envelope, T0/T2 tier router, ACP shim, watch dashboard, acpx-override installer — demo-ready for the "Compute-amortized intelligence: how chitin makes AI dev work bounded and predictable" tech talk on 2026-05-07 (9 days from plan start).

**Architecture:** Openclaw 2026.4.25 orchestrates a swarm of Copilot ACP sessions on its `subagent` lane. Every spawn is intercepted by `chitin-kernel drive copilot --acp --stdio --envelope=<id>` via a one-line acpx config override. Inside the chitin shim, tier router (T0 ollama-local / T2 copilot) + budget envelope (cross-process flock'd JSON) + `gov.Gate.Evaluate` all run before each tool call. The audit log in `~/.chitin/gov-decisions-<date>.jsonl` is the source of truth; `chitin-kernel watch` tails it on stage.

**Tech Stack:** Go 1.25 · existing chitin `gov` package (PR #45 + #51) · openclaw 2026.4.25 (already installed) · Copilot CLI 1.0.35 with `--acp --stdio` mode · Cobra for new subcommands · standard Go testing.

**Parent spec:** `docs/superpowers/specs/2026-04-28-cost-governance-kernel-design.md` (v2, openclaw-as-orchestrator)

**Smoke test artifacts already in place:**
- `~/.openclaw-chitin-test/openclaw.json` — isolated openclaw profile with override pre-configured
- `/tmp/chitin-acpx-wrapper.sh` — placeholder wrapper, replaced by the real binary in Task 1

---

## Execution guidance

### Dispatch hints

The talk narrative is "I used Copilot CLI to build chitin's governance for Copilot CLI." Where the task is mechanical and well-scoped, dogfood by dispatching to Copilot via the v1 `chitin-kernel drive copilot` driver. Cross-file integration and live-system debugging stay with Claude or operator.

- **[COPILOT]** — Well-specified, additive, one-to-two file scope, unit-test-friendly. These tasks produce the dogfood artifacts.
- **[CLAUDE]** — Multi-file integration, ACP transport judgment, debugging across the live openclaw stack.
- **[HUMAN]** — Operator judgment: rehearsal, runbook polish, talk-narrative work, mid-build checkpoint pivot calls.

### Day schedule target

| Day | Date       | Tasks   | Deliverable                                                    |
|-----|------------|---------|----------------------------------------------------------------|
| 1   | 2026-04-29 | 0, 1, 2 | Pin the seam: openclaw spawns chitin shim live (no governance) |
| 2   | 2026-04-30 | 3, 4    | gov.BudgetEnvelope + gov.Decision extensions                   |
| 3   | 2026-05-01 | 5, 6    | cost package + tier router                                     |
| 4   | 2026-05-02 | 7, 8    | ACP frame parser + intercept core                              |
| 5   | 2026-05-03 | 9, 10   | Wire shim through gov.Gate; refusal frames; envelope CLI       |
| 6   | 2026-05-04 | 11, 12  | Watch dashboard + 3-agent parallel live test                   |
| 7   | 2026-05-05 | 13      | **MID-BUILD CHECKPOINT** — all 5 demo beats end-to-end         |
| 8   | 2026-05-06 | 14, 15  | Talk runbook + dress rehearsal                                 |
| 9   | 2026-05-07 | 16      | Final rehearsal AM, talk PM                                    |

If Day 7 surfaces a blocker, drop the `chitin-kernel swarm` fallback (Task 14) without hesitation. The openclaw path is the demo.

### Branch

Work on `feat/cost-governance-kernel` off current `main`. Worktree: `~/workspace/chitin-cost-governance/` per `memory/feedback_always_work_in_worktree.md`.

---

## Task 0: Worktree + branch + prerequisites

**Dispatch:** [CLAUDE]

**Files:**
- Create: `~/workspace/chitin-cost-governance/` (git worktree)

- [ ] **Step 1: Create the worktree**

```bash
cd ~/workspace/chitin
rtk git fetch origin
rtk git worktree add ~/workspace/chitin-cost-governance -b feat/cost-governance-kernel origin/main
cd ~/workspace/chitin-cost-governance
rtk git status
```

Expected: clean worktree on `feat/cost-governance-kernel`, HEAD at latest origin/main (includes the merged v1 driver in `internal/driver/copilot/`).

- [ ] **Step 2: Verify prerequisites**

```bash
which copilot && copilot --version              # → 1.0.35 or newer
which openclaw && openclaw --version            # → 2026.4.25 or newer
go version                                       # → 1.25+
ls go/execution-kernel/internal/gov/             # → action, gate, decision, counter, normalize, policy, etc.
ls go/execution-kernel/internal/driver/copilot/  # → v1 driver merged
ls libs/adapters/ollama-local/                   # → scaffold present
ls ~/.openclaw-chitin-test/openclaw.json         # → smoke-test profile from prep
```

If any check fails, stop and resolve before Task 1.

- [ ] **Step 3: Confirm Copilot's ACP mode is available**

```bash
copilot --acp --help 2>&1 | head -30
```

Expected: `--stdio` or equivalent ACP-over-stdio flag visible. If Copilot's ACP mode doesn't match what we assumed, surface in plan-phase open question #1 and resolve before Task 7.

---

## Task 1: Live e2e of acpx override with placeholder shim

**Dispatch:** [HUMAN] — needs operator at terminal to interact with openclaw. Cannot be dispatched.

**Goal:** Prove the openclaw → acpx → wrapper → copilot chain actually fires. We tested the schema; this tests the runtime.

**Files:**
- Use existing: `/tmp/chitin-acpx-wrapper.sh`, `~/.openclaw-chitin-test/openclaw.json`

- [ ] **Step 1: Confirm wrapper logs are wired**

```bash
> /tmp/chitin-acpx-smoke.log  # truncate
cat /tmp/chitin-acpx-wrapper.sh  # confirms exec copilot $@ + log line
```

- [ ] **Step 2: Trigger an ACP spawn under the test profile**

```bash
# Ensure the gateway can start under the test profile
openclaw --profile chitin-test config validate 2>&1 | tail -5

# Start the gateway in foreground (separate terminal)
openclaw --profile chitin-test gateway

# In another terminal: trigger /acp spawn copilot --bind here from chat,
# OR call sessions_spawn from a one-shot agent run.
# Simplest path: openclaw --profile chitin-test acp client --server-args ...
# OR: write a tiny openclaw skill that spawns copilot.
```

- [ ] **Step 3: Verify the wrapper was invoked**

```bash
cat /tmp/chitin-acpx-smoke.log
```

Expected: at least one line `[<ts>] chitin-wrapper invoked, args: --acp --stdio` (plus any extra acpx-injected args).

If the log is empty: the override is NOT firing. Common causes:
- acpx plugin not enabled (`openclaw --profile chitin-test config get plugins.entries.acpx.enabled` should return `true`)
- Wrong agent id (override is on `copilot`, but the spawn used a different id)
- Copilot auth missing (acpx may abort before spawn)
- Schema accepted the value but the field name is wrong (manually grep `dist/extensions/acpx/register.runtime-CfTvMCxA.js` for `agents` to confirm runtime reads from this path)

**Do not proceed to Task 7 (ACP shim) until this task passes.** The whole plan rests on the override actually firing.

- [ ] **Step 4: Document the trigger command**

In the worktree, add `docs/superpowers/runbooks/2026-05-07-talk-runbook.md` with:
```markdown
## Setup verification
1. <exact command that triggered the spawn in step 2>
2. Tail /tmp/chitin-acpx-smoke.log to confirm wrapper fired.
```

This is the recovery script for the talk if the override seems off — operator can re-run this in 10 seconds to confirm the seam is alive.

- [ ] **Step 5: Commit the runbook stub**

```bash
rtk git add docs/superpowers/runbooks/2026-05-07-talk-runbook.md
rtk git commit -m "runbook: setup verification for acpx override seam"
```

---

## Task 2: Add `--acp --stdio` placeholder mode to chitin-kernel drive copilot

**Dispatch:** [COPILOT] — small, additive, well-bounded.

**Goal:** Replace the bash wrapper with a real `chitin-kernel drive copilot --acp --stdio` Go binary that does the same thing (proxy stdin/stdout to a child `copilot --acp --stdio`, log on entry). No governance yet — that lands in Tasks 7-9.

**Files:**
- Modify: `go/execution-kernel/cmd/chitin-kernel/copilot.go` (or wherever `drive copilot` lives)
- Create: `go/execution-kernel/internal/driver/copilot/acp/shim.go` (placeholder)
- Test: `go/execution-kernel/internal/driver/copilot/acp/shim_test.go`

**Context:** v1 driver embeds the Copilot Go SDK. ACP mode is a different transport — speak ACP frames, don't use the SDK. Keep both modes alive: `drive copilot` (existing SDK mode) and `drive copilot --acp --stdio` (new shim mode).

- [ ] **Step 1: Write the failing test**

```go
// shim_test.go
func TestShim_ProxiesStdinStdoutToChildCopilot(t *testing.T) {
    // Spawn shim with --child-cmd=/bin/cat (simulates copilot)
    // Write "hello\n" to shim stdin
    // Read shim stdout
    // Expect "hello\n" — straight passthrough
}
```

- [ ] **Step 2: Implement minimal shim**

```go
// shim.go
package acp

func Run(ctx context.Context, opts Opts) error {
    cmd := exec.CommandContext(ctx, opts.ChildCmd, opts.ChildArgs...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

type Opts struct {
    ChildCmd  string
    ChildArgs []string
    EnvelopeID string  // captured here, unused until Task 9
}
```

This is intentionally trivial — no frame parsing, no interception. Pure passthrough. Establishes the binary contract so we can replace the bash wrapper.

- [ ] **Step 3: Wire into `drive copilot` subcommand**

When `--acp` and `--stdio` flags are both set, take the shim path; else take the v1 SDK path. Default `--child-cmd=copilot`. Accept `--envelope=<id>` flag (and `CHITIN_BUDGET_ENVELOPE` env var fallback) but only log it for now.

- [ ] **Step 4: Run tests**

```bash
cd go/execution-kernel
go test ./internal/driver/copilot/acp/...
go test ./internal/driver/copilot/... ./internal/gov/...  # regression
```

- [ ] **Step 5: Replace the bash wrapper in the test profile**

```bash
go build -o /tmp/chitin-kernel ./cmd/chitin-kernel
openclaw --profile chitin-test config set \
    plugins.entries.acpx.config.agents.copilot.command \
    "/tmp/chitin-kernel drive copilot --acp --stdio --envelope=\$CHITIN_BUDGET_ENVELOPE"
```

- [ ] **Step 6: Re-run Task 1 verification with the real binary**

Confirm the shim is invoked, copilot proceeds normally, an actual ACP session works end-to-end. If it doesn't, this is a frame-corruption issue — likely stderr leaking into stdout or a missing flag. Resolve before Task 3.

- [ ] **Step 7: Commit**

```bash
rtk git add go/execution-kernel/
rtk git commit -m "feat(driver/copilot): add --acp --stdio placeholder shim mode"
```

---

## Task 3: Add `gov.BudgetEnvelope` (cross-process flock'd state)

**Dispatch:** [COPILOT] — well-specified, single-package, unit-testable.

**Files:**
- Create: `go/execution-kernel/internal/gov/budget.go`
- Test: `go/execution-kernel/internal/gov/budget_test.go`

**Context:** Per spec §Components/budget. Authoritative state at `~/.chitin/budget-<id>.json`. `flock(LOCK_EX)` for write, `LOCK_SH` for read. `Spend` debits and persists `Closed: true` on exhaustion (sticky).

- [ ] **Step 1: Failing tests**

```go
func TestBudget_LoadCreates(t *testing.T) {
    dir := t.TempDir()
    e, err := LoadEnvelopeAt(dir, "01J-test", BudgetLimits{BudgetUSD: 10})
    // expect file at dir/budget-01J-test.json with usd=10, spent=0, closed=false
}

func TestBudget_SpendDebits(t *testing.T) { ... }
func TestBudget_SpendExhaustsAndStays(t *testing.T) { ... }
func TestBudget_GrantRaisesCap(t *testing.T) { ... }
func TestBudget_ConcurrentSpendIsExact(t *testing.T) {
    // 100 goroutines × $0.10 against $10 envelope
    // expect exactly 100 successful spends, 0 over-spend
}
```

- [ ] **Step 2: Implement** per the struct definitions in spec §Components/budget. Use `golang.org/x/sys/unix.Flock` (already in go.sum from another package, or add).

- [ ] **Step 3: Run tests, run full gov suite for regressions, commit.**

```bash
go test ./internal/gov/...
rtk git add internal/gov/budget.go internal/gov/budget_test.go go.mod go.sum
rtk git commit -m "feat(gov): add cross-process BudgetEnvelope with flock"
```

---

## Task 4: Extend `gov.Decision` with envelope/tier/cost fields

**Dispatch:** [COPILOT] — pure additive, mirrors PR #51's Agent-field pattern.

**Files:**
- Modify: `go/execution-kernel/internal/gov/decision.go`
- Test: `go/execution-kernel/internal/gov/decision_test.go`

- [ ] **Step 1: Failing test**

```go
func TestDecision_JSONL_CarriesEnvelopeTierCost(t *testing.T) {
    dir := t.TempDir()
    d := Decision{
        Allowed: true, Mode: "guide", RuleID: "default-allow",
        Agent: "copilot-cli", EnvelopeID: "01J-x", Tier: T2Expensive,
        CostUSD: 0.05, InputTokens: 1234, OutputTokens: 567,
        Action: Action{Type: ActFileRead, Target: "x"},
        Ts: time.Now().UTC().Format(time.RFC3339),
    }
    require.NoError(t, WriteLog(d, dir))
    // read line, assert all 5 new fields present and equal
}
```

- [ ] **Step 2: Add fields to `Decision` struct**

```go
type Decision struct {
    // ... existing fields ...
    EnvelopeID   string
    Tier         Tier   // T0Local | T2Expensive | TierUnset
    CostUSD      float64
    InputTokens  int64
    OutputTokens int64
}
```

- [ ] **Step 3: Add `Tier` enum**

```go
// tier.go (new in gov package)
type Tier string
const (
    TierUnset    Tier = ""
    T0Local      Tier = "T0"
    T1Cheap      Tier = "T1"  // reserved, unused for talk
    T2Expensive  Tier = "T2"
)
```

- [ ] **Step 4: Extend `WriteLog` marshalled struct** with the 5 new fields, all `,omitempty` for backward compat.

- [ ] **Step 5: Run tests, regression, commit.**

---

## Task 5: Implement `internal/cost/`

**Dispatch:** [COPILOT].

**Files:**
- Create: `go/execution-kernel/internal/cost/{cost,rates}.go`
- Test: `go/execution-kernel/internal/cost/cost_test.go`

**Context:** Per spec §Components/cost. Rate table loaded from `chitin.yaml` under `cost.rates.<model_id>`. T0 = `CostDelta{ToolCalls: 1}` only. T2 estimates from action target length and a configurable `max_output_tokens` hint.

- [ ] **Step 1: Failing tests**

```go
func TestEstimate_T0_IsFreeButCountsCall(t *testing.T) {
    d := Estimate(Action{Type: "shell.exec", Target: "ls"}, T0Local, defaultRates)
    assert.Equal(t, CostDelta{ToolCalls: 1}, d)
}

func TestEstimate_T2_UsesRateTable(t *testing.T) {
    rates := RateTable{"copilot-gpt-4.1": {USDPerInputKtok: 0.005, USDPerOutputKtok: 0.015}}
    d := Estimate(Action{Type: "shell.exec", Target: strings.Repeat("x", 4000)}, T2Expensive, rates)
    // ~1k input toks * 0.005 + assumed-max output * 0.015
    assert.Greater(t, d.USD, 0.0)
}

func TestReconcile_ReturnsDelta(t *testing.T) { ... }
```

- [ ] **Step 2: Implement** `Estimate`, `Reconcile`, default rate table for one Copilot model. Approximate token count by bytes/4 — good enough for v1.

- [ ] **Step 3: Run tests, commit.**

---

## Task 6: Implement `internal/tier/`

**Dispatch:** [COPILOT].

**Files:**
- Create: `go/execution-kernel/internal/tier/tier.go`
- Test: `go/execution-kernel/internal/tier/tier_test.go`

**Context:** Per spec §Components/tier. Pure rule table. T0 default for read-shaped actions; T2 default for everything else; specific overrides for git.commit / gh.pr.create / etc.

- [ ] **Step 1: Failing tests** — one per documented rule (file.read → T0; git.commit → T2; shell.exec parse-shaped → T0; etc.).

- [ ] **Step 2: Implement** `Route(action, opts) Tier` as a switch + secondary check on `action.Params["sub_action"]`.

- [ ] **Step 3: Run tests, commit.**

---

## Task 7: ACP frame parser (`acp_decode.go`)

**Dispatch:** [CLAUDE] — needs ACP spec reading + frame-shape judgment.

**Files:**
- Create: `go/execution-kernel/internal/driver/copilot/acp/acp_decode.go`
- Test: `go/execution-kernel/internal/driver/copilot/acp/acp_decode_test.go`
- Fixture: `go/execution-kernel/internal/driver/copilot/acp/testdata/<captured-acp-transcript>.jsonl`

**Context:** Minimal frame parser — only the shapes the shim cares about: `tool_call_request`, `tool_call_response`, `cancel`, `prompt`. Unknown frames pass through as opaque bytes. ACP spec at https://agentclientprotocol.com/. Cross-reference with openclaw's `docs/cli/acp.md` "Compatibility Matrix" — anything Unsupported there is also opaque here.

- [ ] **Step 1: Capture a real ACP transcript**

Run `copilot --acp --stdio` interactively and capture stdin/stdout to a fixture. ~10 frames of one tool call cycle is enough.

- [ ] **Step 2: Failing tests**

```go
func TestDecode_ToolCallRequest_ExtractsAction(t *testing.T) {
    raw := loadFixture(t, "tool_call_request_1.json")
    frame, err := Decode(raw)
    require.NoError(t, err)
    tcr, ok := frame.(*ToolCallRequest)
    require.True(t, ok)
    assert.Equal(t, "shell.exec", tcr.Action.Type)
}

func TestDecode_UnknownFrame_PassesThrough(t *testing.T) {
    // assert Decode returns *OpaqueFrame for unrecognized shapes
}
```

- [ ] **Step 3: Implement** decoder with closed-enum frame types. Key invariant: every byte stream that comes in goes back out byte-equivalent unless it's a recognized tool-call frame.

- [ ] **Step 4: Run tests, commit.**

---

## Task 8: ACP intercept core (`intercept.go`) — no governance yet

**Dispatch:** [CLAUDE].

**Files:**
- Modify: `go/execution-kernel/internal/driver/copilot/acp/{shim,intercept}.go`
- Test: `intercept_test.go`

**Goal:** Replace the dumb passthrough in `shim.go` with a real bidirectional proxy that decodes frames in both directions and gives us the hook points for Task 9 to add governance.

- [ ] **Step 1: Failing test** — spawn shim with `--child-cmd=/path/to/fake-copilot` that emits a tool-call-request frame, assert intercept can read it without breaking the proxy.

- [ ] **Step 2: Refactor** `shim.go` to use a frame-aware loop instead of `io.Copy`. Use `acp.Decode` for incoming frames, dispatch through a `func(Frame) Frame` interceptor (initial impl: identity).

- [ ] **Step 3: Run tests + manually re-verify Task 1 still works** (proxy is end-to-end functional, just frame-aware now).

- [ ] **Step 4: Commit.**

---

## Task 9: Wire shim through `gov.Gate.Evaluate` + envelope.Spend

**Dispatch:** [CLAUDE].

**Goal:** The intercept hook from Task 8 actually does governance now. On every tool-call-request frame: tier route → cost estimate → envelope.Spend → gov.Gate.Evaluate → forward or refuse.

**Files:**
- Modify: `go/execution-kernel/internal/driver/copilot/acp/intercept.go`
- Modify: `go/execution-kernel/internal/gov/gate.go` — add `*BudgetEnvelope` parameter to `Evaluate` (nil = current behavior; non-nil = enforce)
- Test: `intercept_governance_test.go`

- [ ] **Step 1: Failing tests** — three scenarios:
  - Allow: `gov.Gate` returns Allow + envelope has budget → request forwarded unchanged
  - Deny by gate: `gov.Gate` returns Deny → synthesized refusal frame back to child with chitin Reason
  - Deny by budget: `envelope.Spend` returns ErrBudgetExhausted → synthesized refusal frame with envelope-exhausted text

- [ ] **Step 2: Extend `gov.Gate.Evaluate` signature**

```go
func (g *Gate) Evaluate(action Action, agent string, envelope *BudgetEnvelope) Decision {
    // Existing flow runs first.
    // If envelope != nil and decision.Allowed:
    //   estimate := cost.Estimate(action, decision.Tier, g.Rates)
    //   if err := envelope.Spend(estimate); err != nil {
    //     decision = budgetDeny(err)
    //   }
    //   decision.EnvelopeID = envelope.ID
    //   decision.CostUSD = estimate.USD
    //   ...
}
```

All existing call sites pass `nil` for envelope — backward-compatible.

- [ ] **Step 3: Implement intercept governance hook** that constructs the refusal frame in ACP shape (per Task 7's frame inventory). Refusal text = `decision.Reason + "\n\nSuggestion: " + decision.Suggestion + "\n\nCorrected: " + decision.CorrectedCommand` (omit blanks).

- [ ] **Step 4: Run tests, commit.**

---

## Task 10: `chitin-kernel envelope` subcommands + `install acpx-override`

**Dispatch:** [COPILOT] for `envelope`; [CLAUDE] for `install acpx-override` (config-merge logic is judgment-heavy).

**Files:**
- Create: `go/execution-kernel/cmd/chitin-kernel/envelope.go`
- Create: `go/execution-kernel/cmd/chitin-kernel/install_acpx.go`
- Tests for both.

**Context:** Per spec §Components/envelope and §Components/install_acpx. Idempotent merge. Backup before every change. Refuse to overwrite a non-chitin override.

- [ ] **Step 1: Implement `envelope create / inspect / list / grant / close` subcommands** wrapping the Task 3 BudgetEnvelope API.

- [ ] **Step 2: Implement `install acpx-override`** — read `~/.openclaw/openclaw.json` (or `~/.openclaw-<profile>/openclaw.json` with `--profile`), JSON-merge the canonical override block, write back atomically (write-tmp + rename), backup the original.

- [ ] **Step 3: Tests**: dry-run mode, idempotent re-run, profile path, refusal on non-chitin override (operator hand-modified the field).

- [ ] **Step 4: Manual smoke**: re-run Task 1 with `chitin-kernel install acpx-override --profile=chitin-test --dry-run` showing the diff.

- [ ] **Step 5: Commit.**

---

## Task 11: `chitin-kernel watch` dashboard

**Dispatch:** [CLAUDE].

**Files:**
- Create: `go/execution-kernel/cmd/chitin-kernel/watch.go`
- Create: `go/execution-kernel/internal/watch/{tail,render}.go`
- Test: render unit tests.

**Context:** Per spec §Components/watch. Plain ANSI cursor moves — no Bubble Tea unless we hit a feature gap. Tails `~/.chitin/gov-decisions-<today>.jsonl` via inotify (Linux); poll fallback elsewhere.

- [ ] **Step 1: Implement tail loop** — consume Decision lines, dispatch to render.
- [ ] **Step 2: Implement render** — top header (envelope total burn, time elapsed), per-agent panes (last action, tier breakdown, calls used, denials).
- [ ] **Step 3: Manual run** — `chitin-kernel watch &` then run any chitin-kernel command that writes a Decision; verify the pane updates.
- [ ] **Step 4: Commit.**

---

## Task 12: 3-agent parallel live integration test

**Dispatch:** [CLAUDE] + [HUMAN] (interactive).

**Files:**
- Test: `go/execution-kernel/internal/driver/copilot/acp/integration_3agent_test.go` (live-tag, skipped in CI by default).

**Goal:** Prove the demo. Three Copilot agents under one envelope via openclaw `sessions_spawn`, all sharing the same budget, one runaway forces a budget breach, the other two next-tool-calls deny.

- [ ] **Step 1: Operator setup**: `chitin-kernel envelope create --usd=2.00`, export `$CHITIN_BUDGET_ENVELOPE`, `chitin-kernel install acpx-override --profile=chitin-test`.

- [ ] **Step 2: Trigger 3 parallel spawns** via openclaw — exact command depends on Task 1 verification. Document in the runbook.

- [ ] **Step 3: Watch the watch dashboard** — confirm three agents appear with separate panes, budget burn climbs, eventually the cap hits and refusals propagate.

- [ ] **Step 4: Verify audit log** has three agents' decisions all stamped with the same `envelope_id`, mix of T0 and T2 tiers, one budget-exhausted Decision and ≥1 propagated denial.

- [ ] **Step 5: Capture stdout/log artifacts** for the talk slides — sanitized JSONL excerpts the operator can paste.

- [ ] **Step 6: Commit** the live-tag test + artifacts.

---

## Task 13: MID-BUILD CHECKPOINT (Day 7)

**Dispatch:** [HUMAN].

**Goal:** Burn down the entire talk demo in dress-rehearsal mode. If anything fails, decide today (not Day 8) what to drop.

**Demo beats:**

1. **Setup on stage** — `chitin-kernel envelope create --usd=10`, `chitin-kernel install acpx-override`, `chitin-kernel watch &`. Each command should be sub-second.
2. **Sequential T0 win** — spawn one Copilot agent against an issue that's all parsing/grep work. Watch the dashboard show all-T0, $0.00 spend, finishes in seconds.
3. **T2 escalation** — spawn an agent that needs a git.commit + gh.pr.create. Watch the dashboard show T0 → T2 transitions, spend climbing.
4. **3-agent fan-out under budget** — Task 12's scenario, but staying under the cap. All three finish, total spend visible.
5. **Budget breach** — same fan-out with a too-low cap. One agent hits the cap, siblings deny on next call. `chitin-kernel envelope grant <id> +5` and resume.

- [ ] **Run all five beats end-to-end** in one session.
- [ ] **Time each beat** — anything over 30s on stage needs a script-skip option.
- [ ] **Note every flake** — even if it works the second time, it's a flake on stage.
- [ ] **Decide:** is the openclaw path solid enough to demo? If yes, drop the swarm fallback (Task 14). If no, escalate Task 14 to fallback path and rehearse it.

---

## Task 14: `chitin-kernel swarm` fallback (drop if Task 13 succeeds clean)

**Dispatch:** [CLAUDE].

**Files:**
- Create: `go/execution-kernel/cmd/chitin-kernel/swarm.go`
- Schema: `go/execution-kernel/cmd/chitin-kernel/swarm-schema.json`
- Test: `swarm_test.go`

**Context:** Per spec §Components/swarm. Fallback only. In-process goroutines spawning `chitin-kernel drive copilot --acp --stdio --envelope=<id>` per agent. Reads the Lobster-borrow YAML schema.

- [ ] **Steps:** YAML schema validation, goroutine pool with max-parallel cap, context cancel on envelope exhaustion, hardKill timer.

- [ ] **Skip this entire task** if Task 13 confirms openclaw path is demo-solid.

---

## Task 15: Talk runbook + dress rehearsal

**Dispatch:** [HUMAN].

**Files:**
- Modify: `docs/superpowers/runbooks/2026-05-07-talk-runbook.md`

**Sections to write:**

- [ ] **Pre-talk checklist** — env vars set, openclaw on PATH, profile config valid, copilot auth present, ollama running, watch dashboard wired up.
- [ ] **Demo beats** — exact commands, expected dashboard state at each beat, recovery if anything stalls.
- [ ] **Recovery scripts** — envelope grant, profile reset, copilot re-auth.
- [ ] **Spoken-script outline** — ~5 minutes of talking time per beat × 5 beats = ~25 min demo, leaving room for Q&A.
- [ ] **Closing slide** — "We evaluated Lobster, memU, Composio. The kernel layer is what differentiates."

- [ ] **Full 60-min dress rehearsal** — everything live, including the entry/exit chatter. Time it. Note every drift.

- [ ] **Commit runbook.**

---

## Task 16: Final rehearsal + talk (Day 9)

**Dispatch:** [HUMAN].

- [ ] **Morning of**: one final rehearsal run-through.
- [ ] **Backup setup**: laptop + screen-share + mobile hotspot tested.
- [ ] **Talk.**

---

## Reference: existing artifacts in this branch's prep

| Artifact | Path | Purpose |
|---|---|---|
| Spec v2 | `docs/superpowers/specs/2026-04-28-cost-governance-kernel-design.md` | This plan's parent |
| Smoke-test profile | `~/.openclaw-chitin-test/openclaw.json` | Override pre-configured |
| Bash wrapper | `/tmp/chitin-acpx-wrapper.sh` | Replaced by real binary in Task 2 |
| Smoke log | `/tmp/chitin-acpx-smoke.log` | Override-fired evidence |
| openclaw 2026.4.25 | system-installed | Already upgraded |

## Worktree convention reminder

Per `memory/feedback_always_work_in_worktree.md`: all work happens in `~/workspace/chitin-cost-governance/`. Do not work in `~/workspace/chitin/` directly. The same applies to any subagents dispatched.
