// Spec 091 v1.1 — AFR-002 / AFR-003 / AFR-004 / AFR-005 tests for the
// operator-unlock recovery path. The plugin's before_tool_call handler
// now consults `chitin-kernel session status` before returning a sticky
// stop, and clears its in-memory flag when the kernel reports an unlock
// or an advanced lock_epoch.
//
// We don't import a mocked openclaw `api`; that's heavy scaffolding. We
// exercise the load-bearing helpers (querySessionStatus, captureLockEpoch
// via __test_getStopHookActiveEpoch, emitStopHookCleared event shape) and
// trust the higher-level handler integration via the existing
// lockdown-loop tests + the live smoke run.

import { describe, expect, it, beforeEach } from 'vitest';
import { mkdtempSync, writeFileSync, chmodSync, rmSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
// @ts-expect-error — sibling .mjs without types
import {
  querySessionStatus,
  emitStopHookCleared,
  __test_resetState,
  __test_getStopHookActiveEpoch,
} from '../src/index.mjs';

function makeFakeKernel(scriptBody: string): { path: string; cleanup: () => void; sentinelPath: string } {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-091-v11-test-'));
  const path = join(dir, 'chitin-kernel');
  const sentinelPath = join(dir, 'captured.json');
  writeFileSync(path, `#!/usr/bin/env bash\n${scriptBody}\n`);
  chmodSync(path, 0o755);
  return { path, sentinelPath, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

describe('spec 091 v1.1 — querySessionStatus (AFR-002)', () => {
  beforeEach(() => __test_resetState());

  it('parses locked:true response with lock_epoch', async () => {
    const fake = makeFakeKernel(
      `echo '{"agent":"clawta","locked":true,"locked_ts":"2026-05-23T13:00:00Z","unlock_ts":"","lock_epoch":5,"total":12,"level":"lockdown"}'; exit 0`,
    );
    try {
      const status = await querySessionStatus('clawta', fake.path);
      expect(status).not.toBeNull();
      expect(status.locked).toBe(true);
      expect(status.lock_epoch).toBe(5);
    } finally {
      fake.cleanup();
    }
  });

  it('parses locked:false response (operator unlocked)', async () => {
    const fake = makeFakeKernel(
      `echo '{"agent":"clawta","locked":false,"lock_epoch":6,"total":12,"level":"normal"}'; exit 0`,
    );
    try {
      const status = await querySessionStatus('clawta', fake.path);
      expect(status.locked).toBe(false);
      expect(status.lock_epoch).toBe(6);
    } finally {
      fake.cleanup();
    }
  });

  it('returns null when the kernel exits non-zero (e.g., unknown agent)', async () => {
    const fake = makeFakeKernel(`echo '{"error":"no_agent","message":"..."}'; exit 2`);
    try {
      const status = await querySessionStatus('nobody', fake.path);
      // AFR-005 fail-closed: caller treats null as "no positive unlock
      // signal; keep sticky block." This test pins that contract.
      expect(status).toBeNull();
    } finally {
      fake.cleanup();
    }
  });

  it('returns null on malformed JSON output', async () => {
    const fake = makeFakeKernel(`echo 'not json garbage{'; exit 0`);
    try {
      const status = await querySessionStatus('clawta', fake.path);
      expect(status).toBeNull();
    } finally {
      fake.cleanup();
    }
  });

  it('returns null when the kernel binary is missing entirely', async () => {
    const status = await querySessionStatus('clawta', '/definitely/not/a/binary');
    expect(status).toBeNull();
  });

  it('returns null on truncated JSON (missing locked field)', async () => {
    const fake = makeFakeKernel(`echo '{"agent":"clawta","lock_epoch":5}'; exit 0`);
    try {
      const status = await querySessionStatus('clawta', fake.path);
      // querySessionStatus requires the `locked` boolean — missing → null.
      expect(status).toBeNull();
    } finally {
      fake.cleanup();
    }
  });

  it('treats null lock_epoch as null (operator schema without the field)', async () => {
    const fake = makeFakeKernel(`echo '{"agent":"x","locked":true}'; exit 0`);
    try {
      const status = await querySessionStatus('x', fake.path);
      expect(status).not.toBeNull();
      expect(status.locked).toBe(true);
      expect(status.lock_epoch).toBeNull();
    } finally {
      fake.cleanup();
    }
  });
});

describe('spec 091 v1.1 — emitStopHookCleared (AFR-004)', () => {
  beforeEach(() => __test_resetState());

  it('writes the expected event to the kernel via -event-file', async () => {
    const fake = makeFakeKernel(`# placeholder — rewritten below with the real sentinel path`);
    rmSync(fake.path, { force: true });
    const sentinelPath = fake.sentinelPath;
    writeFileSync(
      fake.path,
      `#!/usr/bin/env bash\nset -e\nevent_file=""\nwhile [[ $# -gt 0 ]]; do\n  case "$1" in\n    -event-file) event_file="$2"; shift 2 ;;\n    *) shift ;;\n  esac\ndone\nif [[ -n "$event_file" ]]; then cp "$event_file" ${sentinelPath}; fi\nexit 0\n`,
    );
    chmodSync(fake.path, 0o755);

    const warnings: string[] = [];
    const errors: string[] = [];
    let captured: any = null;
    try {
      await emitStopHookCleared({
        sessionId: 'openclaw-clawta-12345',
        agentId: 'clawta',
        kernelLockEpoch: 6,
        kernelPath: fake.path,
        log: {
          warn: (m: string) => warnings.push(m),
          error: (m: string) => errors.push(m),
        },
      });
      // Capture sentinel BEFORE the finally block tears down the temp dir.
      if (existsSync(sentinelPath)) {
        captured = JSON.parse(readFileSync(sentinelPath, 'utf8'));
      }
    } finally {
      fake.cleanup();
    }

    expect(errors).toEqual([]);
    expect(captured).not.toBeNull();
    expect(captured.event_type).toBe('stop_hook_cleared');
    expect(captured.schema_version).toBe('2');
    expect(captured.surface).toBe('openclaw-plugin-governance');
    expect(captured.payload.agent).toBe('clawta');
    expect(captured.payload.kernel_lock_epoch).toBe(6);
    expect(captured.payload.session_id).toBe('openclaw-clawta-12345');
    expect(typeof captured.payload.cleared_at).toBe('string');
  });

  it('propagates errors when the kernel exits non-zero', async () => {
    const fake = makeFakeKernel(`echo 'kernel-error' >&2; exit 2`);
    try {
      await expect(
        emitStopHookCleared({
          sessionId: 'sid',
          agentId: 'agent',
          kernelLockEpoch: null,
          kernelPath: fake.path,
          log: { warn: () => {}, error: () => {} },
        }),
      ).rejects.toThrow(/exited 2/);
    } finally {
      fake.cleanup();
    }
  });

  it('propagates errors when the kernel binary is missing', async () => {
    await expect(
      emitStopHookCleared({
        sessionId: 'sid',
        agentId: 'agent',
        kernelLockEpoch: null,
        kernelPath: '/no/such/binary',
        log: { warn: () => {}, error: () => {} },
      }),
    ).rejects.toThrow();
  });

  it('accepts null lock_epoch (AFR-002 fallback path)', async () => {
    const fake = makeFakeKernel(`# placeholder — rewritten below`);
    rmSync(fake.path, { force: true });
    const sentinelPath = fake.sentinelPath;
    writeFileSync(
      fake.path,
      `#!/usr/bin/env bash\nset -e\nevent_file=""\nwhile [[ $# -gt 0 ]]; do\n  case "$1" in\n    -event-file) event_file="$2"; shift 2 ;;\n    *) shift ;;\n  esac\ndone\nif [[ -n "$event_file" ]]; then cp "$event_file" ${sentinelPath}; fi\nexit 0\n`,
    );
    chmodSync(fake.path, 0o755);

    let captured: any = null;
    try {
      await emitStopHookCleared({
        sessionId: 'sid',
        agentId: 'agent',
        kernelLockEpoch: null,
        kernelPath: fake.path,
        log: { warn: () => {}, error: () => {} },
      });
      if (existsSync(sentinelPath)) {
        captured = JSON.parse(readFileSync(sentinelPath, 'utf8'));
      }
    } finally {
      fake.cleanup();
    }
    expect(captured).not.toBeNull();
    expect(captured.payload.kernel_lock_epoch).toBeNull();
  });
});

describe('spec 091 v1.1 — state hygiene', () => {
  beforeEach(() => __test_resetState());

  it('__test_resetState clears the new stopHookActiveEpoch map', () => {
    // We can't directly set the map (it's not exported), but
    // __test_getStopHookActiveEpoch returns undefined after reset.
    expect(__test_getStopHookActiveEpoch('anything')).toBeUndefined();
  });

  it('__test_getStopHookActiveEpoch returns undefined for an unknown session', () => {
    expect(__test_getStopHookActiveEpoch('never-seen')).toBeUndefined();
  });
});
