import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join } from 'node:path';

export const SURFACES = ['claude-code', 'codex', 'gemini', 'copilot'] as const;
export type Surface = typeof SURFACES[number];

export interface InstallResult {
  surface: Surface;
  target: string;
  mode: 'hook' | 'wrapper';
  changed: boolean;
}

export interface SurfaceStatus {
  surface: Surface;
  installed: boolean;
  mode: 'hook' | 'wrapper';
  target: string;
  details: string;
}

export function installSurface(surface: Surface, kernelBin: string): InstallResult {
  switch (surface) {
    case 'claude-code':
      return installClaudeCode(kernelBin);
    case 'codex':
      return installCodex(kernelBin);
    case 'gemini':
      return installGemini(kernelBin);
    case 'copilot':
      return installCopilotWrapper(kernelBin);
  }
}

export function getSurfaceStatus(surface: Surface): SurfaceStatus {
  switch (surface) {
    case 'claude-code':
      return getClaudeCodeStatus();
    case 'codex':
      return getCodexStatus();
    case 'gemini':
      return getGeminiStatus();
    case 'copilot':
      return getCopilotStatus();
  }
}

function installClaudeCode(kernelBin: string): InstallResult {
  const target = join(homedir(), '.claude', 'settings.json');
  const command = `${kernelBin} gate evaluate --hook-stdin --agent=claude-code`;
  const wrapper = {
    _tag: 'chitin',
    matcher: '',
    hooks: [{ type: 'command', command }],
  };
  const settings = readJsonFile<Record<string, unknown>>(target, {});
  const hooks = asRecord(settings.hooks);
  const current = Array.isArray(hooks.PreToolUse) ? hooks.PreToolUse : [];
  const next = upsertHookWrapper(current, wrapper, command);
  const changed = JSON.stringify(current) !== JSON.stringify(next);
  hooks.PreToolUse = next;
  settings.hooks = hooks;
  writeJsonFile(target, settings);
  return { surface: 'claude-code', target, mode: 'hook', changed };
}

function installGemini(kernelBin: string): InstallResult {
  const target = join(homedir(), '.gemini', 'settings.json');
  const command = `${kernelBin} gate evaluate --hook-stdin --agent=gemini`;
  const wrapper = {
    _tag: 'chitin',
    matcher: '',
    hooks: [{ type: 'command', command }],
  };
  const settings = readJsonFile<Record<string, unknown>>(target, {});
  const hooks = asRecord(settings.hooks);
  const current = Array.isArray(hooks.BeforeTool) ? hooks.BeforeTool : [];
  const next = upsertHookWrapper(current, wrapper, command);
  const changed = JSON.stringify(current) !== JSON.stringify(next);
  hooks.BeforeTool = next;
  settings.hooks = hooks;
  writeJsonFile(target, settings);
  return { surface: 'gemini', target, mode: 'hook', changed };
}

function installCodex(kernelBin: string): InstallResult {
  const target = join(homedir(), '.codex', 'config.toml');
  const command = `${kernelBin} gate evaluate --hook-stdin --agent=codex`;
  const marker = `command = "${command}"`;
  const block = [
    '',
    '# Added by chitin guard',
    '[[hooks.PreToolUse]]',
    'matcher = ""',
    '[[hooks.PreToolUse.hooks]]',
    'type = "command"',
    marker,
    'timeout = 30',
    '',
  ].join('\n');
  mkdirSync(dirname(target), { recursive: true });
  const existing = existsSync(target) ? readFileSync(target, 'utf8') : '';
  let next = ensureCodexHooksEnabled(existing);
  const hadMarker = next.includes(marker);
  if (!hadMarker) next += block;
  if (next !== existing) writeFileSync(target, next, 'utf8');
  return { surface: 'codex', target, mode: 'hook', changed: next !== existing };
}

