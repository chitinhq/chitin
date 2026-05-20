# 038 — IcarusHarborAgent Adapter: Thin Baseline (terminal-bench@2.0)

> **Spec:** 038-icarus-harbor-agent-adapter
> **Author:** red
> **Status:** draft — awaits Ares + Clawta peer review, then operator ratification
> **Created:** 2026-05-19
> **Consensus:** Path A — thin qwen3-coder/tmux baseline; no deterministic layers until failure clusters emerge (msgs 5406–5408)

---

## 1. Goal

Adapt the existing `IcarusAgent` (currently a Harbor `BaseAgent` subclass in `swarm/icarus_harness/agent.py`) into a standalone **IcarusHarborAgent** thin baseline that runs on terminal-bench@2.0 via Harbor as the public scoreboard.

The adapter is the minimum viable bridge between Icarus's inner loop (parse → exec → observe → repeat until TASK_COMPLETE or loud-fail) and the operator's RTX 3090 running `qwen3-coder:30b-32k` through LiteLLM. No new capabilities. No deterministic router. No orchestration. Just the loop, the model, and the shell.

**Spec number correction:** The root ticket title says "036" for paper-trail continuity, but the canonical spec path is `.specify/specs/038-icarus-harbor-agent-adapter/spec.md`. Two 036 specs already exist on origin/main (`036-dispatch-fault-tolerance-invariants` and `036-ic-001-icarus-local-llm-driver`), plus 037 is taken. The actual spec number is 038.

---

## 2. Scope (IN scope — what ships in v1)

1. **IcarusHarborAgent thin baseline** — a Python module (`swarm/icarus_harness/harbor_adapter.py`) that wires IcarusAgent's parse/exec/observe loop to LiteLLM instead of Harbor's container environment, producing the same JSONL trajectory and summary artifacts IcarusAgent already writes.

2. **Harbor async contract** — the adapter respects Harbor's async execution contract (`BaseAgent.run`) and reuses `AgentContext` / `ExecResult` types. The adapter does NOT run inside Harbor's Docker harness; it calls ollama directly via LiteLLM and executes commands via tmux instead of `environment.exec`. The contract boundary is the `BaseAgent` interface; beneath it, the execution substrate swaps from Docker→tmux.

3. **qwen3-coder:30b-32k via LiteLLM** — the LLM call path uses `litellm.completion()` with provider `ollama/qwen3-coder:30b-32k`. This replaces the direct `urllib` ollama client in the existing `ollama_chat()` function. LiteLLM provides provider normalisation, timeout management, and a clean fallback surface if the model changes later.

4. **tmux multi-turn shell loop** — instead of Harbor's per-turn fresh `environment.exec` (which starts a new process per step with no shell state), the adapter runs a persistent tmux session where shell state (cwd, exports, background jobs) carries across turns. The agent still emits one command per turn; the adapter pastes it into the tmux pane and captures output.

### What each IN-scope item replaces from the existing harness

| Existing harness (Docker/Harbor) | Adapter (tmux/LiteLLM) |
|---|---|
| `harbor.environments.base.BaseEnvironment.exec(cmd)` | tmux send-keys + capture-pane |
| `ollama_chat()` (direct urllib POST to `:11434`) | `litellm.completion()` with `ollama/qwen3-coder:30b-32k` |
| Harbor's container provision + `BaseAgent.setup()` | tmux session creation + `IcarusAgent.setup()` (no-op) |
| Harbor's trial runner (`harbor run`) | `icarus-adapter-runner` CLI entry point |

---

## 3. Scope (OUT of scope — explicitly deferred)

1. **Deterministic-first lane** — no rule-based router that tries bash patterns before invoking the LLM. The thin baseline ships the LLM loop as the sole path. Deterministic layers are deferred until failure clusters emerge from observed traces (see §4 Rationale).

2. **Receipts / kanban / Clawta-escalation** — the adapter produces IcarusAgent's existing trajectory + summary artifacts. It does NOT post WORKER_RECEIPT to the kanban board, does NOT escalate to Clawta, and does NOT integrate with the swarm board. Those surfaces belong to the outer icarus-watcher dispatch layer (spec 036-ic-001), not the adapter.

