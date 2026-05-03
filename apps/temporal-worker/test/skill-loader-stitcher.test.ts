import { describe, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  inlineCompanions,
  loadSkill,
  parseSimpleYaml,
  renderSkill,
  renderSkillBody,
  splitFrontmatter,
  substituteVars,
  skillFolderForRole,
} from '../src/skill-loader/stitcher.ts';

describe('splitFrontmatter', () => {
  it('extracts YAML frontmatter and body', () => {
    const text = '---\nname: foo\ndescription: bar\n---\n\nThe body.\n';
    const { frontmatterText, body } = splitFrontmatter(text);
    expect(frontmatterText).toBe('name: foo\ndescription: bar');
    expect(body).toBe('\nThe body.\n');
  });

  it('returns empty frontmatter for files without it', () => {
    const text = 'No frontmatter, just body.\n';
    const { frontmatterText, body } = splitFrontmatter(text);
    expect(frontmatterText).toBe('');
    expect(body).toBe(text);
  });

  it('handles unclosed frontmatter as body-only (loose)', () => {
    const text = '---\nname: foo\n\nNo closing fence';
    const { frontmatterText } = splitFrontmatter(text);
    expect(frontmatterText).toBe('');
  });
});

describe('parseSimpleYaml', () => {
  it('parses scalar key:value', () => {
    expect(parseSimpleYaml('name: foo\ndescription: bar')).toEqual({
      name: 'foo',
      description: 'bar',
    });
  });

  it('parses inline arrays', () => {
    expect(parseSimpleYaml('name: foo\ntools: [gh, exec, read]')).toEqual({
      name: 'foo',
      description: '',
      tools: ['gh', 'exec', 'read'],
    });
  });

  it('parses block arrays', () => {
    const yaml = `name: foo\ntools:\n  - gh\n  - exec\n  - read`;
    expect(parseSimpleYaml(yaml)).toEqual({
      name: 'foo',
      description: '',
      tools: ['gh', 'exec', 'read'],
    });
  });

  it('strips quotes from quoted scalars', () => {
    expect(parseSimpleYaml('name: foo\ndescription: "with quotes"')).toEqual({
      name: 'foo',
      description: 'with quotes',
    });
  });

  it('throws on missing required `name`', () => {
    expect(() => parseSimpleYaml('description: bar')).toThrow(/name/);
  });

  it('preserves extra fields without strict-schema rejection', () => {
    const parsed = parseSimpleYaml('name: foo\ntier_hint: T2\ncustom_field: hello');
    expect(parsed.tier_hint).toBe('T2');
    expect(parsed.custom_field).toBe('hello');
  });
});

describe('substituteVars', () => {
  it('substitutes simple {{var}} tokens', () => {
    expect(substituteVars('hello {{name}}', { name: 'world' })).toBe('hello world');
  });

  it('substitutes dot-paths', () => {
    expect(
      substituteVars('id={{entry.id}}', { entry: { id: 'pr-199' } }),
    ).toBe('id=pr-199');
  });

  it('substitutes deeper dot-paths', () => {
    expect(
      substituteVars('{{a.b.c}}', { a: { b: { c: 'deep' } } }),
    ).toBe('deep');
  });

  it('JSON-stringifies non-string values', () => {
    expect(substituteVars('{{n}}', { n: 42 })).toBe('42');
    expect(substituteVars('{{xs}}', { xs: ['a', 'b'] })).toBe('["a","b"]');
  });

  it('throws on missing variable', () => {
    expect(() => substituteVars('{{missing}}', {})).toThrow(/{{missing}}/);
  });

  it('throws on non-object dot-path traversal', () => {
    expect(() => substituteVars('{{a.b.c}}', { a: { b: 'leaf' } })).toThrow(/non-object/);
  });

  it('throws on null value', () => {
    expect(() => substituteVars('{{n}}', { n: null })).toThrow(/null/);
  });

  it('leaves ungrouped braces untouched', () => {
    // Single braces should NOT be matched
    expect(substituteVars('plain { not a var }', {})).toBe('plain { not a var }');
  });
});

