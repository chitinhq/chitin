"""Chitin Bench — deterministic bash-only Harbor agent.

Cleanroomed from mini-swe-agent (≈100 LOC core), with three Chitin Bench-
distinctive additions:

1. **Environment bootstrap** — turn 0 injects available interpreters
   (python3, node, go, ...) into the system message, saving the 2-5
   exploration turns top-quartile harnesses save (per "70% lives
   outside the model" pattern survey).
2. **Loop detection** — three consecutive identical commands, or three
   consecutive non-zero exits with no filesystem progress, trigger a
   loud-fail with ``block_reason=loop_detected`` rather than burning
   the remaining step budget.
3. **Loud-fail on parse/ollama failure** — no silent retries; the
   trial terminates with a structured reason that the bench-ticket
   emitter can promote to a kanban ticket.

The harness is bash-only via Harbor's ``environment.exec`` (fresh
exec per turn, no persistent tmux). Output protocol is a single
fenced ```bash`` block plus a ``TASK_COMPLETE`` sentinel. Linear
message history; trajectory persisted as JSONL to ``logs_dir``.
"""

from __future__ import annotations

import asyncio
import json
import os
import re
import shlex
import shutil
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib import error as urllib_error
from urllib import request as urllib_request

from harbor.agents.base import BaseAgent
from harbor.environments.base import BaseEnvironment, ExecResult
from harbor.models.agent.context import AgentContext

# ── Config ─────────────────────────────────────────────────────────

DEFAULT_OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://127.0.0.1:11434")
DEFAULT_MODEL = os.environ.get("CHITIN_BENCH_MODEL", "ollama/qwen3-coder:30b-32k")
DEFAULT_STEP_BUDGET = int(os.environ.get("CHITIN_BENCH_STEP_BUDGET", "30"))
DEFAULT_STEP_TIMEOUT_SEC = int(os.environ.get("CHITIN_BENCH_STEP_TIMEOUT_SEC", "60"))
DEFAULT_WALLCLOCK_SEC = int(os.environ.get("CHITIN_BENCH_WALLCLOCK_SEC", "900"))
DEFAULT_LLM_TIMEOUT_SEC = int(os.environ.get("CHITIN_BENCH_LLM_TIMEOUT_SEC", "180"))
STDOUT_CAPTURE_LIMIT = 3000
STDERR_CAPTURE_LIMIT = 1000

TASK_COMPLETE_SENTINEL = "TASK_COMPLETE"

BASH_BLOCK_RE = re.compile(r"```(?:bash|sh)?\s*\n(.*?)\n```", re.DOTALL)


# ── Data model ─────────────────────────────────────────────────────


@dataclass
class TurnRecord:
    step: int
    role: str  # "system" | "user" | "assistant" | "exec"
    content: str
    metadata: dict[str, Any] = field(default_factory=dict)


# ── Exceptions (loud-fail taxonomy) ────────────────────────────────


class BenchLoudFail(RuntimeError):
    """Base class for harness loud-fails. Carries a structured
    ``block_reason`` consumed by the bench-ticket emitter."""

    block_reason: str = "unknown"


class BenchParseFailure(BenchLoudFail):
    block_reason = "parse_failure"


class BenchOllamaError(BenchLoudFail):
    block_reason = "ollama_error"


class BenchLoopDetected(BenchLoudFail):
    block_reason = "loop_detected"


class BenchStepBudgetExceeded(BenchLoudFail):
    block_reason = "step_budget_exceeded"


class BenchWallclockExceeded(BenchLoudFail):
    block_reason = "wallclock_exceeded"


# ── Ollama client (stdlib urllib, no litellm) ──────────────────────


def _strip_provider_prefix(model: str) -> str:
    """Strip ``ollama/`` prefix if present so the API gets the raw
    model tag. Idempotent."""
    if model.startswith("ollama/"):
        return model[len("ollama/"):]
    return model


