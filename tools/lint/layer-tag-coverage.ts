// Structural linter for the chitin Nx workspace. Three enforced
// invariants, all shifted from review-time to author-time:
//
//   1. Tag-coverage — every `layer:*` and `scope:*` nx tag carried by
//      any project has a matching `depConstraints` entry in
//      `eslint.config.mjs`. Four separate Copilot review cycles (PRs
//      #194 / #195 / #199 / #192-cli-edge) caught the same omission:
//      adding a layer tag without a depConstraint, so the boundary
//      rule silently fails to constrain the new layer.
//
//   2. Single mechanism (spec 074, FR-004 / INV-004) — every project
//      registers via `project.json`, never the `package.json` `nx`
//      field. An `nx` field is a second mechanism and a violation.
//
//   3. Project convention (spec 074, FR-002 / FR-003 / FR-005) — every
//      project.json carries the full `type:/scope:/layer:/lang:` tag
//      set and declares a `validate` target. `validate` is the
//      universal verification target every project runs in CI; each
//      project's `validate` composes its own build/lint/test, so
//      enforcing its presence enforces that the project is verified.
//
// Pure functions are exported so the test suite can pin every branch
// of the logic without standing up the filesystem walk.

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
 * Defensive: only tags starting with `prefix` are returned, so callers
 * can ask for `layer:` or `scope:` (or any other namespace) without
 * picking up unrelated tags.
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

// ── Project convention (spec 074) ──────────────────────────────────

/**
 * The four tag namespaces every project MUST carry (FR-003). The gate
 * checks only *presence* of each namespace, not the chosen value.
 */
export const REQUIRED_TAG_NAMESPACES = [
  'type:',
  'scope:',
  'layer:',
  'lang:',
] as const;

/**
 * The one target every project MUST declare (FR-002). `validate` is the
 * umbrella: Go's runs `vet && test`, Python's `compileall && pytest`,
 * TS's `tsc && vitest`, and CI runs `nx affected -t validate`. Other
 * standard targets (build/test/lint) are present where they apply; the
 * gate keys on `validate` because its presence is the provable
 * invariant that the project is verified at all.
 */
export const REQUIRED_TARGET = 'validate';

/** A project as seen by the convention checker — one project.json. */
export interface ProjectShape {
  /** Repo-relative path to the project.json. */
  path: string;
  /** Project name (project.json `name`), or the dir path if unnamed. */
  name: string;
  /** Root-level `tags` array. */
  tags: string[];
  /** Declared target names (`targets` object keys). */
  targetNames: string[];
}

/** A project that fails the convention gate. */
export interface ConventionViolation {
  /** Project name (for the error message). */
  project: string;
  /** Repo-relative project.json path. */
  path: string;
  /** Required tag namespaces with no carrying tag, in canonical order. */
  missingTagNamespaces: string[];
  /** Required targets not declared. */
  missingTargets: string[];
}

/**
 * Project the parsed JSON of a project.json file onto a ProjectShape.
 * Returns null for any file that is not a project.json — package.json
 * files are handled by the single-mechanism check, not here.
 *
 * Tolerant of malformed shapes: a missing/non-array `tags` becomes
 * `[]`, a missing/non-object `targets` becomes `[]`. Such a project
 * then surfaces as a violation (everything missing) rather than
 * crashing the run.
 */
export function toProjectShape(pkg: PackageJsonShape): ProjectShape | null {
  if (!pkg.path.endsWith('project.json')) return null;
  const json = (pkg.json && typeof pkg.json === 'object' ? pkg.json : {}) as {
    name?: unknown;
    tags?: unknown;
    targets?: unknown;
  };
  const dirPath = pkg.path.slice(0, -'/project.json'.length);
  const name =
    typeof json.name === 'string' && json.name.length > 0
      ? json.name
      : dirPath;
  const tags = Array.isArray(json.tags)
    ? json.tags.filter((t): t is string => typeof t === 'string')
    : [];
  const targetNames =
    json.targets && typeof json.targets === 'object'
      ? Object.keys(json.targets as Record<string, unknown>)
      : [];
  return { path: pkg.path, name, tags, targetNames };
}

/**
 * Pure: given every project.json shape, find those missing a required
 * tag namespace or the `validate` target.
 *
 * Knuth-style invariant: a project appears in the result IFF it is
 * missing at least one required namespace or target, and a returned
 * violation always lists ≥1 concrete gap. Output is sorted by project
 * path so two runs over the same workspace are byte-identical.
 */
export function findConventionViolations(
  projects: readonly ProjectShape[],
): ConventionViolation[] {
  const violations: ConventionViolation[] = [];
  for (const project of projects) {
    const missingTagNamespaces: string[] = REQUIRED_TAG_NAMESPACES.filter(
      (ns) => !project.tags.some((t) => t.startsWith(ns)),
    );
    const missingTargets = project.targetNames.includes(REQUIRED_TARGET)
      ? []
      : [REQUIRED_TARGET];
    if (missingTagNamespaces.length > 0 || missingTargets.length > 0) {
      violations.push({
        project: project.name,
        path: project.path,
        missingTagNamespaces,
        missingTargets,
      });
    }
  }
  violations.sort((a, b) => a.path.localeCompare(b.path));
  return violations;
}