3. **Autofix layers** — no speculative code-fix heuristics (lint-fix, import-sort, dead-code-prune) wired into the adapter. Those belong to the skill-lane dispatch in spec 036-ic-001, layered on top of this baseline.

4. **Multi-model routing** — one model (`qwen3-coder:30b-32k`). No fallback to a smaller or cloud model. The adapter loud-fails when the model is unavailable.

5. **Harbor Docker harness changes** — the existing `harbor run` codepath and `IcarusAgent` for Docker-based runs are untouched. The adapter is an alternative entry point, not a replacement.

6. **VRAM lease/lock** — unlike the icarus-watcher (spec 036-ic-001 §Invariant 4), this adapter does NOT manage `~/.icarus/model-lease.lock`. The operator is responsible for ensuring the model is loaded (or the adapter loud-fails on connection refused).

---

## 4. Rationale: Why path A (thin baseline first)

This section documents the three-way consensus from agent-bus messages 5406–5408 (red, Ares, Clawta) that selected **path A**: ship a thin LLM-backed baseline first, defer all deterministic routing until failure clusters emerge from observed traces.

### Path A: Thin baseline (selected)

Ship the minimum loop — LLM call → parse → exec → observe → repeat — with IcarusAgent's existing loud-fail taxonomy but no deterministic routing layer. Every turn goes through the model. Trajectories are recorded. Failure clusters are mined from those trajectories later to inform which deterministic shortcuts (if any) are worth adding.

### Path B: Deterministic router first (deferred)

Pre-classify tasks into "bash-solvable" vs "LLM-required" buckets. Route bash-solvable tasks through deterministic heuristics without invoking the model at all. Only fall through to the LLM for tasks that fail classification.

### Path C: Dual-path parallel (deferred)

Run both deterministic and LLM paths for every task, compare results, and gate the deterministic path's promotion on beating the LLM path on latency + correctness over a rolling window.

### Consensus summary (msgs 5406–5408)

| Voter | Position | Key argument |
|---|---|---|
| red (msg 5406) | Path A | No data to justify deterministic routing. Shipping the baseline collects the traces needed to design any future deterministic layer from evidence, not speculation. Premature abstraction is the risk. |
| Ares (msg 5407) | Path A | The Harbor harness already exists and works. Layering a tmux adapter on top of the proven IcarusAgent inner loop is tractable. A deterministic router would need its own validation surface; we don't have the failure data yet. |
| Clawta (msg 5408) | Path A with amendment | Agrees with thin baseline, but insists the adapter MUST preserve IcarusAgent's loud-fail taxonomy (block_reasons: parse_failure, ollama_error, loop_detected, step_budget_exceeded, wallclock_exceeded) verbatim so the watcher layer can consume failures without schema drift. Amendment accepted. |

**Result:** Path A carries unanimously. The explicit contract is: the adapter ships the LLM-only loop first, records every trajectory, and gates any future deterministic layer on failure-cluster evidence mined from those trajectories.

---

## 5. Invariant (locked)

**INV-038-HA-1: No speculative deterministic router.**

The adapter MUST NOT ship a deterministic-first routing layer. Every turn in the thin baseline goes through the LLM. Failure traces from this baseline are the ONLY gate for any future deterministic layer: a deterministic shortcut is promoted into the loop only when the failure-cluster evidence from recorded trajectories demonstrates that the shortcut would have resolved the task faster and more reliably than the LLM path alone. Until such evidence exists, the baseline stays LLM-only.

This invariant is the three-way consensus (msgs 5406–5408) made explicit. Violating it — by shipping a deterministic router without trace evidence — is a process bug.

---

## 6. Architecture

```
                    ┌─────────────────────────────────────────┐
                    │  icarus-adapter-runner (CLI entry)      │
                    │  parses args → creates tmux session     │
                    │  → invokes IcarusAgent via adapter       │
                    └──────────────┬──────────────────────────┘
                                   │
                    ┌──────────────▼──────────────────────────┐
                    │  harbor_adapter.py                       │
                    │  TmuxEnvironment(BaseEnvironment)       │
                    │    .exec(cmd) → tmux send-keys + capture │
                    │  LiteLLMChat (replaces ollama_chat)      │
                    │    .complete(messages) → litellm call    │
                    │  IcarusAgent.run(instruction, env, ctx)  │
                    └──────────────┬──────────────────────────┘
                                   │
                    ┌──────────┬───┴───────────┬──────────────┐
                    │          │               │              │
                    ▼          ▼               ▼              ▼
              tmux pane   litellm/ollama   logs_dir/      summary
              (stateful   (qwen3-coder    .jsonl       .json
               shell)      :30b-32k)      trajectory)   block_reason)
```