def ollama_chat(
    model: str,
    messages: list[dict[str, str]],
    *,
    host: str = DEFAULT_OLLAMA_HOST,
    timeout_s: int = DEFAULT_LLM_TIMEOUT_SEC,
    temperature: float = 0.0,
    seed: int = 0,
) -> str:
    """POST to ``/api/chat``. Returns the assistant response content.

    Determinism: ``temperature=0`` and ``seed=0`` are the defaults.
    Caller can override per-call but the bench loop holds them fixed.

    Raises :class:`BenchOllamaError` on any network / HTTP / decode
    failure (loud-fail; the trial aborts).
    """
    payload = {
        "model": _strip_provider_prefix(model),
        "messages": messages,
        "stream": False,
        "options": {"temperature": temperature, "seed": seed},
    }
    body = json.dumps(payload).encode()
    req = urllib_request.Request(
        f"{host.rstrip('/')}/api/chat",
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib_request.urlopen(req, timeout=timeout_s) as r:
            data = json.loads(r.read())
    except (urllib_error.URLError, urllib_error.HTTPError, OSError) as exc:
        raise BenchOllamaError(f"ollama HTTP failed: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise BenchOllamaError(f"ollama returned non-JSON: {exc}") from exc

    msg = data.get("message") or {}
    content = msg.get("content")
    if not isinstance(content, str):
        raise BenchOllamaError(
            f"ollama response missing assistant content: keys={list(data.keys())}"
        )
    return content


# ── Output parser ──────────────────────────────────────────────────


def parse_bash_block(response: str) -> str | None:
    """Return the FIRST fenced bash block from the assistant response.

    Accepts ```bash / ```sh / ``` (untagged). Returns ``None`` when no
    block is present (caller raises :class:`BenchParseFailure` if a
    block was required).
    """
    m = BASH_BLOCK_RE.search(response)
    if not m:
        return None
    return m.group(1).strip()


def is_task_complete(response: str) -> bool:
    """True iff the response contains the ``TASK_COMPLETE`` sentinel
    on a line by itself or as a standalone token."""
    for line in response.splitlines():
        stripped = line.strip()
        if stripped == TASK_COMPLETE_SENTINEL:
            return True
    return False


def wrap_command_with_timeout(command: str, timeout_s: int) -> str:
    """Execute ``command`` under GNU ``timeout`` inside the container.

    Harness-side ``asyncio.wait_for`` only times out the await; it does
    not guarantee the in-container subprocess is reaped. Wrapping the
    command ensures package managers and similar long-running tools do
    not leak into later turns and consume the rest of the step budget.
    """
    return (
        f"timeout --kill-after=5s {timeout_s}s "
        f"bash -lc {shlex.quote(command)}"
    )


# ── Loop detector ──────────────────────────────────────────────────


class LoopDetector:
    """Three-strike loop detector.

    Trips when:
    - The same command appears in three consecutive turns, OR
    - Three consecutive turns return non-zero exit AND no stdout/stderr
      progress (same captured output).

    Reset on any successful exec or any new command.
    """

    def __init__(self) -> None:
        self._cmds: list[str] = []
        self._results: list[tuple[int, str]] = []

    def record(self, cmd: str, result: ExecResult) -> None:
        self._cmds.append(cmd)
        self._results.append((result.return_code, (result.stdout or "") + (result.stderr or "")))
        # Keep only the last 3 — anything older is irrelevant.
        if len(self._cmds) > 3:
            self._cmds.pop(0)
            self._results.pop(0)

    def is_looping(self) -> bool:
        if len(self._cmds) < 3:
            return False
        if all(c == self._cmds[0] for c in self._cmds):
            return True
        if (
            all(rc != 0 for rc, _ in self._results)
            and all(out == self._results[0][1] for _, out in self._results)
        ):
            return True
        return False


# ── Environment bootstrap ──────────────────────────────────────────


BOOTSTRAP_PROBE_CMDS = [
    "uname -a",
    "pwd",
    "ls -la",
    "command -v python3 python perl pip pip3 node npm pnpm go cargo rustc make gcc g++ git curl wget jq sqlite3 ruff black eslint pytest 2>/dev/null | head -50",
]


async def gather_environment_bootstrap(environment: BaseEnvironment) -> str:
    """Probe the container for OS, cwd, top-level files, and available
    tool binaries. Returns a single ``str`` block injected into the
    system prompt so the model doesn't burn 2-5 turns discovering basics.
    """
    sections: list[str] = []
    for cmd in BOOTSTRAP_PROBE_CMDS:
        try:
            result = await asyncio.wait_for(environment.exec(cmd), timeout=15)
        except (asyncio.TimeoutError, Exception):  # noqa: BLE001 — best effort
            continue
        out = (result.stdout or "").strip()
        if not out:
            continue
        sections.append(f"$ {cmd}\n{out[:1500]}")
    if not sections:
        return ""
    return "## Environment\n\n" + "\n\n".join(sections)


# ── System prompt ──────────────────────────────────────────────────


SYSTEM_PROMPT = """\
You are Chitin Bench, a deterministic terminal agent solving a benchmark task.

You operate inside a Linux container. You issue ONE bash command per turn
inside a single fenced code block and read the output before deciding
the next step. The harness executes your block via a fresh subprocess —
shell state (cwd, exports, jobs) does NOT persist across turns. If you
need persistence, either (a) chain commands with `&&` in one block, or
(b) use `nohup ... &` and read its log file in a later turn.

## Tool surface
You have exactly one tool: bash. Output it like this:

```bash
your command here
```

The harness ignores any text outside the fenced block on each turn
(treat it as private scratch). Exactly one block per turn.

## Task completion
When you have verified the task is done, output the literal sentinel
`TASK_COMPLETE` on its own line. The harness terminates the trial
when it sees this sentinel.

## Hard rules
- ONE fenced bash block per turn. If you emit multiple blocks the
  harness uses only the first; if you emit none it loud-fails.
- Cite a file path before reading or modifying it; the model that
  guesses paths burns budget.
- Do not assume internet access. Some tasks have it disabled.
- If a command returns non-zero, read the error, decide what to do
  next, and try a different command. Do NOT repeat the same command
  three times — the harness detects loops and aborts.
- Per-command timeout: {step_timeout}s. Whole-trial wallclock cap:
  {wallclock}s. Step budget: {step_budget} turns.

## Determinism
Outputs are reproducible: temperature=0, seed=0. Two runs of the same
task with the same trajectory file are byte-equal.
"""


# ── Main agent ─────────────────────────────────────────────────────


class BenchAgent(BaseAgent):
    """Chitin Bench — single-loop, bash-only, deterministic Harbor agent.

    Invocation:
        harbor run \\
            --agent-import-path swarm.chitin_bench.agent:BenchAgent \\
            --model ollama/qwen3-coder:30b-32k \\
            --path <task-dir>
    """

    SUPPORTS_ATIF = False
    SUPPORTS_WINDOWS = False

    @staticmethod
    def name() -> str:
        # Not in Harbor's enum; this name is only used when the agent
        # is invoked via --agent-import-path. Harbor records it in
        # ``AgentInfo.name`` for the trial result.
        return "chitin-bench"

    def version(self) -> str:
        return "1.0.0"

    def __init__(
        self,
        logs_dir: Path,
        model_name: str | None = None,
        *args,
        step_budget: int = DEFAULT_STEP_BUDGET,
        step_timeout_sec: int = DEFAULT_STEP_TIMEOUT_SEC,
        wallclock_sec: int = DEFAULT_WALLCLOCK_SEC,
        ollama_host: str = DEFAULT_OLLAMA_HOST,
        **kwargs,
    ) -> None:
        super().__init__(logs_dir=logs_dir, model_name=model_name, *args, **kwargs)
        self._step_budget = step_budget
        self._step_timeout_sec = step_timeout_sec
        self._wallclock_sec = wallclock_sec
        self._ollama_host = ollama_host
        self._trajectory: list[TurnRecord] = []
        self._block_reason: str | None = None

    async def setup(self, environment: BaseEnvironment) -> None:
        """No-op for v1 — the harness needs no in-container install."""
        return None

    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        """Drive the bash-loop until ``TASK_COMPLETE``, step budget,
        wallclock, parse failure, ollama failure, or loop detection.
        Trajectory is written to ``logs_dir / chitin-bench-trajectory.jsonl``
        whether the trial succeeds or loud-fails."""
        model = self.model_name or DEFAULT_MODEL
        start = time.monotonic()

        # Turn 0: probe environment, build system prompt.
        bootstrap = await gather_environment_bootstrap(environment)
        sys_msg = (
            SYSTEM_PROMPT.format(
                step_timeout=self._step_timeout_sec,
                wallclock=self._wallclock_sec,
                step_budget=self._step_budget,
            )
            + ("\n\n" + bootstrap if bootstrap else "")
        )
        messages: list[dict[str, str]] = [
            {"role": "system", "content": sys_msg},
            {"role": "user", "content": instruction},
        ]
        self._record(0, "system", sys_msg)
        self._record(0, "user", instruction)

        loop_detector = LoopDetector()
        step = 0
        try:
            while True:
                if step >= self._step_budget:
                    raise BenchStepBudgetExceeded(
                        f"step budget {self._step_budget} exhausted"
                    )
                if time.monotonic() - start > self._wallclock_sec:
                    raise BenchWallclockExceeded(
                        f"wallclock {self._wallclock_sec}s exceeded"
                    )

                step += 1
                self.logger.info(f"[chitin-bench] step {step}/{self._step_budget}")

                # LLM call.
                response = ollama_chat(
                    model, messages, host=self._ollama_host
                )
                self._record(step, "assistant", response)
                messages.append({"role": "assistant", "content": response})

                if is_task_complete(response):
                    self.logger.info(f"[chitin-bench] TASK_COMPLETE at step {step}")
                    break

                cmd = parse_bash_block(response)
                if cmd is None:
                    raise BenchParseFailure(
                        f"step {step}: no fenced bash block in assistant response "
                        f"(len={len(response)})"
                    )

                # Exec inside the container. Timeout is enforced in the
                # container process itself so long-running commands do
                # not survive into later turns and cause lock thrash.
                wrapped_cmd = wrap_command_with_timeout(cmd, self._step_timeout_sec)
                try:
                    result = await asyncio.wait_for(
                        environment.exec(wrapped_cmd),
                        timeout=self._step_timeout_sec + 10,
                    )
                except asyncio.TimeoutError:
                    result = ExecResult(
                        stdout=None,
                        stderr=f"[chitin-bench] step timeout after "
                        f"{self._step_timeout_sec}s",
                        return_code=124,
                    )

                loop_detector.record(cmd, result)
                if loop_detector.is_looping():
                    raise BenchLoopDetected(
                        f"step {step}: 3-strike loop on `{cmd[:80]}`"
                    )

                obs = self._format_observation(cmd, result)
                self._record(step, "exec", obs, metadata={
                    "cmd": cmd, "return_code": result.return_code,
                })
                messages.append({"role": "user", "content": obs})

        except BenchLoudFail as exc:
            self._block_reason = exc.block_reason
            self._record(step, "exit", f"loud_fail: {exc.block_reason}: {exc}")
            self.logger.warning(
                f"[chitin-bench] loud-fail at step {step}: "
                f"{exc.block_reason}: {exc}"
            )
        finally:
            self._persist_trajectory()
            self._populate_context(context, step)

    # ── helpers ────────────────────────────────────────────────────

    @staticmethod
    def _truncate_stream(
        text: str | None,
        *,
        limit: int,
        label: str,
        tail: int,
    ) -> str:
        """Preserve both the beginning and end of long output.

        A head-only truncation makes broad reads like ``cat`` look
        incomplete, which can push the model into rereading the same
        file and tripping loop detection. Keeping the tail visible
        preserves end-of-file assertions and stack traces.
        """
        if not text:
            return ""
        if len(text) <= limit:
            return text
        head = max(limit - tail, 0)
        omitted = len(text) - head - tail
        return (
            f"{text[:head]}\n"
            f"[{label} truncated: omitted {omitted} chars; showing head+tail, "
            "re-run a narrower read for the middle]\n"
            f"{text[-tail:]}"
        )

    @staticmethod
    def _format_observation(cmd: str, result: ExecResult) -> str:
        """Format the exec result as the next user message.

        Long output preserves both head and tail so end-of-file asserts
        and tracebacks do not disappear behind a silent prefix cut."""
        head = f"$ {cmd}"
        rc = f"[return_code={result.return_code}]"
        full_out = result.stdout or ""
        full_err = result.stderr or ""
        out = BenchAgent._truncate_stream(
            full_out,
            limit=STDOUT_CAPTURE_LIMIT,
            label="stdout",
            tail=700,
        )
        err = BenchAgent._truncate_stream(
            full_err,
            limit=STDERR_CAPTURE_LIMIT,
            label="stderr",
            tail=300,
        )
        parts = [head, rc]
        if out:
            parts.append("STDOUT:\n" + out)
        if err:
            parts.append("STDERR:\n" + err)
        return "\n".join(parts)

    def _record(
        self,
        step: int,
        role: str,
        content: str,
        *,
        metadata: dict[str, Any] | None = None,
    ) -> None:
        self._trajectory.append(
            TurnRecord(
                step=step, role=role, content=content,
                metadata=metadata or {},
            )
        )

    def _persist_trajectory(self) -> None:
        """Write the trajectory as JSONL to ``logs_dir``. Also write a
        summary JSON for the bench-ticket emitter."""
        traj_path = self.logs_dir / "chitin-bench-trajectory.jsonl"
        self.logs_dir.mkdir(parents=True, exist_ok=True)
        with traj_path.open("w") as fh:
            for r in self._trajectory:
                fh.write(json.dumps({
                    "step": r.step, "role": r.role,
                    "content": r.content, "metadata": r.metadata,
                }) + "\n")

        summary_path = self.logs_dir / "chitin-bench-summary.json"
        summary_path.write_text(json.dumps({
            "agent": self.name(),
            "version": self.version(),
            "model": self.model_name,
            "block_reason": self._block_reason,
            "steps": max((r.step for r in self._trajectory), default=0),
        }, indent=2))

    def _populate_context(self, context: AgentContext, steps_used: int) -> None:
        context.metadata = (context.metadata or {}) | {
            "chitin_bench_block_reason": self._block_reason,
            "chitin_bench_steps_used": steps_used,
        }
