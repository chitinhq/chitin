// Structural linter: every `Role` enum value in @chitin/contracts'
// RoleSchema has a corresponding ROLE_PROMPTS entry in
// apps/temporal-worker/src/role-prompts.ts (and vice versa).
//
// Why: adding a role to RoleSchema without touching ROLE_PROMPTS
// fails silently at dispatch time — the dispatcher's prompt-builder
// lookup falls through to a stub or a runtime error. Lint catches
// the symmetric drift at PR time, before a real workflow tries to
// dispatch to the missing role.
//
// Pure functions are exported so the test suite can pin every branch
// of the symmetric-difference logic without dynamic-imports.

import { resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

// ── Pure logic ──────────────────────────────────────────────────────

export interface RoleCoverageGaps {
  /** Roles in RoleSchema but missing from ROLE_PROMPTS — the dispatcher
   *  has no prompt builder for them, so a workflow that requests this
   *  role will fail at dispatch. */
  missingPrompts: string[];
  /** Roles in ROLE_PROMPTS but not in RoleSchema — orphaned prompt
   *  builders the schema can't validate. Either the role was renamed
   *  in the schema and the prompt key wasn't, or the prompt is stale. */
  orphanedPrompts: string[];
}

/**
 * Pure: given the schema's enum values + the prompt map's keys,
 * compute the gaps. Both inputs as ReadonlySet for invariance.
 *
 * Knuth-style invariant: every input role appears in exactly one of
 * (matched, missingPrompts) and every prompt key appears in exactly
 * one of (matched, orphanedPrompts). No silent drops.
 */
export function findRoleCoverageGaps(
  schemaRoles: ReadonlySet<string>,
  promptKeys: ReadonlySet<string>,
): RoleCoverageGaps {
  const missingPrompts = [...schemaRoles].filter((r) => !promptKeys.has(r)).sort();
  const orphanedPrompts = [...promptKeys].filter((k) => !schemaRoles.has(k)).sort();
  return { missingPrompts, orphanedPrompts };
}

// ── I/O wrappers ───────────────────────────────────────────────────

/**
 * Dynamic-import RoleSchema from @chitin/contracts and return its
 * enum option set. Reading the resolved zod schema (vs parsing the
 * .ts file as text) means the linter sees exactly what the dispatcher
 * sees.
 */
export async function loadRoleSchemaValues(): Promise<Set<string>> {
  const mod = await import('@chitin/contracts');
  const schema = (mod as { RoleSchema?: unknown }).RoleSchema;
  if (!schema || typeof schema !== 'object') {
    throw new Error('@chitin/contracts did not export a RoleSchema');
  }
  // zod enum exposes options as `.options`; defensive against shape
  // changes via a fallback to the legacy `.values` array.
  const opts = (schema as { options?: unknown; values?: unknown }).options
    ?? (schema as { values?: unknown }).values;
  if (!Array.isArray(opts)) {
    throw new Error('RoleSchema.options is not an array (zod shape changed?)');
  }
  return new Set(opts.filter((v): v is string => typeof v === 'string'));
}

/**
 * Dynamic-import role-prompts.ts and pull the ROLE_PROMPTS keyset out
 * of its `__test__` re-export (already exposed for vitest fixtures —
 * piggybacking on it keeps role-prompts.ts's public surface clean).
 */
export async function loadRolePromptKeys(rolePromptsPath: string): Promise<Set<string>> {
  const url = pathToFileURL(resolve(rolePromptsPath)).href;
  const mod = await import(url) as { __test__?: { ROLE_VOCAB?: unknown } };
  const vocab = mod.__test__?.ROLE_VOCAB;
  if (!(vocab instanceof Set)) {
    throw new Error(`role-prompts.ts did not export __test__.ROLE_VOCAB as a Set (got ${typeof vocab})`);
  }
  return vocab as Set<string>;
}

// ── main entrypoint ────────────────────────────────────────────────

export interface LintResult {
  ok: boolean;
  missingPrompts: string[];
  orphanedPrompts: string[];
}

export async function lintRoleCoverage(opts: {
  rolePromptsPath: string;
}): Promise<LintResult> {
  const [schemaRoles, promptKeys] = await Promise.all([
    loadRoleSchemaValues(),
    loadRolePromptKeys(opts.rolePromptsPath),
  ]);
  const gaps = findRoleCoverageGaps(schemaRoles, promptKeys);
  return {
    ok: gaps.missingPrompts.length === 0 && gaps.orphanedPrompts.length === 0,
    ...gaps,
  };
}

async function main(): Promise<void> {
  const root = process.cwd();
  const rolePromptsPath = resolve(root, 'apps/temporal-worker/src/role-prompts.ts');
  const result = await lintRoleCoverage({ rolePromptsPath });

  if (result.missingPrompts.length > 0) {
    console.error('role-coverage: ERROR — Role enum values without ROLE_PROMPTS entries:');
    for (const role of result.missingPrompts) {
      console.error(`  ${role} (in RoleSchema but no prompt builder registered)`);
    }
    console.error('');
    console.error(
      'Add a builder entry to ROLE_PROMPTS in ' +
      'apps/temporal-worker/src/role-prompts.ts',
    );
  }

  if (result.orphanedPrompts.length > 0) {
    console.error('role-coverage: ERROR — ROLE_PROMPTS keys without matching RoleSchema entries:');
    for (const key of result.orphanedPrompts) {
      console.error(`  ${key} (prompt builder registered but role not in schema)`);
    }
    console.error('');
    console.error(
      'Either remove the orphan from ROLE_PROMPTS or add the role to ' +
      'RoleSchema in libs/contracts/src/execution-request.schema.ts',
    );
  }

  if (result.ok) {
    console.error('role-coverage: ok (RoleSchema and ROLE_PROMPTS are symmetric)');
  }

  process.exit(result.ok ? 0 : 1);
}

const isCli = fileURLToPath(import.meta.url) === process.argv[1];
if (isCli) {
  main().catch((err: unknown) => {
    console.error('role-coverage: fatal:', err instanceof Error ? err.message : err);
    process.exit(1);
  });
}
