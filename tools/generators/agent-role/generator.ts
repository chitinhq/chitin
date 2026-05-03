// Nx generator: `nx g @chitin/agent-role:agent-role <name> --shape <shape>`
//
// Creates the cross-cutting set every new agent role touches:
//   - apps/temporal-worker/src/<name>/prompt.ts   (shape-specific prompt builder)
//   - apps/temporal-worker/src/<name>/dispatch.ts  (enqueue helper)
//   - apps/temporal-worker/src/<name>/index.ts     (barrel)
//   - apps/temporal-worker/test/<name>.test.ts     (stub tests)
// Updates (AST-aware, idempotent):
//   - libs/contracts/src/execution-request.schema.ts  (RoleSchema enum)
//   - apps/temporal-worker/src/role-prompts.ts         (import + ROLE_PROMPTS entry)

import { type Tree, joinPathFragments, generateFiles, names } from '@nx/devkit';
import { Project, SyntaxKind, type SourceFile } from 'ts-morph';
import { fileURLToPath } from 'node:url';
import { dirname } from 'node:path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

export type Shape = 'reviewer' | 'patcher' | 'analyst' | 'researcher';

export interface AgentRoleGeneratorOptions {
  name: string;
  shape: Shape;
}

// Per-shape default bounds. Network + write_policy enforce the role's
// trust level; the numeric bounds are conservative but non-trivial.
const SHAPE_DEFAULTS: Record<
  Shape,
  {
    networkPolicy: string;
    writePolicy: string;
    maxToolCalls: number;
    maxCostUsd: number;
    wallTimeoutS: number;
  }
> = {
  // reviewer: read-only. Reads diff, posts review comment. No writes.
  reviewer: {
    networkPolicy: 'allowlist',
    writePolicy: 'none',
    maxToolCalls: 60,
    maxCostUsd: 2.0,
    wallTimeoutS: 1800,
  },
  // patcher: commits fix to PR's branch. Bounded at R3 ceiling (30 min).
  patcher: {
    networkPolicy: 'allowlist',
    writePolicy: 'branch',
    maxToolCalls: 80,
    maxCostUsd: 3.0,
    wallTimeoutS: 1800,
  },
  // analyst: writes report to worktree. Recipe-driven; no open network.
  analyst: {
    networkPolicy: 'allowlist',
    writePolicy: 'worktree',
    maxToolCalls: 40,
    maxCostUsd: 2.0,
    wallTimeoutS: 1200,
  },
  // researcher: open network for external signal fetches; no code writes.
  researcher: {
    networkPolicy: 'open',
    writePolicy: 'none',
    maxToolCalls: 40,
    maxCostUsd: 2.0,
    wallTimeoutS: 1200,
  },
};

export async function agentRoleGenerator(
  tree: Tree,
  options: AgentRoleGeneratorOptions,
): Promise<void> {
  const { name, shape } = options;
  const n = names(name);
  const bounds = SHAPE_DEFAULTS[shape];

  // Copy EJS templates from ./files/ into the workspace root.
  // __name__ in paths → name; __tmpl__ → '' (strips the suffix).
  generateFiles(tree, joinPathFragments(__dirname, 'files'), '.', {
    ...n,
    name,
    shape,
    ...bounds,
    tmpl: '',
  });

  updateRoleSchema(tree, name);
  updateRolePrompts(tree, name, n.className);
}

function parseSf(tree: Tree, filePath: string): SourceFile {
  const content = tree.read(filePath, 'utf-8');
  if (content === null) throw new Error(`Generator: cannot read ${filePath}`);
  const project = new Project({ useInMemoryFileSystem: true });
  return project.createSourceFile(filePath, content);
}

// Add `name` to the RoleSchema z.enum([...]) array.
// Invariant: after this call, RoleSchema contains exactly one occurrence of name.
function updateRoleSchema(tree: Tree, name: string): void {
  const filePath = 'libs/contracts/src/execution-request.schema.ts';
  const sf = parseSf(tree, filePath);

  const decl = sf.getVariableDeclaration('RoleSchema');
  if (!decl) throw new Error('Generator: RoleSchema declaration not found');

  const callExpr = decl.getInitializerIfKindOrThrow(SyntaxKind.CallExpression);
  const [arg] = callExpr.getArguments();
  if (!arg) throw new Error('Generator: RoleSchema z.enum() has no arguments');

  const arr = arg.asKindOrThrow(SyntaxKind.ArrayLiteralExpression);
  const existing = arr.getElements().map((e) => e.getText().replace(/['"]/g, ''));

  if (existing.includes(name)) return; // idempotent

  arr.addElement(`'${name}'`);
  tree.write(filePath, sf.getFullText());
}

// Add the import for build<ClassName>Prompt and a ROLE_PROMPTS entry.
// Invariant: after this call, exactly one import and one entry for name exist.
function updateRolePrompts(tree: Tree, name: string, className: string): void {
  const filePath = 'apps/temporal-worker/src/role-prompts.ts';
  const sf = parseSf(tree, filePath);

  const builderName = `build${className}Prompt`;
  const importPath = `./${name}/prompt.ts`;

  // Add import if not already present.
  const alreadyImported = sf
    .getImportDeclarations()
    .some((imp) => imp.getModuleSpecifierValue() === importPath);

  if (!alreadyImported) {
    sf.addImportDeclaration({
      namedImports: [builderName],
      moduleSpecifier: importPath,
    });
  }

  // Add ROLE_PROMPTS entry if not already present.
  const decl = sf.getVariableDeclaration('ROLE_PROMPTS');
  if (!decl) throw new Error('Generator: ROLE_PROMPTS declaration not found');

  const objLit = decl.getInitializerIfKindOrThrow(SyntaxKind.ObjectLiteralExpression);

  const existingKeys = objLit.getProperties().map((p) => {
    // Properties may be quoted ('peer-reviewer') or unquoted (programmer).
    const text = p.getText();
    const quoted = text.match(/^['"]([^'"]+)['"]/);
    return quoted ? quoted[1] : text.split(':')[0].trim();
  });

  if (!existingKeys.includes(name)) {
    objLit.addPropertyAssignment({
      name: `'${name}'`,
      initializer: builderName,
    });
  }

  tree.write(filePath, sf.getFullText());
}

export default agentRoleGenerator;
