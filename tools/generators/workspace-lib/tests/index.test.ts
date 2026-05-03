// Test runner: tsx tools/generators/workspace-lib/tests/index.test.ts
// Uses node:assert — no vitest required (package not in pnpm-workspace yet).

import assert from 'node:assert/strict';
import {
  buildPackageJson,
  buildTsconfig,
  buildTsconfigLib,
  insertDepConstraint,
  parseAllowsDeps,
} from '../index.ts';

let passed = 0;
let failed = 0;

function test(name: string, fn: () => void): void {
  try {
    fn();
    console.log(`  ✓ ${name}`);
    passed++;
  } catch (err) {
    console.error(`  ✗ ${name}`);
    console.error(`    ${err instanceof Error ? err.message : String(err)}`);
    failed++;
  }
}

// ── parseAllowsDeps ───────────────────────────────────────────────────

test('parseAllowsDeps: defaults to contracts + telemetry', () => {
  assert.deepEqual(parseAllowsDeps(undefined), ['layer:contracts', 'layer:telemetry']);
});

test('parseAllowsDeps: preserves layer: prefix', () => {
  assert.deepEqual(parseAllowsDeps('layer:contracts,layer:telemetry'), [
    'layer:contracts',
    'layer:telemetry',
  ]);
});

test('parseAllowsDeps: adds layer: prefix when missing', () => {
  assert.deepEqual(parseAllowsDeps('contracts,governance'), [
    'layer:contracts',
    'layer:governance',
  ]);
});

test('parseAllowsDeps: sorts output', () => {
  assert.deepEqual(parseAllowsDeps('telemetry,contracts'), [
    'layer:contracts',
    'layer:telemetry',
  ]);
});

test('parseAllowsDeps: ignores empty segments', () => {
  assert.deepEqual(parseAllowsDeps('contracts,,telemetry'), [
    'layer:contracts',
    'layer:telemetry',
  ]);
});

// ── buildPackageJson ──────────────────────────────────────────────────

test('buildPackageJson: sets correct name and layer tag', () => {
  const result = JSON.parse(buildPackageJson('my-lib', 'my-layer')) as {
    name: string;
    nx: { tags: string[] };
  };
  assert.equal(result.name, '@chitin/my-lib');
  assert.ok(result.nx.tags.includes('layer:my-layer'));
});

test('buildPackageJson: test target points to libs/<name>/tests', () => {
  const result = JSON.parse(buildPackageJson('scheduler', 'scheduler')) as {
    nx: { targets: { test: { options: { command: string } } } };
  };
  assert.ok(result.nx.targets.test.options.command.includes('libs/scheduler/tests'));
});

test('buildPackageJson: ends with trailing newline', () => {
  assert.ok(buildPackageJson('x', 'y').endsWith('\n'));
});

// ── buildTsconfig ─────────────────────────────────────────────────────

test('buildTsconfig: extends with correct path for depth 2', () => {
  const result = JSON.parse(buildTsconfig(2)) as { extends: string };
  assert.equal(result.extends, '../../tsconfig.base.json');
});

test('buildTsconfig: references tsconfig.lib.json', () => {
  const result = JSON.parse(buildTsconfig(2)) as {
    references: Array<{ path: string }>;
  };
  assert.deepEqual(result.references, [{ path: './tsconfig.lib.json' }]);
});

// ── buildTsconfigLib ──────────────────────────────────────────────────

test('buildTsconfigLib: extends with correct path for depth 2', () => {
  const result = JSON.parse(buildTsconfigLib(2)) as { extends: string };
  assert.equal(result.extends, '../../tsconfig.base.json');
});

test('buildTsconfigLib: sets rootDir=src and outDir=dist', () => {
  const result = JSON.parse(buildTsconfigLib(2)) as {
    compilerOptions: { rootDir: string; outDir: string };
  };
  assert.equal(result.compilerOptions.rootDir, 'src');
  assert.equal(result.compilerOptions.outDir, 'dist');
});

test('buildTsconfigLib: includes only src/**/*.ts', () => {
  const result = JSON.parse(buildTsconfigLib(2)) as { include: string[] };
  assert.deepEqual(result.include, ['src/**/*.ts']);
});

// ── insertDepConstraint ───────────────────────────────────────────────

const ESLINT_FIXTURE = `import nx from '@nx/eslint-plugin';

export default [
  ...nx.configs['flat/base'],
  {
    files: ['**/*.ts'],
    rules: {
      '@nx/enforce-module-boundaries': [
        'error',
        {
          enforceBuildableLibDependency: true,
          allow: [],
          depConstraints: [
            { sourceTag: 'layer:contracts',  onlyDependOnLibsWithTags: [] },
            { sourceTag: 'layer:telemetry',  onlyDependOnLibsWithTags: ['layer:contracts'] },
          ],
        },
      ],
    },
  },
];
`;

test('insertDepConstraint: inserts before the closing ],', () => {
  const result = insertDepConstraint(ESLINT_FIXTURE, 'scheduler', [
    'layer:contracts',
    'layer:telemetry',
  ]);
  assert.ok(result.includes("sourceTag: 'layer:scheduler'"));
  const insertIdx = result.indexOf("sourceTag: 'layer:scheduler'");
  const closingIdx = result.indexOf('          ],');
  assert.ok(insertIdx < closingIdx, 'new entry must appear before closing ],');
});

test('insertDepConstraint: is idempotent', () => {
  const once = insertDepConstraint(ESLINT_FIXTURE, 'scheduler', [
    'layer:contracts',
    'layer:telemetry',
  ]);
  const twice = insertDepConstraint(once, 'scheduler', [
    'layer:contracts',
    'layer:telemetry',
  ]);
  assert.equal(twice, once);
});

test('insertDepConstraint: does not remove existing layers', () => {
  const result = insertDepConstraint(ESLINT_FIXTURE, 'governance', [
    'layer:contracts',
    'layer:telemetry',
  ]);
  assert.ok(result.includes("sourceTag: 'layer:contracts'"));
  assert.ok(result.includes("sourceTag: 'layer:telemetry'"));
  assert.ok(result.includes("sourceTag: 'layer:governance'"));
});

test('insertDepConstraint: formats deps as quoted list', () => {
  const result = insertDepConstraint(ESLINT_FIXTURE, 'slack', [
    'layer:contracts',
    'layer:telemetry',
  ]);
  assert.ok(result.includes("['layer:contracts', 'layer:telemetry']"));
});

test('insertDepConstraint: supports empty deps array', () => {
  const result = insertDepConstraint(ESLINT_FIXTURE, 'kernel', []);
  assert.ok(result.includes("sourceTag: 'layer:kernel'"));
  assert.ok(result.includes('onlyDependOnLibsWithTags: []'));
});

test('insertDepConstraint: throws when no closing bracket found', () => {
  assert.throws(
    () => insertDepConstraint('no closing bracket here', 'x', []),
    /workspace-lib generator/,
  );
});

// ── Summary ───────────────────────────────────────────────────────────

console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
