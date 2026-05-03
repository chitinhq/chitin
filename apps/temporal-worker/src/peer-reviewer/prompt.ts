// Prompt builder for the `peer-reviewer` role.
//
// Independent second-opinion reviewer that fires per-PR alongside
// Copilot's R0 review. Distinct from the existing R1-R3 escalation
// chain — peer-reviewer is non-escalating, runs at one tier, and is
// dispatched in parallel (not sequentially) with the comment-responder
// when both apply.
//
// SOURCE OF TRUTH: apps/temporal-worker/skills/peer-reviewer/SKILL.md
// (with companions checklist.md + review-template.md). This file is
// now a thin shim — it loads the skill folder via the stitcher and
// substitutes the entry's runtime values. Keeping the function
// exported with the same signature lets role-prompts.ts continue to
// register it without further changes.
//
// The role's contract:
//   IN  — a PR URL + diff metadata
//   OUT — a single review comment posted to the PR with structured
//         findings (🔴 / 🟡 / 🟢 per the §5 reviewer convention)
//
// Bounds shape:
//   - write_policy=none: read-only (no commits)
//   - network=allowlist: gh CLI to read PR, post comment
//   - max_tool_calls=30
//   - wall_timeout=900s (15 min — peer review shouldn't be slow)

import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { BacklogEntry } from '../grooming/parse-backlog.ts';
import { renderSkill } from '../skill-loader/stitcher.ts';

// Resolve the skill folder via import.meta.url so the load works
// regardless of cwd. The skill folder lives one level up from src/
// (under apps/temporal-worker/skills/peer-reviewer).
const HERE = dirname(fileURLToPath(import.meta.url));
const SKILL_FOLDER = resolve(HERE, '..', '..', 'skills', 'peer-reviewer');

/**
 * Build the prompt for a peer-reviewer dispatch by rendering the
 * skill folder against the entry's runtime values. The rendered
 * output is observably equivalent to the legacy template literal —
 * existing tests on this builder act as the regression guard for
 * the migration. (Skill-folder body, including the inlined
 * checklist + review-template companions, was lifted from the
 * legacy template literal verbatim before this migration.)
 */
export function buildPeerReviewerPrompt(entry: BacklogEntry): string {
  return renderSkill(SKILL_FOLDER, { entry });
}
