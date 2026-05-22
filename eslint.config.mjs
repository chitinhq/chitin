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
            { sourceTag: 'layer:contracts',  onlyDependOnLibsWithTags: [] },
            { sourceTag: 'layer:telemetry',  onlyDependOnLibsWithTags: ['layer:contracts'] },
            { sourceTag: 'layer:analysis',   onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry'] },
            { sourceTag: 'layer:plugin-api', onlyDependOnLibsWithTags: ['layer:contracts'] },
            { sourceTag: 'layer:adapter',    onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry', 'layer:plugin-api'] },
            { sourceTag: 'layer:plugin',     onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry', 'layer:plugin-api', 'layer:adapter'] },
            { sourceTag: 'layer:cli',        onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry', 'layer:adapter'] },
            { sourceTag: 'layer:console',    onlyDependOnLibsWithTags: ['layer:contracts', 'layer:telemetry', 'layer:adapter'] },
            { sourceTag: 'layer:tooling',    onlyDependOnLibsWithTags: ['layer:contracts'] },
            { sourceTag: 'layer:sdk',        onlyDependOnLibsWithTags: ['layer:contracts'] },
            { sourceTag: 'layer:kernel',     onlyDependOnLibsWithTags: [] },
            { sourceTag: 'scope:analytics',  onlyDependOnLibsWithTags: ['scope:analytics'] },
            { sourceTag: 'scope:plugin',     onlyDependOnLibsWithTags: ['scope:analytics', 'scope:plugin'] },
            { sourceTag: 'scope:app',        onlyDependOnLibsWithTags: ['scope:analytics'] },
            { sourceTag: 'scope:tooling',    onlyDependOnLibsWithTags: ['scope:analytics', 'scope:tooling'] },
            { sourceTag: 'scope:sdk',        onlyDependOnLibsWithTags: ['scope:analytics', 'scope:sdk'] },
            { sourceTag: 'scope:kernel',     onlyDependOnLibsWithTags: [] },
          ],
        },
      ],
    },
  },
];
