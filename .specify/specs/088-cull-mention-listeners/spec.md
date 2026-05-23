# Feature Specification: Retire the agent-bus mention listeners

**Feature Branch**: `feat/088-cull-mention-listeners`

**Created**: 2026-05-22

**Status**: Draft

**Input**: User description: "Cull `clawta-mention-listener` (and document the cleanup of its `mini-mention-listener` sibling) — both crons depend on `~/.chitin/agent-bus/bus.db`, which spec 069 decommissioned. Today the operator surfaced 'why don't clawta respond on discord' — investigation traced the symptom to this dead listener writing `bus_db_missing` to its log every minute and processing zero mentions. The path to Discord-mention routing is severed; retiring the listener stops the noise and removes the corpse, leaving the future Discord-ingest path (if any) as a clean greenfield design rather than a half-zombie."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Zero agent-bus residue in active source (Priority: P1)

The operator can `git pull` chitin main onto a fresh box, run the standard install sequence, and end up with no script anywhere on disk that reads `~/.chitin/agent-bus/bus.db`. The `clawta-mention-listener` source script and its installer are gone from the chitin repo. The operator's user crontab no longer carries a `# managed: clawta-mention-listener` block. The minute-by-minute `bus_db_missing` log noise stops on existing boxes after the documented one-line `crontab` cleanup.

**Why this priority**: The dead listener is the literal cause of the user's reported "clawta doesn't respond on Discord" symptom today. Anyone reading `~/.openclaw/logs/clawta-mention-listener.log` sees the same false-error every minute, which obscures real failures elsewhere. Removing it converts a confusing zombie state into a clean "no Discord ingest path exists" state.

**Independent test**: From a fresh clone of chitin at this feature's merge commit, `find swarm/bin -name '*clawta-mention-listener*'` returns zero results, AND `grep -rln 'agent-bus/bus.db' apps/ go/ libs/ services/ swarm/` (excluding `specs/` and history) returns zero hits. On an existing operator box, after running the documented cleanup steps, `crontab -l | grep mention-listener` returns nothing and the listener log files stop growing within one minute.

---

### User Story 2 — Future operator can answer "where did Discord ingest go?" (Priority: P2)

