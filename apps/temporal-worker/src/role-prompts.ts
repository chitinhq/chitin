// Phase 1 of the swarm-as-software-factory design (see
// docs/design/2026-05-02-swarm-as-software-factory.md §3-4): each role
// has its own prompt template. The dispatcher picks the right one
// based on the BacklogEntry's `role:` field. Roles without a
// dedicated template fall back to `programmer` (the pre-Phase-1
// behavior).
//
// `programmer` has the pre-Phase-1 template; `reviewer` has the
// Phase-2 adversarial-review template. Other roles are stubs until
// their dedicated entries land.

import type { Role } from '@chitin/contracts';
import type { BacklogEntry } from './grooming/parse-backlog.ts';

// ---------------------------------------------------------------------------
// Adversarial reviewer — types, schema, and prompt builder
// ---------------------------------------------------------------------------

export interface PrMeta {
  prNumber: number;
  title: string;
  author: string;
  baseBranch: string;
}

export interface CopilotComment {
  file: string;
  line?: number;
  body: string;
}

export type FindingSeverity = '🔴' | '🟡' | '🟢';
export type FindingCategory = 'bug' | 'test_gap' | 'design' | 'doc';
export type ReviewDecision = 'approve' | 'request_changes' | 'escalate';
export type ReviewConfidence = 'high' | 'medium' | 'low';

export interface ReviewFinding {
  severity: FindingSeverity;
  file: string;
  line?: number;
  category: FindingCategory;
  summary: string;
  suggested_fix?: string;
}

// Structured output emitted by the reviewer agent — consumed by
// review-graph-executor's escalateOneTier to decide next-tier.
export interface AdversarialReviewOutput {
  decision: ReviewDecision;
  confidence: ReviewConfidence;
  findings: ReviewFinding[];
}

// Severity rubric:
//   🔴  Real bug — incorrect behavior, data loss, security issue → block merge.
//       If confidence:low, decision becomes "escalate" (next tier decides).
//   🟡  Worth fixing but not blocking — code smell, minor logic issue, test gap.
//       → request_changes, do not block merge.
//   🟢  Doc/nit — comment, naming, formatting → approve with comment.
// escalate: triggered when any 🔴 finding exists but confidence is "low".

const VALID_DECISIONS = new Set<ReviewDecision>(['approve', 'request_changes', 'escalate']);
const VALID_CONFIDENCES = new Set<ReviewConfidence>(['high', 'medium', 'low']);
const VALID_SEVERITIES = new Set<FindingSeverity>(['🔴', '🟡', '🟢']);
const VALID_CATEGORIES = new Set<FindingCategory>(['bug', 'test_gap', 'design', 'doc']);

// Runtime validator for AdversarialReviewOutput. Throws on invalid shape.
// Used by review-graph-executor to verify the LLM emitted the right JSON.
export function parseReviewOutput(raw: unknown): AdversarialReviewOutput {
  if (typeof raw !== 'object' || raw === null) throw new Error('review output must be an object');
  const obj = raw as Record<string, unknown>;

  if (!VALID_DECISIONS.has(obj['decision'] as ReviewDecision)) {
    throw new Error(`invalid decision: ${String(obj['decision'])}`);
  }
  if (!VALID_CONFIDENCES.has(obj['confidence'] as ReviewConfidence)) {
    throw new Error(`invalid confidence: ${String(obj['confidence'])}`);
  }
  if (!Array.isArray(obj['findings'])) throw new Error('findings must be an array');

  const findings: ReviewFinding[] = obj['findings'].map((f: unknown, i: number) => {
    if (typeof f !== 'object' || f === null) throw new Error(`finding[${i}] must be an object`);
    const fi = f as Record<string, unknown>;
    if (!VALID_SEVERITIES.has(fi['severity'] as FindingSeverity)) {
      throw new Error(`finding[${i}].severity invalid: ${String(fi['severity'])}`);
    }
    if (typeof fi['file'] !== 'string') throw new Error(`finding[${i}].file must be a string`);
    if (!VALID_CATEGORIES.has(fi['category'] as FindingCategory)) {
      throw new Error(`finding[${i}].category invalid: ${String(fi['category'])}`);
    }
    if (typeof fi['summary'] !== 'string') throw new Error(`finding[${i}].summary must be a string`);
    return {
      severity: fi['severity'] as FindingSeverity,
      file: fi['file'] as string,
      line: typeof fi['line'] === 'number' ? fi['line'] : undefined,
      category: fi['category'] as FindingCategory,
      summary: fi['summary'] as string,
      suggested_fix: typeof fi['suggested_fix'] === 'string' ? fi['suggested_fix'] : undefined,
    };
  });

  return {
    decision: obj['decision'] as ReviewDecision,
    confidence: obj['confidence'] as ReviewConfidence,
    findings,
  };
}

