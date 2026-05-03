// Prompt builder for the `comment-responder` role.
//
// SOURCE OF TRUTH: apps/temporal-worker/skills/comment-responder/SKILL.md
// (with companions operator-rule.md + decision-rubric.md +
// summary-template.md). This file is now a thin shim — it loads the
// skill folder via the stitcher and substitutes the entry's runtime
// values. Keeping the function exported with the same signature lets
// role-prompts.ts continue to register it without further changes.
//
// The role's contract:
//   IN  — a PR URL (and optional repo override) carried through the
//         entry's `description` field by the dispatch helper
//         (apps/temporal-worker/src/comment-responder/dispatch.ts).
//   OUT — at most one fix commit pushed to the PR's branch + one
//         summary comment posted on the PR. If no comments need
//         action, the agent posts a "no-op" summary and exits clean.
//
// Bounds shape:
//   - write_policy=branch: agent commits + pushes to the PR's branch
//   - network=allowlist: gh CLI calls + npm registry only
//   - max_tool_calls=80: large but bounded; reading 5-15 comments + a
//     handful of edits + tests + commit + push fits inside this
//   - wall_timeout=1800s: 30 min; tracks the R3 reviewer ceiling

import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { BacklogEntry } from '../grooming/parse-backlog.ts';
import { renderSkill } from '../skill-loader/stitcher.ts';

// Resolve the skill folder via import.meta.url so the load works
// regardless of cwd. The skill folder lives one level up from src/
// (under apps/temporal-worker/skills/comment-responder).
const HERE = dirname(fileURLToPath(import.meta.url));
const SKILL_FOLDER = resolve(HERE, '..', '..', 'skills', 'comment-responder');

/**
 * Build the prompt for a comment-responder dispatch by rendering the
 * skill folder against the entry's runtime values. The rendered
 * output is observably equivalent to the legacy template literal —
 * existing tests on this builder act as the regression guard for
 * the migration. (Skill-folder body, including the inlined
 * operator-rule + decision-rubric + summary-template companions, was
 * lifted from the legacy template literal verbatim before this
 * migration.)
 */
export function buildCommentResponderPrompt(entry: BacklogEntry): string {
  return renderSkill(SKILL_FOLDER, { entry });
}
