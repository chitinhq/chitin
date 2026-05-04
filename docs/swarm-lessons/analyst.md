# Swarm lessons learned

One-sentence lesson per merged swarm PR. The dispatcher prepends the
most recent N entries to every analyst prompt — so the next swarm
worker starts with what the last one learned.

Format: `- YYYY-MM-DD #<pr-number> — <lesson>`. Newest first.

Curated by:
- chitin-lessons.timer (periodic) — heuristic distillation from PR
  title + body + diff stat
- operator (manual) — high-leverage corrections that the heuristic
  missed; preserved across runs (the extractor only appends, never
  rewrites existing entries)