The adapter reuses IcarusAgent's core loop (parse → exec → observe → loop detection → loud-fail taxonomy) by swapping two substrates:

1. **LLM calls**: `ollama_chat()` → `litellm.completion()` (provider `ollama/qwen3-coder:30b-32k`). Same temperature=0, seed=0 determinism contract. LiteLLM handles provider routing; the adapter doesn't need to know whether ollama is local or remote.

2. **Command execution**: `BaseEnvironment.exec()` → `TmuxEnvironment.exec()` (new subclass). `TmuxEnvironment` manages a tmux session per trial, pastes each command block via `send-keys`, and captures output via `capture-pane`. Shell state (cwd, env vars, background processes) persists across turns — this is the primary behavioral difference from Harbor's per-turn fresh exec.

---

## 7. Class Contract

### 7.1 Hierarchy

```
harbor.agents.base.BaseAgent          (Harbor ABC — abstract)
 └── swarm.icarus_harness.agent.IcarusAgent   (existing: Ollama, bash-subprocess)
      └── swarm.icarus_harness.agent.IcarusHarborAgent  (NEW: LiteLLM, tmux)
```

### 7.2 Constructor

```python
class IcarusHarborAgent(IcarusAgent):
    """Harbor-integrated Icarus agent with tmux sessions and LiteLLM routing.

    Invoked via:
        harbor run \\
            --agent-import-path swarm.icarus_harness.agent:IcarusHarborAgent \\
            --model litellm:qwen3-coder:30b-32k \\
            --path <task-dir>

    Unlike IcarusAgent (Ollama-only, fresh subprocess per turn),
    IcarusHarborAgent drives the multi-turn loop through a persistent
    tmux session inside the Harbor Docker container, and routes LLM
    calls through LiteLLM for provider-agnostic model access.
    """

    SUPPORTS_ATIF = False
    SUPPORTS_WINDOWS = False

    def __init__(
        self,
        logs_dir: Path,
        model_name: str | None = None,
        *args,
        step_budget: int = 30,
        step_timeout_sec: int = 60,
        wallclock_sec: int = 900,
        litellm_model: str | None = None,
        tmux_session_name: str = "icarus-harbor",
        tmux_socket_path: str | None = None,
        llm_timeout_sec: int = 180,
        ollama_host: str = "http://127.0.0.1:11434",
        **kwargs,
    ) -> None: ...
```

**Constructor invariants:**

1. `model_name` is never `None` in production — Harbor always injects it via `--model`.
2. If `model_name` starts with `"litellm:"`, the agent MUST use the LiteLLM completion path; the `"litellm:"` prefix is stripped for the actual model identifier.
3. If `model_name` does NOT start with `"litellm:"`, the agent falls back to the Ollama client (backwards compatibility with existing `IcarusAgent` behavior).
4. `tmux_session_name` must be a valid tmux session name (no `.` or `:` characters, non-empty).

### 7.3 Lifecycle methods

#### `async setup(environment: BaseEnvironment) -> None`

1. Verify tmux is available inside the container via `environment.exec("which tmux")`.
   - If `return_code != 0`: raise `IcarusHarborSetupError("tmux not found in container")`
2. Create a new tmux session: `tmux new-session -d -s <tmux_session_name>`
   - If `return_code != 0`: raise `IcarusHarborSetupError(...)`
3. (If litellm model) Verify LiteLLM connectivity with a 1-token probe.
   - If probe fails: raise `IcarusHarborSetupError("LiteLLM connectivity check failed")`
4. (If ollama model) Verify Ollama connectivity with a 1-token probe.
5. Emit environment bootstrap probes into the system prompt (inherited IcarusAgent behavior).

**BaseAgent compliance:** `setup()` is abstract in `BaseAgent`. `IcarusAgent` provides a no-op implementation. `IcarusHarborAgent` overrides it to create the tmux session and verify LLM connectivity.

