# Local GLM-4.7-Flash Stack Runbook

> **Status: draft, pending operator validation on the 3090 rig.** This
> runbook captures the operator-side wiring needed to make the new
> `local-glm-flash` driver (added 2026-05-03) actually dispatch work
> through `glm-4.7-flash:latest` when chitin's dispatcher routes T0
> there. Reference companion to `local-qwen-stack.md`.

`local-glm-flash` is the T0 default as of 2026-05-03 — the cheapest
local mechanical-tier driver (free, on-the-3090, ~32K context, 19 GB
on disk, currently hot in GPU memory). Replaces `copilot` at T0
unless the operator overrides via `CHITIN_TIER_DRIVER_T0`.

## Stack versions

| Component | Required | Verify with |
|---|---|---|
| ollama | ≥ 0.22.x | `ollama --version` |
| glm-4.7-flash | latest | `ollama list \| grep glm-4.7-flash` |
| openclaw | 2026.4.x | `openclaw --version` |

## 1. Pull the model

```bash
ollama pull glm-4.7-flash:latest
ollama list | grep glm-4.7-flash    # confirm 19-20 GB on disk
```

Heat the GPU once before chitin's first dispatch; cold-load on first
hit adds ~2-5 s latency on a 3090:

```bash
ollama run glm-4.7-flash:latest "ready" >/dev/null
ollama ps                            # confirm it's loaded on GPU
```

## 2. Configure the openclaw `glm-flash-agent`

The chitin activity (`apps/temporal-worker/src/activity.ts`) routes
`local-glm-flash` to an openclaw agent named `glm-flash-agent`.
Configure that agent in `~/.openclaw/openclaw.json`:

```jsonc
{
  "agents": {
    "glm-flash-agent": {
      "model": "ollama/glm-4.7-flash:latest",
      "max_tokens": 8192,
      "temperature": 0.2,
      "context_window": 32768,
      // Tools are governed via chitin-governance plugin's
      // before_tool_call; openclaw's allow-list is permissive here.
      "tools": ["shell.exec", "read_file", "write_file", "search_files"]
    }
    // ...other agents (qwen-agent, glm-agent, deepseek-agent, main)
    // unchanged.
  },
  "plugins": {
    "allow": ["chitin-governance"]
  }
}
```

Override the agent name with `CHITIN_AGENT_LOCAL_GLM_FLASH=my-agent`
if a different agent name is preferable.

## 3. Smoke test the dispatch

With the agent configured, dispatch a one-off T0 entry through the
worker:

```bash
WORKFLOW_ID="smoke-glm-flash-$(date +%s)" \
DRIVER=local-glm-flash \
PROMPT='Use shell.exec to run exactly: echo hello-from-glm-flash. Then stop.' \
MAX_TOOL_CALLS=3 \
WALL_TIMEOUT_S=60 \
pnpm exec tsx apps/temporal-worker/src/submit.ts
```

Watch the worker log; expect:
- `openclaw agent --local --agent glm-flash-agent ...` in the spawn line
- one `before_tool_call` event in the chitin gate event log
  (`docs/observations/governance-debt-ledger.md` or wherever your
   gov-decisions chain is)
- one tool call to `shell.exec` with `cmd: echo hello-from-glm-flash`
- exit code 0

## 4. Operator override — pulling T0 back to copilot

If the local model regresses (slow, errors, hallucination), revert
to copilot at T0 without a code change:

```bash
# In the dispatcher's environment (systemd unit drop-in or shell env):
export CHITIN_TIER_DRIVER_T0=copilot

systemctl --user restart chitin-dispatcher.timer    # or however the
                                                    # dispatcher is run
```

The dispatcher reads `CHITIN_TIER_DRIVER_T0` at tick time;
flipping back to `local-glm-flash` is the same env var unset.

## 5. Failure modes and recovery

| Symptom | Likely cause | Recovery |
|---|---|---|
| `ollama: model not found` in agent stderr | model not pulled | `ollama pull glm-4.7-flash:latest` |
| Agent times out at wall_timeout | model cold-loading + bounds too tight | warm the model (`ollama run …`); bump `wall_timeout_s` for that entry |
| Repeated tool-call denials in the chain | governance policy blocking the model's actions | inspect `gov-decisions-*.jsonl`; if false-positive, the policy needs a rule (file as backlog entry) |
| Quality degradation on tasks T0 used to handle on copilot | model under-fit | flip back via `CHITIN_TIER_DRIVER_T0=copilot`; file backlog entry to either tune the prompt (skill folder) or escalate to T1 |
| GPU OOM | model loaded alongside another model | `ollama stop <other>`; or run a second 3090, or give T0 a smaller budget so it doesn't keep two models hot |

## 6. Tier-router consultation (when it ships)

Once `tier-router-with-advisor-consultation` (filed in PR #208's
skill-folder cohort) lands, GLM-4.7-flash hitting a judgment call
will get a structured T2 (Sonnet) consultation rather than failing
silently or escalating the whole task. Until then: low-confidence
classification is just a deny-with-reason on the chain, and the
operator (or the dispatcher's tier ladder) re-dispatches at T1.

## Reference

- The driver definition: `libs/contracts/src/execution-request.schema.ts`
  (`DriverIdSchema` enum)
- Agent mapping: `apps/temporal-worker/src/activity.ts`
  (`DRIVER_AGENT_MAP`)
- Tier defaults + env override: `apps/temporal-worker/src/dispatcher.ts`
  (`TIER_DRIVER_DEFAULTS` + `envDriverOverride`)
