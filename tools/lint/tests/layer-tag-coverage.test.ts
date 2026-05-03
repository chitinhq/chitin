import { describe, expect, it } from 'vitest';
import {
  extractDepConstraintLayerTags,
  extractLayerTagsFromPackageJson,
  findCoverageGaps,
  type PackageJsonShape,
} from '../layer-tag-coverage.ts';

describe('extractLayerTagsFromPackageJson', () => {
  function pkg(json: unknown): PackageJsonShape {
    return { path: 'apps/test/package.json', json };
  }

  it('returns layer:* tags from nx.tags', () => {
    expect(extractLayerTagsFromPackageJson(pkg({
      nx: { tags: ['layer:scheduler', 'scope:lib'] },
    }))).toEqual(['layer:scheduler']);
  });

  it('returns multiple layer:* tags', () => {
    expect(extractLayerTagsFromPackageJson(pkg({
      nx: { tags: ['layer:app', 'layer:slack', 'scope:app'] },
    }))).toEqual(['layer:app', 'layer:slack']);
  });

  it('returns empty for package with no nx field', () => {
    expect(extractLayerTagsFromPackageJson(pkg({ name: 'pkg' }))).toEqual([]);
  });

  it('returns empty for package with no tags', () => {
    expect(extractLayerTagsFromPackageJson(pkg({ nx: { targets: {} } }))).toEqual([]);
  });

  it('returns empty when nx.tags is not an array', () => {
    expect(extractLayerTagsFromPackageJson(pkg({ nx: { tags: 'layer:foo' } }))).toEqual([]);
  });

  it('filters out non-layer tags', () => {
    expect(extractLayerTagsFromPackageJson(pkg({
      nx: { tags: ['scope:lib', 'foo:bar', 'layer:contracts'] },
    }))).toEqual(['layer:contracts']);
  });

  it('handles malformed json gracefully', () => {
    expect(extractLayerTagsFromPackageJson(pkg(null))).toEqual([]);
    expect(extractLayerTagsFromPackageJson(pkg(undefined))).toEqual([]);
    expect(extractLayerTagsFromPackageJson(pkg('not an object'))).toEqual([]);
  });
});

describe('extractDepConstraintLayerTags', () => {
  it('returns layer:* sourceTags', () => {
    const dc = [
      { sourceTag: 'layer:contracts', onlyDependOnLibsWithTags: [] },
      { sourceTag: 'layer:telemetry', onlyDependOnLibsWithTags: ['layer:contracts'] },
    ];
    expect(extractDepConstraintLayerTags(dc)).toEqual(['layer:contracts', 'layer:telemetry']);
  });

  it('skips non-layer sourceTags', () => {
    const dc = [
      { sourceTag: 'layer:contracts', onlyDependOnLibsWithTags: [] },
      { sourceTag: 'scope:lib', onlyDependOnLibsWithTags: [] },
    ];
    expect(extractDepConstraintLayerTags(dc)).toEqual(['layer:contracts']);
  });

  it('skips entries with non-string sourceTag', () => {
    const dc = [{ sourceTag: 'layer:foo' }, { sourceTag: 123 }, {}];
    expect(extractDepConstraintLayerTags(dc)).toEqual(['layer:foo']);
  });
});

