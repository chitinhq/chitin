import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';

/**
 * Wire the Claude Code PreToolUse hook to invoke the chitin-kernel binary.
 * Idempotent: if an entry tagged chitin-v2 already exists, it's replaced.
 */
export function initClaudeCodeCommand(opts: { workspace?: string }): void {
  const workspace = resolve(opts.workspace ?? process.cwd());
  const kernelBin = resolve(workspace, 'dist', 'go', 'execution-kernel', 'chitin-kernel');
  if (!existsSync(kernelBin)) {
    throw new Error(
      `kernel binary not built at ${kernelBin}; run: pnpm exec nx run execution-kernel:build`,
    );
  }

  const claudeDir = join(homedir(), '.claude');
  const settingsPath = join(claudeDir, 'settings.json');
  mkdirSync(claudeDir, { recursive: true });

  let settings: Record<string, unknown> = {};
  if (existsSync(settingsPath)) {
    try {
      settings = JSON.parse(readFileSync(settingsPath, 'utf8'));
    } catch {
      throw new Error(
        `${settingsPath} exists but is not valid JSON — please fix before re-running init`,
      );
    }
  }

  const hooks = (settings['hooks'] as Record<string, unknown>) ?? {};
  const pre = (hooks['PreToolUse'] as unknown[]) ?? [];
  settings['hooks'] = hooks;
  hooks['PreToolUse'] = pre;

  const tag = 'chitin-v2';
  const entry = {
    _tag: tag,
    matcher: '',
    hooks: [{ type: 'command', command: kernelBin }],
    env: { CHITIN_WORKSPACE: workspace },
  };
  const idx = pre.findIndex((h) => (h as Record<string, unknown>)?.['_tag'] === tag);
  if (idx >= 0) {
    pre[idx] = entry;
  } else {
    pre.push(entry);
  }

  writeFileSync(settingsPath, JSON.stringify(settings, null, 2));

  mkdirSync(join(workspace, '.chitin'), { recursive: true });
  writeFileSync(
    join(workspace, '.chitin', 'init.json'),
    JSON.stringify({ surface: 'claude-code', kernelBin, ts: new Date().toISOString() }, null, 2),
  );

  process.stdout.write(
    `wired Claude Code PreToolUse hook → ${kernelBin}\nsettings: ${settingsPath}\nworkspace: ${workspace}\n`,
  );
}
