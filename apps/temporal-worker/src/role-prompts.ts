// Phase 1 of the swarm-as-software-factory design (see
// docs/design/2026-05-02-swarm-as-software-factory.md §3-4): each role
// has its own prompt template. The dispatcher picks the right one
// based on the BacklogEntry's `role:` field. Roles without a
// dedicated template fall back to `programmer` (the pre-Phase-1
// behavior).
//
// What's here is intentionally minimal in this slice — only
// `programmer` has a real template (it's what slice-7b shipped).
// Other roles are stubs with role-aware framing so the dispatcher
// doesn't crash when they're picked, but the actual per-role prompt
// engineering lands in follow-up entries (one per role).

import { resolve } from 'node:path';
import type { Role } from '@chitin/contracts';
import type { BacklogEntry } from './grooming/parse-backlog.ts';
import { RESEARCHER_OUTPUT_INSTRUCTIONS } from './researcher-prompts.ts';
import { getRecentLessonsSync } from './lessons.ts';

// How many of the most-recent lessons to prepend to a programmer
// prompt. Picked low enough that the overhead per dispatch is small,
// high enough that the recent-history pattern (last week's swarm
// activity) is visible to the next worker.
const LESSONS_PROMPT_HEAD = 12;
const LESSONS_PATH = resolve(process.cwd(), 'docs/swarm-lessons.md');

export type RolePromptBuilder = (entry: BacklogEntry) => string;

// The pre-Phase-1 prompt template — what slice-7b's `buildPrompt`
// produced. Wrapping it in a role registry lets future programmer-
// prompt tweaks happen without touching the dispatcher.
function buildProgrammerPrompt(entry: BacklogEntry): string {
  const rawFile = entry.file?.split(',')[0]?.trim();
  let targetFile: string;
  if (rawFile) {
    targetFile = rawFile.startsWith('./') || rawFile.startsWith('/') ? rawFile : `./${rawFile}`;
  } else {
    targetFile = 'the file named in the entry';
  }

  // Recent lessons prepended so the next swarm worker starts with
  // what the last N already learned. Empty string when the file is
  // missing or empty (first-tick state) — the section is skipped
  // rather than rendering an empty heading.
  const lessonsBlock = getRecentLessonsSync(LESSONS_PATH, LESSONS_PROMPT_HEAD);
  const lessonsHeader = lessonsBlock
    ? `RECENT LESSONS (most recent first — apply these before writing code; ignore irrelevant ones):
${lessonsBlock}

`
    : '';

  return `${lessonsHeader}You are a swarm worker executing one backlog entry. Output text is ignored — only TOOL DISPATCHES count. If you finish without dispatching tools, the work is lost.

ENTRY ID: ${entry.id}
TARGET FILE: ${targetFile}

YOUR FIRST ACTION: dispatch the \`read\` tool on ${targetFile}. Do not respond with text first. Read the file, understand the change required, then dispatch \`edit\` or \`write\` to make the change. Run \`exec\` if tests are needed. Finally dispatch \`exec\` with a git command to commit your work (e.g., git add -A && git commit -m "..."), so the apply pipeline can push the branch.

ENTRY DETAIL (frontmatter + description):
${entry.description}

CONSTRAINTS:
- Do not modify chitin.yaml or anything under .chitin/ — governance is human-only and chitin's gate will deny those writes anyway.
- Only edit files referenced in the entry. Do not invent scope.
- Forbid editing files not named in the entry's \`file\` field, and instruct the agent to \`read\` ONLY the target file before editing.
- If you decide the entry is misclassified or requires human judgment, exit without committing — empty worktrees are not pushed.

REMEMBER: chat replies do nothing. Tool calls are the only thing that produces work. Start by reading ${targetFile} now.`;
}

