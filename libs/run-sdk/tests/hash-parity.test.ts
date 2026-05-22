import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { hashEvent } from '@chitin/contracts';

/**
 * Cross-language hash parity (spec 086, FR-002 / FR-005 / SC-001).
 *
 * For every event in the shared parity corpus, the TypeScript `hashEvent`
 * (the cross-language reference of record) must produce a hash byte-identical
 * to both the corpus's baked `expected_hash` and the live Go `chainhash`
 * output. If any of the three diverge, this test fails — that is the guard
 * that keeps the consolidation consolidated.
 */
describe('cross-language hash parity', () => {
  it('TypeScript hashEvent agrees with Go chainhash for every corpus event', () => {
    const chainhashDir = new URL('../../../go/chainhash/', import.meta.url);
    const corpusPath = new URL('testdata/parity-corpus.json', chainhashDir);

    const corpus = JSON.parse(readFileSync(corpusPath, 'utf8')) as Array<{
      name: string;
      event: Record<string, unknown>;
      expected_hash: string;
    }>;
    expect(corpus.length).toBeGreaterThan(0);

    // Live Go hashes — `go` resolved from PATH (CI's setup-go installs it
    // outside /usr/local/go, so a hardcoded path fails there).
    const goOutput = execFileSync(
      process.env.GO_BINARY ?? 'go',
      ['run', './cmd/hash-fixture'],
      { cwd: chainhashDir, encoding: 'utf8' },
    );
    const goHashes = new Map(
      (JSON.parse(goOutput) as Array<{ name: string; hash: string }>).map(
        (r) => [r.name, r.hash] as const,
      ),
    );

    for (const entry of corpus) {
      const tsHash = hashEvent(entry.event);
      // TypeScript agrees with the baked corpus value.
      expect(tsHash, `${entry.name}: TypeScript vs corpus`).toBe(entry.expected_hash);
      // Go agrees with the baked corpus value.
      expect(goHashes.get(entry.name), `${entry.name}: Go vs corpus`).toBe(
        entry.expected_hash,
      );
      // ...and therefore TypeScript and Go agree with each other.
      expect(tsHash, `${entry.name}: TypeScript vs Go`).toBe(goHashes.get(entry.name));
    }
  });
});
