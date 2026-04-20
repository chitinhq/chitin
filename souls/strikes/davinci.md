# Strike log — da Vinci

Operational feedback against the da Vinci soul. Each entry records a case
where the lens (or the agent operating under it) produced work that failed
to meet its own stated heuristics. Kept separately from `souls/canonical/davinci.md`
so the soul_hash remains stable for forensic event chains.

Format per strike:
- ID, date, session context
- What the lens was supposed to catch
- What actually happened
- Which heuristic was violated
- Remediation / learning

---

## Strike 1 — 2026-04-19 — Implemented Phase B against an assumed hook schema

**Context.** Dogfood-debt-ledger plan, Phase B (Claude Code global hook
install). Worktree `dogfood-debt-ledger` → PR #19 on chitinhq/chitin.
CI green, self-review green. Adversarial pass caught two blockers that
would have shipped silently.

**What the lens was supposed to catch.**
Heuristic 2 — *Observation over dogma.* "Don't trust README claims — go
look. Read the actual events.jsonl. Query the actual DB. Run the actual
binary." The whole point of the da Vinci lens in this session was to
ground claims in the running system, not documentation or assumed shape.

**What actually happened.**
1. `global.go` wrote Claude Code hook entries as flat
   `{type, command}` objects. Claude Code's actual schema is the nested
   `{matcher, hooks:[{type, command}]}` wrapper — already used correctly
   in this repo by `apps/cli/src/commands/init-claude-code.ts:39-44`.
   Adjacent code was the right reference; I didn't read it carefully.
2. `adapter-context.ts` minted a fresh `randomUUID()` for session_id on
   every hook invocation, discarding the `input.session_id` that
   Claude Code already provides on stdin. Every event becomes an orphan
   chain of length 1 — the chain-of-events contract is broken.

Install would have succeeded silently. Zero events captured. No alert.
**This is exactly the class of regression the dogfood-debt-ledger is
being built to detect** — shipping it in the ledger's own implementation
PR is a category error.

**Heuristic violated.**
Observation over dogma. I implemented against an assumed hook schema
without reading Claude Code's actual contract or the working adjacent
file in the same repo.

Secondarily, heuristic 6 (curiosity, "always one more why"): I did not
ask "does this format actually match what Claude Code expects?" before
declaring the task done.

**Remediation.**
- PR #19 closed without merge.
- New feedback memory written:
  `feedback_verify_external_contracts.md` — verify third-party schemas
  against the authoritative source before implementing.
- Phase B restart will begin with a capture of a real Claude Code hook
  invocation on this machine (observe the running system) to ground the
  schema, then re-implement.
- Phase A work (chitinDir resolver, kernel symlink) is unaffected and
  remains on branch.

**Learning for the lens.**
The da Vinci heuristic "observation over dogma" must extend to
third-party contracts, not only to internal system state. Running the
binary includes running *the system that calls your binary*.