A future operator (or the same operator in 3 months) wonders why @Clawta on Discord goes unanswered. They search the active operator docs and find a short note explaining: agent-bus was decommissioned by spec 069; the mention-listener that turned Discord @-mentions into Clawta dispatches was retired by spec 088; no replacement Discord-mention ingest exists today; if Discord-mention routing is wanted again, it must be designed and shipped as a new feature against the current substrate (chitin-console-api's Discord poller, or direct Discord polling).

**Why this priority**: Without this note, the next "why doesn't clawta respond on Discord?" question takes another investigation cycle. The note is the cheap insurance that converts institutional memory into queryable text.

**Independent test**: `grep -rln 'mention-listener\|Discord ingest' docs/ AGENTS.md OPERATOR.md` (any active operator doc) returns at least one hit explaining the post-retirement state and naming spec 088. A new operator reading that doc reaches the right conclusion ("Discord-mention routing is unimplemented today, not broken") without spelunking through chain events.

---

### Edge Cases

- **Operator's box has an old install with the cron still present.** The cleanup is idempotent: `crontab -l | grep -v 'clawta-mention-listener\|mini-mention-listener' | crontab -` (or the equivalent `# managed:` block removal in the cron installer) leaves the rest of the crontab untouched.
- **Operator wants to keep the log files for forensic value.** Logs at `~/.openclaw/logs/{clawta,mini}-mention-listener.log` are operator-owned data; the retirement stops writing to them but MUST NOT delete them.
- **A future operator wants Clawta-on-Discord back.** That is out of scope here — they file a new spec for Discord-mention ingest against the current substrate; the retirement does not preclude that path, it just stops pretending the old one works.
- **The `mini-mention-listener` script lives outside the chitin repo** (only installed at `~/.openclaw/bin/mini-mention-listener`, not in `swarm/bin/`). The chitin-side retirement covers the `clawta-mention-listener` source + installer; documenting the operator-side cron-removal step is the only chitin-side action for `mini`.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: All in-repo files implementing or installing `clawta-mention-listener` MUST be removed. Concretely: `swarm/bin/clawta-mention-listener` and `swarm/bin/install-clawta-mention-listener.sh`.
- **FR-002**: No active source under `apps/`, `go/`, `libs/`, `services/`, or `swarm/` MAY reference `~/.chitin/agent-bus/bus.db`, `AGENT_BUS_DB`, or `agent-bus/bus.db` after this spec lands. (Documentation under `docs/`, `specs/`, and decision history under `decisions/` MAY reference them historically — they explain WHY the retirement happened.)
- **FR-003**: An active operator document MUST gain a short subsection explaining the post-retirement state of Discord-mention routing for Clawta/mini: that no ingest path exists, that spec 069 decommissioned agent-bus, that spec 088 retired the residual listener, and that any future Discord-mention routing is a new feature against the current substrate.
- **FR-004**: A one-line idempotent shell command for operators to remove the `# managed: clawta-mention-listener` and `# managed: mini-mention-listener` blocks from their user crontab MUST be documented (in the operator doc subsection from FR-003 or in the spec's quickstart).
- **FR-005**: This retirement MUST NOT delete operator-owned log files (`~/.openclaw/logs/clawta-mention-listener.log`, `~/.openclaw/logs/mini-mention-listener.log`) or operator-owned databases. Operator-owned artifacts stay until the operator chooses to clear them.
- **FR-006**: This retirement MUST NOT touch the operational concerns surfaced as secondary findings in the investigation: the inactive `swarm-controller.service`, the `lockdown_loop_detected` chain events for clawta, or the failed `chitin-kernel-redeploy.service` / `swarm-audit.service`. Each is a separate ticket if the operator wants to chase it.

### Success Criteria *(mandatory)*

- **SC-001 (Source clean)**: After this spec merges to main, `grep -rln 'agent-bus/bus.db\|AGENT_BUS_DB' apps/ go/ libs/ services/ swarm/` returns zero hits. `find swarm/bin -iname '*clawta-mention-listener*'` returns zero results.
- **SC-002 (Operator cleanup verifiable)**: On an existing operator box, running the documented one-line cron cleanup makes `crontab -l | grep -E 'clawta-mention-listener|mini-mention-listener'` return empty. Within one minute, `tail -F ~/.openclaw/logs/clawta-mention-listener.log ~/.openclaw/logs/mini-mention-listener.log` produces no new `bus_db_missing` lines.
- **SC-003 (Documented post-retirement state)**: `grep -rln '088\|mention-listener' docs/ AGENTS.md OPERATOR.md` (or whichever active operator doc the team uses) returns at least one hit explaining that Discord-mention ingest for Clawta/mini is unimplemented today.

## Assumptions

- The operator does NOT want @Clawta-on-Discord working today. Evidence: spec 069 decommissioned agent-bus and the listener has been broken for several days without a replacement being shipped, and the operator's directive in this session was "cull dead stuff" (echoing the kanban retirement spec 087 reasoning).
- The `chitin-console-api` Discord poller (Pathfinder F12) is for the console UI's threads view, NOT for routing mentions to agents. It is unaffected by this retirement.
- The `mini-mention-listener` script is installed only at `~/.openclaw/bin/mini-mention-listener` and is NOT tracked in the chitin repo. The chitin-side spec can document the cron-cleanup for `mini` but the script itself lives in whatever repo installed it (likely openclaw / hermes).
- Operator-owned files under `~/.openclaw/`, `~/.chitin/`, and `~/.hermes/` are NEVER deleted by chitin code. Retirement code MAY stop WRITING to them; only the operator deletes.

### Scope

**In scope** (active chitin source):
- `swarm/bin/clawta-mention-listener` (delete)
- `swarm/bin/install-clawta-mention-listener.sh` (delete)
- Active operator documentation (gains a short subsection per FR-003)
- Spec history under `specs/088-cull-mention-listeners/` (this spec)

**Out of scope**:
- `~/.chitin/agent-bus/` — already absent; the spec 069 decommission is upstream of this.
- `~/.openclaw/bin/mini-mention-listener` — operator-installed from outside the chitin repo; documentation step only.
- `~/.openclaw/logs/*mention-listener.log` — operator-owned data.
- `~/.openclaw/data/clawta.db`, `~/.openclaw/data/clawta_decisions.db` — operator-owned, unrelated.
- The console-api Discord poller (F12) — unaffected, no changes.
- swarm-controller, lockdown-loop, redeploy-service, and audit-service failures — each is a separate ticket.
- Designing a new Discord-mention ingest path — out of scope; a future feature if and when the operator wants Clawta-on-Discord back.

### Dependencies

- Depends on: spec 069 (agent-bus decommission) — the upstream cause of this orphaned listener.
- Related but separate: spec 087 (retire-kanban-substrate) — same "cull the dead substrate" lineage, different substrate.
- Not blocked by: anything else. This is a pure deletion + documentation change.
