import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: false,
    environment: 'node',
    include: ['tools/generators/agent-role/tests/**/*.spec.ts'],
  },
});