**Harbor lifecycle position:** Called once per trial, after `environment.start()` and before `run()`.

#### `async run(instruction: str, environment: BaseEnvironment, context: AgentContext) -> None`

The core agent loop:

1. Build system prompt (inherited from IcarusAgent, with tmux session awareness added).
2. Append the instruction as the first user message.
3. For each step up to `step_budget`:
   a. Call LLM (LiteLLM or Ollama, depending on model_name prefix).
   b. Record assistant response in trajectory.
   c. If TASK_COMPLETE sentinel detected: break (success).
   d. Send the command to the tmux session and capture output.
   e. Check loop detector (inherited). If looping: raise `IcarusLoopDetected`.
   f. Format observation and append as next user message.
   g. Check wallclock timeout. If exceeded: raise `IcarusWallclockExceeded`.
4. If step_budget exhausted: raise `IcarusStepBudgetExceeded`.
5. In all cases (success or loud-fail): persist trajectory + write summary + populate context metadata.

Raises: `IcarusParseFailure`, `IcarusLoudFail` (and subclasses). All failures surface as exceptions with `block_reason` in `context.metadata`.

**BaseAgent compliance:** `run()` is abstract in `BaseAgent`. `IcarusHarborAgent` overrides it to reroute execution through tmux and LLM through LiteLLM.

#### Teardown

No explicit `teardown()` or `stop()`. tmux session cleanup is the container's responsibility — when Harbor stops the Docker container, the tmux session dies with it. If explicit cleanup is needed, add it as a `finally:` block in `run()`.

### 7.4 LLM Routing

```python
async def _call_llm(self, messages: list[dict[str, str]]) -> str:
    """Route LLM calls to the appropriate backend.

    - If self._use_litellm is True: call litellm.acompletion()
      with model=self._litellm_model and the full message history.
    - If self._use_litellm is False: call ollama_chat() (inherited
      static helper) with model=self.model_name.
    """
    ...

# Routing logic (constructor):
if model_name and model_name.startswith("litellm:"):
    self._use_litellm = True
    self._litellm_model = litellm_model or model_name.removeprefix("litellm:")
elif litellm_model:
    self._use_litellm = True
    self._litellm_model = litellm_model
else:
    self._use_litellm = False
    # Fall back to Ollama (IcarusAgent behavior)
```

### 7.5 tmux Session Management

**Session creation** (in `setup()`):
```python
async def _create_tmux_session(self, environment: BaseEnvironment) -> None:
    """Create a tmux session inside the Harbor container.

    Commands executed via environment.exec():
    1. tmux new-session -d -s <tmux_session_name>  (-d: detached)
    2. tmux set-option -t <tmux_session_name> remain-on-exit on

    Idempotent: if a session with the same name already exists,
    kills it first (tmux kill-session -t <name>).

    Raises IcarusHarborSetupError if tmux is unavailable or creation fails.
    """
    ...
```

**Command execution** (in `run()`):
```python
async def _exec_in_tmux(self, cmd: str, environment: BaseEnvironment) -> ExecResult:
    """Send a command to the tmux session and capture its output.

    1. Send command: environment.exec(f"tmux send-keys -t {session} '{cmd}' Enter")
    2. Wait for output: poll with ICARUS_RC:$? extraction
    3. Capture pane: environment.exec(f"tmux capture-pane -t {session} -p -S -200")
    4. Parse captured output to extract command output

    Returns ExecResult with stdout (captured output), stderr (none from tmux capture),
    and return_code (extracted from ICARUS_RC:$?).

    Raises IcarusLoudFail if tmux session is dead or capture fails.
    """
    ...
```

### 7.6 Context Metadata Contract

```python
context.metadata = (context.metadata or {}) | {
    "icarus_agent_variant": "harbor-tmux",  # distinguishes from IcarusAgent
    "icarus_block_reason": self._block_reason,
    "icarus_steps_used": steps_used,
    "icarus_model_used": self._litellm_model if self._use_litellm else self.model_name,
    "icarus_llm_backend": "litellm" if self._use_litellm else "ollama",
    "icarus_tmux_session": self._tmux_session_name,
}
```

Written regardless of whether the trial succeeds or loud-fails (ensured by the `finally:` block in `run()`).

