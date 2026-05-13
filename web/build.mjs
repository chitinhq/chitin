#!/usr/bin/env node
// Builds dist/index.html by injecting recent-merges data from `gh pr list`
// into web/index.html between <!-- MERGES:* --> markers.
// Requires `gh` CLI in PATH; in CI it's preinstalled and uses GITHUB_TOKEN.
// No npm deps. Run: node web/build.mjs

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { execSync } from 'node:child_process';

const repoRoot = resolve(new URL('..', import.meta.url).pathname);
const TEMPLATE = resolve(repoRoot, 'web/index.html');
const OUT = resolve(repoRoot, 'dist/index.html');
const REPO = 'chitinhq/chitin';
const LIST_LIMIT = 6;
const FETCH_LIMIT = 50;
const STAT_WINDOW_DAYS = 7;

function fetchMerges() {
  try {
    const raw = execSync(
      `gh pr list --repo ${REPO} --state merged --limit ${FETCH_LIMIT} --json number,title,mergedAt,author`,
      { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] },
    );
    const data = JSON.parse(raw);
    if (!Array.isArray(data)) throw new Error('gh returned non-array');
    return data
      .filter((pr) => pr.mergedAt)
      .sort((a, b) => new Date(b.mergedAt) - new Date(a.mergedAt));
  } catch (e) {
    console.warn(`warn: gh pr list failed (${e.message}). emitting placeholder.`);
    return null;
  }
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function splitConventional(title) {
  const m = title.match(/^([a-z0-9]+(?:\([^)]+\))?):\s*(.+)$/i);
  if (m) return { prefix: m[1], rest: m[2] };
  return { prefix: null, rest: title };
}

function relativeDay(date) {
  const ms = Date.now() - new Date(date).getTime();
  const day = Math.floor(ms / 86_400_000);
  if (day < 1) return 'today';
  if (day < 2) return 'yesterday';
  if (day < 14) return `${day}d ago`;
  return new Date(date).toISOString().slice(0, 10);
}

function countSince(merges, days) {
  const cutoff = Date.now() - days * 86_400_000;
  return merges.filter((pr) => new Date(pr.mergedAt).getTime() >= cutoff).length;
}

function renderItem(pr) {
  const { prefix, rest } = splitConventional(pr.title);
  const url = `https://github.com/${REPO}/pull/${pr.number}`;
  const prefixHtml = prefix
    ? `<span class="mono text-xs text-chitin/70 mr-1.5">${escapeHtml(prefix)}:</span>`
    : '';
  return (
    `<li class="py-3 flex items-baseline gap-3">` +
    `<a href="${url}" class="mono text-xs text-chitin/80 hover:text-chitin shrink-0 w-12">#${pr.number}</a>` +
    `<span class="flex-1 min-w-0 truncate-fallback">${prefixHtml}<span class="text-bone/90">${escapeHtml(rest)}</span></span>` +
    `<span class="mono text-xs text-muted/70 shrink-0">${escapeHtml(relativeDay(pr.mergedAt))}</span>` +
    `</li>`
  );
}

function renderPlaceholder() {
  return (
    `<li class="py-3 text-muted">` +
    `GitHub data unavailable at build time. ` +
    `<a href="https://github.com/${REPO}/pulls?q=is%3Apr+is%3Amerged" class="text-chitin hover:underline">View merged PRs →</a>` +
    `</li>`
  );
}

let sha = 'local';
try {
  sha = execSync('git rev-parse --short HEAD', { cwd: repoRoot, encoding: 'utf8' }).trim();
} catch {
  // Keep the local fallback when git metadata is unavailable.
}
if (process.env.GITHUB_SHA) sha = process.env.GITHUB_SHA.slice(0, 7);
const dateStamp = new Date().toISOString().slice(0, 10);

const merges = fetchMerges();
const template = readFileSync(TEMPLATE, 'utf8');

let listHtml;
let statHtml;
if (merges && merges.length > 0) {
  const top = merges.slice(0, LIST_LIMIT);
  listHtml = top.map(renderItem).join('\n      ');
  const recent = countSince(merges, STAT_WINDOW_DAYS);
  statHtml = `<span class="text-chitin font-bold">${recent}</span> merged in last ${STAT_WINDOW_DAYS} days`;
} else {
  listHtml = renderPlaceholder();
  statHtml = `<span class="text-muted">—</span> data unavailable`;
}

const replacements = [
  {
    name: 'list',
    re: /<!-- MERGES:LIST -->[\s\S]*?<!-- \/MERGES:LIST -->/,
    body: `<!-- MERGES:LIST -->\n      ${listHtml}\n      <!-- /MERGES:LIST -->`,
  },
  {
    name: 'stat',
    re: /<!-- MERGES:STAT -->[\s\S]*?<!-- \/MERGES:STAT -->/,
    body: `<!-- MERGES:STAT -->${statHtml}<!-- /MERGES:STAT -->`,
  },
  {
    name: 'sync',
    re: /<!-- MERGES:SYNC -->[\s\S]*?<!-- \/MERGES:SYNC -->/,
    body: `<!-- MERGES:SYNC -->source: <a href="https://github.com/${REPO}/pulls?q=is%3Apr+is%3Amerged" class="hover:text-chitin underline-offset-2 hover:underline">gh pr list --state merged</a> · synced ${dateStamp} · <code class="mono">${sha}</code><!-- /MERGES:SYNC -->`,
  },
];

let out = template;
for (const r of replacements) {
  if (!r.re.test(out)) throw new Error(`marker not found in template: ${r.name}`);
  out = out.replace(r.re, r.body);
}

mkdirSync(dirname(OUT), { recursive: true });
writeFileSync(OUT, out);
console.log(
  `built ${OUT.replace(repoRoot + '/', '')}: ` +
  `${merges ? merges.slice(0, LIST_LIMIT).length : 0} merges shown, ` +
  `${merges ? countSince(merges, STAT_WINDOW_DAYS) : '—'} in last ${STAT_WINDOW_DAYS}d, sha=${sha}`,
);