// Build the adversarial-reviewer system prompt. Called by the
// review-graph-executor when dispatching a reviewer-role agent turn.
// The ROLE_PROMPTS registry calls this with no runtime data (entry-only);
// the executor calls it directly with the full PR context.
export function buildAdversarialReviewerPrompt(
  prMeta: PrMeta | undefined,
  diff: string | undefined,
  copilotComments: CopilotComment[],
  entryFileScope: string | undefined,
): string {
  const prSection = prMeta
    ? `PR #${prMeta.prNumber}: "${prMeta.title}" by @${prMeta.author} (base: ${prMeta.baseBranch})`
    : '(PR metadata not provided — fetch it via `gh pr view <number>` from entry context)';

  const diffSection = diff
    ? `\`\`\`diff\n${diff}\n\`\`\``
    : '(diff not injected — fetch it via `gh pr diff <number>` before proceeding)';

  const copilotSection =
    copilotComments.length > 0
      ? copilotComments
          .map(
            (c, i) =>
              `[${i + 1}] ${c.file}${c.line != null ? `:${c.line}` : ''}\n${c.body}`,
          )
          .join('\n\n')
      : '(no Copilot comments provided — skip this section)';

  const scopeSection = entryFileScope
    ? `Declared \`file:\` scope in the backlog entry: ${entryFileScope}`
    : '(no file scope declared in entry — flag any changes to undeclared files)';

  return `You are an adversarial code reviewer. Your job is to find problems the author and Copilot missed.

Be hostile. Look for:
- Unhandled cases (boundary, null, out-of-order, empty collection, max-value)
- Wrong assumptions (API contracts, ordering guarantees, encoding, clock drift)
- Race conditions and concurrency hazards
- Security vulnerabilities (injection, data exposure, auth bypass, path traversal)
- Test gaps (happy-path only, wrong mock boundary, missing edge cases)
- Design flaws (naming that conceals intent, hidden coupling, abstraction leakage)

---
## PR UNDER REVIEW

${prSection}

## DIFF

${diffSection}

## COPILOT COMMENTS TO VERIFY

IMPORTANT: Do NOT auto-dismiss these as noise. Historical data shows ~73% of Copilot flags are real bugs (PR #78: 8 of 11 confirmed). Verify each comment against the actual diff above and report your finding.

${copilotSection}

## ENTRY FILE SCOPE

${scopeSection}

Invariant: every changed file in the diff must intersect the declared \`file:\` scope.
If the diff touches a file NOT in the declared scope, emit a 🔴 finding:
  category: "design", summary: "diff touches <file> which is outside declared file: scope — possible bucket-B misclassification".

---
## SEVERITY RUBRIC

| Severity | Meaning | Decision trigger |
|----------|---------|-----------------|
| 🔴 | Real bug — incorrect behavior, data loss, security issue | "request_changes" (confidence:high) or "escalate" (confidence:low) |
| 🟡 | Worth fixing but not blocking — smell, minor logic issue, test gap | "request_changes" |
| 🟢 | Doc/nit — comment, naming, formatting | "approve" with comment |

Decision rules (applied after collecting all findings):
- "approve"           → zero 🔴 findings AND confidence is "high"
- "request_changes"   → any 🟡, OR any 🔴 with confidence "high"
- "escalate"          → any 🔴 with confidence "low" (you see a problem but aren't certain)

---
## REQUIRED OUTPUT FORMAT

Emit ONLY valid JSON. No markdown fences, no explanation text — raw JSON only.

{
  "decision": "approve" | "request_changes" | "escalate",
  "confidence": "high" | "medium" | "low",
  "findings": [
    {
      "severity": "🔴" | "🟡" | "🟢",
      "file": "<path relative to repo root>",
      "line": <integer line number, omit if not applicable>,
      "category": "bug" | "test_gap" | "design" | "doc",
      "summary": "<one precise sentence>",
      "suggested_fix": "<optional: what to change>"
    }
  ]
}

An empty findings array with decision "approve" and confidence "high" is valid — it means you examined the diff carefully and found nothing. Do not fabricate findings.`;
}

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

  return `You are a swarm worker executing one backlog entry. Output text is ignored — only TOOL DISPATCHES count. If you finish without dispatching tools, the work is lost.

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

// Stub for non-programmer roles. Future entries replace these with
// real per-role prompts (researcher reads HN/arxiv; reviewer reads
// the PR's diff; etc.). For now the stub frames the role and points
// at the entry — better than crashing, worse than the eventual
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

  // Stubs — replaced by dedicated templates in follow-up entries.
  researcher: (entry) => buildStubPrompt('researcher', entry),
  product: (entry) => buildStubPrompt('product', entry),
  groomer: (entry) => buildStubPrompt('groomer', entry),
  architect: (entry) => buildStubPrompt('architect', entry),
  // Phase 2: real adversarial-review template. The executor calls
  // buildAdversarialReviewerPrompt directly with full PR context;
  // this registry path is used when the dispatcher fires without it.
  reviewer: (entry) => buildAdversarialReviewerPrompt(undefined, undefined, [], entry.file),
  qa: (entry) => buildStubPrompt('qa', entry),
  gatekeeper: (entry) => buildStubPrompt('gatekeeper', entry),
  'tech-writer': (entry) => buildStubPrompt('tech-writer', entry),
  analyst: (entry) => buildStubPrompt('analyst', entry),
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
  // Integration-test fixture: R0+R1+R2 approve, R3 finds a 🔴.
  // Validates that parseReviewOutput correctly round-trips the schema
  // and that escalateOneTier can consume decision+confidence+findings.
  fixtureR3RedFinding: (): AdversarialReviewOutput => ({
    decision: 'request_changes',
    confidence: 'high',
    findings: [
      {
        severity: '🔴',
        file: 'apps/temporal-worker/src/role-prompts.ts',
        line: 42,
        category: 'bug',
        summary: 'parseReviewOutput does not handle null line field from LLM output',
        suggested_fix: 'Add `typeof fi.line === "number"` guard before coercing to integer',
      },
    ],
  }),
};
