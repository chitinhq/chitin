# Driver conformance matrix

Status: active operator reference. Keep this file aligned with
`go/execution-kernel/internal/driver/*/normalize.go` and the installer
scripts.

Chitin's moat is one canonical action vocabulary across heterogeneous
agent drivers. A driver is conformant when every tool-call surface lands
in one of these outcomes:

- A canonical `gov.ActionType` with a meaningful target.
- `unknown`, deliberately fail-closed, with a documented gap.
- A structured cross-driver warning when another driver's tool name leaked
  through the wrong hook.

## Current surfaces

| Driver | Integration | Normalizer | Coverage | Current gaps |
|---|---|---|---|---|
| Claude Code | `PreToolUse` hook via `chitin-kernel install --surface claude-code --global` | `internal/driver/claudecode` | Bash, read/write/edit, web, MCP, task/delegate, task-state, worktree, cron/schedule, browse tools, todo | Future Anthropic tools intentionally hit `unknown` until mapped. |
| Codex CLI | `PreToolUse` hook via `scripts/install-codex-hook.sh` | `internal/driver/codex` | Bash, `apply_patch`, `read_file`, MCP, Claude-tool leak fallback | Narrow native enum; any new Codex tool should be added from live hook captures before policy loosening. |
| Gemini CLI | `BeforeTool` hook via `scripts/install-gemini-hook.sh` | `internal/driver/gemini` | Shell, read/list/search, edit/replace/write, web/search, memory/topic, Claude-tool leak fallback | Last tool-registry check in comments was Gemini CLI `0.40.1`; reverify on upgrade. |
| Hermes | `pre_tool_call` shell hook via `scripts/install-hermes-hook.sh` | `internal/driver/hermes` | Terminal/code, file, patch/search, web/browser, delegation, skills, kanban plumbing, process, MCP, Claude-tool leak fallback | `image_generate`, `text_to_speech`, `vision_analyze`, `cronjob`, and `clarify` are intentionally unmapped. Decide canonical types before allowing them. |
| Copilot CLI | In-kernel SDK wrapper via `chitin-kernel drive copilot` | `internal/driver/copilot` | SDK permission kinds: shell, write, read, MCP, URL, memory, custom tool, hook | Closed-vendor wrapper only. This does not cover VS Code Copilot agent-mode tool execution. |
| OpenClaw | `before_tool_call` plugin via `apps/openclaw-plugin-governance` | Plugin bridge into `chitin-kernel gate evaluate` | Tool calls dispatched by OpenClaw's native pi-agent-core runtime | Does not gate standalone Claude/Codex/Gemini/Copilot processes; use their native driver integrations. |
| VS Code Copilot | Repository instructions + `AGENTS.md` context | No execution normalizer | Uses repo guidance to steer agent behavior in the IDE | No pre-tool hook surface. Treat this as guidance only; route terminal-side agent execution through chitin-aware CLIs where enforcement is required. |

## Near-term work

1. Mine `default-deny` / `unknown` rows from `~/.chitin/gov-decisions-*.jsonl`
   by `(agent, tool_name, action_target)` and map the highest-volume real
   tools first. Last local Hermes pass: 2026-05-12; see
   "Hermes unknown and lockdown classification" below.
2. Add a fixture and normalizer test for each mapped tool before changing
   `chitin.yaml`.
3. For Hermes modality tools, decide whether the canonical vocabulary needs
   new action types (`media.generate`, `speech.generate`, `vision.analyze`,
   `schedule.job`) or whether they should stay fail-closed as substrate
   features.
4. For VS Code Copilot, keep instructions current and explicit that IDE
   guidance is not governance. The enforceable Copilot path remains
   `chitin-kernel drive copilot`.

## External status notes

- VS Code and GitHub Copilot support repository-wide instructions at
  `.github/copilot-instructions.md`, path-specific files in
  `.github/instructions/*.instructions.md`, and `AGENTS.md` for agent
  context.
- VS Code currently exposes `github.copilot.chat.codeGeneration.useInstructionFiles`
  and `chat.useAgentsMdFile` settings for these instruction surfaces.
- None of those instruction files are a security boundary. They improve
  behavior but do not replace chitin's gate.

## Hermes unknown and lockdown classification

Local chain mining over `~/.chitin/gov-decisions-*.jsonl` from
2026-04-22 through 2026-05-12 showed Hermes `unknown` denies only through
2026-05-09. There were no Hermes `unknown` rows on or after 2026-05-10 in
that state directory. Aggregate Hermes deny rows in the local slice were
`lockdown x unknown` = 439, `lockdown x kanban.call` = 165, and
`default-deny-unknown x unknown` = 23. The high-volume unknown rows are
therefore historical unless a fresh chain slice reintroduces them.
The pass grouped denied Hermes rows by `rule_id`, `action_type`,
`action_target`, and decision day, then checked the post-2026-05-09 window
for fresh `unknown` rows before recommending any normalizer or policy change.

