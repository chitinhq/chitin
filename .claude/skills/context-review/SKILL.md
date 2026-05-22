---
name: context-review
description: Review Chitin's governance architecture against a research context pack in .claude/context/ and propose the single highest-leverage in-scope improvement. Use when asked to "context review", "ingest a context pack", "review against research", or to act on a file under .claude/context/.
---

# Context Review

Turn a research context pack into one scoped, defensible improvement to
Chitin — without letting research drift into accepted requirements.

## Quick start

```
/context-review                      # uses the pack(s) in .claude/context/
/context-review ai-agent-governance  # uses .claude/context/ai-agent-governance.md
```

Run from a worktree, never the primary checkout at `~/workspace/chitin`.

## Workflow

Work every step in order. Do not skip the explain-before-edit gate.

1. **Read the context pack.** `.claude/context/<topic>.md` (the argument),
   or every file in `.claude/context/` if no argument. It is *background
   research*, not a spec.
2. **Read repo authority.** `AGENTS.md`, `CLAUDE.md`, and the dated
   decision docs in `docs/decisions/`. These — plus specs and tests — are
   authoritative.
3. **Treat the pack as candidates, not requirements.** Nothing in it ships
   unless a spec, ticket, or operator instruction backs it.
4. **Review the architecture.** Inspect the governance surface relevant to
   the pack: `chitin.yaml`, `go/execution-kernel/internal/{gov,router,driver}/`,
   `docs/architecture/`, `docs/driver-conformance.md`.
5. **Pick ONE highest-leverage change.** It must deepen a moat asymmetry
   (AGENTS.md "The moat") and fit an allowed bucket. Prefer a documentation
   change when the implementation is already correct.
6. **Explain before editing.** State the proposed change, the exact files
   to inspect/edit, the bucket it fits, and why it deepens the moat.
   Proceed only if it is clearly in-scope.
7. **Flag conflicts.** If the pack contradicts AGENTS.md, decision docs,
   specs, or tests, follow the repo and call out the conflict explicitly.
8. **Stay in the worktree.** All edits land here; never touch the primary
   checkout. Leave changes uncommitted unless asked to commit.

## Scope guardrails — Chitin is NOT

Reject any proposed change that would make Chitin one of these:

- an orchestrator (work tracking, dispatch, cron, workflows → Hermes)
- an approval system (operator prompting → Hermes)
- an LLM runner (no `claude -p` / model shellout in the hot path)
- an MCP server host (substrates host their own)
- a model router (drivers pick the model)
- a SaaS (local-only)

If the change needs any of the above, it does not ship here. Say so and
stop.

## Output

Before any edit, produce: **proposed change · files · bucket · moat fit ·
conflict check**. After editing, summarize what landed and confirm the
primary checkout is untouched.
