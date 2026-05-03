import { type Tree } from '@nx/devkit';
import type { AppGeneratorSchema } from './schema.js';

export default async function appGenerator(
  tree: Tree,
  options: AppGeneratorSchema,
): Promise<void> {
  const { name, daemon = false } = options;
  const appRoot = `apps/${name}`;

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
    `ExecStart=/home/red/.vite-plus/bin/pnpm exec tsx apps/${name}/src/main.ts`,
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