// Researcher prompt for the BacklogEntry path. The richer runner-level
// version (buildResearcherPrompt in researcher-prompts.ts) takes
// pre-fetched source summaries + existing-candidate ids; that path is
// invoked by the future external-signal-fetchers runner. When a
// backlog entry just declares `role: researcher` without that runner
// context, the entry itself describes the research task — the agent
// reads source material itself rather than receiving summaries. Both
// paths emit the same `<<<CANDIDATES>>>` marker so a single parser
// (parseResearcherOutput) handles both.
function buildResearcherEntryPrompt(entry: BacklogEntry): string {
  return `You are playing the researcher role in chitin's autonomous swarm — see docs/design/2026-05-02-swarm-as-software-factory.md §3 for the role's scope.

This research task came in as a backlog entry rather than via the periodic external-signal-fetchers runner, so no source summaries are pre-fetched. Read the entry detail to learn what to research, fetch the source material yourself (arxiv abstracts, GitHub READMEs, blog posts, etc.) using available tools, then synthesize candidates for \`docs/roadmap.md\`'s "Candidates from external signal" section.

ENTRY ID: ${entry.id}
ROLE: researcher

ENTRY DETAIL:
${entry.description}

Synthesis rules:
- One candidate per genuinely-new finding. Don't over-batch related items into one row; don't fragment one finding into multiple rows.
- The "why" field is load-bearing — spend the words on chitin-specific implications, not generic novelty.
- Skip restatements of existing chitin work. Skip pure marketing.
- If you can't tell whether something matters, skip it. The roadmap reader trusts you to filter — false-positives are worse than false-negatives.
- Do not edit roadmap.md directly from this prompt. The runner inserts candidates after parsing your structured emit.

${RESEARCHER_OUTPUT_INSTRUCTIONS}`;
}

// Analyst prompt for the BacklogEntry path. The analyst role owns
// internal-telemetry analysis (gov-decisions chain, swarm rollups,
// debt ledger). It's distinct from researcher (which pulls EXTERNAL
// signals — arxiv, reddit, openclaw): analyst processes the swarm's
// own audit trail to find regressions, debt patterns, success-rate
// dips, and proposes either a fix entry or a needs_human escalation.
//
// Why a dedicated template (vs reusing programmer): the analyst is
// expected to NOT write code. Its output is a finding + recommended
// action, parsed via `<<<ANALYSIS>>>` so the apply step records a
// markdown report rather than expecting a code commit. An empty
// worktree on success is the EXPECTED outcome.
function buildAnalystEntryPrompt(entry: BacklogEntry): string {
  return `You are playing the analyst role in chitin's autonomous swarm — see docs/design/2026-05-02-swarm-as-software-factory.md §3 for the role's scope.

Your toolkit is \`python/analysis/\`: a typed library of loaders + reports against chitin's canonical event chain. The lib already produces three streams:

  - \`analysis.decisions\` — per-rule deny/allow analysis of governance decisions
  - \`analysis.debt\` — debt-ledger loader + reporting
  - \`analysis.swarm_health\` — daily rollup (bucket-B rate, success-by-tier, alarms[])
  - \`analysis.swarm_runs\` — per-run records (driver, tier, exit_code, duration_ms)

Source data:
  - \`~/.cache/chitin/.../events-*.jsonl\` — gov-decisions chain (canonical)
  - \`~/.cache/chitin/swarm-state/dispatched/<entry-id>.json\` — dispatch markers
  - \`~/.cache/chitin/swarm-rollups/<YYYY-MM-DD>.json\` — daily rollup
  - \`tmp/result-swarm-*.json\` — workflow result envelopes
  - \`docs/debt-ledger.md\` — operator-curated + auto-curated debt

ENTRY ID: ${entry.id}
ROLE: analyst

ENTRY DETAIL:
${entry.description}

What good output looks like:
- Run an existing analysis module (\`python -m analysis.swarm_health\` etc.) OR author a one-off Python query against the chain JSONL.
- Surface the smallest set of facts that explain WHY the entry was filed (root cause, not symptoms).
- Recommend a concrete next action: a fix-shape backlog entry, a tier-rule change, a debt-ledger promotion, or \`status: needs_human\` if the cause is non-obvious.

Output rules:
- Write your findings to \`python/analysis/out/${entry.id}.md\` (the apply step picks up files in this directory). Use the existing analysis writers in \`python/analysis/writers.py\` so format stays consistent across runs.
- Empty worktree on completion is EXPECTED for the analyst role — your output is the markdown report under \`out/\`, not a code change. The apply step understands this.
- Do NOT modify code under \`apps/\`, \`go/\`, or \`libs/\`. Pure analysis is your scope; if the analysis surfaces a code change, that's a follow-up backlog entry.
- Do NOT modify chitin.yaml or anything under .chitin/ — governance is human-only.
- If you cannot reproduce the regression or the rollup data is missing, exit cleanly with a stub report saying so. Better silent than guessed.

At the END of your run, emit EXACTLY ONE JSON object on a single line, prefixed with the literal token \`<<<ANALYSIS>>>\` and nothing else after the closing brace on that line:

<<<ANALYSIS>>>{"root_cause":"<one sentence>","recommended_action":"file-fix-entry"|"needs_human"|"no-action","report_path":"python/analysis/out/${entry.id}.md","confidence":"high"|"medium"|"low"}

If you couldn't determine a root cause, emit:

<<<ANALYSIS>>>{"root_cause":"unable to determine","recommended_action":"needs_human","report_path":"python/analysis/out/${entry.id}.md","confidence":"low"}`;
}

