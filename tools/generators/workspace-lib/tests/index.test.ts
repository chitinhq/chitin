import { describe, expect, it } from 'vitest';
import {
  buildPackageJson,
  buildTsconfig,
  buildTsconfigLib,
  insertDepConstraint,
  parseAllowsDeps,
} from '../index.ts';

// ── parseAllowsDeps ───────────────────────────────────────────────────

describe('parseAllowsDeps', () => {
  it('defaults to contracts + telemetry', () => {
    expect(parseAllowsDeps(undefined)).toEqual(['layer:contracts', 'layer:telemetry']);
  });

  it('preserves layer: prefix', () => {
    expect(parseAllowsDeps('layer:contracts,layer:telemetry')).toEqual([
      'layer:contracts',
      'layer:telemetry',
    ]);
  });

  it('adds layer: prefix when missing', () => {
    expect(parseAllowsDeps('contracts,governance')).toEqual([
      'layer:contracts',
      'layer:governance',
    ]);
  });

  it('sorts output', () => {
    expect(parseAllowsDeps('telemetry,contracts')).toEqual([
      'layer:contracts',
      'layer:telemetry',
    ]);
  });

  it('ignores empty segments', () => {
    expect(parseAllowsDeps('contracts,,telemetry')).toEqual([
      'layer:contracts',
      'layer:telemetry',
    ]);
  });
});

// ── buildPackageJson ──────────────────────────────────────────────────

describe('buildPackageJson', () => {
  it('sets correct name and layer tag', () => {
    const result = JSON.parse(buildPackageJson('my-lib', 'my-layer'));
    expect(result.name).toBe('@chitin/my-lib');
    expect(result.nx.tags).toContain('layer:my-layer');
  });

  it('includes the test target pointing to libs/<name>/tests', () => {
    const result = JSON.parse(buildPackageJson('scheduler', 'scheduler'));
    expect(result.nx.targets.test.options.command).toContain('libs/scheduler/tests');
  });

  it('ends with a trailing newline', () => {
    expect(buildPackageJson('x', 'y').endsWith('\n')).toBe(true);
  });
});

// ── buildTsconfig ─────────────────────────────────────────────────────

describe('buildTsconfig', () => {
  it('extends with correct relative path for depth 2', () => {
    const result = JSON.parse(buildTsconfig(2));
    expect(result.extends).toBe('../../tsconfig.base.json');
  });

  it('references tsconfig.lib.json', () => {
    const result = JSON.parse(buildTsconfig(2));
    expect(result.references).toEqual([{ path: './tsconfig.lib.json' }]);
  });
});

// ── buildTsconfigLib ──────────────────────────────────────────────────

describe('buildTsconfigLib', () => {
  it('extends with correct relative path for depth 2', () => {
    const result = JSON.parse(buildTsconfigLib(2));
    expect(result.extends).toBe('../../tsconfig.base.json');
  });

  it('sets rootDir=src and outDir=dist', () => {
    const result = JSON.parse(buildTsconfigLib(2));
    expect(result.compilerOptions.rootDir).toBe('src');
    expect(result.compilerOptions.outDir).toBe('dist');
  });

  it('includes only src/**/*.ts', () => {
    const result = JSON.parse(buildTsconfigLib(2));
    expect(result.include).toEqual(['src/**/*.ts']);
  });
});

// ── insertDepConstraint ───────────────────────────────────────────────

// A minimal eslint.config.mjs fixture that mirrors the real file's
// depConstraints shape.
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

describe('insertDepConstraint', () => {
  it('inserts a new constraint before the closing ],', () => {
    const result = insertDepConstraint(
      ESLINT_FIXTURE,
      'scheduler',
      ['layer:contracts', 'layer:telemetry'],
    );
    expect(result).toContain("sourceTag: 'layer:scheduler'");
    // New entry is before the closing ],
    const insertIdx = result.indexOf("sourceTag: 'layer:scheduler'");
    const closingIdx = result.indexOf('          ],');
    expect(insertIdx).toBeLessThan(closingIdx);
  });

  it('is idempotent — second call returns the same string', () => {
    const once = insertDepConstraint(
      ESLINT_FIXTURE,
      'scheduler',
      ['layer:contracts', 'layer:telemetry'],
    );
    const twice = insertDepConstraint(
      once,
      'scheduler',
      ['layer:contracts', 'layer:telemetry'],
    );
    expect(twice).toBe(once);
  });

  it('does not touch unrelated layers', () => {
    const result = insertDepConstraint(
      ESLINT_FIXTURE,
      'governance',
      ['layer:contracts', 'layer:telemetry'],
    );
    expect(result).toContain("sourceTag: 'layer:contracts'");
    expect(result).toContain("sourceTag: 'layer:telemetry'");
    expect(result).toContain("sourceTag: 'layer:governance'");
  });

  it('formats deps as quoted list', () => {
    const result = insertDepConstraint(
      ESLINT_FIXTURE,
      'slack',
      ['layer:contracts', 'layer:telemetry'],
    );
    expect(result).toContain("['layer:contracts', 'layer:telemetry']");
  });

  it('supports empty deps (e.g. contracts layer itself)', () => {
    const result = insertDepConstraint(ESLINT_FIXTURE, 'kernel', []);
    expect(result).toContain("sourceTag: 'layer:kernel'");
    expect(result).toContain('onlyDependOnLibsWithTags: []');
  });

  it('throws when no depConstraints closing bracket is found', () => {
    expect(() =>
      insertDepConstraint('no closing bracket here', 'x', []),
    ).toThrow('workspace-lib generator');
  });
});
