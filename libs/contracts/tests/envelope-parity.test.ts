import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { EnvelopeSchema } from '../src/envelope.schema';

// Schema-drift gate (closes #17). The shared fixture
// libs/contracts/tests/envelope.golden.json has every v2 envelope field
// populated. This test asserts:
//
//   1. zod parses the fixture without errors (zod.Envelope == fixture shape).
//   2. The fixture's key set matches EnvelopeSchema's key set + 'payload'.
//
// The Go side (go/execution-kernel/internal/event/event_parity_test.go)
// has a mirrored test that decodes the same fixture into Event and
// asserts ToMap's output preserves the key set. A field added to zod
// without the matching Go change fails the Go test; a field added to
// Go without the zod change fails the TS test.

describe('Envelope schema drift gate', () => {
  const fixture = JSON.parse(
    readFileSync(join(__dirname, 'envelope.golden.json'), 'utf8'),
  );

  it('zod parses the shared envelope fixture without error', () => {
    // Strip payload (the schema is envelope-only; payload has its own
    // discriminated schemas keyed by event_type).
    const { payload: _payload, ...envelope } = fixture;
    const parsed = EnvelopeSchema.parse(envelope);
    expect(parsed.schema_version).toBe('2');
  });

  it('fixture key set matches the EnvelopeSchema key set + payload', () => {
    const fixtureKeys = new Set(Object.keys(fixture));
    // envelope-side keys (everything except payload)
    const fixtureEnvelopeKeys = new Set(
      [...fixtureKeys].filter((k) => k !== 'payload'),
    );

    const schemaKeys = new Set(Object.keys(EnvelopeSchema.shape));

    // No extras in fixture
    const fixtureOnly = [...fixtureEnvelopeKeys].filter(
      (k) => !schemaKeys.has(k),
    );
    expect(fixtureOnly).toEqual([]);
    // No missing in fixture
    const schemaOnly = [...schemaKeys].filter(
      (k) => !fixtureEnvelopeKeys.has(k),
    );
    expect(schemaOnly).toEqual([]);
    // Fixture must include payload (Event has payload too)
    expect(fixtureKeys.has('payload')).toBe(true);
  });
});
