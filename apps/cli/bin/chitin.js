#!/usr/bin/env node
import { existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

// fileURLToPath, not URL.pathname: the latter leaves %20 in install paths
// with spaces and a non-portable /C:/ form on Windows, so the dist-vs-src
// entrypoint check below would miss a perfectly valid build.
const here = dirname(fileURLToPath(import.meta.url));
const distEntry = join(here, '..', 'dist', 'main.js');

if (existsSync(distEntry)) {
  await import(pathToFileURL(distEntry).href);
} else {
  await import('tsx/esm');
  await import(pathToFileURL(join(here, '..', 'src', 'main.ts')).href);
}
