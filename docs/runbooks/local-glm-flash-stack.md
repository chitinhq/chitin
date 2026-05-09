# Local GLM-4.7-Flash Stack Runbook

> **Status: draft, pending operator validation on the 3090 rig.** This
> runbook captures the operator-side wiring needed to run the
> `local-glm-flash` driver through openclaw's `glm-flash-agent`
> gated by chitin. Reference companion to `local-qwen-stack.md`.

`local-glm-flash` is the cheapest local mechanical-tier driver
(free, on-the-3090, ~32K context, 19 GB on disk, currently hot in
GPU memory).

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

Heat the GPU once before first dispatch; cold-load on first
hit adds ~2-5 s latency on a 3090:

```bash
ollama run glm-4.7-flash:latest "ready" >/dev/null
ollama ps                            # confirm it's loaded on GPU
```

## 2. Configure the openclaw `glm-flash-agent`

The openclaw `glm-flash-agent` is configured in
`~/.openclaw/openclaw.json`. The agent's tool calls are gated through
the chitin-governance plugin's `before_tool_call` hook, which invokes
`chitin-kernel gate evaluate` for every tool call.

Example configuration:

```jsonc
{
  "agents": {
    "list": [
      {
        "id": "glm-flash-agent",
        "name": "glm-flash-agent",
        "workspace": "/home/red/.openclaw/workspaces/glm-flash-agent",
        "agentDir": "/home/red/.openclaw/agents/glm-flash-agent/agent",
        "model": "ollama/glm-4.7-flash:latest"
      }
    ]
  },
  "plugins": {
    "allow": ["chitin-governance"]
  }
}
```

## 3. Smoke test through the gate

With the agent configured, run a one-off task through the chitin gate
to verify end-to-end:

```bash
chitin-kernel gate evaluate \
  -agent openclaw \
  -tool Bash \
  -args-json '{"command": "echo hello-from-glm-flash"}' \
  -cwd /home/red/workspace/chitin
# Expect exit 0 (allowed) and JSON with allowed: true
```

Then run through openclaw directly:

```bash
openclaw chat --agent glm-flash-agent "Use the Bash tool to run exactly: echo hello-from-glm-flash. Then stop."
```

Watch the chitin gate event log for the decision:

```bash
cat ~/.chitin/gov-decisions-$(date +%Y-%m-%d).jsonl | tail -5
# Expect a decision row for the shell.exec action
```

## 4. Failure modes and recovery

| Symptom | Likely cause | Recovery |
|---|---|---|
| `ollama: model not found` in agent stderr | model not pulled | `ollama pull glm-4.7-flash:latest` |
| Agent times out | model cold-loading + bounds too tight | warm the model (`ollama run …`); bump wall timeout for the entry |
| Repeated tool-call denials in the chain | governance policy blocking the model's actions | inspect `~/.chitin/gov-decisions-*.jsonl`; add a rule to `chitin.yaml` if false-positive |
| Quality degradation | model under-fit for the task | consider escalating to a higher-tier model via hermes |
| GPU OOM | model loaded alongside another model | `ollama stop <other>`; or run a second 3090 |

## Reference

- Driver definition: `libs/contracts/src/execution-request.schema.ts` (`DriverIdSchema` enum)
- chitin-governance plugin: `apps/openclaw-plugin-governance/`
- chitin.yaml policy: `~/.chitin/chitin.yaml`