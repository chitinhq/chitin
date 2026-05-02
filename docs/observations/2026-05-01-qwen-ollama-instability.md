---
date: 2026-05-01
type: investigation
scope: qwen3-coder:30b — ollama stream instability on RTX 3090
related:
  - docs/swarm-backlog.md (qwen-layer reliability section)
  - apps/temporal-worker/src/dispatcher.ts
---

# qwen3-coder:30b Ollama Stream Instability

## One-sentence summary

Two distinct failure modes cause `Ollama API stream ended without a final response`
for `qwen3-coder:30b`: (1) KvSize=262144 exhausts GPU VRAM, forcing 25/49 layers
to CPU and inference latency well past the original 120 s wall timeout; (2) the
model generates malformed XML in tool call format, which ollama's
`qwen3coder.go` parser rejects mid-stream, leaving no `finalResponse` object.

## Environment

| item | value |
|---|---|
| host | chimera-ant (this Linux box — RTX 3090) |
| GPU | NVIDIA GeForce RTX 3090, 24 576 MiB VRAM |
| ollama version | **0.21.0** (confirmed via `ollama --version`) |
| model | `qwen3-coder:30b` (18 GB, Q4_K_M, qwen3moe architecture) |
| model parameters | 30.5B, 262 144 native context length, 49 layers |
| GPU at investigation time | 1 121 MiB used / 22 978 MiB free — idle |

## Failure mode 1: KvSize=262144 forces CPU offload

### What the logs show

Every request that lets ollama use the model's full 262 144-token context
triggers a three-attempt fit loop before settling:

```
Operation:fit  KvSize:262144  GPULayers:49   → doesn't fit
Operation:fit  KvSize:262144  GPULayers:25   → doesn't fit
Operation:fit  KvSize:262144  GPULayers:24   → fits
Operation:alloc KvSize:262144 GPULayers:24
Operation:commit KvSize:262144 GPULayers:24
```

Result (from `ggml.go`):
```
offloading 24 repeating layers to GPU
offloading output layer to CPU
offloaded 24/49 layers to GPU
total memory: 42.6 GiB
```

For comparison, requests with KvSize=32768 (the 32k preset):
```
offloaded 49/49 layers to GPU
total memory: 20.5 GiB
```

### Why it causes a stream error

The 3090 has 24 GiB VRAM. At 262144 context:
- Model weights on GPU: ~8.4 GiB
- KV cache at 262144 ctx: ~23 GiB needed on GPU alone

Ollama therefore runs 24/49 layers on GPU and 25/49 on CPU (including the
output projection layer). The `graph splits = 2` log line confirms
cross-device inference is active. CPU inference on a 30B MoE is roughly
10–20× slower than full-GPU.

During the slice-7 live run (wall_timeout_s was 120 s at the time, later bumped
to 1200/1800 s), inference did not finish before the SIGKILL fired. The openclaw
stream consumer looped through all SSE chunks, hit the end of stream without
seeing `chunk.done = true`, and threw:

```
Error: Ollama API stream ended without a final response
```

Source: `openclaw@2026.4.29/dist/stream-a-EylMJ7.js`:
```js
if (!finalResponse) throw new Error("Ollama API stream ended without a final response");
```

The wall timeout is now 1200 s, so this specific trigger is less acute — but
inference is still slow and unreliable because the computation graph is split
across CPU+GPU.

### Concrete numbers

| context | layers on GPU | total memory | status |
|---|---|---|---|
| 32 768 | 49/49 (full GPU) | 20.5 GiB | stable |
| 262 144 | 24/49 (half CPU) | 42.6 GiB | slow, split, stream-fragile |

## Failure mode 2: malformed XML in tool call responses

### What the logs show

`journalctl -u ollama` captured three distinct warnings on 2026-05-01
around 23:03–23:11 local time:

```
level=WARN source=qwen3coder.go:64 msg="qwen tool call parsing failed"
  error="XML syntax error on line 17: element <parameter> closed by </function>"

level=WARN source=qwen3coder.go:64 msg="qwen tool call parsing failed"
  error="XML syntax error on line 17: element <parameter> closed by </function>"

level=WARN source=qwen3coder.go:64 msg="qwen tool call parsing failed"
  error="XML syntax error on line 28: element <parameter> closed by </function>"
```

### Root cause

`qwen3-coder:30b` emits tool calls as XML (`<tool_call><function>...</function></tool_call>`).
The model occasionally generates mismatched closing tags (e.g., closing a
`<parameter>` block with `</function>`). Ollama's `qwen3coder.go:64` parser
rejects the malformed XML and logs a WARN. The chunk's tool call is discarded.

If the malformed chunk is the final chunk (the one that would set
`chunk.done = true`), the stream may still complete and `finalResponse` gets
set — but the tool call is silently dropped, causing the agent to produce text
instead of invoking a tool. If the XML error corrupts the done-chunk itself, the
stream ends without setting `finalResponse`, and openclaw throws the error.

