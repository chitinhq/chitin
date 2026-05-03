// Structural linter: every `layer:*` nx tag in any workspace
// package.json has a matching `depConstraints` entry in
// `eslint.config.mjs`.
//
// Why: four separate Copilot review cycles (PRs #194 / #195 / #199 /
// #192-cli-edge) caught the same omission — adding a new layer tag
// without a corresponding depConstraint. The boundary rule then
// silently fails to constrain the new layer (or fails open / fails
// strict depending on @nx/enforce-module-boundaries' interpretation).
// This linter moves the catch from review-time to author-time.
//
// Pure functions are exported so the test suite can pin every branch
// of the symmetric-difference logic without standing up the
// filesystem walk.

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// ── Pure logic ──────────────────────────────────────────────────────

export interface PackageJsonShape {
  /** Path relative to repo root, for error messages. */
  path: string;
  /** Parsed JSON. Only `nx.tags` matters for this linter. */
  json: unknown;
}

/**
 * Extract `layer:*` tags from a package.json's `nx.tags` array.
 * Tolerant: returns an empty list if `nx.tags` is missing or not an
 * array (those packages aren't covered by the boundary rule, which
 * is fine — the linter's job is to spot OPT-IN omissions).
 */
export function extractLayerTagsFromPackageJson(
  pkg: PackageJsonShape,
): string[] {
  const json = pkg.json;
  if (!json || typeof json !== 'object') return [];
  const nxField = (json as { nx?: unknown }).nx;
  if (!nxField || typeof nxField !== 'object') return [];
  const tagsField = (nxField as { tags?: unknown }).tags;
  if (!Array.isArray(tagsField)) return [];
  return tagsField
    .filter((t): t is string => typeof t === 'string' && t.startsWith('layer:'));
}

/**
 * Pull the `sourceTag` strings from a `depConstraints` array — the
 * shape ESLint loads from `eslint.config.mjs` at runtime.
 *
 * Defensive: only tags starting with `layer:` are returned, mirroring
 * the linter's scope (other depConstraint shapes — e.g., `scope:*` —
 * are out of scope here).
 */
export function extractDepConstraintLayerTags(
  depConstraints: ReadonlyArray<{ sourceTag?: unknown }>,
): string[] {
  const out: string[] = [];
  for (const c of depConstraints) {
    if (typeof c.sourceTag === 'string' && c.sourceTag.startsWith('layer:')) {
      out.push(c.sourceTag);
    }
  }
  return out;
}

export interface CoverageGaps {
  /** layer:* tags that appear in some package.json but have no
   *  matching depConstraint. These are *errors* — the boundary rule
   *  isn't actually constraining the new layer. */
  missing: Array<{ tag: string; foundIn: string[] }>;
  /** layer:* tags in depConstraints that don't appear in any
   *  package.json. Allowed (warn only) — sometimes a layer is reserved
   *  for future use (e.g., `layer:kernel` exists but no JS package
   *  carries it; the Go kernel does). */
  orphaned: string[];
}

/**
 * Pure: given the workspace state (per-package layer tags +
 * depConstraint sourceTags), compute the coverage gaps.
 *
 * Knuth-style invariant: every input layer tag appears in exactly
 * one of `missing` or matches a depConstraint. No silent drops.
 */
export function findCoverageGaps(
  packageLayerTags: ReadonlyMap<string, readonly string[]>,
  depConstraintTags: readonly string[],
): CoverageGaps {
  const constraintSet = new Set(depConstraintTags);
  const missingByTag = new Map<string, string[]>();
  const usedTags = new Set<string>();

  for (const [pkgPath, tags] of packageLayerTags) {
    for (const tag of tags) {
      usedTags.add(tag);
      if (!constraintSet.has(tag)) {
        const list = missingByTag.get(tag) ?? [];
        list.push(pkgPath);
        missingByTag.set(tag, list);
      }
    }
  }

  const missing = [...missingByTag.entries()].map(([tag, foundIn]) => ({
    tag,
    foundIn: foundIn.sort(),
  }));
  missing.sort((a, b) => a.tag.localeCompare(b.tag));

  const orphaned = depConstraintTags
    .filter((t) => !usedTags.has(t))
    .sort();

  return { missing, orphaned };
}

// ── I/O wrappers ───────────────────────────────────────────────────

/**
 * Walk a workspace root for package.json files under `apps/` and
 * `libs/`, skipping node_modules and dist. Returns parsed JSON keyed
 * by repo-relative path.
 */
export function loadWorkspacePackageJsons(rootDir: string): PackageJsonShape[] {
  const targets = ['apps', 'libs'];
  const out: PackageJsonShape[] = [];
  for (const top of targets) {
    const topPath = join(rootDir, top);
    if (!safeIsDir(topPath)) continue;
    walkForPackageJsons(topPath, rootDir, out);
  }
  return out;
}

