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

- 2026-05-04 #291 — When provisioning context for analyst drivers from generators, filter chain history by affected files; discard rows entirely if no files are known to prevent prompt bloat from unrelated signals.
- 2026-05-04 #287 — Explicit nulls in fingerprints, not omitted fields, keep hash space stable when joining across different system layers.
- 2026-05-04 #284 — Allowlist owned paths for destructive ops; `relative()` checks true containment—`startsWith()` prefix matching is a trap (`/tmp` vs `/tmp-backup`).
- 2026-05-04 #283 — Signal parsing for deduplication must be pure: use stable sentinels for missing data, not generated timestamps, or idempotence breaks.
- 2026-05-04 #282 — Envelope-class denials bypass the denials table (gate.go RecordDenial exemption), so auto-recovery must classify recent denials via JSONL parsing separately from lifetime policy checks to avoid false recovery on infrast…
- 2026-05-04 #276 — Nx's typecheck inference breaks when tsconfig has `noEmit: true`; define explicit per-project targets with dependsOn to restore visibility.
- 2026-05-04 #273 — Set PYTHONPATH explicitly in systemd service Environment= for repo-local Python modules; default sys.path skips subdirectories.
- 2026-05-04 #265 — Entry IDs can contain regex metacharacters (dots, parens); `git log --grep` silently mis-matches them — scan PR subjects in-memory with word-boundary checks instead.
- 2026-05-04 #264 — Decision branching in procedural loops escapes test coverage; extract to pure helpers with structured skip-reasons to expose gaps in the decision matrix.
- 2026-05-04 #251 — Calendar-triggered systemd timers shouldn't add OnBootSec—it fires on both boot and the calendar schedule, almost never what operators intend; reserve OnBootSec for interval timers.
- 2026-05-04 #238 — Nx Tree.exists() includes pending writes from the generator invocation, not just on-disk files—always validate before writing to prevent silent collision errors.
- 2026-05-03 #195 — Verify Slack signatures uniformly across all endpoints; admin allowlist is checked post-verification only for destructive operations.
- 2026-05-03 #192 — Every item must produce exactly one item_decision telemetry record with explicit rationale code (slotted/unslotted:reason); enables observability of all scheduling outcomes including failures.
- 2026-05-03 #191 — Spawning chitin-kernel: errors are in stderr JSON with error.kind; parse stderr before stdout or real errors become silent parse failures.
- 2026-05-03 #189 — nx-angular-workspace-install
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