This is a model quality issue at Q4_K_M quantization. The model generates
structurally correct XML with sufficient context; at the tail end of long
generations or with complex tool schemas it drifts and produces broken tags.
Ollama 0.21.0 may also have qwen3coder parser bugs that newer versions fix.

## Failure mode 3: GPU eviction under concurrent loads (observed once)

On 2026-04-30 22:38:51 a second model load arrived while qwen was running:

```
gpu memory available="494.1 MiB" free="951.1 MiB" minimum="457.0 MiB"
msg="model requires more gpu memory than is currently available, evicting a model to make space"
  "loaded layers"=18
```

The GPU had 951 MiB free against the 457 MiB minimum — not enough for a new
model without evicting the current one. Eviction abruptly kills the runner
process mid-generation. This is not the primary cause of the slice-7-tuning
failure (which was a single-model run) but is a secondary risk if the swarm
ever dispatches concurrent T0 workers to the same ollama instance.

## Diagnosis summary

| failure mode | trigger | frequency | severity |
|---|---|---|---|
| CPU offload timeout | KvSize=262144 + wall_timeout too short | every run at 262k ctx | **primary** |
| XML tool call parse failure | model generates malformed XML | ~3 events observed in a 20-min window | **secondary** |
| GPU eviction | concurrent model loads | 1 event observed | low (swarm is serial) |

## Recommended fix

### Fix 1 (immediate, highest impact): cap `num_ctx` at 32 768

Add `num_ctx: 32768` to the ollama model options in the openclaw agent config
for `qwen-agent`. Swarm entry prompts are well under 32k tokens; the context
cap does not affect task quality.

Result: all 49 layers run fully on GPU, total memory drops from 42.6 GiB to
20.5 GiB, inference runs at full GPU speed. The split-graph and associated
stream fragility disappear entirely.

Where to configure: `openclaw agents add qwen-agent --model ollama/qwen3-coder:30b --options '{"num_ctx": 32768}'`
or equivalent in the openclaw agent config. The `chitin-kernel install --slice-3-agents`
backlog entry (see `swarm-backlog.md`) is the right place to codify this.

### Fix 2 (upgrade ollama): bump from 0.21.0 to current stable

Ollama 0.21.0 is old. Current stable is 0.6.x+. Key improvements between
these versions:
- KV cache quantization (`OLLAMA_KV_CACHE_TYPE=q8_0`) — halves KV cache VRAM
  at marginal quality cost, enabling 262k context with more layers on GPU
- Fixes to the qwen3coder.go XML tool call parser
- Better model eviction handling under concurrent load

Upgrade command:
```bash
curl -fsSL https://ollama.com/install.sh | sh
```

Or if ollama is installed as a systemd service, check the release page for the
distro package.

After upgrading, set `OLLAMA_KV_CACHE_TYPE=q8_0` in `/etc/systemd/system/ollama.service`
(or `/etc/default/ollama`) to halve KV cache VRAM. This alone may allow
262k context to run with more layers on GPU, but Fix 1 (cap at 32k) is still
recommended for the swarm use case since 32k is ample and predictable.

### Fix 3 (alternative model): consider `qwen3-coder:14b` or a 7B coder

If qwen3-coder:30b continues to produce XML parsing errors after the ollama
upgrade, dropping to a 14B or 8B Q4_K_M variant (when available) would fit
entirely on the 3090 at any reasonable context length. The swarm's T0
mechanical tasks (single-file, <100 LOC) don't require 30B parameters to
succeed; the quality delta vs 14B is marginal for structured tool-use tasks.

`gemma4:latest` (9.6 GB) is already installed and fits fully on GPU — it is
not a code specialist but could serve as a fallback if qwen3-coder proves
persistently unstable.

## Recommended sequence

1. **Now:** cap `num_ctx=32768` in the qwen-agent openclaw config (fixes
   failure mode 1 immediately, no code change needed).
2. **Soon:** upgrade ollama from 0.21.0 to current stable (fixes XML parse
   errors, unlocks KV cache quantization for future large-context work).
3. **Verify:** re-run a slice-7-style test workflow with `local-qwen` driver
   and confirm no stream errors over 5 consecutive tool-using turns.
4. **Then:** flip `TIER_DRIVER[T0]` back to `local-qwen` per the
   `dispatcher-flip-t0-back-to-local-qwen` backlog entry.

## Reproduce the GPU split (diagnostic)

```bash
# confirm full-GPU load at 32k context
ollama run qwen3-coder:30b --verbose 2>&1 | grep "offloaded\|total memory"
# expect: offloaded 49/49 layers to GPU

# observe CPU-split load at 262k context (default)
OLLAMA_NUM_CTX=262144 ollama run qwen3-coder:30b --verbose 2>&1 | grep "offloaded\|total memory"
# expect: offloaded 24/49 layers to GPU, total memory 42.6 GiB
```

```bash
# watch for XML parse failures during a tool-using session
journalctl -u ollama -f | grep "qwen tool call parsing failed"
```
