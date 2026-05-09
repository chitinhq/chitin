# Claude Code Hook — Cold-Start Latency Verdict

**Date:** 2026-04-29
**Spec:** `docs/superpowers/specs/2026-04-29-cost-governance-kernel-design.md` §"Path A — Claude Code session"
**Plan:** `docs/superpowers/plans/2026-04-29-cost-governance-kernel.md` Milestone C
**Bench:** `go/execution-kernel/cmd/chitin-kernel/bench_coldstart_test.go`

## Decision gate

The plan's acceptance gate for the Claude Code hook driver:

- **p95 ≤ 100ms** → ship cold-start. No daemon mode.
- **p95 > 100ms** → design and build daemon mode (`gate daemon` listener on `~/.chitin/gate.sock`) before shipping.

## Result (operator's box)

100 cold-start invocations of `chitin-kernel gate evaluate --hook-stdin --agent=claude-code` against a fixture `Read /etc/hosts` PreToolUse payload, after 3 warm-up runs:

| metric | latency |
|---|---|
| min | 3 ms |
| p50 | 3 ms |
| p95 | 3 ms |
| p99 | 4 ms |
| max | 4 ms |

**Verdict: ship cold-start. Daemon mode is not needed for this slice.**

Each invocation includes: subprocess spawn, sqlite open + WAL setup, policy load + parse from chitin.yaml, action normalize, gate evaluate, decision append to JSONL audit log, response write, exit. The headroom (3ms vs 100ms) is large enough to absorb 30x degradation on slower hardware before the gate flips.

## Reproduce

```bash
cd go/execution-kernel
COLDSTART=1 go test ./cmd/chitin-kernel/... -run ColdStart -v -count=1
```

The test is gated on `COLDSTART=1` so unit-test runs aren't slowed by full subprocess invocations. Set `COLDSTART_ITERS=N` to change the sample size (default 100).

## Caveats

- Box: Linux 6.17, Ryzen + NVMe (operator's RTX 3090 dev box per `memory/project_machine_topology.md`).
- macOS and slower-disk hardware are likely to be different. Operators should re-run on their own box once before relying on the verdict for production hooks.
- Test fixture is a deterministic-allow `Read /etc/hosts`. Deny paths and envelope-spend paths weren't measured separately — they share most of the cold-start cost (subprocess + sqlite + policy), so the difference is sub-ms.
- Policy used is a 1-rule allow. Larger rule sets (~50+ rules, regex-heavy) might shift p95 — re-bench when the operator's policy stabilizes.

## Future work

If `BUILD DAEMON MODE` is ever the verdict on a box, the daemon spec lives at `gate daemon` in the plan: long-running listener on `~/.chitin/gate.sock`, hook command becomes `chitin-kernel gate evaluate --hook-stdin --via=sock`. Out of scope for this milestone since the gate didn't trip.
