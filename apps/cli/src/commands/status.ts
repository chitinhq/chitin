import type { Command } from 'commander';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { findChitinDir } from '../chitindir.js';
import { getSurfaceStatus, SURFACES } from '../installer.js';
import { getKernelCachePath, isExecutable } from '../kernel.js';

export function registerStatus(program: Command): void {
  program
    .command('status')
    .description('Report kernel and adapter installation status')
    .option('--workspace <dir>', 'workspace dir (default: cwd)')
    .action((opts: { workspace?: string }) => {
      const workspace = opts.workspace ?? process.cwd();
      // Read-only lookup: `status` must report missing state accurately, not
      // create an orphan ~/.chitin as a side effect of being run.
      const chitinDir = findChitinDir(workspace, '');
      const kernelPath = process.env.CHITIN_KERNEL_BINARY ?? getKernelCachePath();
      console.log(`kernel: ${isExecutable(kernelPath) ? 'installed' : 'missing'} -> ${kernelPath}`);
      if (chitinDir === null) {
        console.log('state: missing -> (no .chitin dir found)');
        console.log('events-db: missing');
      } else {
        console.log(`state: present -> ${chitinDir}`);
        console.log(`events-db: ${existsSync(join(chitinDir, 'events.db')) ? 'present' : 'missing'}`);
      }
      for (const surface of SURFACES) {
        const status = getSurfaceStatus(surface);
        console.log(
          `${surface}: ${status.installed ? 'installed' : 'missing'} (${status.mode}) -> ${status.target} [${status.details}]`,
        );
      }
    });
}
