import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'node:url';
import { dirname } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));

// Local override — workspace default include patterns scope to libs/
// and apps/, so this package's tests would otherwise be invisible.
// Setting `root` to the package dir keeps include patterns relative
// to it regardless of where the runner is launched from.
export default defineConfig({
  test: {
    root: here,
    include: ['tests/**/*.test.ts'],
  },
});
