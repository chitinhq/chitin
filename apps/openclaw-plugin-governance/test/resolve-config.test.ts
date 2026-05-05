import { describe, expect, it } from 'vitest';
// @ts-expect-error — sibling .mjs without types
import { resolveConfig } from '../src/index.mjs';

// Slice 3 flipped the default mode from 'observe' to 'enforce'. These tests
// lock the defaulting contract so a future config refactor can't silently
// reverse it. Default-enforce is safe because slice 3a's normalizer covers
// all 19 pi-runtime tools — no tool falls into ActUnknown anymore.

describe('resolveConfig (plugin defaults — slice 3 default-enforce)', () => {
  it('mode defaults to enforce when raw is undefined', () => {
    expect(resolveConfig(undefined).mode).toBe('enforce');
  });

  it('mode defaults to enforce when raw is empty object', () => {
    expect(resolveConfig({}).mode).toBe('enforce');
  });

  it('mode defaults to enforce when raw is null (defensive — schema permits unset)', () => {
    expect(resolveConfig(null as unknown as undefined).mode).toBe('enforce');
  });

  it('mode honors explicit observe opt-out', () => {
    expect(resolveConfig({ mode: 'observe' }).mode).toBe('observe');
  });

  it('mode honors explicit enforce', () => {
    expect(resolveConfig({ mode: 'enforce' }).mode).toBe('enforce');
  });

  it('mode falls back to enforce on unknown values (typo-safe)', () => {
    expect(resolveConfig({ mode: 'invalid' }).mode).toBe('enforce');
    expect(resolveConfig({ mode: '' }).mode).toBe('enforce');
    expect(resolveConfig({ mode: 42 }).mode).toBe('enforce');
  });

  it('kernelPath defaults to PATH lookup when unset or empty', () => {
    expect(resolveConfig({}).kernelPath).toBe('chitin-kernel');
    expect(resolveConfig({ kernelPath: '' }).kernelPath).toBe('chitin-kernel');
    expect(resolveConfig({ kernelPath: 42 }).kernelPath).toBe('chitin-kernel');
  });

  it('kernelPath honors explicit absolute path', () => {
    expect(resolveConfig({ kernelPath: '/usr/local/bin/chitin-kernel' }).kernelPath).toBe(
      '/usr/local/bin/chitin-kernel',
    );
  });

  it('workerMode defaults to false; only true literal opts in', () => {
    expect(resolveConfig({}).workerMode).toBe(false);
    expect(resolveConfig({ workerMode: 'true' }).workerMode).toBe(false);
    expect(resolveConfig({ workerMode: 1 }).workerMode).toBe(false);
    expect(resolveConfig({ workerMode: true }).workerMode).toBe(true);
  });

  it('denyOnError defaults to true (fail-closed); only false literal opts to fail-open', () => {
    expect(resolveConfig({}).denyOnError).toBe(true);
    expect(resolveConfig({ denyOnError: undefined }).denyOnError).toBe(true);
    expect(resolveConfig({ denyOnError: 0 }).denyOnError).toBe(true);
    expect(resolveConfig({ denyOnError: false }).denyOnError).toBe(false);
  });

  it('timeoutMs defaults to 30000; rejects <100 and non-numbers', () => {
    // Default bumped 5000 → 30000 when the plugin switched from
    // `evaluateGate` to `evaluateRouter` (router pipeline can include
    // a 5-15s claude-advisor LLM round-trip; 5s ceiling was too tight).
    expect(resolveConfig({}).timeoutMs).toBe(30000);
    expect(resolveConfig({ timeoutMs: '5000' }).timeoutMs).toBe(30000); // string falls to default
    expect(resolveConfig({ timeoutMs: 50 }).timeoutMs).toBe(30000); // <100 falls to default
    expect(resolveConfig({ timeoutMs: 100 }).timeoutMs).toBe(100);
    expect(resolveConfig({ timeoutMs: 1500 }).timeoutMs).toBe(1500);
  });
});
