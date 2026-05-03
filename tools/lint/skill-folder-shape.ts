// Linter for skill folder shape in apps/temporal-worker/skills/**
// Implements 8 rules as described in backlog entry lint-skill-folder-shape
// Pure rules, dynamic-import I/O, Nx target, CI step handled elsewhere

import fs from 'fs/promises';
import path from 'path';
import matter from 'gray-matter';
import { unified } from 'unified';
import remarkParse from 'remark-parse';
import { visit } from 'unist-util-visit';

// Configurable per-tier budgets
const TOKEN_BUDGETS = {
  T0: parseInt(process.env.SKILL_T0_TOKEN_BUDGET || '1500', 10),
  T1: parseInt(process.env.SKILL_T1_TOKEN_BUDGET || '3000', 10),
  T2: parseInt(process.env.SKILL_T2_TOKEN_BUDGET || '6000', 10),
  T3: parseInt(process.env.SKILL_T3_TOKEN_BUDGET || '6000', 10),
  T4: parseInt(process.env.SKILL_T4_TOKEN_BUDGET || '6000', 10),
};
const TOOL_BUDGETS = {
  T0: parseInt(process.env.SKILL_T0_TOOL_BUDGET || '6', 10),
  T1: parseInt(process.env.SKILL_T1_TOOL_BUDGET || '12', 10),
  T2: parseInt(process.env.SKILL_T2_TOOL_BUDGET || '25', 10),
  T3: parseInt(process.env.SKILL_T3_TOOL_BUDGET || '25', 10),
  T4: parseInt(process.env.SKILL_T4_TOOL_BUDGET || '25', 10),
};

// Utility: crude token count (words * 1.3)
function estimateTokens(text: string): number {
  return Math.ceil(text.split(/\s+/).length * 1.3);
}

// Rule 1: Every skill folder has a SKILL.md
async function checkSkillMdExists(skillDir: string, errors: string[]) {
  const skillMd = path.join(skillDir, 'SKILL.md');
  try {
    await fs.access(skillMd);
  } catch {
    errors.push(`Missing SKILL.md in ${skillDir}`);
  }
}

// Rule 2: SKILL.md has required frontmatter fields
function checkFrontmatterFields(frontmatter: any, skillDir: string, errors: string[]) {
  const required = ['name', 'activation', 'tier_hint'];
  for (const field of required) {
    if (!(field in frontmatter)) {
      errors.push(`Missing frontmatter field '${field}' in ${skillDir}/SKILL.md`);
    }
  }
  if (!('tools' in frontmatter) && !('no tools' in frontmatter)) {
    errors.push(`Missing 'tools' or 'no tools' in frontmatter of ${skillDir}/SKILL.md`);
  }
}

// Rule 3: Referenced files exist
async function checkReferencedFiles(ast: any, skillDir: string, errors: string[]) {
  const referenced: string[] = [];
  visit(ast, 'link', (node: any) => {
    if (typeof node.url === 'string' && !node.url.startsWith('http')) {
      referenced.push(node.url);
    }
  });
  for (const relPath of referenced) {
    const absPath = path.join(skillDir, relPath);
    try {
      await fs.access(absPath);
    } catch {
      errors.push(`Referenced file not found: ${relPath} in ${skillDir}/SKILL.md`);
    }
  }
}

// Rule 4: Token count caps per tier_hint
function checkTokenBudget(tier: string, content: string, skillDir: string, errors: string[]) {
  const budget = TOKEN_BUDGETS[tier as keyof typeof TOKEN_BUDGETS];
  if (!budget) return;
  const tokens = estimateTokens(content);
  if (tokens > budget) {
    errors.push(`Token count ${tokens} exceeds budget ${budget} for tier ${tier} in ${skillDir}/SKILL.md`);
  }
}

// Rule 5: Tool count caps per tier
function checkToolBudget(tier: string, frontmatter: any, skillDir: string, errors: string[]) {
  const budget = TOOL_BUDGETS[tier as keyof typeof TOOL_BUDGETS];
  if (!budget) return;
  let count = 0;
  if (Array.isArray(frontmatter.tools)) count = frontmatter.tools.length;
  if (count > budget) {
    errors.push(`Tool count ${count} exceeds budget ${budget} for tier ${tier} in ${skillDir}/SKILL.md`);
  }
}

