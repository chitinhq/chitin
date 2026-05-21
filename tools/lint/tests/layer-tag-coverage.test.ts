// spec: 074-polyglot-monorepo-layout
import { describe, expect, it } from 'vitest';
import {
  extractDepConstraintScopeTags,
  extractDepConstraintLayerTags,
  extractLayerTagsFromPackageJson,
  extractScopeTagsFromPackageJson,
  findConventionViolations,
  findCoverageGaps,
  findNxFieldPackageJsons,
  toProjectShape,
  type PackageJsonShape,
  type ProjectShape,
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

  it('reads tags from a project.json-shape (root-level tags array)', () => {
    // libs/adapters/* and go/execution-kernel use project.json with
    // root-level `tags` rather than the nx.tags nested under nx.
    expect(extractLayerTagsFromPackageJson({
      path: 'libs/adapters/openclaw/project.json',
      json: { name: '@chitin/adapter-openclaw', tags: ['layer:adapter'] },
    })).toEqual(['layer:adapter']);
  });

  it('reads tags from BOTH nx.tags and root tags when both present', () => {
    // Edge case: a file shaped like both. Both are valid Nx locations;
    // either could land a tag. Walk both, combine, filter.
    expect(extractLayerTagsFromPackageJson(pkg({
      nx: { tags: ['layer:contracts'] },
      tags: ['layer:adapter'],
    })).sort()).toEqual(['layer:adapter', 'layer:contracts']);
  });
});

