import { execFileSync } from 'node:child_process';
import { describe, expect, it } from 'vitest';
import { EventSchema } from '@chitin/contracts';

describe('Go SDK schema parity', () => {
  it('emits events that validate against the canonical event schema', () => {
    const output = execFileSync(
      process.env.GO_BINARY ?? '/usr/local/go/bin/go',
      ['run', './cmd/sdk-fixture'],
      {
        cwd: new URL('../../../go/run-sdk/', import.meta.url),
        encoding: 'utf8',
      },
    ).trim();

    const lines = output.split('\n').filter(Boolean);
    expect(lines).toHaveLength(3);
    for (const line of lines) {
      expect(() => EventSchema.parse(JSON.parse(line))).not.toThrow();
    }
  });
});
