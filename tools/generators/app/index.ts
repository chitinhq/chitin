import { type Tree } from '@nx/devkit';
import type { AppGeneratorSchema } from './schema.js';

// Lockdown for app names: kebab-case alphanumerics only. Without
// this, a name like `../foo` would write outside `apps/`, and
// names with quotes/newlines would generate invalid TS sources.
const APP_NAME_RE = /^[a-z][a-z0-9-]*[a-z0-9]$/;

function validateName(name: string): void {
  if (typeof name !== 'string' || name.length === 0) {
    throw new Error('appGenerator: name is required');
  }
  if (name.length > 64) {
    throw new Error(`appGenerator: name too long (${name.length} > 64): ${JSON.stringify(name)}`);
  }
  if (!APP_NAME_RE.test(name)) {
    throw new Error(
      `appGenerator: name must match /^[a-z][a-z0-9-]*[a-z0-9]$/ ` +
        `(kebab-case, no path separators, no quotes/newlines). got: ${JSON.stringify(name)}`,
    );
  }
}

export default async function appGenerator(
  tree: Tree,
  options: AppGeneratorSchema,
): Promise<void> {
  const { name, daemon = false } = options;
  validateName(name);

  const appRoot = `apps/${name}`;

  // Refuse to silently overwrite an existing app. The Nx Tree's
  // exists() returns true for any path the tree knows about
  // (already-on-disk OR pending-write); both are reasons to bail.
  if (tree.exists(appRoot)) {
    throw new Error(
      `appGenerator: refusing to overwrite existing path ${appRoot}. ` +
        `Pick a different name, or remove the existing directory first.`,
    );
  }

  tree.write(`${appRoot}/package.json`, packageJson(name));
  tree.write(`${appRoot}/tsconfig.json`, tsconfig(name));
  tree.write(`${appRoot}/tsconfig.spec.json`, tsconfigSpec(name));
  tree.write(`${appRoot}/src/main.ts`, mainTs(name));
  tree.write(`${appRoot}/tests/${name}.test.ts`, testTs(name));
  tree.write(`${appRoot}/README.md`, readme(name));

  if (daemon) {
    tree.write(
      `${appRoot}/systemd/chitin-${name}.service`,
      systemdService(name),
    );
    tree.write(
      `${appRoot}/systemd/chitin-${name}.timer`,
      systemdTimer(name),
    );
  }
}

function packageJson(name: string): string {
  return (
    JSON.stringify(
      {
        name: `@chitin/${name}`,
        version: '0.0.1',
        private: true,
        type: 'module',
        main: './src/main.ts',
        nx: {
          tags: ['layer:app'],
          targets: {
            run: {
              executor: 'nx:run-commands',
              options: {
                command: `pnpm exec tsx apps/${name}/src/main.ts`,
                cwd: '{workspaceRoot}',
              },
            },
            test: {
              executor: 'nx:run-commands',
              options: {
                command: `pnpm exec vitest run apps/${name}/tests`,
                cwd: '{workspaceRoot}',
              },
            },
          },
        },
      },
      null,
      2,
    ) + '\n'
  );
}

function tsconfig(name: string): string {
  return (
    JSON.stringify(
      {
        extends: '../../tsconfig.base.json',
        compilerOptions: {
          outDir: `../../dist/apps/${name}`,
          module: 'esnext',
          moduleResolution: 'bundler',
          allowImportingTsExtensions: true,
          noEmit: true,
        },
        include: ['src/**/*', 'tests/**/*'],
      },
      null,
      2,
    ) + '\n'
  );
}

function tsconfigSpec(name: string): string {
  return (
    JSON.stringify(
      {
        extends: '../../tsconfig.base.json',
        compilerOptions: {
          outDir: `../../dist/apps/${name}`,
          module: 'esnext',
          moduleResolution: 'bundler',
          allowImportingTsExtensions: true,
          noEmit: true,
        },
        include: ['tests/**/*'],
      },
      null,
      2,
    ) + '\n'
  );
}

function mainTs(name: string): string {
  return `// ${name} entry point\n\nconsole.log('${name} started');\n`;
}

function testTs(name: string): string {
  return [
    `import { describe, it, expect } from 'vitest';`,
    ``,
    `describe('${name}', () => {`,
    `  it('placeholder', () => {`,
    `    expect(true).toBe(true);`,
    `  });`,
    `});`,
    ``,
  ].join('\n');
}

function readme(name: string): string {
  return [
    `# @chitin/${name}`,
    ``,
    `Scaffolded by \`nx g @chitin/generators:app ${name}\`.`,
    ``,
    `## Run`,
    ``,
    `\`\`\`bash`,
    `nx run @chitin/${name}:run`,
    `\`\`\``,
    ``,
    `## Test`,
    ``,
    `\`\`\`bash`,
    `nx run @chitin/${name}:test`,
    `\`\`\``,
    ``,
  ].join('\n');
}

function systemdService(name: string): string {
  return [
    `# One-shot service for chitin-${name}, fired by chitin-${name}.timer.`,
    `#`,
    `# Install:`,
    `#   cp apps/${name}/systemd/chitin-${name}.{service,timer} ~/.config/systemd/user/`,
    `#   systemctl --user daemon-reload`,
    `#   systemctl --user enable --now chitin-${name}.timer`,
    ``,
    `[Unit]`,
    `Description=Chitin ${name}`,
    ``,
    `[Service]`,
    `Type=oneshot`,
    `WorkingDirectory=%h/workspace/chitin`,
    `Environment=CHITIN_REPO_ROOT=%h/workspace/chitin`,
    `Environment=PATH=%h/.local/bin:%h/.vite-plus/bin:/snap/bin:/usr/local/bin:/usr/bin:/bin`,
    `EnvironmentFile=-%h/.config/systemd/user/chitin.env`,
    // ExecStart relies on the unit's PATH (set above to include
    // %h/.vite-plus/bin) rather than hardcoding /home/<operator>/...
    // — the latter breaks the moment another operator or CI runs
    // the unit.
    `ExecStart=pnpm exec tsx apps/${name}/src/main.ts`,
    `StandardOutput=journal`,
    `StandardError=journal`,
    `TimeoutStartSec=1200`,
    ``,
  ].join('\n');
}

function systemdTimer(name: string): string {
  return [
    `# Timer for chitin-${name}.service.`,
    ``,
    `[Unit]`,
    `Description=Chitin ${name} periodic timer`,
    ``,
    `[Timer]`,
    `OnBootSec=5min`,
    `OnUnitActiveSec=1h`,
    `Persistent=false`,
    `Unit=chitin-${name}.service`,
    ``,
    `[Install]`,
    `WantedBy=timers.target`,
    ``,
  ].join('\n');
}
