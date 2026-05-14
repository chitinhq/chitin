import type { Command } from 'commander';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { installSurface, SURFACES, type Surface } from '../installer.js';
import { ensureKernelBinary } from '../kernel.js';

interface GuardOpts {
  surface?: string;
}

export function registerGuard(program: Command): void {
  program
    .command('guard')
    .description('Install governed adapters for supported agent surfaces')
    .option('--surface <name>', 'install only one surface')
    .action(async (opts: GuardOpts) => {
      const version = readPackageVersion();
      const kernelBin = await ensureKernelBinary(version);
      const surfaces = resolveSurfaces(opts.surface);
      for (const surface of surfaces) {
        const result = installSurface(surface, kernelBin);
        console.log(
          `${surface}: ${result.changed ? 'installed' : 'already configured'} (${result.mode}) -> ${result.target}`,
        );
      }
      console.log(`kernel: ${kernelBin}`);
    });
}

function resolveSurfaces(surface?: string): Surface[] {
  if (!surface) return [...SURFACES];
  if (!SURFACES.includes(surface as Surface)) {
    throw new Error(`unknown surface: ${surface}`);
  }
  return [surface as Surface];
}

function readPackageVersion(): string {
  // fileURLToPath, not URL.pathname: the latter leaves %20 in paths with
  // spaces and yields a non-portable /C:/ form on Windows.
  const here = dirname(fileURLToPath(import.meta.url));
  const raw = readFileSync(join(here, '..', '..', 'package.json'), 'utf8');
  return (JSON.parse(raw) as { version: string }).version;
}