| Tool / action target | Local evidence | Bucket | Recommendation |
|---|---:|---|---|
| Current high-volume gap | 0 Hermes `unknown` rows found from 2026-05-10 through 2026-05-12 | Real current gap: none found | No normalizer or policy expansion for this ticket. Re-mine current chain data before adding mappings. |
| `kanban_show` | 286 `unknown` rows, concentrated on 2026-05-07; plus 109 later `lockdown x kanban.call x show` rows after mapping shipped | Stale/archived surface, already fixed | Keep `ActKanbanCall` mapping. Do not add policy broadening for `unknown`; if an operator still sees `lockdown x kanban.call`, clear stale Hermes lockdown state rather than changing the normalizer. |
| `kanban_block` | 116 `unknown` rows on 2026-05-07; plus 46 later `lockdown x kanban.call x block` rows | Stale/archived surface, already fixed | Keep `ActKanbanCall` mapping and per-verb target. No new action type needed. |
| `kanban_comment`, `kanban_complete`, `kanban_heartbeat`, `kanban_link` | 29 combined `unknown` rows on 2026-05-07; comments/completes also appear later as `kanban.call` while Hermes was already locked down | Stale/archived surface, already fixed | Keep these in the Hermes `kanban_*` closed set. Add new kanban verbs only from live hook captures or Hermes display registry changes. |
| `process` | 5 `default-deny-unknown` rows on 2026-05-07 | Stale/archived surface, already fixed | Keep `ActHermesProcess`; this is Hermes runtime plumbing, not shell execution. |
| `Write` | 7 `unknown` rows on 2026-05-06 | Cross-driver leak | Keep Claude-Code leak re-normalization plus structured warning. Fix upstream dispatch wiring if this returns; do not create a Hermes-native `Write` mapping. |
| `skills_list` | 3 `default-deny-unknown` rows and 1 `lockdown` row on 2026-05-09 | Stale/archived surface, already fixed | Keep `file.read` mapping. Treat future rows as evidence that an old kernel binary or stale hook is still installed. |
| `memory` | 2 `default-deny-unknown` rows and 2 `lockdown` rows on 2026-05-09 | Stale/archived surface, already fixed | Keep `file.write` mapping with stable target `memory`; policy can govern durable memory as one sink. |
| `todo` | 1 `default-deny-unknown` row on 2026-05-09 | Stale/archived surface, already fixed | Keep `file.write` mapping with stable target `todo`. |
| `skill_manage` | 2 `lockdown` rows on 2026-05-09 | Stale/archived surface, already fixed | Keep `file.write` mapping. This is a write to operator skill state, so policy should decide allow/deny after classification. |
| `session_search` | 1 `lockdown x unknown` row on 2026-05-07 | Stale/archived surface, already fixed | Keep `file.read` mapping with query target and `session_search` fallback. |
| `browser_navigate` | 1 `lockdown x unknown` row on 2026-05-09, paired with a normalized `http.request` duplicate for the same envelope | Stale/archived surface, already fixed | Keep `http.request` mapping. If duplicates recur, inspect old hook/binary paths before changing policy. |
| `clarify` | 8 `unknown` rows across 2026-05-07 and 2026-05-09 | Intentional fail-closed | Keep `ActUnknown` until Hermes gives this a stable machine-action contract. It is chat/control flow, not a host side effect, and should not gain a broad allow rule. |
| `image_generate`, `text_to_speech`, `vision_analyze` | No local unknown rows in this slice, but explicitly listed as unmapped Hermes tools | Intentional fail-closed | Keep fail-closed for Hermes until there is a policy requirement for media side effects. Candidate future canonical types: `media.generate`, `speech.generate`, `vision.analyze`. |
| `cronjob` | No local unknown rows in this slice, but explicitly listed as unmapped Hermes tool | Intentional fail-closed | Keep fail-closed. Scheduling belongs to Hermes as the substrate; add a canonical type only if chitin needs to govern schedule creation as a cross-driver action. Candidate future type: `schedule.job`. |
| `lockdown x shell.exec` | 28 classified lockdown rows after Hermes was already locked, mostly kanban inspection shell commands plus a few explicit probe commands | Not a conformance gap | Leave policy and normalizers unchanged. Lockdown correctly denies every action regardless of action type. |
| `lockdown x file.read`, `lockdown x file.write`, `lockdown x http.request` | 8 classified lockdown rows, including skill/memory/file probes and one `browser_navigate` duplicate already normalized as `http.request` | Not a conformance gap | Treat as consequences of existing lockdown state, not unmapped Hermes tools. Reset lockdown state operationally if appropriate. |

Current high-volume Hermes conformance gap: none found in the local
2026-05-10 through 2026-05-12 chain rows. The main historical failure was
kanban/runtime plumbing falling to `unknown`, which is now classified by the
Hermes normalizer. The remaining `lockdown x kanban.call` rows in the mined
data reflect an already-locked agent continuing to make now-classified calls,
not an unmapped tool.

Recommendation: docs only for this ticket. Do not expand `chitin.yaml` to
allow `unknown`, and do not add new canonical action types until a live,
high-volume current surface proves that media or scheduling side effects need
cross-driver governance.
