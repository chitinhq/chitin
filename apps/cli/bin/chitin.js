#!/usr/bin/env node
import { existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const here = dirname(new URL(import.meta.url).pathname);
const distEntry = join(here, '..', 'dist', 'main.js');

if (existsSync(distEntry)) {
  await import(pathToFileURL(distEntry).href);
} else {
  await import('tsx/esm');
  await import(pathToFileURL(join(here, '..', 'src', 'main.ts')).href);
}
