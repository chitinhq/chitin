// Nx generator: @chitin/workspace-lib
//
// Scaffolds a chitin-flavored library under libs/<name>/ and ensures
// eslint.config.mjs carries a matching depConstraint for the layer.
//
// CLI usage (from workspace root):
//   tsx tools/generators/workspace-lib/index.ts <name> --layer <layer> [--allows-deps <layers>]
//
// Nx usage (once pnpm-workspace.yaml includes tools/generators/*):
//   nx g @chitin/workspace-lib <name> --layer <layer> [--allowsDeps <layers>]

import type { Tree } from '@nx/devkit';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// ── Types ─────────────────────────────────────────────────────────────

export interface WorkspaceLibSchema {
  name: string;
  layer: string;
  /** Comma-separated layer names (without 'layer:' prefix). */
  allowsDeps?: string;
}

// ── Pure helpers ──────────────────────────────────────────────────────

/** Normalise an allowsDeps string → sorted 'layer:X' array. */
export function parseAllowsDeps(raw: string | undefined): string[] {
  const base = raw ?? 'contracts,telemetry';
  return base
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => (s.startsWith('layer:') ? s : `layer:${s}`))
    .sort();
}

/** Generate the package.json content for a new lib. */
export function buildPackageJson(name: string, layer: string): string {
  const obj = {
    name: `@chitin/${name}`,
    version: '0.0.1',
    private: true,
    type: 'module',
    main: './src/index.ts',
    types: './src/index.ts',
    exports: {
      '.': {
        types: './src/index.ts',
        import: './src/index.ts',
        default: './src/index.ts',
      },
      './package.json': './package.json',
    },
    scripts: { test: 'vitest run tests' },
    nx: {
      tags: [`layer:${layer}`],
      targets: {
        test: {
          executor: 'nx:run-commands',
          options: {
            command: `pnpm exec vitest run libs/${name}/tests`,
            cwd: '{workspaceRoot}',
          },
        },
      },
    },
    dependencies: {},
  };
  return JSON.stringify(obj, null, 2) + '\n';
}

/** Generate the tsconfig.json (reference config) content. */
export function buildTsconfig(depth: number): string {
  const base = '../'.repeat(depth) + 'tsconfig.base.json';
  const obj = {
    extends: base,
    files: [],
    include: [],
    references: [{ path: './tsconfig.lib.json' }],
  };
  return JSON.stringify(obj, null, 2) + '\n';
}

/** Generate the tsconfig.lib.json (build config) content. */
export function buildTsconfigLib(depth: number): string {
  const base = '../'.repeat(depth) + 'tsconfig.base.json';
  const obj = {
    extends: base,
    compilerOptions: {
      baseUrl: '.',
      rootDir: 'src',
      outDir: 'dist',
      tsBuildInfoFile: 'dist/tsconfig.lib.tsbuildinfo',
      emitDeclarationOnly: true,
      forceConsistentCasingInFileNames: true,
      types: ['node'],
    },
    include: ['src/**/*.ts'],
    references: [],
  };
  return JSON.stringify(obj, null, 2) + '\n';
}

/**
 * Insert a new depConstraint into eslint.config.mjs content.
 *
 * Invariant: the returned string contains exactly one entry for
 * `layer:<layer>` in depConstraints — either it was already there
 * (no-op) or it has been added before the closing `],`.
 */
export function insertDepConstraint(
  eslintContent: string,
  layer: string,
  allowsDeps: string[],
): string {
  const sourceTag = `layer:${layer}`;

  // Idempotency: already present → return unchanged.
  if (eslintContent.includes(`sourceTag: '${sourceTag}'`)) {
    return eslintContent;
  }

  const depsLiteral = allowsDeps.map((d) => `'${d}'`).join(', ');
  const newLine = `            { sourceTag: '${sourceTag}',      onlyDependOnLibsWithTags: [${depsLiteral}] },`;

  // The depConstraints array closes with "          ]," (10-space indent).
  // That exact indent pattern is unique in the file.
  const CLOSING = '          ],';
  const idx = eslintContent.indexOf(CLOSING);
  if (idx === -1) {
    throw new Error(
      'workspace-lib generator: cannot find depConstraints closing "]," in eslint.config.mjs',
    );
  }

  return eslintContent.slice(0, idx) + newLine + '\n' + eslintContent.slice(idx);
}

// ── Nx generator (Tree API) ───────────────────────────────────────────