/**
 * Pure: find every package.json that still declares an `nx` field.
 * Spec 074 converges on project.json as the single registration
 * mechanism (FR-004, INV-004) — an `nx` field in package.json is a
 * second mechanism and a violation.
 *
 * Returns repo-relative paths, sorted for deterministic output.
 */
export function findNxFieldPackageJsons(
  pkgs: readonly PackageJsonShape[],
): string[] {
  const out: string[] = [];
  for (const pkg of pkgs) {
    if (!pkg.path.endsWith('package.json')) continue;
    const json = pkg.json;
    if (!json || typeof json !== 'object') continue;
    const nxField = (json as { nx?: unknown }).nx;
    if (nxField && typeof nxField === 'object') out.push(pkg.path);
  }
  return out.sort();
}

// ── I/O wrappers ───────────────────────────────────────────────────

/**
 * Walk a workspace root for package.json AND project.json files under
 * every top-level dir that can hold a project, skipping node_modules
 * and dist. Returns parsed JSON keyed by repo-relative path.
 *
 * Both file shapes are walked because Nx accepts tags in either:
 * package.json (`nx.tags`) for inferred projects, project.json (root
 * `tags`) for explicit ones. Limiting to one location would silently
 * skip the other — exactly the omission the linter is supposed to catch.
 */
export function loadWorkspacePackageJsons(rootDir: string): PackageJsonShape[] {
  // Every top-level dir that can contain a workspace project. `go/` and
  // `python/` carry Nx project.json files (the kernel/analysis); `tools/`
  // holds this lib; `services/`, `bench/`, and `swarm/` hold projects
  // registered by spec 074 Phase 1. A dir omitted here makes its
  // projects invisible to the gate — the registration gap this spec
  // closes (FR-005: coverage for *all* projects, every language).
  const targets = [
    'apps',
    'libs',
    'tools',
    'go',
    'python',
    'services',
    'bench',
    'swarm',
  ];
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
  /** Projects missing a required tag namespace or the validate target. */
  convention: ConventionViolation[];
  /** package.json files still declaring an `nx` field (FR-004). */
  nxFieldPackageJsons: string[];
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
  const projects = pkgs
    .map(toProjectShape)
    .filter((p): p is ProjectShape => p !== null);
  const convention = findConventionViolations(projects);
  const nxFieldPackageJsons = findNxFieldPackageJsons(pkgs);
  return {
    ok:
      layerGaps.missing.length === 0 &&
      scopeGaps.missing.length === 0 &&
      convention.length === 0 &&
      nxFieldPackageJsons.length === 0,
    layer: layerGaps,
    scope: scopeGaps,
    convention,
    nxFieldPackageJsons,
  };
}

function printCoverageResult(
  label: string,
  kind: string,
  gaps: CoverageGaps,
): void {
  if (gaps.missing.length > 0) {
    console.error(`${label}: ERROR — ${kind} tags without depConstraints:`);
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

function printConventionResult(result: LintResult): void {
  if (result.nxFieldPackageJsons.length > 0) {
    console.error(
      'single-mechanism: ERROR — package.json files declaring an `nx` field:',
    );
    for (const path of result.nxFieldPackageJsons) {
      console.error(`  ${path}`);
    }
    console.error(
      '  → move the Nx config to a project.json (spec 074 FR-004 / INV-004).',
    );
    console.error('');
  }

  if (result.convention.length > 0) {
    console.error(
      'project-convention: ERROR — projects missing a required tag or target:',
    );
    for (const v of result.convention) {
      console.error(`  ${v.project}  (${v.path})`);
      if (v.missingTagNamespaces.length > 0) {
        console.error(
          `    missing tag namespace(s): ${v.missingTagNamespaces.join(', ')}`,
        );
      }
      if (v.missingTargets.length > 0) {
        console.error(`    missing target(s): ${v.missingTargets.join(', ')}`);
      }
    }
    console.error(
      '  → every project carries type:/scope:/layer:/lang: tags and a ' +
        '`validate` target (spec 074 FR-002 / FR-003).',
    );
    console.error('');
  }
}

async function main(): Promise<void> {
  const root = process.cwd();
  const eslintConfig = join(root, 'eslint.config.mjs');
  const result = await lintLayerTagCoverage({
    rootDir: root,
    eslintConfigPath: eslintConfig,
  });

  printCoverageResult('layer-tag-coverage', 'layer', result.layer);
  printCoverageResult('scope-tag-coverage', 'scope', result.scope);
  printConventionResult(result);

  if (result.layer.missing.length > 0 || result.scope.missing.length > 0) {
    console.error('');
    console.error(
      'Add the missing entries to `eslint.config.mjs` under ' +
      '`@nx/enforce-module-boundaries`.depConstraints.',
    );
  }

  if (result.ok) {
    const orphans =
      result.layer.orphaned.length + result.scope.orphaned.length;
    console.error(
      `structural-lint: ok (0 errors, ${orphans} orphan warning` +
        `${orphans === 1 ? '' : 's'})`,
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
