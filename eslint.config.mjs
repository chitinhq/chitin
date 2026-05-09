// Narrow ESLint config — runs the @nx/enforce-module-boundaries rule only.
// Oxlint (via .oxlintrc.json) is the primary fast-pass linter; this ESLint
// exists solely to enforce Nx project-tag dependency constraints because Oxlint
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
            {
              sourceTag: 'type:app',
              onlyDependOnLibsWithTags: [
                'type:feature',
                'type:data-access',
                'type:contract',
                'type:adapter',
                'type:util',
              ],
            },
            {
              sourceTag: 'type:feature',
              onlyDependOnLibsWithTags: [
                'type:feature',
                'type:data-access',
                'type:contract',
                'type:adapter',
                'type:ui',
                'type:util',
              ],
            },
            {
              sourceTag: 'type:adapter',
              onlyDependOnLibsWithTags: ['type:contract', 'type:util'],
            },
            {
              sourceTag: 'type:data-access',
              onlyDependOnLibsWithTags: ['type:data-access', 'type:contract', 'type:util'],
            },
            {
              sourceTag: 'type:contract',
              onlyDependOnLibsWithTags: ['type:util'],
            },
            {
              sourceTag: 'type:ui',
              onlyDependOnLibsWithTags: ['type:ui', 'type:util'],
            },
            {
              sourceTag: 'type:util',
              onlyDependOnLibsWithTags: ['type:util'],
            },
            {
              sourceTag: 'type:tooling',
              onlyDependOnLibsWithTags: [
                'type:contract',
                'type:data-access',
                'type:util',
                'type:tooling',
              ],
            },
          ],
        },
      ],
    },
  },
];
