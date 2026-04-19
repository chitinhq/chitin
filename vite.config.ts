import { defineConfig } from 'vite';

// Root Vite config. Vitest picks this up; libs/apps can extend as needed.
export default defineConfig({
  test: {
    globals: false,
    environment: 'node',
    include: [
      'libs/**/*.test.ts',
      'apps/**/*.test.ts',
      'libs/**/tests/**/*.test.ts',
      'apps/**/tests/**/*.test.ts',
    ],
    reporters: ['default'],
  },
});