export default async function workspaceLibGenerator(
  tree: Tree,
  opts: WorkspaceLibSchema,
): Promise<void> {
  const { name, layer } = opts;
  const allowsDeps = parseAllowsDeps(opts.allowsDeps);
  const libPath = `libs/${name}`;

  // Idempotency: if package.json exists with the right layer tag, skip.
  const pkgPath = `${libPath}/package.json`;
  if (tree.exists(pkgPath)) {
    const existing = JSON.parse(tree.read(pkgPath, 'utf-8') ?? '{}') as {
      nx?: { tags?: string[] };
    };
    if (existing.nx?.tags?.includes(`layer:${layer}`)) {
      console.log(`[workspace-lib] no-op: ${libPath} already has layer:${layer}`);
      return;
    }
  }

  // Depth from libs/<name> to workspace root is 2.
  const depth = 2;

  tree.write(pkgPath, buildPackageJson(name, layer));
  tree.write(`${libPath}/tsconfig.json`, buildTsconfig(depth));
  tree.write(`${libPath}/tsconfig.lib.json`, buildTsconfigLib(depth));
  tree.write(`${libPath}/src/index.ts`, `// @chitin/${name} public API\n`);
  tree.write(`${libPath}/tests/.gitkeep`, '');

  // Update eslint.config.mjs.
  const eslintPath = 'eslint.config.mjs';
  const eslintContent = tree.read(eslintPath, 'utf-8');
  if (!eslintContent) {
    throw new Error('workspace-lib generator: eslint.config.mjs not found at workspace root');
  }
  const updated = insertDepConstraint(eslintContent, layer, allowsDeps);
  if (updated !== eslintContent) {
    tree.write(eslintPath, updated);
    console.log(`[workspace-lib] added layer:${layer} to eslint.config.mjs`);
  }

  const { formatFiles } = await import('@nx/devkit');
  await formatFiles(tree);
  console.log(`[workspace-lib] scaffolded ${libPath}`);
}

// ── CLI entrypoint ────────────────────────────────────────────────────

interface CliOpts {
  name: string;
  layer: string;
  allowsDeps: string[];
}

function parseCliArgs(argv: string[]): CliOpts {
  const args = argv.slice(2);
  if (args.length === 0 || args[0].startsWith('-')) {
    console.error(
      'Usage: tsx tools/generators/workspace-lib/index.ts <name> --layer <layer> [--allows-deps <layers>]',
    );
    process.exit(1);
  }
  const name = args[0];
  let layer = '';
  let rawDeps: string | undefined;

  for (let i = 1; i < args.length; i++) {
    if (args[i] === '--layer') {
      layer = args[++i] ?? '';
    } else if (args[i] === '--allows-deps') {
      rawDeps = args[++i];
    }
  }

  if (!layer) {
    console.error('Error: --layer is required');
    process.exit(1);
  }

  return { name, layer, allowsDeps: parseAllowsDeps(rawDeps) };
}

function cliRun(): void {
  const opts = parseCliArgs(process.argv);
  const __filename = fileURLToPath(import.meta.url);
  const workspaceRoot = resolve(dirname(__filename), '../../..');

  const libDir = join(workspaceRoot, 'libs', opts.name);

  // Idempotency check.
  const pkgFile = join(libDir, 'package.json');
  if (existsSync(pkgFile)) {
    const existing = JSON.parse(readFileSync(pkgFile, 'utf-8')) as {
      nx?: { tags?: string[] };
    };
    if (existing.nx?.tags?.includes(`layer:${opts.layer}`)) {
      console.log(`[workspace-lib] no-op: libs/${opts.name} already has layer:${opts.layer}`);
      return;
    }
  }

  const depth = 2;
  mkdirSync(join(libDir, 'src'), { recursive: true });
  mkdirSync(join(libDir, 'tests'), { recursive: true });

  writeFileSync(pkgFile, buildPackageJson(opts.name, opts.layer));
  writeFileSync(join(libDir, 'tsconfig.json'), buildTsconfig(depth));
  writeFileSync(join(libDir, 'tsconfig.lib.json'), buildTsconfigLib(depth));
  writeFileSync(join(libDir, 'src', 'index.ts'), `// @chitin/${opts.name} public API\n`);
  writeFileSync(join(libDir, 'tests', '.gitkeep'), '');

  const eslintFile = join(workspaceRoot, 'eslint.config.mjs');
  const eslintContent = readFileSync(eslintFile, 'utf-8');
  const updated = insertDepConstraint(eslintContent, opts.layer, opts.allowsDeps);
  if (updated !== eslintContent) {
    writeFileSync(eslintFile, updated);
    console.log(`[workspace-lib] added layer:${opts.layer} to eslint.config.mjs`);
  } else {
    console.log(`[workspace-lib] layer:${opts.layer} already in eslint.config.mjs`);
  }

  console.log(`[workspace-lib] scaffolded libs/${opts.name}`);
}

// Run CLI only when invoked directly (not when imported as an Nx generator).
const isCli =
  typeof process !== 'undefined' &&
  process.argv[1] != null &&
  fileURLToPath(import.meta.url) === resolve(process.argv[1]);

if (isCli) {
  cliRun();
}