// Rule 6: Required sections: INVARIANTS and DON'T blocks
function checkRequiredSections(ast: any, skillDir: string, errors: string[]) {
  let hasInvariants = false, hasDont = false;
  visit(ast, 'heading', (node: any) => {
    const text = node.children?.map((c: any) => c.value).join('')?.toUpperCase() || '';
    if (text.includes('INVARIANT')) hasInvariants = true;
    if (text.includes("DON'T") || text.includes('DONT')) hasDont = true;
  });
  if (!hasInvariants) errors.push(`Missing INVARIANTS section in ${skillDir}/SKILL.md`);
  if (!hasDont) errors.push(`Missing DON'T section in ${skillDir}/SKILL.md`);
}

// Rule 7: Output marker convention
function checkOutputMarkers(ast: any, skillDir: string, errors: string[]) {
  let markerMentioned = false;
  let markerName = '';
  visit(ast, 'code', (node: any) => {
    if (node.value && node.value.match(/^<<<[A-Z0-9_\-]+>>>{json}/)) {
      markerMentioned = true;
      markerName = node.value.match(/^<<<([A-Z0-9_\-]+)>>>{json}/)?.[1] || '';
    }
  });
  if (markerMentioned && markerName) {
    let found = false;
    visit(ast, 'text', (node: any) => {
      if (typeof node.value === 'string' && node.value.includes(markerName)) found = true;
    });
    if (!found) errors.push(`Output marker <<<${markerName}>>>{json} not named in SKILL.md in ${skillDir}`);
  }
}

// Rule 8: Source-of-truth check for verify/validate/confirm
function checkSourceOfTruth(ast: any, skillDir: string, errors: string[]) {
  let lastLine = -2;
  let lastFileMention = false;
  visit(ast, (node: any) => {
    if (node.type === 'text' && /verify|validate|confirm/i.test(node.value)) {
      lastLine = node.position?.start.line || -2;
      lastFileMention = false;
    }
    if (node.type === 'text' && /\.(md|ts|json|yaml|yml|txt|js|py|go|sh|test)/i.test(node.value)) {
      if (node.position?.start.line === lastLine + 1 || node.position?.start.line === lastLine) {
        lastFileMention = true;
      }
    }
    if (lastLine > 0 && !lastFileMention) {
      errors.push(`Line with verify/validate/confirm missing file/path/test reference in ${skillDir}/SKILL.md`);
      lastLine = -2;
    }
  });
}

// Main linter
export async function lintSkillFolderShape(rootDir: string) {
  const skillsRoot = path.join(rootDir, 'apps/temporal-worker/skills');
  const skillDirs = (await fs.readdir(skillsRoot, { withFileTypes: true }))
    .filter(d => d.isDirectory())
    .map(d => path.join(skillsRoot, d.name));
  const allErrors: string[] = [];
  for (const skillDir of skillDirs) {
    const errors: string[] = [];
    await checkSkillMdExists(skillDir, errors);
    const skillMd = path.join(skillDir, 'SKILL.md');
    let mdRaw = '';
    try {
      mdRaw = await fs.readFile(skillMd, 'utf8');
    } catch { continue; }
    const { content, data: frontmatter } = matter(mdRaw);
    const ast = unified().use(remarkParse).parse(mdRaw);
    checkFrontmatterFields(frontmatter, skillDir, errors);
    await checkReferencedFiles(ast, skillDir, errors);
    checkTokenBudget(frontmatter.tier_hint, content, skillDir, errors);
    checkToolBudget(frontmatter.tier_hint, frontmatter, skillDir, errors);
    checkRequiredSections(ast, skillDir, errors);
    checkOutputMarkers(ast, skillDir, errors);
    checkSourceOfTruth(ast, skillDir, errors);
    if (errors.length) allErrors.push(...errors);
  }
  if (allErrors.length) {
    for (const err of allErrors) console.error(err);
    process.exitCode = 1;
  } else {
    console.log('skill-folder-shape: all checks passed');
  }
}

// If run directly
if (require.main === module) {
  lintSkillFolderShape(process.cwd());
}
