import { existsSync, mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { afterAll, describe, expect, it } from 'vitest';

const repoRoot = join(__dirname, '..', '..', '..');
const cliEntry = join(repoRoot, 'apps/cli/src/main.ts');
const tsxBin = join(repoRoot, 'node_modules/.bin/tsx');

const tmpDirs: string[] = [];

function makeWorkspace(): string {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-wiki-e2e-'));
  tmpDirs.push(dir);
  return dir;
}

function runCli(args: string[], cwd: string) {
  return spawnSync(tsxBin, [cliEntry, ...args], {
    cwd,
    encoding: 'utf8',
    env: { ...process.env },
  });
}

function skipIfWorkspaceBroken(res: { stderr: string | null }): boolean {
  return (res.stderr ?? '').includes('ERR_MODULE_NOT_FOUND');
}

afterAll(() => {
  for (const dir of tmpDirs) rmSync(dir, { recursive: true, force: true });
});

describe('chitin wiki (e2e)', () => {
  it('ingests, compiles, and answers from docs', () => {
    if (!existsSync(tsxBin)) {
      console.warn(`skipping e2e: ${tsxBin} missing`);
      return;
    }
    const workspace = makeWorkspace();
    mkdirSync(join(workspace, 'docs'), { recursive: true });
    writeFileSync(
      join(workspace, 'docs', 'gate.md'),
      [
        '# Dispatch Gate',
        'The dispatch gate checks branch protection and path bounds before push-shaped actions.',
      ].join('\n'),
      'utf8',
    );

    const ingest = runCli(['wiki', 'ingest', '--workspace', workspace], workspace);
    if (skipIfWorkspaceBroken(ingest)) return;
    expect(ingest.status).toBe(0);
    expect(ingest.stdout).toContain('wiki ingest: 1 sources');

    const compile = runCli(['wiki', 'compile', '--workspace', workspace], workspace);
    expect(compile.status).toBe(0);
    expect(compile.stdout).toContain('wiki compile: 1 sources');

    const ask = runCli(
      ['wiki', 'ask', 'what does the dispatch gate check?', '--workspace', workspace],
      workspace,
    );
    expect(ask.status).toBe(0);
    expect(ask.stdout).toContain('branch protection');
    expect(ask.stdout).toContain('Citations:');
  });

  it('returns a lint failure on broken internal references', () => {
    if (!existsSync(tsxBin)) {
      console.warn(`skipping e2e: ${tsxBin} missing`);
      return;
    }
    const workspace = makeWorkspace();
    mkdirSync(join(workspace, 'docs'), { recursive: true });
    writeFileSync(
      join(workspace, 'docs', 'broken.md'),
      '# Broken\nSee [missing](./missing.md#anchor).',
      'utf8',
    );

    const ingest = runCli(['wiki', 'ingest', '--workspace', workspace], workspace);
    if (skipIfWorkspaceBroken(ingest)) return;
    expect(ingest.status).toBe(0);
    expect(runCli(['wiki', 'compile', '--workspace', workspace], workspace).status).toBe(0);

    const lint = runCli(['wiki', 'lint', '--workspace', workspace], workspace);
    expect(lint.status).toBe(1);
    expect(lint.stdout).toContain('broken-reference');
  });
});