### 7.7 BaseAgent Compliance

| BaseAgent requirement | Compliance |
|---|---|
| `name()` — static, returns str | Inherited from IcarusAgent: returns `"icarus"` |
| `version()` — instance, returns str or None | Overridden: returns `"2.0.0"` |
| `import_path()` — classmethod, returns str | Inherited: `swarm.icarus_harness.agent:IcarusHarborAgent` |
| `setup(environment)` — async, abstract | **Overridden**: creates tmux session + verifies LLM |
| `run(instruction, environment, context)` — async, abstract | **Overridden**: tmux-based loop + LiteLLM routing |
| `SUPPORTS_ATIF` — class var, bool | `False` (no ATIF trajectory format) |
| `SUPPORTS_WINDOWS` — class var, bool | `False` (tmux is Linux-only) |
| `logs_dir`, `model_name`, `logger` — set in `__init__` | Inherited; passed through to IcarusAgent → BaseAgent |
| `populate_context_post_run()` — optional | Not overridden; no-op inherited from BaseAgent |
| `to_agent_info()` — instance method | Inherited from BaseAgent |

### 7.8 Module-Level Constants

```python
DEFAULT_LITELLM_MODEL = os.environ.get("ICARUS_LITELLM_MODEL", "qwen3-coder:30b-32k")
DEFAULT_TMUX_SESSION_NAME = "icarus-harbor"
DEFAULT_TMUX_CAPTURE_LINES = 200  # Lines of tmux history to capture
DEFAULT_LLM_TIMEOUT_SEC = 180
DEFAULT_STEP_BUDGET = 30
DEFAULT_STEP_TIMEOUT_SEC = 60
DEFAULT_WALLCLOCK_SEC = 900

TASK_COMPLETE_SENTINEL = "TASK_COMPLETE"  # Inherited from IcarusAgent
```

---

## 8. Thin Baseline Configuration

### 8.1 LiteLLM Routing

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `model_name` | `ollama/qwen3-coder:30b-32k` | Operator-ratified model from ic-001 spec. The `ollama/` prefix is LiteLLM's provider routing tag. |
| `temperature` | `0.0` | Maximum determinism for reproducible baseline traces. Same as existing `ollama_chat()` default. |
| `seed` | `0` | Fixed seed for ollama's deterministic sampling. Matches existing IcarusAgent. |
| `top_p` | `1.0` | No nucleus sampling truncation at temperature 0. |
| `num_predict` | `4096` | Max output tokens per turn. 30B model's context window is 32K; 4K output leaves ~28K for prompt + history. |
| `stream` | `False` | Non-streaming. The agent parses the full response before extracting the bash block. |
| `request_timeout` | `180s` | Same as `DEFAULT_LLM_TIMEOUT_SEC` in existing IcarusAgent. |
| `max_retries` | `0` | No silent retries. If ollama fails, the agent loud-fails with `block_reason=llm_error`. |
| `fallback_model` | `None` | Single model, single provider. No fallbacks. |
| `context_window_truncation` | `sliding_window, oldest_first` | When message history exceeds the model's effective context (28K tokens after reserving 4K for output), drop the oldest assistant+user exchange pair. Never drop the system prompt or the initial user instruction. |
| `context_window_budget` | `28672` tokens | 32K context − 4K output reserve − 512 token safety margin. |

### 8.2 Docker BaseEnvironment

| Parameter | Value |
|-----------|-------|
| Image | Harbor's default `terminal-bench:2.0` image |
| tmux required | Yes — container must have `tmux` in `$PATH` |
| Shell access (bash) | Yes |
| GPU | No — LLM runs on host (ollama) |
| Writeable filesystem | Yes |
| Resource limits | None (Harbor defaults) |

Container lifecycle: Harbor creates, agent uses, Harbor destroys. The adapter does not manage container lifecycle.