function installCopilotWrapper(kernelBin: string): InstallResult {
  const target = join(homedir(), '.local', 'bin', 'chitin-copilot');
  const script = [
    '#!/usr/bin/env bash',
    'set -euo pipefail',
    `exec "${kernelBin}" drive copilot "$@"`,
    '',
  ].join('\n');
  mkdirSync(dirname(target), { recursive: true });
  const existing = existsSync(target) ? readFileSync(target, 'utf8') : '';
  const changed = existing !== script;
  if (changed) {
    writeFileSync(target, script, { mode: 0o755 });
  } else if ((statSync(target).mode & 0o111) === 0) {
    writeFileSync(target, script, { mode: 0o755 });
  }
  return { surface: 'copilot', target, mode: 'wrapper', changed };
}

function getClaudeCodeStatus(): SurfaceStatus {
  const target = join(homedir(), '.claude', 'settings.json');
  const commandNeedle = '--agent=claude-code';
  const settings = readJsonFile<Record<string, unknown>>(target, {});
  const hooks = asRecord(settings.hooks);
  const wrappers = Array.isArray(hooks.PreToolUse) ? hooks.PreToolUse : [];
  const installed = wrappers.some((entry) => hasCommand(entry, commandNeedle));
  return {
    surface: 'claude-code',
    installed,
    mode: 'hook',
    target,
    details: installed ? 'PreToolUse hook installed' : 'PreToolUse hook missing',
  };
}

function getGeminiStatus(): SurfaceStatus {
  const target = join(homedir(), '.gemini', 'settings.json');
  const commandNeedle = '--agent=gemini';
  const settings = readJsonFile<Record<string, unknown>>(target, {});
  const hooks = asRecord(settings.hooks);
  const wrappers = Array.isArray(hooks.BeforeTool) ? hooks.BeforeTool : [];
  const installed = wrappers.some((entry) => hasCommand(entry, commandNeedle));
  return {
    surface: 'gemini',
    installed,
    mode: 'hook',
    target,
    details: installed ? 'BeforeTool hook installed' : 'BeforeTool hook missing',
  };
}

function getCodexStatus(): SurfaceStatus {
  const target = join(homedir(), '.codex', 'config.toml');
  const installed = existsSync(target)
    && readFileSync(target, 'utf8').includes('--agent=codex');
  return {
    surface: 'codex',
    installed,
    mode: 'hook',
    target,
    details: installed ? 'PreToolUse hook installed' : 'PreToolUse hook missing',
  };
}

function getCopilotStatus(): SurfaceStatus {
  const target = join(homedir(), '.local', 'bin', 'chitin-copilot');
  const installed = existsSync(target)
    && readFileSync(target, 'utf8').includes('drive copilot');
  return {
    surface: 'copilot',
    installed,
    mode: 'wrapper',
    target,
    details: installed ? 'governed wrapper installed' : 'governed wrapper missing',
  };
}

function ensureCodexHooksEnabled(input: string): string {
  if (/\bcodex_hooks\s*=\s*true\b/.test(input)) return input;
  if (/^\[features\]$/m.test(input)) {
    return input.replace(/^\[features\]$/m, '[features]\ncodex_hooks = true');
  }
  return `${input}${input.endsWith('\n') || input.length === 0 ? '' : '\n'}[features]\ncodex_hooks = true\n`;
}

function upsertHookWrapper(current: unknown[], wrapper: Record<string, unknown>, command: string): unknown[] {
  const filtered = current.filter((entry) => !hasCommand(entry, command));
  return [...filtered, wrapper];
}

function hasCommand(entry: unknown, commandNeedle: string): boolean {
  if (!entry || typeof entry !== 'object') return false;
  const hooks = (entry as { hooks?: Array<{ command?: string }> }).hooks ?? [];
  return hooks.some((hook) => typeof hook.command === 'string' && hook.command.includes(commandNeedle));
}

function readJsonFile<T>(path: string, fallback: T): T {
  if (!existsSync(path)) return fallback;
  const raw = readFileSync(path, 'utf8').trim();
  if (!raw) return fallback;
  return JSON.parse(raw) as T;
}

function writeJsonFile(path: string, data: unknown): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(data, null, 2)}\n`, 'utf8');
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? { ...(value as Record<string, unknown>) } : {};
}
