// Structural linter: every `layer:*` and `scope:*` nx tag in any
// workspace project metadata has a matching `depConstraints` entry in
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
import { fileURLToPath, pathToFileURL } from 'node:url';

// ── Pure logic ──────────────────────────────────────────────────────

export interface PackageJsonShape {
  /** Path relative to repo root, for error messages. */
  path: string;
  /** Parsed JSON. The linter inspects `nx.tags` (package.json shape)
   *  AND `tags` at the root (project.json shape) — both are valid
   *  Nx tag locations. */
  json: unknown;
}

/**
 * Extract `layer:*` tags from a package.json's `nx.tags` array OR a
 * project.json's root-level `tags` array. Both are valid in nx —
 * libs/adapters/* in this repo use project.json; libs/* + apps/* use
 * package.json. Walking both keeps the linter from missing the
 * adapters' tags (and any future project.json-shaped projects).
 *
 * Tolerant: returns an empty list if neither location has tags. Those
 * packages aren't covered by the boundary rule, which is fine — the
 * linter's job is to spot OPT-IN omissions.
 */
export function extractTagsFromPackageJson(
  pkg: PackageJsonShape,
  prefix: string,
): string[] {
  const json = pkg.json;
  if (!json || typeof json !== 'object') return [];
  const tagSources: unknown[] = [];

  // package.json shape: nx.tags
  const nxField = (json as { nx?: unknown }).nx;
  if (nxField && typeof nxField === 'object') {
    const t = (nxField as { tags?: unknown }).tags;
    if (Array.isArray(t)) tagSources.push(...t);
  }

  // project.json shape: root-level tags
  const rootTags = (json as { tags?: unknown }).tags;
  if (Array.isArray(rootTags)) tagSources.push(...rootTags);

  return tagSources.filter(
    (t): t is string => typeof t === 'string' && t.startsWith(prefix),
  );
}

export function extractLayerTagsFromPackageJson(
  pkg: PackageJsonShape,
): string[] {
  return extractTagsFromPackageJson(pkg, 'layer:');
}

export function extractScopeTagsFromPackageJson(
  pkg: PackageJsonShape,
): string[] {
  return extractTagsFromPackageJson(pkg, 'scope:');
}

/**
 * Pull the `sourceTag` strings from a `depConstraints` array — the
 * shape ESLint loads from `eslint.config.mjs` at runtime.
 *
 * Defensive: only tags starting with `layer:` are returned, mirroring
 * the linter's scope (other depConstraint shapes — e.g., `scope:*` —
 * are out of scope here).
 */
export function extractDepConstraintTags(
  depConstraints: ReadonlyArray<{ sourceTag?: unknown }>,
  prefix: string,
): string[] {
  const out: string[] = [];
  for (const c of depConstraints) {
    if (typeof c.sourceTag === 'string' && c.sourceTag.startsWith(prefix)) {
      out.push(c.sourceTag);
    }
  }
  return out;
}

export function extractDepConstraintLayerTags(
  depConstraints: ReadonlyArray<{ sourceTag?: unknown }>,
): string[] {
  return extractDepConstraintTags(depConstraints, 'layer:');
}

