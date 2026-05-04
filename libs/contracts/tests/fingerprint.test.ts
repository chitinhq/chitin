import { describe, expect, it } from 'vitest';
import { computeFingerprint } from '../src/fingerprint.js';

describe('computeFingerprint', () => {
  // Deterministic input fixture used across multiple tests.
  const baseDims = {
    driver: 'copilot',
    model: 'claude-haiku-4-5',
    role: 'reviewer',
    stationPromptHash: 'abc123',
    skillsToolsHash: 'def456',
    soulLens: 'da-vinci',
  };

  it('produces a 12-char hex hash', () => {
    const { hash } = computeFingerprint(baseDims);
    expect(hash).toMatch(/^[a-f0-9]{12}$/);
  });

  it('is canonical: same input always produces same hash', () => {
    const a = computeFingerprint(baseDims);
    const b = computeFingerprint(baseDims);
    expect(a.hash).toBe(b.hash);
  });

  it('hash changes when any single dimension changes', () => {
    const baseline = computeFingerprint(baseDims).hash;
    expect(computeFingerprint({ ...baseDims, driver: 'codex' }).hash).not.toBe(baseline);
    expect(computeFingerprint({ ...baseDims, model: 'gpt-5-mini' }).hash).not.toBe(baseline);
    expect(computeFingerprint({ ...baseDims, role: 'programmer' }).hash).not.toBe(baseline);
    expect(computeFingerprint({ ...baseDims, stationPromptHash: 'zzz' }).hash).not.toBe(baseline);
    expect(computeFingerprint({ ...baseDims, skillsToolsHash: 'zzz' }).hash).not.toBe(baseline);
    expect(computeFingerprint({ ...baseDims, soulLens: 'knuth' }).hash).not.toBe(baseline);
  });

  it('payload includes ALL 6 dimensions with explicit nulls (not omitted)', () => {
    const { payload } = computeFingerprint({});
    expect(payload).toHaveProperty('driver');
    expect(payload).toHaveProperty('model');
    expect(payload).toHaveProperty('role');
    expect(payload).toHaveProperty('stationPromptHash');
    expect(payload).toHaveProperty('skillsToolsHash');
    expect(payload).toHaveProperty('soulLens');
    // 5 of 6 are explicitly null (soulLens reads env or 'none', tested below)
    expect(payload.driver).toBeNull();
    expect(payload.model).toBeNull();
    expect(payload.role).toBeNull();
    expect(payload.stationPromptHash).toBeNull();
    expect(payload.skillsToolsHash).toBeNull();
  });

  it('hash space is stable: missing dims hash same as explicit-null dims', () => {
    const omitted = computeFingerprint({ driver: 'copilot' });
    const explicitNull = computeFingerprint({
      driver: 'copilot',
      model: null,
      role: null,
      stationPromptHash: null,
      skillsToolsHash: null,
      // Note: soulLens passed explicit null vs omitted produces same
      // result because both fall through to the env/'none' fallback.
      soulLens: null,
    });
    expect(omitted.hash).toBe(explicitNull.hash);
  });

  it('soul lens defaults to CHITIN_ACTIVE_SOUL env when not in dimensions', () => {
    const orig = process.env.CHITIN_ACTIVE_SOUL;
    process.env.CHITIN_ACTIVE_SOUL = 'knuth';
    try {
      const { payload } = computeFingerprint({});
      expect(payload.soulLens).toBe('knuth');
    } finally {
      if (orig === undefined) delete process.env.CHITIN_ACTIVE_SOUL;
      else process.env.CHITIN_ACTIVE_SOUL = orig;
    }
  });

  it("soul lens falls back to 'none' when env is unset and dim is omitted", () => {
    const orig = process.env.CHITIN_ACTIVE_SOUL;
    delete process.env.CHITIN_ACTIVE_SOUL;
    try {
      const { payload } = computeFingerprint({});
      expect(payload.soulLens).toBe('none');
    } finally {
      if (orig !== undefined) process.env.CHITIN_ACTIVE_SOUL = orig;
    }
  });

  it('explicit soul lens dimension overrides env', () => {
    const orig = process.env.CHITIN_ACTIVE_SOUL;
    process.env.CHITIN_ACTIVE_SOUL = 'knuth';
    try {
      const { payload } = computeFingerprint({ soulLens: 'sun-tzu' });
      expect(payload.soulLens).toBe('sun-tzu');
    } finally {
      if (orig === undefined) delete process.env.CHITIN_ACTIVE_SOUL;
      else process.env.CHITIN_ACTIVE_SOUL = orig;
    }
  });

  it('hash matches the 12-char prefix shape required by ExecutionRequest schema', () => {
    // ExecutionRequestSchema accepts fingerprint as /^[a-f0-9]{12}$/.
    // Pin both ends — function output and schema regex must agree.
    const { hash } = computeFingerprint(baseDims);
    expect(hash).toHaveLength(12);
    expect(hash).toMatch(/^[a-f0-9]+$/);
  });
});