describe('findCoverageGaps — invariants + cases', () => {
  function tagsMap(...entries: [string, string[]][]): Map<string, string[]> {
    return new Map(entries);
  }

  it('zero packages, zero constraints → no gaps', () => {
    const gaps = findCoverageGaps(tagsMap(), []);
    expect(gaps.missing).toEqual([]);
    expect(gaps.orphaned).toEqual([]);
  });

  it('all packages covered → no missing', () => {
    const gaps = findCoverageGaps(
      tagsMap(['libs/a/package.json', ['layer:contracts']]),
      ['layer:contracts'],
    );
    expect(gaps.missing).toEqual([]);
    expect(gaps.orphaned).toEqual([]);
  });

  it('package with uncovered tag → missing entry', () => {
    const gaps = findCoverageGaps(
      tagsMap(['apps/x/package.json', ['layer:slack']]),
      ['layer:contracts'],
    );
    expect(gaps.missing).toEqual([
      { tag: 'layer:slack', foundIn: ['apps/x/package.json'] },
    ]);
    expect(gaps.orphaned).toEqual(['layer:contracts']);
  });

  it('multiple packages share a missing tag → grouped under one entry', () => {
    const gaps = findCoverageGaps(
      tagsMap(
        ['apps/x/package.json', ['layer:scheduler']],
        ['libs/y/package.json', ['layer:scheduler']],
      ),
      [],
    );
    expect(gaps.missing).toEqual([
      {
        tag: 'layer:scheduler',
        foundIn: ['apps/x/package.json', 'libs/y/package.json'],
      },
    ]);
  });

  it('depConstraint without a package → orphaned (warn only)', () => {
    const gaps = findCoverageGaps(
      tagsMap(['libs/a/package.json', ['layer:contracts']]),
      ['layer:contracts', 'layer:kernel'],
    );
    expect(gaps.missing).toEqual([]);
    expect(gaps.orphaned).toEqual(['layer:kernel']);
  });

  it('mixed scenario — some covered, some missing, some orphaned', () => {
    const gaps = findCoverageGaps(
      tagsMap(
        ['libs/a/package.json', ['layer:contracts']],
        ['libs/b/package.json', ['layer:slack']],
        ['apps/c/package.json', ['layer:app']],
      ),
      ['layer:contracts', 'layer:kernel', 'layer:app'],
    );
    expect(gaps.missing).toEqual([
      { tag: 'layer:slack', foundIn: ['libs/b/package.json'] },
    ]);
    expect(gaps.orphaned).toEqual(['layer:kernel']);
  });

  it('missing entries sorted by tag (deterministic output)', () => {
    const gaps = findCoverageGaps(
      tagsMap(
        ['libs/z/package.json', ['layer:zeta']],
        ['libs/a/package.json', ['layer:alpha']],
      ),
      [],
    );
    expect(gaps.missing.map((m) => m.tag)).toEqual(['layer:alpha', 'layer:zeta']);
  });

  it('foundIn paths sorted (deterministic output)', () => {
    const gaps = findCoverageGaps(
      tagsMap(
        ['libs/z/package.json', ['layer:foo']],
        ['libs/a/package.json', ['layer:foo']],
        ['libs/m/package.json', ['layer:foo']],
      ),
      [],
    );
    expect(gaps.missing[0]?.foundIn).toEqual([
      'libs/a/package.json',
      'libs/m/package.json',
      'libs/z/package.json',
    ]);
  });

  it('package with multiple tags — each evaluated independently', () => {
    const gaps = findCoverageGaps(
      tagsMap(['apps/x/package.json', ['layer:app', 'layer:slack']]),
      ['layer:app'],
    );
    expect(gaps.missing).toEqual([
      { tag: 'layer:slack', foundIn: ['apps/x/package.json'] },
    ]);
  });
});

describe('findCoverageGaps — partition invariant', () => {
  // Knuth-style: every input layer tag is accounted for either by
  // matching a constraint OR by appearing in `missing`. Nothing is
  // silently dropped.
  it('every input tag is either matched or in missing', () => {
    const inputs = new Map<string, string[]>([
      ['libs/a/package.json', ['layer:contracts', 'layer:scheduler']],
      ['apps/b/package.json', ['layer:app']],
    ]);
    const constraints = ['layer:contracts'];
    const gaps = findCoverageGaps(inputs, constraints);
    const allInputTags = new Set<string>();
    for (const tags of inputs.values()) for (const t of tags) allInputTags.add(t);
    const matched = new Set([...allInputTags].filter((t) => constraints.includes(t)));
    const missing = new Set(gaps.missing.map((m) => m.tag));
    const accountedFor = new Set([...matched, ...missing]);
    expect(accountedFor).toEqual(allInputTags);
  });
});
