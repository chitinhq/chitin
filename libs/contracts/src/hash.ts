import { createHash } from 'node:crypto';

/**
 * Canonical JSON: keys sorted lexicographically at every level, no whitespace, UTF-8.
 * Used as SHA-256 hash input for event records.
 */
export function canonicalJSON(value: unknown): string {
  if (value === null || typeof value !== 'object') {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return '[' + value.map(canonicalJSON).join(',') + ']';
  }
  const keys = Object.keys(value as Record<string, unknown>).sort();
  const pairs = keys.map(
    (k) => JSON.stringify(k) + ':' + canonicalJSON((value as Record<string, unknown>)[k]),
  );
  return '{' + pairs.join(',') + '}';
}

export function sha256Hex(input: string): string {
  return createHash('sha256').update(input, 'utf8').digest('hex');
}

/**
 * Hash an event record excluding its own `this_hash` field from the hash input.
 */
export function hashEvent(event: Record<string, unknown>): string {
  const { this_hash: _ignored, ...rest } = event;
  return sha256Hex(canonicalJSON(rest));
}
