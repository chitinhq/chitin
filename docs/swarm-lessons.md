# Swarm lessons learned

One-sentence lesson per merged swarm PR. The dispatcher prepends the
most recent N entries to every programmer prompt — so the next swarm
worker starts with what the last one learned.

Format: `- YYYY-MM-DD #<pr-number> — <lesson>`. Newest first.

Curated by:
- chitin-lessons.timer (periodic) — heuristic distillation from PR
  title + body + diff stat
- operator (manual) — high-leverage corrections that the heuristic
  missed; preserved across runs (the extractor only appends, never
  rewrites existing entries)

- 2026-05-02 #145 — chitin-researcher-systemd-units
- 2026-05-02 #144 — dispatcher-respect-blocks-field
- 2026-05-02 #143 — researcher-role-prompt-template
- 2026-05-02 #142 — debt-ledger-analysis-loader
- 2026-05-02 #137 — tech-debt-ledger
- 2026-05-02 #127 — swarm-daily-rollup-healthcheck
- 2026-05-02 #126 — analysis-swarm-runs-loader
- 2026-05-02 #125 — qwen-ollama-config-bump-and-validate
- 2026-05-02 #122 — dispatcher-preflight-scrub-claude-settings-backup
- 2026-05-02 #119 — openclaw-tool-coverage-audit
- 2026-05-02 #116 — cron-subagents-image-granular-targets
- 2026-05-02 #115 — tools-summary-structured-result
- 2026-05-02 #113 — read-vs-read_file-file_path-alias
- 2026-05-02 #112 — qwen-ollama-stream-instability-investigation
- 2026-05-02 #108 — activity-include-hook-events-flag
- 2026-05-02 #106 — dispatcher-prompt-scope-discipline
- 2026-05-02 #105 — dispatcher-prompt-relative-path-prefix
- 2026-05-02 #103 — repo-regex-tighten
- 2026-05-02 #101 — normalize-decision-params-truthiness