function walkForPackageJsons(
  dir: string,
  rootDir: string,
  out: PackageJsonShape[],
): void {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    if (entry === 'node_modules' || entry === 'dist' || entry === '.nx') continue;
    const full = join(dir, entry);
    let stat;
    try {
      stat = statSync(full);
    } catch {
      continue;
    }
    if (stat.isDirectory()) {
      walkForPackageJsons(full, rootDir, out);
    } else if (entry === 'package.json') {
      const rel = full.startsWith(rootDir + '/')
        ? full.slice(rootDir.length + 1)
        : full;
      try {
        const raw = readFileSync(full, 'utf8');
        out.push({ path: rel, json: JSON.parse(raw) });
      } catch {
        // Ignore unreadable / malformed package.json files; the
        // linter shouldn't fail the run because of unrelated bugs.
      }
    }
  }
}

function safeIsDir(p: string): boolean {
  try {
    return statSync(p).isDirectory();
  } catch {
    return false;
  }
}

/**
 * Load eslint.config.mjs at runtime and extract the depConstraints
 * array from the @nx/enforce-module-boundaries rule. Reading the
 * resolved value (vs parsing the file as text) means the linter sees
 * exactly what eslint sees.
 */
export async function loadDepConstraints(
  configPath: string,
): Promise<ReadonlyArray<{ sourceTag?: unknown }>> {
  const url = configPath.startsWith('file://')
    ? configPath
    : `file://${resolve(configPath)}`;
  const mod = await import(url) as { default: unknown };
  const config = mod.default;
  if (!Array.isArray(config)) {
    throw new Error(`eslint.config.mjs default export is not an array (got ${typeof config})`);
  }
  for (const block of config) {
    if (!block || typeof block !== 'object') continue;
    const rules = (block as { rules?: unknown }).rules;
    if (!rules || typeof rules !== 'object') continue;
    const rule = (rules as Record<string, unknown>)['@nx/enforce-module-boundaries'];
    if (!Array.isArray(rule) || rule.length < 2) continue;
    const opts = rule[1];
    if (!opts || typeof opts !== 'object') continue;
    const dc = (opts as { depConstraints?: unknown }).depConstraints;
    if (Array.isArray(dc)) {
      return dc as Array<{ sourceTag?: unknown }>;
    }
  }
  throw new Error('Could not find @nx/enforce-module-boundaries depConstraints in eslint.config.mjs');
}

// ── main entrypoint ────────────────────────────────────────────────

export interface LintResult {
  ok: boolean;
  missing: CoverageGaps['missing'];
  orphaned: CoverageGaps['orphaned'];
}

export async function lintLayerTagCoverage(opts: {
  rootDir: string;
  eslintConfigPath: string;
}): Promise<LintResult> {
  const pkgs = loadWorkspacePackageJsons(opts.rootDir);
  const tagsByPath = new Map<string, string[]>();
  for (const pkg of pkgs) {
    const tags = extractLayerTagsFromPackageJson(pkg);
    if (tags.length > 0) tagsByPath.set(pkg.path, tags);
  }
  const depConstraints = await loadDepConstraints(opts.eslintConfigPath);
  const dcTags = extractDepConstraintLayerTags(depConstraints);
  const gaps = findCoverageGaps(tagsByPath, dcTags);
  return {
    ok: gaps.missing.length === 0,
    missing: gaps.missing,
    orphaned: gaps.orphaned,
  };
}

async function main(): Promise<void> {
  const root = process.cwd();
  const eslintConfig = join(root, 'eslint.config.mjs');
  const result = await lintLayerTagCoverage({
    rootDir: root,
    eslintConfigPath: eslintConfig,
  });

  if (result.missing.length > 0) {
    console.error('layer-tag-coverage: ERROR — layer:* tags without depConstraints:');
    for (const { tag, foundIn } of result.missing) {
      console.error(`  ${tag}`);
      for (const path of foundIn) {
        console.error(`    used by ${path}`);
      }
    }
    console.error('');
    console.error(
      'Add the missing entries to `eslint.config.mjs` under ' +
      '`@nx/enforce-module-boundaries`.depConstraints.',
    );
  }

  if (result.orphaned.length > 0) {
    console.error('layer-tag-coverage: warning — depConstraints with no matching package:');
    for (const tag of result.orphaned) {
      console.error(`  ${tag} (defined but no package carries this tag yet)`);
    }
  }

  if (result.ok) {
    console.error(
      `layer-tag-coverage: ok (` +
      `${result.missing.length} errors, ` +
      `${result.orphaned.length} orphan warning${result.orphaned.length === 1 ? '' : 's'})`,
    );
  }

  process.exit(result.ok ? 0 : 1);
}

const isCli = fileURLToPath(import.meta.url) === process.argv[1];
if (isCli) {
  main().catch((err: unknown) => {
    console.error('layer-tag-coverage: fatal:', err instanceof Error ? err.message : err);
    process.exit(1);
  });
}
