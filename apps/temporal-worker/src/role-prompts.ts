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

WORKFLOW (in order):

1. \`read\` ${targetFile}. Understand the change required.
2. \`edit\` or \`write\` to make the change.
3. **Commit immediately**: \`exec\` with \`git add -A && git commit -m "<entry-id>: one-line summary"\`. Commit your work BEFORE running tests. The apply pipeline only pushes committed work; uncommitted changes are dropped.
4. (Optional) \`exec\` to run tests. Test failures are diagnostic — they go in your stdout for the reviewer to inspect, NOT a reason to discard your work. If tests fail, you may attempt a fix-up commit, but do not exit without at least one commit.

If you finish without committing, the work is lost. If tests fail after your commit, that's a signal for the reviewer chain — your job is to produce work the reviewer can evaluate, not to ship perfectly-tested work in one shot. The review-graph (R1→R2→R3) catches what you couldn't.

ENTRY DETAIL (frontmatter + description):
${entry.description}

CONSTRAINTS:
- Do not modify chitin.yaml or anything under .chitin/ — governance is human-only and chitin's gate will deny those writes anyway.
- Only edit files referenced in the entry. Do not invent scope.
- Forbid editing files not named in the entry's \`file\` field, and instruct the agent to \`read\` ONLY the target file before editing.
- If you decide the entry is misclassified or requires human judgment, exit without committing — empty worktrees are not pushed.

REMEMBER: chat replies do nothing. Tool calls are the only thing that produces work. Commit BEFORE testing. Start by reading ${targetFile} now.`;
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
  // Pull the alarm signal out of the entry description so the agent
  // can hand it directly to the recipe. The alarm-feeder formats
  // entries with `> <alarm text>` as a blockquote line; if a
  // different upstream filed this entry, the agent reads the
  // description for context.
  const alarmHint = '<extract the alarm string from the ENTRY DETAIL — typically a blockquote line under "Auto-filed by chitin-alarm-feeder.timer">';

  return `You are playing the analyst role in chitin's autonomous swarm — see docs/design/2026-05-02-swarm-as-software-factory.md §3.

The investigation is fully recipe-driven. The recipe owns the analysis (deterministic: same alarm + same data → same finding); your job is to invoke it and report the result. Do NOT author bespoke Python — chitin's determinism-first model means the recipe is the contract.

ENTRY ID: ${entry.id}
ROLE: analyst

ENTRY DETAIL:
${entry.description}

YOUR ENTIRE WORKFLOW (3 tool calls):

1. Extract the alarm string from ENTRY DETAIL above (look for a \`> <alarm text>\` blockquote, typically on a line right after "Auto-filed by chitin-alarm-feeder.timer").

2. Dispatch the \`exec\` tool to run the investigation recipe:
   \`\`\`
   cd python && python3 -m analysis.investigate --entry "${entry.id}" --alarm "<the alarm string from step 1>"
   \`\`\`
   The recipe writes a markdown report to \`python/analysis/out/${entry.id}.md\` + a JSON sidecar to \`python/analysis/out/${entry.id}.json\`, and prints a \`<<<ANALYSIS>>>{...}\` line on stdout.

3. The last line of stdout from step 2 IS your structured emit. Echo it as your final reply (the marker line must appear in your output for the runner's parser to find it).

That's it. The recipe handles every alarm kind it knows (bucket-B, low-success, qwen-idle); unknown kinds fall back to needs_human with a stub report. New alarm kinds are added by extending \`analysis.investigate.ALARM_HANDLERS\` — that's a separate backlog entry for the operator, not your job here.

CONSTRAINTS:
- The agent's stdout must include the \`<<<ANALYSIS>>>\` line from the recipe. The runner's parser keys on it.
- Do NOT author Python yourself. If the recipe doesn't recognize the alarm kind, it writes a needs_human stub — that's the correct behavior. File a separate backlog entry to teach the recipe later.
- Do NOT modify code under \`apps/\`, \`go/\`, or \`libs/\`. Pure analysis is your scope.
- Do NOT modify chitin.yaml or anything under .chitin/.
- Empty worktree on completion is EXPECTED — your output is the markdown report under \`python/analysis/out/\` (which the apply step commits + pushes if it's a tracked path) and the marker-line on stdout (which the runner reads for the chain).

Reference: \`python/analysis/investigate.py\` is the recipe source. Read it before extending; the alarm kinds it handles + the Finding shape are pinned by tests in \`tests/test_investigate.py\`.`;
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
  // Comment-responder role: stub, replaced by dedicated template in follow-up entry.
  'comment-responder': (entry) => buildStubPrompt('comment-responder', entry),
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
