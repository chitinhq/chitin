# Quickstart: Verify the §7 Amendment

**Feature**: 092-codify-swarm-orchestrator
**Audience**: the reviewer landing the implementation PR, and any operator pulling main after merge.

This is the post-merge verification recipe. The 8 success criteria from `spec.md` are reduced here to copy-pasteable commands.

## Pre-flight

```bash
git checkout main
git pull
```

## SC-001 — Constitution carries §7

```bash
grep -c '^## 7\. The swarm is the orchestrator' .specify/memory/constitution.md
# Expected: 1
```

## SC-002 — Driver table enumerates all 7 drivers

```bash
for driver in claudecode openclaw hermes codex copilot local-llm gemini; do
  count=$(grep -cE "^\| \`${driver}\` " .specify/memory/constitution.md)
  printf "%-12s %s\n" "$driver:" "$count"
done
# Expected: each driver shows at least 1
```

## SC-003 — Single-surface implementation rule present

```bash
grep -c 'No implementation PR may be opened' .specify/memory/constitution.md
# Expected: 1
```

## SC-004 — Supersession block names "agent-as-swarm-member"

```bash
grep -c 'agent-as-swarm-member' .specify/memory/constitution.md
# Expected: ≥ 1
```

## SC-005 — Amendment block is dated 2026-05-22

```bash
grep -c 'Amendment 2026-05-22' .specify/memory/constitution.md
# Expected: 1
```

## SC-006 — No vendor names in the §7 body

```bash
# Extract just §7 (from its heading to the next ## heading or EOF), then grep:
awk '/^## 7\. The swarm/,/^## [0-9]+\. |^$/' .specify/memory/constitution.md \
  | grep -ciE 'anthropic|openai|cognition|devin' || true
# Expected: 0
```

(Amendment HTML comments at the top of the file are exempt — they MAY cite for
traceability. Body text MUST NOT.)

## SC-007 — Companion research doc landed

```bash
test -f docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md \
  && echo PASS || echo FAIL
# Expected: PASS — after the companion PR lands
```

## SC-008 — Multi-driver alignment (post-merge smoke)

Manual operator check, not scripted. After §7 merges and each driver's startup
context is refreshed:

1. In a fresh Claude Code session, ask: **"what is the swarm?"** → expect the
   answer to cite §7's load-bearing claim ("swarm IS the orchestrator") and
   name drivers per the table.
2. In Discord, mention Ares: **"@Ares what is the swarm?"** → same expectation.
3. In Discord, mention Clawta: **"@Clawta what is the swarm?"** → same
   expectation. (Note: Clawta's startup context patch — fixing the inverted
   self-model — is a separate operator action that may be needed before this
   check passes for Clawta. Track that follow-up.)

All three should converge on the same architectural frame within one
operator-question. If they diverge, file as drift.

## Run-all

```bash
echo "SC-001:" && grep -c '^## 7\. The swarm is the orchestrator' .specify/memory/constitution.md
echo "SC-002 driver enumeration:"
for d in claudecode openclaw hermes codex copilot local-llm gemini; do
  printf "  %-12s %d\n" "$d" "$(grep -cE "^\| \`${d}\` " .specify/memory/constitution.md)"
done
echo "SC-003:" && grep -c 'No implementation PR may be opened' .specify/memory/constitution.md
echo "SC-004:" && grep -c 'agent-as-swarm-member' .specify/memory/constitution.md
echo "SC-005:" && grep -c 'Amendment 2026-05-22' .specify/memory/constitution.md
echo "SC-006 (expect 0):"
awk '/^## 7\. The swarm/,/^## [0-9]+\. |^$/' .specify/memory/constitution.md \
  | grep -ciE 'anthropic|openai|cognition|devin' || echo "  0"
echo "SC-007:" && (test -f docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md && echo "  PASS" || echo "  PENDING (companion PR not landed yet)")
```

## Failure modes the verification catches

| Symptom | Likely cause |
|---|---|
| SC-001 returns 0 | The §7 section wasn't appended; check the PR diff. |
| SC-002 a driver shows 0 | The table is malformed (missing backticks around driver name) or a row was dropped. |
| SC-003 returns 0 | The implementation gate text was paraphrased; restore from `contracts/canonical-section-7.md`. |
| SC-005 returns 0 | The 2026-05-22 amendment HTML comment wasn't prepended at the top of the file. |
| SC-006 returns >0 | A vendor name leaked into the §7 body; strip it (move to `docs/strategy/` if needed). |
| SC-007 PENDING for >24h | The companion research PR is delayed; not blocking this spec but worth chasing. |
| SC-008 driver answers diverge | Either the driver hasn't refreshed its session, or its startup context still carries the old framing — patch the startup context. Clawta's inverted self-model is a known instance. |