describe('extractScopeTagsFromPackageJson', () => {
  function pkg(json: unknown): PackageJsonShape {
    return { path: 'apps/test/package.json', json };
  }

  it('returns scope:* tags from nx.tags', () => {
    expect(extractScopeTagsFromPackageJson(pkg({
      nx: { tags: ['layer:cli', 'scope:app'] },
    }))).toEqual(['scope:app']);
  });

  it('reads root-level tags from project.json-shape files', () => {
    expect(extractScopeTagsFromPackageJson({
      path: 'python/analysis/project.json',
      json: { tags: ['scope:analytics', 'layer:analysis'] },
    })).toEqual(['scope:analytics']);
  });

  it('returns empty when no scope tags exist', () => {
    expect(extractScopeTagsFromPackageJson(pkg({
      nx: { tags: ['layer:contracts'] },
    }))).toEqual([]);
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

describe('extractDepConstraintScopeTags', () => {
  it('returns scope:* sourceTags', () => {
    const dc = [
      { sourceTag: 'scope:analytics', onlyDependOnLibsWithTags: ['scope:analytics'] },
      { sourceTag: 'scope:app', onlyDependOnLibsWithTags: ['scope:analytics'] },
    ];
    expect(extractDepConstraintScopeTags(dc)).toEqual(['scope:analytics', 'scope:app']);
  });

  it('skips non-scope tags', () => {
    const dc = [
      { sourceTag: 'scope:analytics', onlyDependOnLibsWithTags: [] },
      { sourceTag: 'layer:contracts', onlyDependOnLibsWithTags: [] },
    ];
    expect(extractDepConstraintScopeTags(dc)).toEqual(['scope:analytics']);
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

// ── spec 074 Phase 2: project convention gate ──────────────────────

describe('toProjectShape', () => {
  it('returns null for a package.json path', () => {
    expect(
      toProjectShape({ path: 'libs/a/package.json', json: { name: 'a' } }),
    ).toBeNull();
  });

  it('extracts name, tags, and target names from a project.json', () => {
    expect(
      toProjectShape({
        path: 'libs/run-sdk/project.json',
        json: {
          name: '@chitin/run-sdk',
          tags: ['type:lib', 'lang:ts'],
          targets: { test: {}, validate: {} },
        },
      }),
    ).toEqual({
      path: 'libs/run-sdk/project.json',
      name: '@chitin/run-sdk',
      tags: ['type:lib', 'lang:ts'],
      targetNames: ['test', 'validate'],
    });
  });

  it('falls back to the directory path when name is absent', () => {
    expect(
      toProjectShape({
        path: 'go/run-sdk/project.json',
        json: { tags: [], targets: {} },
      })?.name,
    ).toBe('go/run-sdk');
  });

  it('treats missing or non-array tags as []', () => {
    expect(
      toProjectShape({ path: 'a/project.json', json: {} })?.tags,
    ).toEqual([]);
    expect(
      toProjectShape({ path: 'a/project.json', json: { tags: 'type:lib' } })
        ?.tags,
    ).toEqual([]);
  });

  it('filters non-string tag entries', () => {
    expect(
      toProjectShape({
        path: 'a/project.json',
        json: { tags: ['type:lib', 42, null] },
      })?.tags,
    ).toEqual(['type:lib']);
  });

  it('treats missing or non-object targets as []', () => {
    expect(
      toProjectShape({ path: 'a/project.json', json: { tags: [] } })
        ?.targetNames,
    ).toEqual([]);
  });

  it('handles a non-object json without crashing', () => {
    expect(toProjectShape({ path: 'a/project.json', json: null })).toEqual({
      path: 'a/project.json',
      name: 'a',
      tags: [],
      targetNames: [],
    });
  });
});

describe('findConventionViolations', () => {
  function project(
    over: Partial<ProjectShape> & Pick<ProjectShape, 'path'>,
  ): ProjectShape {
    return {
      path: over.path,
      name: over.name ?? over.path,
      tags: over.tags ?? [
        'type:lib',
        'scope:analytics',
        'layer:contracts',
        'lang:ts',
      ],
      targetNames: over.targetNames ?? ['validate'],
    };
  }

  it('no projects → no violations', () => {
    expect(findConventionViolations([])).toEqual([]);
  });

  it('a fully compliant project → no violation', () => {
    expect(
      findConventionViolations([project({ path: 'libs/a/project.json' })]),
    ).toEqual([]);
  });

  it('extra tags beyond the required set are fine', () => {
    expect(
      findConventionViolations([
        project({
          path: 'a/project.json',
          tags: [
            'type:app',
            'scope:kernel',
            'layer:kernel',
            'lang:go',
            'runtime:local',
          ],
        }),
      ]),
    ).toEqual([]);
  });

  it('flags a missing tag namespace', () => {
    expect(
      findConventionViolations([
        project({
          path: 'a/project.json',
          name: 'a',
          tags: ['type:lib', 'scope:analytics', 'layer:contracts'],
        }),
      ]),
    ).toEqual([
      {
        project: 'a',
        path: 'a/project.json',
        missingTagNamespaces: ['lang:'],
        missingTargets: [],
      },
    ]);
  });

  it('flags an empty tag set as all four namespaces missing', () => {
    expect(
      findConventionViolations([
        project({ path: 'a/project.json', name: 'a', tags: [] }),
      ])[0]?.missingTagNamespaces,
    ).toEqual(['type:', 'scope:', 'layer:', 'lang:']);
  });

  it('flags a missing validate target', () => {
    expect(
      findConventionViolations([
        project({
          path: 'a/project.json',
          name: 'a',
          targetNames: ['build', 'test'],
        }),
      ]),
    ).toEqual([
      {
        project: 'a',
        path: 'a/project.json',
        missingTagNamespaces: [],
        missingTargets: ['validate'],
      },
    ]);
  });

  it('sorts violations by project path (deterministic output)', () => {
    const v = findConventionViolations([
      project({ path: 'z/project.json', tags: [] }),
      project({ path: 'a/project.json', tags: [] }),
      project({ path: 'm/project.json', tags: [] }),
    ]);
    expect(v.map((x) => x.path)).toEqual([
      'a/project.json',
      'm/project.json',
      'z/project.json',
    ]);
  });

  // Knuth-style: a project is in the result IFF it is missing something,
  // and every returned violation lists at least one concrete gap.
  it('partition invariant — compliant projects never appear', () => {
    const v = findConventionViolations([
      project({ path: 'good/project.json' }),
      project({ path: 'bad/project.json', tags: [], targetNames: [] }),
    ]);
    expect(v.map((x) => x.path)).toEqual(['bad/project.json']);
    for (const violation of v) {
      expect(
        violation.missingTagNamespaces.length +
          violation.missingTargets.length,
      ).toBeGreaterThan(0);
    }
  });
});

describe('findNxFieldPackageJsons', () => {
  it('flags a package.json declaring an nx field', () => {
    expect(
      findNxFieldPackageJsons([
        {
          path: 'apps/cli/package.json',
          json: { name: 'cli', nx: { tags: [] } },
        },
      ]),
    ).toEqual(['apps/cli/package.json']);
  });

  it('ignores a package.json with no nx field', () => {
    expect(
      findNxFieldPackageJsons([
        { path: 'apps/cli/package.json', json: { name: 'cli' } },
      ]),
    ).toEqual([]);
  });

  it('ignores project.json files — the single allowed mechanism', () => {
    expect(
      findNxFieldPackageJsons([
        { path: 'apps/cli/project.json', json: { nx: { tags: [] } } },
      ]),
    ).toEqual([]);
  });

  it('ignores an nx field that is null or not an object', () => {
    expect(
      findNxFieldPackageJsons([
        { path: 'a/package.json', json: { nx: null } },
        { path: 'b/package.json', json: { nx: 'yes' } },
      ]),
    ).toEqual([]);
  });

  it('skips malformed json without crashing', () => {
    expect(
      findNxFieldPackageJsons([
        { path: 'a/package.json', json: null },
        { path: 'b/package.json', json: 'not an object' },
      ]),
    ).toEqual([]);
  });

  it('returns sorted paths (deterministic output)', () => {
    expect(
      findNxFieldPackageJsons([
        { path: 'z/package.json', json: { nx: {} } },
        { path: 'a/package.json', json: { nx: {} } },
      ]),
    ).toEqual(['a/package.json', 'z/package.json']);
  });
});
