// Tests for the agent-role generator itself.
// Runs the generator against an in-memory fixture workspace and asserts
// the file-generation + AST-update invariants.

import { describe, expect, it } from 'vitest';
import { createTreeWithEmptyWorkspace } from '@nx/devkit/testing';
import type { Tree } from '@nx/devkit';
import agentRoleGenerator, { type Shape } from '../generator.ts';

// Seed the minimum files the generator touches (AST updates).
function seedWorkspace(tree: Tree): void {
  tree.write(
    'libs/contracts/src/execution-request.schema.ts',
    `import { z } from 'zod';

export const RoleSchema = z.enum([
  'researcher',
  'programmer',
]);

export type Role = z.infer<typeof RoleSchema>;
`,
  );

  tree.write(
    'apps/temporal-worker/src/role-prompts.ts',
    `import type { Role } from '@chitin/contracts';
import type { BacklogEntry } from './grooming/parse-backlog.ts';

export type RolePromptBuilder = (entry: BacklogEntry) => string;

const ROLE_PROMPTS: Record<Role, RolePromptBuilder> = {
  researcher: (e) => e.description,
  programmer: (e) => e.description,
};

export const __test__ = { ROLE_PROMPTS };
`,
  );
}

const SHAPES: Shape[] = ['reviewer', 'patcher', 'analyst', 'researcher'];

describe('agentRoleGenerator — file generation', () => {
  it.each(SHAPES)('generates all scaffold files for shape: %s', async (shape) => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'test-role', shape });

    expect(tree.exists('apps/temporal-worker/src/test-role/prompt.ts')).toBe(true);
    expect(tree.exists('apps/temporal-worker/src/test-role/dispatch.ts')).toBe(true);
    expect(tree.exists('apps/temporal-worker/src/test-role/index.ts')).toBe(true);
    expect(tree.exists('apps/temporal-worker/test/test-role.test.ts')).toBe(true);
  });

  it('prompt.ts contains the role name', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'test-role', shape: 'analyst' });

    const content = tree.read('apps/temporal-worker/src/test-role/prompt.ts', 'utf-8')!;
    expect(content).toContain('test-role');
    expect(content).toContain('buildTestRolePrompt');
  });

  it('dispatch.ts reflects shape-appropriate bounds for researcher', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'test-role', shape: 'researcher' });

    const content = tree.read('apps/temporal-worker/src/test-role/dispatch.ts', 'utf-8')!;
    // researcher: network=open, write=none
    expect(content).toContain("'open'");
    expect(content).toContain("'none'");
  });

  it('dispatch.ts reflects shape-appropriate bounds for patcher', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'test-role', shape: 'patcher' });

    const content = tree.read('apps/temporal-worker/src/test-role/dispatch.ts', 'utf-8')!;
    // patcher: network=allowlist, write=branch
    expect(content).toContain("'allowlist'");
    expect(content).toContain("'branch'");
  });

  it('index.ts re-exports both prompt builder and dispatch enqueuer', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'test-role', shape: 'reviewer' });

    const content = tree.read('apps/temporal-worker/src/test-role/index.ts', 'utf-8')!;
    expect(content).toContain('buildTestRolePrompt');
    expect(content).toContain('enqueueTestRole');
  });

  it('test file contains shape-specific marker assertion for analyst', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'test-role', shape: 'analyst' });

    const content = tree.read('apps/temporal-worker/test/test-role.test.ts', 'utf-8')!;
    expect(content).toContain('<<<ANALYSIS>>>');
    expect(content).toContain('python3 -m analysis.investigate');
  });
});

describe('agentRoleGenerator — AST updates', () => {
  it('adds the role to RoleSchema', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'new-role', shape: 'reviewer' });

    const content = tree.read('libs/contracts/src/execution-request.schema.ts', 'utf-8')!;
    expect(content).toContain("'new-role'");
  });

  it('adds import to role-prompts.ts', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'new-role', shape: 'reviewer' });

    const content = tree.read('apps/temporal-worker/src/role-prompts.ts', 'utf-8')!;
    // ts-morph may use single or double quotes; check the path substring only
    expect(content).toContain('./new-role/prompt.ts');
    expect(content).toContain('buildNewRolePrompt');
  });

  it('adds ROLE_PROMPTS entry to role-prompts.ts', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'new-role', shape: 'reviewer' });

    const content = tree.read('apps/temporal-worker/src/role-prompts.ts', 'utf-8')!;
    expect(content).toContain("'new-role'");
    expect(content).toContain('buildNewRolePrompt');
  });
});

describe('agentRoleGenerator — idempotency', () => {
  it('re-running does not duplicate RoleSchema entry', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'dup-role', shape: 'reviewer' });
    await agentRoleGenerator(tree, { name: 'dup-role', shape: 'reviewer' });

    const content = tree.read('libs/contracts/src/execution-request.schema.ts', 'utf-8')!;
    const matches = content.match(/'dup-role'/g) ?? [];
    expect(matches).toHaveLength(1);
  });

  it('re-running does not duplicate import in role-prompts.ts', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'dup-role', shape: 'patcher' });
    await agentRoleGenerator(tree, { name: 'dup-role', shape: 'patcher' });

    const content = tree.read('apps/temporal-worker/src/role-prompts.ts', 'utf-8')!;
    // Match either quote style — ts-morph may emit double quotes
    const matches = content.match(/from ['"]\.\/dup-role\/prompt\.ts['"]/g) ?? [];
    expect(matches).toHaveLength(1);
  });

  it('re-running does not duplicate ROLE_PROMPTS entry', async () => {
    const tree = createTreeWithEmptyWorkspace();
    seedWorkspace(tree);

    await agentRoleGenerator(tree, { name: 'dup-role', shape: 'analyst' });
    await agentRoleGenerator(tree, { name: 'dup-role', shape: 'analyst' });

    const content = tree.read('apps/temporal-worker/src/role-prompts.ts', 'utf-8')!;
    // Count occurrences of the property key (not counting the import line)
    const propMatches = content.match(/'dup-role'\s*:/g) ?? [];
    expect(propMatches).toHaveLength(1);
  });
});