describe('inlineCompanions', () => {
  it('inlines [label](./companion.md) links', () => {
    const body = 'See [the rules](./rules.md) for details.';
    const out = inlineCompanions(body, (path) => {
      expect(path).toBe('rules.md');
      return 'RULE 1\nRULE 2';
    });
    expect(out).toContain('--- the rules (rules.md) ---');
    expect(out).toContain('RULE 1\nRULE 2');
    expect(out).toContain('--- end of rules.md ---');
  });

  it('rejects parent traversal in companion paths (./../ form)', () => {
    // The regex matches `./<path>` shape; `./../escape.md` matches and
    // then the explicit ".."-rejection fires.
    const body = '[bad](./../escape.md)';
    expect(() =>
      inlineCompanions(body, () => 'never reached'),
    ).toThrow(/\.\./);
  });

  it('does not match bare ../ (no leading ./) — those silently pass through', () => {
    // The regex requires `./` or a `.md`-suffixed bare name, so `../x`
    // doesn't match and isn't inlined. That's a fail-safe: the link is
    // left as-is, agent sees a literal markdown link, no file is read.
    const body = '[bad](../escape.md)';
    const out = inlineCompanions(body, () => 'should not run');
    expect(out).toBe(body);
  });

  it('leaves non-link markdown alone', () => {
    const body = 'Just text. [external link](https://example.com).';
    const out = inlineCompanions(body, () => 'should not run');
    expect(out).toBe(body);
  });
});

describe('renderSkillBody — composition', () => {
  it('substitutes vars, then inlines companions', () => {
    const body = 'Hello {{name}}.\n\nSee [doc](./{{file}}.md).';
    const out = renderSkillBody(
      body,
      { name: 'agent', file: 'companion' },
      (path) => {
        expect(path).toBe('companion.md');
        return 'companion content';
      },
    );
    expect(out).toContain('Hello agent.');
    expect(out).toContain('companion content');
  });
});

describe('loadSkill / renderSkill — fixture roundtrip', () => {
  function makeFixture(): string {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-skill-'));
    writeFileSync(
      join(dir, 'SKILL.md'),
      `---
name: test-skill
description: A test
tier_hint: T2
tools: [gh]
---

You are a test agent.

ENTRY: {{entry.id}}

See [the checklist](./checklist.md).
`,
    );
    writeFileSync(
      join(dir, 'checklist.md'),
      'Item 1\nItem 2\nItem 3\n',
    );
    return dir;
  }

  it('loadSkill parses frontmatter + body', () => {
    const dir = makeFixture();
    const skill = loadSkill(dir);
    expect(skill.frontmatter.name).toBe('test-skill');
    expect(skill.frontmatter.tier_hint).toBe('T2');
    expect(skill.frontmatter.tools).toEqual(['gh']);
    expect(skill.body).toContain('You are a test agent.');
  });

  it('renderSkill substitutes vars + inlines companions end-to-end', () => {
    const dir = makeFixture();
    const out = renderSkill(dir, { entry: { id: 'pr-199' } });
    expect(out).toContain('You are a test agent.');
    expect(out).toContain('ENTRY: pr-199');
    expect(out).toContain('Item 1');
    expect(out).toContain('Item 2');
    expect(out).toContain('Item 3');
  });

  it('throws when SKILL.md is missing', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-empty-'));
    expect(() => loadSkill(dir)).toThrow(/SKILL\.md/);
  });

  it('throws when a companion file is referenced but missing', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-broken-'));
    writeFileSync(
      join(dir, 'SKILL.md'),
      `---\nname: broken\n---\n\n[missing](./not-here.md)`,
    );
    expect(() => renderSkill(dir, {})).toThrow(/not-here\.md/);
  });
});

describe('skillFolderForRole', () => {
  it('resolves the conventional path', () => {
    expect(skillFolderForRole('/repo', 'peer-reviewer')).toBe(
      '/repo/apps/temporal-worker/skills/peer-reviewer',
    );
  });
});

describe('renderSkill — with the bundled peer-reviewer fixture', () => {
  it('renders the real peer-reviewer skill folder', () => {
    // Resolve against the worktree root so the test runs from
    // wherever vitest is launched.
    const folder = join(
      process.cwd(),
      'apps/temporal-worker/skills/peer-reviewer',
    );
    const out = renderSkill(folder, {
      entry: {
        id: 'peer-review-pr-207',
        description: 'Peer-review PR https://github.com/chitinhq/chitin/pull/207',
      },
    });
    // Spot-check the rendering: vars substituted, companions inlined.
    expect(out).toContain('peer-review-pr-207');
    expect(out).toContain('https://github.com/chitinhq/chitin/pull/207');
    // Companion content present
    expect(out).toContain('Correctness');
    expect(out).toContain('Scope drift');
    expect(out).toContain('Security');
    expect(out).toContain('🔴 (real bug) findings:');
    // Step-0 dispatch-shape guard preserved
    expect(out).toMatch(/Verify your dispatch shape FIRST/);
  });
});
