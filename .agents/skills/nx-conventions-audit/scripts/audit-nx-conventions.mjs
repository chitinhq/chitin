#!/usr/bin/env node
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const skillDir = dirname(dirname(fileURLToPath(import.meta.url)));
const repoRoot = findRepoRoot(process.cwd());
const conventionPath = join(repoRoot, 'docs/architecture/nx-workspace-conventions.md');

function findRepoRoot(start) {
  let dir = start;
  while (dir !== dirname(dir)) {
    if (existsSync(join(dir, 'nx.json')) && existsSync(join(dir, 'AGENTS.md'))) {
      return dir;
    }
    dir = dirname(dir);
  }
  throw new Error('Could not find repo root containing nx.json and AGENTS.md');
}

function run(command, args) {
  return execFileSync(command, args, {
    cwd: repoRoot,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  }).trim();
}

function hasTag(project, prefix) {
  return (project.tags ?? []).some((tag) => tag.startsWith(prefix));
}

function bullet(lines, text) {
  lines.push(`- ${text}`);
}

function section(lines, title, items) {
  lines.push(`\n## ${title}`);
  if (items.length === 0) {
    lines.push('\nNo findings.');
    return;
  }
  lines.push('');
  lines.push(...items);
}

const lines = [];
const projects = JSON.parse(run('pnpm', ['exec', 'nx', 'show', 'projects', '--json']));
const projectDetails = [];

for (const name of projects) {
  const detail = JSON.parse(run('pnpm', ['exec', 'nx', 'show', 'project', name, '--json']));
  projectDetails.push({ name, ...detail });
}

const workspaceYaml = existsSync(join(repoRoot, 'pnpm-workspace.yaml'))
  ? readFileSync(join(repoRoot, 'pnpm-workspace.yaml'), 'utf8')
  : '';
const eslintConfig = existsSync(join(repoRoot, 'eslint.config.mjs'))
  ? readFileSync(join(repoRoot, 'eslint.config.mjs'), 'utf8')
  : '';
const conventionExists = existsSync(conventionPath);

const tagFindings = [];
const folderFindings = [];
const boundaryFindings = [];
const cullFindings = [];
const docsFindings = [];

for (const project of projectDetails.sort((a, b) => a.name.localeCompare(b.name))) {
  const tags = project.tags ?? [];
  if (!hasTag(project, 'type:')) {
    bullet(tagFindings, `\`${project.name}\` is missing a \`type:*\` tag. Current tags: ${JSON.stringify(tags)}.`);
  }
  if (!hasTag(project, 'scope:')) {
    bullet(tagFindings, `\`${project.name}\` is missing a \`scope:*\` tag. Current tags: ${JSON.stringify(tags)}.`);
  }
  if (!hasTag(project, 'lang:')) {
    bullet(tagFindings, `\`${project.name}\` is missing a \`lang:*\` tag. Current tags: ${JSON.stringify(tags)}.`);
  }
  const staleLayerTags = tags.filter((tag) => tag.startsWith('layer:'));
  if (staleLayerTags.length > 0) {
    bullet(tagFindings, `\`${project.name}\` still uses stale layer tags: ${staleLayerTags.map((tag) => `\`${tag}\``).join(', ')}.`);
  }
  if (project.root?.startsWith('apps/') && project.root !== 'apps/cli') {
    bullet(folderFindings, `\`${project.name}\` lives under \`${project.root}\`; current conventions allow only thin runnable apps under \`apps/\`.`);
  }
}

const knownExpectedTags = new Map([
  ['@chitin/cli', ['type:app', 'scope:cli', 'lang:ts']],
  ['execution-kernel', ['type:app', 'scope:kernel', 'lang:go']],
  ['@chitin/contracts', ['type:contract', 'scope:contracts', 'lang:ts']],
  ['@chitin/telemetry', ['type:data-access', 'scope:telemetry', 'lang:ts']],
  ['@chitin/adapter-claude-code', ['type:adapter', 'scope:driver', 'lang:ts']],
  ['@chitin/adapter-openclaw', ['type:adapter', 'scope:driver', 'lang:ts']],
  ['@chitin/adapter-ollama-local', ['type:adapter', 'scope:driver', 'lang:ts']],
  ['@chitin/generators', ['type:tooling', 'scope:tooling', 'lang:ts']],
  ['@chitin/tooling-lint', ['type:tooling', 'scope:tooling', 'lang:ts']],
]);

