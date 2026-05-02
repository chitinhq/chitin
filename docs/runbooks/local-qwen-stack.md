# Local Qwen Stack Runbook

Canonical reference for configuring and validating the local-qwen driver on the RTX 3090 box.

## Stack versions (as of 2026-05-02)

| Component | Required version |
|-----------|-----------------|
| ollama | 0.22.x |
| qwen3-coder | latest pulled via `ollama pull` |

Check current version:

```bash
ollama --version
```

## 1. Upgrade ollama to 0.22.x

```bash
curl -fsSL https://ollama.com/install.sh | sh
```

Verify:

```bash
ollama --version   # should print 0.22.x
```

Restart the service after upgrade:

```bash
sudo systemctl restart ollama
```

## 2. Set KV cache type (halves VRAM usage)

Edit `/etc/systemd/system/ollama.service` and add under the `[Service]` section:

```ini
Environment="OLLAMA_KV_CACHE_TYPE=q8_0"
```

Full minimal service block for reference:

```ini
[Service]
ExecStart=/usr/local/bin/ollama serve
Environment="OLLAMA_KV_CACHE_TYPE=q8_0"
Restart=always
RestartSec=3
```

Reload and restart:

```bash
sudo systemctl daemon-reload
sudo systemctl restart ollama
sudo systemctl status ollama   # confirm Active: running
```

## 3. Set context window on qwen-agent

`num_ctx=262144` (default) spills to CPU when the KV cache overflows 24 GB VRAM. Cap it at 32768 to keep all inference on-GPU.

Locate the chitin agent config for local-qwen (typically `config/agents/local-qwen.yaml` or equivalent) and ensure:

```yaml
model_options:
  num_ctx: 32768
```

Do **not** set this in `chitin.yaml` — that file is human-gated. Set it in the agent's own config layer.

## 4. Smoke-test

Force one T0 entry through local-qwen to confirm the stack is healthy. Create a minimal backlog entry with the driver override:

```yaml
id: qwen-smoke-test
tier: T0
allowed_drivers: ['local-qwen']
description: |
  Echo "ok" to a temp file and commit it. Verifies local-qwen end-to-end.
```

Expected result: `exit_code=0`, agent produces a real diff, no CPU-offload warnings in `journalctl -u ollama`.

Check ollama logs during the run:

```bash
journalctl -u ollama -f
```

Look for lines like `llm_load_tensors: offloaded 0/N layers to CPU` — any offload means num_ctx is still too high or another process is holding VRAM.

## 5. Re-enable T0 routing to local-qwen

After smoke-test passes, flip `TIER_DRIVER[T0]` in the dispatcher config from `copilot` back to `local-qwen`. That change is a one-liner and is tracked as a separate backlog entry.

## Diagnostics

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `exit_code=1`, timeout | num_ctx too large → CPU spill | Confirm `num_ctx=32768` in agent config |
| OOM / ollama crash | KV cache not q8_0 | Check `OLLAMA_KV_CACHE_TYPE` in service env |
| 0 dispatches to local-qwen | TIER_DRIVER still points to copilot | Flip T0 routing after smoke-test |
| ollama 0.21.0 still installed | Upgrade script didn't run | Re-run `curl -fsSL https://ollama.com/install.sh | sh` |

## Why this matters

The 3090 has 24 GB VRAM. At the default `num_ctx=262144`, the qwen3-coder KV cache alone exceeds that budget, causing silent CPU offload that turns a 2-minute task into a 20-minute timeout. `q8_0` KV quantization cuts cache size ~50%; `num_ctx=32768` caps the worst-case allocation. Together they keep all inference on-GPU and make local-qwen a reliable T0/T1 driver rather than a fallback that always times out.