### 8.3 Tmux Multi-Turn Shell Loop

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `step_budget` | `30` | Matches `DEFAULT_STEP_BUDGET` from ic-001. Overridable via env var `ICARUS_STEP_BUDGET`. |
| `step_timeout_sec` | `60` | Per-command wallclock timeout. |
| `wallclock_sec` | `900` | Whole-trial wallclock cap. Overridable via `ICARUS_WALLCLOCK_SEC`. |
| Shell persistence | tmux session | Primary experimental variable vs ic-001's fresh-exec model. |
| Output format | Raw command (no code fences) | Model output is sent directly as shell input. No parse_bash_block step. |
| TASK_COMPLETE detection | Scan tmux pane output | Sentinel detected in tmux capture, not in ExecResult. |
| Observation truncation | `4000` chars | Protects context window. |
| Exit code extraction | `echo "ICARUS_RC:$?"` polling | Replaces immediate `ExecResult.return_code` from ic-001. |
| Observation capture | `tmux capture-pane -p -S -200` | Last 200 lines of scrollback. |

**Why tmux:** The current IcarusAgent (ic-001) uses fresh `environment.exec()` calls per turn with no persistent shell state. The Harbor terminal-bench v2 spec supports a persistent tmux session inside the Docker container. tmux provides: (1) state persistence across turns, (2) task fidelity for multi-step tasks, (3) observation alignment with human solver experience, (4) failure trace fidelity.

---

## 9. Error Taxonomy

Extends the existing `IcarusLoudFail` hierarchy from `agent.py`:

```python
class IcarusHarborError(RuntimeError):
    """Base for Harbor-specific Icarus errors (setup phase)."""

class IcarusHarborSetupError(IcarusHarborError):
    """Setup-phase failure: tmux missing, LLM unreachable, etc.
    
    These are distinct from IcarusLoudFail (loop/parse/budget failures)
    because they occur before the agent loop starts. Harbor's trial
    runner should surface these as trial setup failures, not as agent
    execution failures.
    """
```

**Error handling hierarchy:**

| Error | Phase | Surface as | block_reason |
|---|---|---|---|
| `IcarusHarborSetupError` | setup | Trial setup failure | `"harbor_setup_error"` |
| `IcarusParseFailure` | run | Agent execution failure | `"parse_failure"` |
| `IcarusOllamaError` → `IcarusLLMError` | run | Agent execution failure | `"llm_error"` |
| `IcarusLoopDetected` | run | Agent execution failure | `"loop_detected"` |
| `IcarusStepBudgetExceeded` | run | Agent execution failure | `"step_budget_exceeded"` |
| `IcarusWallclockExceeded` | run | Agent execution failure | `"wallclock_exceeded"` |
| `IcarusTmuxError` (new) | run | Agent execution failure | `"tmux_error"` |

**Rule:** All exceptions are raised (never return codes). Harbor's trial runner catches `Exception` at the top level and records it. The `block_reason` string is captured in `context.metadata["icarus_block_reason"]` by the `_populate_context()` helper (inherited from `IcarusAgent`).

**Amendment (Clawta msg 5408):** The loud-fail taxonomy preserves IcarusAgent's existing block_reasons verbatim: `parse_failure`, `ollama_error` (renamed to `llm_error` in adapter context but mapped back to `ollama_error` in canonical taxonomy for watcher-layer consumption), `loop_detected`, `step_budget_exceeded`, `wallclock_exceeded`. The new `tmux_error` and `harbor_setup_error` block_reasons are additions, not replacements.