export function extractDepConstraintScopeTags(
  depConstraints: ReadonlyArray<{ sourceTag?: unknown }>,
): string[] {
  return extractDepConstraintTags(depConstraints, 'scope:');
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
 * Walk a workspace root for package.json AND project.json files
 * under `apps/`, `libs/`, `tools/`, `go/`, and `python/`, skipping node_modules
 * and dist. Returns parsed JSON keyed by repo-relative path.
 *
 * Both file shapes are walked because Nx accepts tags in either:
 * package.json (`nx.tags`) for inferred projects, project.json (root
 * `tags`) for explicit ones. Limiting to one location would silently
 * skip the other — exactly the omission the linter is supposed to catch.
 */
export function loadWorkspacePackageJsons(rootDir: string): PackageJsonShape[] {
  // Top-level dirs that can contain workspace projects. Includes
  // `tools/` (this lib lives there), `go/` (the Go kernel carries
  // an Nx project.json with layer:kernel), and `python/` (the analysis
  // library carries layer:analysis); otherwise those tags are
  // mis-reported as orphaned.
  const targets = ['apps', 'libs', 'tools', 'go', 'python'];
  const out: PackageJsonShape[] = [];
  for (const top of targets) {
    const topPath = join(rootDir, top);
    if (!safeIsDir(topPath)) continue;
    walkForProjectFiles(topPath, rootDir, out);
  }
  return out;
}

function walkForProjectFiles(
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
      walkForProjectFiles(full, rootDir, out);
    } else if (entry === 'package.json' || entry === 'project.json') {
      const rel = full.startsWith(rootDir + '/')
        ? full.slice(rootDir.length + 1)
        : full;
      try {
        const raw = readFileSync(full, 'utf8');
        out.push({ path: rel, json: JSON.parse(raw) });
      } catch {
        // Ignore unreadable / malformed files; the linter shouldn't
        // fail the run because of unrelated bugs.
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
  // pathToFileURL handles cross-platform encoding correctly. A naïve
  // `file://${resolve(configPath)}` produces `file://C:\...` on
  // Windows, which Node's import() rejects.
  const url = configPath.startsWith('file://')
    ? configPath
    : pathToFileURL(resolve(configPath)).href;
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
  layer: CoverageGaps;
  scope: CoverageGaps;
}

export async function lintLayerTagCoverage(opts: {
  rootDir: string;
  eslintConfigPath: string;
}): Promise<LintResult> {
  const pkgs = loadWorkspacePackageJsons(opts.rootDir);
  const layerTagsByPath = new Map<string, string[]>();
  const scopeTagsByPath = new Map<string, string[]>();
  for (const pkg of pkgs) {
    const layerTags = extractLayerTagsFromPackageJson(pkg);
    if (layerTags.length > 0) layerTagsByPath.set(pkg.path, layerTags);
    const scopeTags = extractScopeTagsFromPackageJson(pkg);
    if (scopeTags.length > 0) scopeTagsByPath.set(pkg.path, scopeTags);
  }
  const depConstraints = await loadDepConstraints(opts.eslintConfigPath);
  const layerGaps = findCoverageGaps(
    layerTagsByPath,
    extractDepConstraintLayerTags(depConstraints),
  );
  const scopeGaps = findCoverageGaps(
    scopeTagsByPath,
    extractDepConstraintScopeTags(depConstraints),
  );
  return {
    ok: layerGaps.missing.length === 0 && scopeGaps.missing.length === 0,
    layer: layerGaps,
    scope: scopeGaps,
  };
}

function printCoverageResult(
  label: string,
  gaps: CoverageGaps,
): void {
  if (gaps.missing.length > 0) {
    console.error(`${label}: ERROR — ${label.split(':')[0]} tags without depConstraints:`);
    for (const { tag, foundIn } of gaps.missing) {
      console.error(`  ${tag}`);
      for (const path of foundIn) {
        console.error(`    used by ${path}`);
      }
    }
    console.error('');
  }

  if (gaps.orphaned.length > 0) {
    console.error(`${label}: warning — depConstraints with no matching package:`);
    for (const tag of gaps.orphaned) {
      console.error(`  ${tag} (defined but no package carries this tag yet)`);
    }
  }
}

async function main(): Promise<void> {
  const root = process.cwd();
  const eslintConfig = join(root, 'eslint.config.mjs');
  const result = await lintLayerTagCoverage({
    rootDir: root,
    eslintConfigPath: eslintConfig,
  });

  printCoverageResult('layer-tag-coverage', result.layer);
  printCoverageResult('scope-tag-coverage', result.scope);

  if (result.layer.missing.length > 0 || result.scope.missing.length > 0) {
    console.error('');
    console.error(
      'Add the missing entries to `eslint.config.mjs` under ' +
      '`@nx/enforce-module-boundaries`.depConstraints.',
    );
  }

  if (result.ok) {
    console.error(
      `tag-coverage: ok (` +
      `${result.layer.missing.length + result.scope.missing.length} errors, ` +
      `${result.layer.orphaned.length + result.scope.orphaned.length} orphan warning${
        result.layer.orphaned.length + result.scope.orphaned.length === 1 ? '' : 's'
      })`,
    );
  }

  process.exit(result.ok ? 0 : 1);
}

const isCli = fileURLToPath(import.meta.url) === process.argv[1];
if (isCli) {
  main().catch((err: unknown) => {
    console.error('tag-coverage: fatal:', err instanceof Error ? err.message : err);
    process.exit(1);
  });
}