// Stub for the remaining non-programmer roles. Future entries replace
// these with real per-role prompts (reviewer reads the PR's diff; qa
// runs validation suites; etc.). For now the stub frames the role and
// points at the entry — better than crashing, worse than the eventual
// dedicated template.
function buildStubPrompt(role: Role, entry: BacklogEntry): string {
  return `You are playing the ${role} role in the chitin swarm — see docs/design/2026-05-02-swarm-as-software-factory.md §3 for what this role owns.

This entry's per-role prompt template hasn't been authored yet (Phase 1 ships the routing only; per-role templates land in follow-up entries). For this run, treat the entry below as a generic programmer task while the right ${role}-specific tooling is being designed.

ENTRY ID: ${entry.id}
ROLE: ${role}

ENTRY DETAIL:
${entry.description}

CONSTRAINTS:
- Do not modify chitin.yaml or anything under .chitin/ — governance edits go through T5 (a human path).
- Stay within the entry's declared file: scope.
- If the entry is genuinely a ${role}-shape task and you can't make progress without tooling that doesn't exist yet, exit cleanly — empty worktrees are not pushed.`;
}

const ROLE_PROMPTS: Record<Role, RolePromptBuilder> = {
  programmer: buildProgrammerPrompt,

  // Researcher uses its dedicated entry-level template (the runner-
  // level template lives in researcher-prompts.ts, called directly
  // by the external-signal-fetchers runner with richer context).
  researcher: buildResearcherEntryPrompt,

  // Stubs — replaced by dedicated templates in follow-up entries.
  product: (entry) => buildStubPrompt('product', entry),
  groomer: (entry) => buildStubPrompt('groomer', entry),
  architect: (entry) => buildStubPrompt('architect', entry),
  // The reviewer stub here fires only when a BACKLOG ENTRY explicitly
  // says `role: reviewer` (rare — entries are usually programmer-shape
  // work). The richer adversarial-review prompt the review-graph
  // workflow dispatches at tiers R1-R3 lives in reviewer-prompts.ts —
  // it takes PR context (diff, scope, prior findings, copilot
  // comments) that a single BacklogEntry can't carry. See
  // docs/design/2026-05-02-swarm-as-software-factory.md §5.
  reviewer: (entry) => buildStubPrompt('reviewer', entry),
  qa: (entry) => buildStubPrompt('qa', entry),
  gatekeeper: (entry) => buildStubPrompt('gatekeeper', entry),
  'tech-writer': (entry) => buildStubPrompt('tech-writer', entry),
  // Analyst uses its dedicated entry-level template. Internal-telemetry
  // analysis distinct from researcher's external-signal pulls.
  analyst: buildAnalystEntryPrompt,
  refactorer: (entry) => buildStubPrompt('refactorer', entry),
  'debt-curator': (entry) => buildStubPrompt('debt-curator', entry),
};

const ROLE_VOCAB = new Set<string>(Object.keys(ROLE_PROMPTS));

/**
 * Return the role for an entry, normalized + validated. Unknown role
 * strings (typos, future vocabulary) fall back to programmer with a
 * warning logged by the caller. Absent role = programmer (the
 * pre-Phase-1 default behavior).
 */
export function resolveEntryRole(entry: BacklogEntry): { role: Role; warning?: string } {
  const raw = entry.role?.trim();
  if (!raw) return { role: 'programmer' };
  if (ROLE_VOCAB.has(raw)) return { role: raw as Role };
  return {
    role: 'programmer',
    warning: `entry ${entry.id} has unknown role "${raw}"; falling back to programmer. Add it to RoleSchema if it's a real new role.`,
  };
}

/**
 * Build the prompt for an entry, dispatching to the role-specific
 * template (or programmer if role is unset/unknown).
 */
export function buildPromptForEntry(entry: BacklogEntry): string {
  const { role } = resolveEntryRole(entry);
  return ROLE_PROMPTS[role](entry);
}

// Exported for tests + the dispatcher's existing import path.
export { buildProgrammerPrompt };

export const __test__ = {
  ROLE_PROMPTS,
  ROLE_VOCAB,
  buildStubPrompt,
  buildResearcherEntryPrompt,
  buildAnalystEntryPrompt,
};