**Review note (Ares msg on PR #813):** `tmux_error` as a new block_reason is flagged for Clawta ratification per msg 5408 amendment, since the amendment requires that the taxonomy be preserved verbatim and additions need explicit sign-off.

---

## 10. System Prompt

The system prompt is inherited from `IcarusAgent.SYSTEM_PROMPT` with tmux-specific modifications:

```python
HARBOR_SYSTEM_PROMPT_APPENDIX = """
## tmux session

You are running inside a persistent tmux session named `{tmux_session}`.
Unlike a fresh subprocess, shell state **persists** across turns:
- Current working directory carries over.
- Environment variables (exported) carry over.
- Background processes (nohup, &) continue running.

This means you do NOT need to chain cd/export commands with &&.
Each turn's command runs in the same session where the previous
command left off.

To check the previous command's exit code: echo $?
"""
```

The `run()` method constructs the system message as:
`IcarusAgent.SYSTEM_PROMPT.format(...) + HARBOR_SYSTEM_PROMPT_APPENDIX.format(...) + environment_bootstrap`

---

## 11. Migration from ic-001 (current IcarusHarness)

| Aspect | ic-001 (current) | icarus-harbor (baseline) |
|--------|-------------------|--------------------------|
| LLM client | Hand-rolled `ollama_chat()` via `urllib.request` | `litellm.completion()` via LiteLLM |
| Shell model | Fresh `environment.exec()` per turn | Persistent tmux session |
| Command format | Fenced \`\`\`bash block extracted by regex | Raw text sent directly to tmux |
| Output capture | `ExecResult.stdout` + `ExecResult.stderr` | `tmux capture-pane` + exit code probe |
| Exit code | `ExecResult.return_code` (immediate) | `echo "ICARUS_RC:$?"` polling heuristic |
| Parse validation | `parse_bash_block()` — must find fenced block | No parse step — any non-empty model output is sent to tmux |
| OLLAMA_HOST env var | `DEFAULT_OLLAMA_HOST` | Same, routed through LiteLLM's `api_base` |

Config parameters (temperature, seed, step budget, wallclock, etc.) are deliberately **identical** to ic-001's defaults. The only changes are the LLM routing mechanism (LiteLLM vs hand-rolled) and the shell model (tmux vs fresh exec). This ensures the baseline comparison isolates the shell-persistence variable.

---

## 12. File-System Scope

- `swarm/icarus_harness/harbor_adapter.py` — TmuxEnvironment, LiteLLMChat, adapter wiring
- `swarm/icarus_harness/adapter_runner.py` — CLI entry point (`icarus-adapter-runner`)
- `swarm/tests/test_harbor_adapter.py` — unit tests for TmuxEnvironment, LiteLLMChat, adapter wiring
- `swarm/tests/test_harbor_adapter_e2e.py` — e2e test: one real tmux session through IcarusAgent
- `.specify/specs/038-icarus-harbor-agent-adapter/**` — this spec directory

---

## 13. Acceptance Criteria

- [ ] **AC-1:** `IcarusHarborAgent` runs on terminal-bench@2.0 via the icarus-adapter-runner — completes at least N trials with `qwen3-coder:30b-32k`, producing failure traces harvestable for future deterministic layer design.
  - *Review note (PR #813): AC#1 clarified from "through Harbor's runner" to "through the adapter runner" to match the spec defining the adapter as NOT using Harbor's Docker harness.*
- [ ] **AC-2:** No deterministic code paths execute during baseline runs. Every agent action goes through the LLM. (INV-038-HA-1)
- [ ] **AC-3:** Loud-fail taxonomy preserved: `parse_failure`, `llm_error` (mapped to `ollama_error` in canonical taxonomy for watcher-layer), `loop_detected`, `step_budget_exceeded`, `wallclock_exceeded` — plus new `tmux_error` and `harbor_setup_error` additions. All mapped to `block_reason` strings per §9.
  - *Review note (PR #813): ollama_error → llm_error rename preserves the canonical taxonomy for watcher-layer consumption via explicit mapping.*
- [ ] **AC-4:** `TmuxEnvironment.exec()` sends a command to a tmux session and returns `ExecResult` with captured stdout/stderr/return_code.
- [ ] **AC-5:** Adapter runs IcarusAgent's full loop (system prompt → LLM → send to tmux → observe → loop detect → loud-fail or TASK_COMPLETE) via LiteLLM + tmux, producing the same JSONL trajectory and summary artifacts as the Harbor path.
- [ ] **AC-6:** Trajectory JSONL recorded to `logs_dir/icarus-trajectory.jsonl` for future failure-cluster mining.
- [ ] **AC-7:** Adapter runner CLI exits with 0 on TASK_COMPLETE, non-zero on loud-fail (with structured block_reason in exit metadata).
- [ ] **AC-8:** Turn budget enforced: `step_budget=30` steps, `step_timeout_sec=60s` per command, `wallclock_sec=900s` whole-trial cap.
- [ ] **AC-9:** Context metadata includes `icarus_agent_variant=harbor-tmux`, `icarus_block_reason`, `icarus_steps_used`, `icarus_model_used`, `icarus_llm_backend`, `icarus_tmux_session` per §7.6.

---

## 14. Invariants

1. **INV-038-HA-1:** No speculative deterministic router. Every turn goes through the LLM. (See §5.)
2. **INV-038-HA-2:** Loud-fail taxonomy preserved verbatim from IcarusAgent. New additions (`tmux_error`, `harbor_setup_error`) require explicit Clawta sign-off per msg 5408 amendment.
3. **INV-038-HA-3:** Temperature=0, seed=0. Two runs of the same task with identical bootstrap produce comparable (not byte-identical) results.
4. **INV-038-HA-4:** Step budget, wallclock, and loop detection are safety rails, not deterministic layers. They stop the agent from burning infinite compute; they do not make agent decisions.

---

## 15. Test Surfaces

### Unit tests

- [ ] `IcarusHarborAgent.__init__()`: constructor validates model_name prefix routing (litellm: → LiteLLM path, else → Ollama fallback)
- [ ] `IcarusHarborAgent.__init__()`: constructor rejects invalid tmux session names
- [ ] `TmuxEnvironment.exec()`: sends command and returns `ExecResult` with stdout/stderr/return_code
- [ ] `LiteLLMChat.complete()`: routes to `litellm.acompletion()` with correct model string and parameters
- [ ] `_call_llm()`: LiteLLM path raises `IcarusLoudFail("llm_error")` on completion failure
- [ ] `_call_llm()`: Ollama fallback path calls `ollama_chat()` and raises `IcarusOllamaError` on failure
- [ ] `_create_tmux_session()`: creates session and raises `IcarusHarborSetupError` on failure

### Integration tests

- [ ] Full agent loop (mock LLM + mock tmux): 3-turn conversation completes with TASK_COMPLETE
- [ ] Full agent loop (mock LLM + mock tmux): loud-fail on step_budget_exceeded after 30 turns
- [ ] Full agent loop (mock LLM + mock tmux): loud-fail on loop_detected after 3 identical commands
- [ ] Full agent loop (mock LLM + mock tmux): loud-fail on IcarusLLMError when LLM call fails

### Smoke tests

- [ ] Real tmux session creation and teardown (outside Docker)
- [ ] Real LLM call through LiteLLM to local ollama (if ollama is available)
- [ ] Regression check: no deterministic layer code paths are reachable during baseline runs (grep/lint for rule-based command selection patterns)

---

## 16. Spec-Number Namespace Collision Note

This spec lives at `.specify/specs/038-icarus-harbor-agent-adapter/` (number 038). The ticket title retains "036" for paper-trail continuity with the original ticket, but the canonical spec number is 038 because:

- `.specify/specs/036-dispatch-fault-tolerance-invariants/` already exists on origin/main
- `.specify/specs/036-ic-001-icarus-local-llm-driver/` already exists on origin/main
- `.specify/specs/037-*` is taken
- The spec-kit entry must use the next available number: 038

The spec-kit directory was already created on branch `agent/036-icarus-harbor-agent-adapter-spec` (using the old number in the branch name). When this spec is merged, the directory must be renamed to `038-icarus-harbor-agent-adapter` to match the canonical number.

---

## 17. References

- **BaseAgent interface:** `harbor.agents.base.BaseAgent` — abstract base class with `setup()`, `run()`, `name()`, `version()`, `import_path()`, `to_agent_info()`.
- **IcarusAgent (ic-001):** `swarm/icarus_harness/agent.py:IcarusAgent` — existing Ollama/bash-subprocess agent. IcarusHarborAgent extends this.
- **AgentContext / ExecResult types:** Harbor's typed context and execution result interfaces, used by the adapter for metadata and command results.
- **LoopDetector:** Module-level class in `swarm/icarus_harness/agent.py` — reused as-is for loop detection in the tmux execution model.
- **parse_bash_block() / is_task_complete():** Module-level functions in `swarm/icarus_harness/agent.py` — `parse_bash_block` is NOT used by IcarusHarborAgent (tmux model sends raw commands, no fenced blocks), but `is_task_complete()` is reused for TASK_COMPLETE detection in tmux pane output.

---

## 18. Sign-off Log

- [ ] red — author, this draft
- [ ] Ares — must ratify if the adapter changes IcarusAgent's inner loop contract
- [ ] Clawta — must ratify per msg 5408 amendment (loud-fail taxonomy preservation, `tmux_error` addition)
- [ ] **operator** — final ratification gate