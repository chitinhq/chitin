import { describe, expect, it } from 'vitest';
import { precheckShellCommand } from '../src/skills/comment-responder.ts';

describe('precheckShellCommand — bootstrap pattern detection', () => {
  it('rejects the exact 2026-05-04 / 2026-05-05 lockdown shape', () => {
    const cmd =
      'rm -rf ./* .[^.]* 2>/dev/null || true && gh repo clone chitinhq/chitin . && gh pr checkout 305';
    const result = precheckShellCommand(cmd);
    expect(result.ok).toBe(false);
    expect(result.event_kind).toBe('bootstrap-rejected');
    expect(result.reason).toMatch(/SKILL\.md step 1/);
    expect(result.reason).toMatch(/subdirectory/);
  });

  it('rejects `rm -rf ./* && gh repo clone .` minimal form', () => {
    const result = precheckShellCommand('rm -rf ./* && gh repo clone .');
    expect(result.ok).toBe(false);
    expect(result.event_kind).toBe('bootstrap-rejected');
  });

  it('rejects flag variants: -fr, -Rf, -rfv', () => {
    expect(precheckShellCommand('rm -fr ./* && gh repo clone .').ok).toBe(false);
    expect(precheckShellCommand('rm -Rf ./* && gh repo clone .').ok).toBe(false);
    expect(precheckShellCommand('rm -rfv ./* && gh repo clone .').ok).toBe(false);
  });

  it('rejects across `;` and `|` shell chain operators', () => {
    expect(precheckShellCommand('rm -rf .; gh repo clone foo bar').ok).toBe(false);
    expect(precheckShellCommand('rm -rf . | gh repo clone foo bar').ok).toBe(false);
  });
});

describe('precheckShellCommand — must NOT false-positive', () => {
  it('allows `rm -rf node_modules` alone (no gh repo clone)', () => {
    expect(precheckShellCommand('rm -rf node_modules').ok).toBe(true);
  });

  it('allows `rm -rf dist && pnpm build` (no gh repo clone in chain)', () => {
    expect(precheckShellCommand('rm -rf dist && pnpm build').ok).toBe(true);
  });

  it('allows `gh repo clone` alone (no rm -rf)', () => {
    expect(precheckShellCommand('gh repo clone chitinhq/chitin').ok).toBe(true);
  });

  it('allows `gh repo clone foo && rm -rf bar` — rm AFTER clone is not the bootstrap shape', () => {
    expect(precheckShellCommand('gh repo clone foo && rm -rf bar').ok).toBe(true);
  });

  it('allows multi-line script where rm and gh are on independent lines', () => {
    const cmd = ['rm -rf node_modules', 'pnpm install', 'gh repo clone elsewhere /tmp/x'].join('\n');
    expect(precheckShellCommand(cmd).ok).toBe(true);
  });

  it('allows empty string', () => {
    expect(precheckShellCommand('').ok).toBe(true);
  });

  it('allows `gh repo view` + `rm -rf` chain (gh subcommand is not clone)', () => {
    expect(precheckShellCommand('rm -rf ./* && gh repo view chitinhq/chitin').ok).toBe(true);
    expect(precheckShellCommand('rm -rf ./* && gh repo create foo').ok).toBe(true);
  });

  it('allows benign tokens that share substrings (confirm-rf, armrf, ghclone-helper)', () => {
    expect(precheckShellCommand('confirm-rf && ghclone-helper .').ok).toBe(true);
  });
});

describe('precheckShellCommand — chain-event contract', () => {
  it('returns a stable event_kind so callers can record `bootstrap-rejected` chain events', () => {
    const cmd = 'rm -rf ./* && gh repo clone chitinhq/chitin .';
    const result = precheckShellCommand(cmd);
    expect(result.ok).toBe(false);
    expect(result.event_kind).toBe('bootstrap-rejected');
  });

  it('reason cites both the lockdown history and the SKILL.md retry recipe', () => {
    const result = precheckShellCommand('rm -rf ./* && gh repo clone .');
    expect(result.reason).toBeDefined();
    expect(result.reason).toMatch(/2026-05-04/);
    expect(result.reason).toMatch(/2026-05-05/);
    expect(result.reason).toMatch(/SKILL\.md/);
    expect(result.reason).toMatch(/gh pr checkout/);
  });

  it('ok=true results carry no reason or event_kind', () => {
    const result = precheckShellCommand('echo hello');
    expect(result.ok).toBe(true);
    expect(result.reason).toBeUndefined();
    expect(result.event_kind).toBeUndefined();
  });
});
