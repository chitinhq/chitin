import { describe, expect, it } from 'vitest';
import { canonicalJSON, sha256Hex, hashEvent } from '../src/hash';

describe('canonicalJSON', () => {
  it('sorts keys lexicographically', () => {
    expect(canonicalJSON({ b: 1, a: 2 })).toBe('{"a":2,"b":1}');
  });

  it('emits no whitespace', () => {
    expect(canonicalJSON({ a: [1, 2, 3] })).toBe('{"a":[1,2,3]}');
  });

  it('sorts nested object keys', () => {
    expect(canonicalJSON({ z: { b: 1, a: 2 } })).toBe('{"z":{"a":2,"b":1}}');
  });

  it('preserves array order', () => {
    expect(canonicalJSON([3, 1, 2])).toBe('[3,1,2]');
  });

  it('handles nulls', () => {
    expect(canonicalJSON({ a: null })).toBe('{"a":null}');
  });
});

describe('sha256Hex', () => {
  it('computes known hash of empty string', () => {
    expect(sha256Hex('')).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
  });

  it('computes known hash of "abc"', () => {
    expect(sha256Hex('abc')).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
  });

  it('is deterministic', () => {
    const h1 = sha256Hex('chitin');
    const h2 = sha256Hex('chitin');
    expect(h1).toBe(h2);
  });
});

describe('hashEvent', () => {
  it('excludes this_hash field from hash input', () => {
    const e1 = { a: 1, b: 2, this_hash: 'xyz' };
    const e2 = { a: 1, b: 2, this_hash: 'abc' };
    expect(hashEvent(e1)).toBe(hashEvent(e2));
  });

  it('differs when other fields differ', () => {
    expect(hashEvent({ a: 1, this_hash: '' })).not.toBe(hashEvent({ a: 2, this_hash: '' }));
  });

  it('produces 64-char hex', () => {
    expect(hashEvent({ x: 'y', this_hash: '' })).toMatch(/^[a-f0-9]{64}$/);
  });
});
