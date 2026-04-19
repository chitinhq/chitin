// Narrow ESLint config — runs the @nx/enforce-module-boundaries rule only.
// Oxlint (via .oxlintrc.json) is the primary fast-pass linter; this ESLint
// exists solely to enforce Nx layer-tag dependency constraints because Oxlint
// does not support custom ESLint plugin rules.
import nx from '@nx/eslint-plugin';

/** @type {import('eslint').Linter.Config[]} */
export default [
  ...nx.configs['flat/base'],
  ...nx.configs['flat/typescript'],
  ...nx.configs['flat/javascript'],
  {
    ignores: ['**/dist', '**/.nx', 'go/**', 'python/**', '**/node_modules'],
  },
  {
    files: ['**/*.ts', '**/*.tsx', '**/*.js', '**/*.jsx'],
    rules: {
      '@nx/enforce-module-boundaries': [
        'error',
        {
          enforceBuildableLibDependency: true,
          allow: [],
          depConstraints: [
            { sourceTag: 'layer:contracts', onlyDependOnLibsWithTags: [] },
            { sourceTag: 'layer:telemetry', onlyDependOnLibsWithTags: ['layer:contracts'] },
            { sourceTag: 'layer:adapter',   onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry'] },
            { sourceTag: 'layer:cli',       onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry'] },
            { sourceTag: 'layer:kernel',    onlyDependOnLibsWithTags: [] },
          ],
        },
      ],
    },
  },
];