for (const project of projectDetails) {
  const expected = knownExpectedTags.get(project.name);
  if (!expected) continue;
  const tags = new Set(project.tags ?? []);
  const missing = expected.filter((tag) => !tags.has(tag));
  if (missing.length > 0) {
    bullet(tagFindings, `\`${project.name}\` should include ${missing.map((tag) => `\`${tag}\``).join(', ')} per chitin conventions.`);
  }
}

const roots = new Map(projectDetails.map((project) => [project.root, project.name]));
const pathExpectations = [
  ['python/analysis', 'Move candidate: Python analysis code should be modeled as `libs/analysis` with `type:data-access`, `scope:analysis`, `lang:py`.'],
  ['tools/generators', 'Move candidate: repo generators should live under `libs/tooling/generators` or be explicitly documented as tooling exceptions.'],
  ['tools/lint', 'Move candidate: lint tooling should live under `libs/tooling/lint` or be explicitly documented as a tooling exception.'],
  ['libs/router-plugin-api', 'Cull candidate: router plugin API may conflict with the post-cull “not an MCP/plugin host” boundary; verify against decisions before keeping.'],
  ['examples/router-plugins', 'Cull candidate: router plugin examples may be stale after the cull; verify whether they describe live behavior.'],
  ['scratch/copilot-spike', 'Cull candidate: scratch Copilot spike is not part of the target Nx product shape.'],
];

for (const [path, message] of pathExpectations) {
  if (existsSync(join(repoRoot, path))) {
    const suffix = roots.has(path) ? ` Nx project: \`${roots.get(path)}\`.` : '';
    const target = message.startsWith('Cull') ? cullFindings : folderFindings;
    bullet(target, `${message} Current path: \`${path}\`.${suffix}`);
  }
}

if (workspaceYaml.includes("libs/router-plugin-api/typescript")) {
  bullet(boundaryFindings, '`pnpm-workspace.yaml` still includes `libs/router-plugin-api/typescript`; validate this against the cull decision.');
}
if (workspaceYaml.includes("tools/*")) {
  bullet(folderFindings, '`pnpm-workspace.yaml` includes `tools/*`; target conventions prefer repo tooling under `libs/tooling/*`.');
}
if (eslintConfig.includes('layer:')) {
  bullet(boundaryFindings, '`eslint.config.mjs` still enforces stale `layer:*` tags instead of `type:*` / `scope:*` / `lang:*` dimensions.');
}
for (const stale of ['layer:scheduler', 'layer:slack', 'layer:mcp']) {
  if (eslintConfig.includes(stale)) {
    bullet(boundaryFindings, `\`eslint.config.mjs\` still references culled or stale tag \`${stale}\`.`);
  }
}
if (!conventionExists) {
  bullet(docsFindings, '`docs/architecture/nx-workspace-conventions.md` is missing; regenerate or restore the source-derived convention report.');
}

for (const docPath of [
  'docs/observations/2026-05-02-self-improving-swarm-sota.md',
  'docs/superpowers/observations/2026-05-01-swarm-running-verification.md',
  'docs/superpowers/plans/2026-05-01-openclaw-plugin-governance.md',
]) {
  if (existsSync(join(repoRoot, docPath))) {
    bullet(docsFindings, `Review \`${docPath}\` and confirm it is historical/superseded, not active guidance.`);
  }
}

lines.push('# Nx Convention Audit');
lines.push('');
lines.push(`- Repo root: \`${repoRoot}\``);
lines.push(`- Skill: \`${skillDir}\``);
lines.push(`- Convention doc: \`${conventionPath}\`${conventionExists ? '' : ' (missing)'}`);
lines.push(`- Nx projects: ${projectDetails.length}`);

lines.push('\n## Projects');
lines.push('');
for (const project of projectDetails.sort((a, b) => a.name.localeCompare(b.name))) {
  lines.push(`- \`${project.name}\` at \`${project.root}\` tags=${JSON.stringify(project.tags ?? [])}`);
}

section(lines, 'Tag Findings', tagFindings);
section(lines, 'Folder Findings', folderFindings);
section(lines, 'Boundary Findings', boundaryFindings);
section(lines, 'Cull Findings', cullFindings);
section(lines, 'Docs Findings', docsFindings);

lines.push('\n## Next Step');
lines.push('');
lines.push('Use `spec-driven-development` to turn these findings into a phased fix spec before moving or deleting code.');

console.log(lines.join('\n'));
