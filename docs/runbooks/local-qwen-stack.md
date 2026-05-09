# Local Qwen Stack Runbook

> **Status: draft, pending operator validation on the 3090 rig.** This
> runbook captures the remediations the 2026-05-01 instability
> investigation (PR #112) recommended, scoped to commands that match
> the toolchain actually shipped on this rig (ollama systemd unit,
> openclaw 2026.4.25 agents CLI, Modelfile-based parameter override).
> The "validate" half of the qwen-ollama-config-bump-and-validate
> entry is operator-action — running these on the 3090 and pasting
> the smoke-test journalctl output back into this doc.

Canonical reference for configuring and validating the local-qwen
driver on the RTX 3090 box.

## Stack versions

| Component | Required | Verify with |
|-----------|----------|-------------|
| ollama | ≥ 0.22.x | `ollama --version` |
| qwen3-coder | latest | `ollama list \| grep qwen3-coder` |
| openclaw | 2026.4.x | `openclaw --version` |

The current latest ollama tag can be confirmed against
`gh api repos/ollama/ollama/releases --jq '.[0].tag_name'` before
upgrading; as of 2026-05-02 it's `v0.22.1`.

## 1. Upgrade ollama

```bash
curl -fsSL https://ollama.com/install.sh | sh
ollama --version   # expect 0.22.x
sudo systemctl restart ollama
```

## 2. KV cache type via systemd drop-in

Don't edit `/etc/systemd/system/ollama.service` directly — that file
is owned by the ollama installer and gets clobbered on the next
upgrade. Use a drop-in override:

```bash
sudo mkdir -p /etc/systemd/system/ollama.service.d
sudo tee /etc/systemd/system/ollama.service.d/kv-cache.conf >/dev/null <<'EOF'
[Service]
Environment="OLLAMA_KV_CACHE_TYPE=q8_0"
EOF

sudo systemctl daemon-reload
sudo systemctl restart ollama

# Verify the env var is live in the running process:
systemctl show ollama -p Environment | grep -F OLLAMA_KV_CACHE_TYPE
```

The drop-in survives ollama upgrades; the upstream `Unit`/`Install`
sections (paths, dependencies, user, RuntimeDirectory, etc.) stay
intact.

## 3. Cap context window via Modelfile

`num_ctx=262144` (qwen3-coder default) overflows the 3090's 24 GB
VRAM and silently spills to CPU. Cap at 32768 by creating a custom
model variant — ollama's parameter system is per-model, not per-
agent, so this lives at the ollama layer.

```bash
cat > /tmp/qwen3-coder-32k.Modelfile <<'EOF'
FROM qwen3-coder:30b
PARAMETER num_ctx 32768
EOF

ollama create qwen3-coder:30b-32k -f /tmp/qwen3-coder-32k.Modelfile
ollama show qwen3-coder:30b-32k --parameters
# Expect a `num_ctx 32768` line in the output. The exact format varies
# slightly across ollama 0.22.x point releases — eyeball rather than
# grep so a render-format change doesn't make verification fail noisily
# while the model is actually fine.
```

Then point the openclaw `qwen-agent` at the new model id. The agent
config lives in `~/.openclaw/openclaw.json` (the canonical source —
there is no `config/agents/local-qwen.yaml` in this repo). Either
edit the `model` field directly:

```bash
# Inspect:
jq '.agents.list[] | select(.id=="qwen-agent")' ~/.openclaw/openclaw.json
```

```jsonc
// expected shape (post-edit):
{
  "id": "qwen-agent",
  "name": "qwen-agent",
  "workspace": "/home/red/.openclaw/workspaces/qwen-agent",
  "agentDir": "/home/red/.openclaw/agents/qwen-agent/agent",
  "model": "ollama/qwen3-coder:30b-32k"
}
```

Or via the CLI (delete + re-add — `agents update` doesn't expose a
`--model` flag in 2026.4.25):

```bash
openclaw agents delete qwen-agent
openclaw agents add qwen-agent \
  --workspace /home/red/.openclaw/workspaces/qwen-agent \
  --agent-dir /home/red/.openclaw/agents/qwen-agent/agent \
  --model ollama/qwen3-coder:30b-32k \
  --non-interactive
```

Direct edit is preferred — the delete/re-add path may drop bindings
and identity fields the CLI defaults differently.

**After either path, restart openclaw** (or any long-running consumer
of `~/.openclaw/openclaw.json`) so the agents.list cache reflects the
new model. The chitin worker spawns openclaw fresh per workflow, so
it will pick up the change on the next dispatch automatically; an
already-running interactive openclaw session won't.

```bash
# Verify the agent is now bound to the 32k model:
jq '.agents.list[] | select(.id=="qwen-agent") | .model' ~/.openclaw/openclaw.json
# Expect: "ollama/qwen3-coder:30b-32k"
```

## 4. Smoke-test (direct gate invocation)

To exercise local-qwen through the chitin gate, run:

```bash
chitin-kernel gate evaluate \
  -agent openclaw \
  -tool Bash \
  -args-json '{"command": "echo ok > /tmp/qwen-smoke.txt"}' \
  -cwd /home/red/workspace/chitin
# Expect exit 0 (allowed) and a JSON decision with allowed: true
```

Then run the actual command through the openclaw agent:

```bash
openclaw chat --agent qwen-agent "Use the Bash tool to run exactly: echo ok > /tmp/qwen-smoke.txt. Then stop."
```

While the workflow runs, watch ollama:

```bash
journalctl -u ollama -f
```

**Expected outcome:**

- `/tmp/qwen-smoke.txt` contains `ok`.
- `journalctl` shows model load + inference, **no** `offloaded N/M layers to CPU` lines.

If the smoke test fails, see Diagnostics below before iterating on
the config.

## 5. Verify routing in chitin.yaml

Once smoke-test passes, verify that `chitin.yaml` has appropriate rules for the openclaw agent's tool calls. The local-qwen stack works through the openclaw driver, which routes through `chitin-kernel gate evaluate`. No TIER_DRIVER or dispatcher flip is needed — chitin routes by agent identity, not by environment variable.

## Diagnostics

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `exit_code=1`, timeout | num_ctx still too large → CPU spill | Confirm Modelfile has `PARAMETER num_ctx 32768` and the `qwen-agent` config points at the `:30b-32k` variant |
| OOM / ollama crash | KV cache not q8_0 | `systemctl show ollama -p Environment` should include `OLLAMA_KV_CACHE_TYPE=q8_0`; if absent, drop-in didn't apply |
| 0 dispatches to local-qwen | chitin.yaml rules may be denying tool calls | Check `~/.chitin/gov-decisions-$(date +%Y-%m-%d).jsonl` for denials |
| ollama 0.21.0 still installed | Upgrade script didn't run / ollama wasn't restarted | `curl -fsSL https://ollama.com/install.sh \| sh` + `sudo systemctl restart ollama` |
| `qwen3-coder:30b-32k` not found | Modelfile create skipped | Re-run section 3 |

## Why this matters

The 3090 has 24 GB VRAM. At qwen3-coder's default `num_ctx=262144`
the KV cache alone exceeds that budget, causing silent CPU offload
that turns a 2-minute task into a 20-minute timeout. `q8_0` KV
quantization cuts cache size ~50%; `num_ctx=32768` caps the
worst-case allocation. Together they keep all inference on-GPU and
make local-qwen a reliable T0/T1 driver rather than a fallback that
always times out — which is what overnight 2026-05-02 telemetry
showed (0 dispatches to local-qwen, $0.50 routed to claude-code-
headless instead, while the 3090 sat idle).
