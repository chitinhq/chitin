// researcher.ts
// Implements external signal fetchers for roadmap.md as described in the backlog entry.
// ESM-compatible, no new deps, uses Node 18+ fetch, fs/promises, child_process for gh CLI.
// Dedupes against roadmap.md, caps new candidates, emits telemetry log.

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { readFile, writeFile, access } from 'node:fs/promises';
import { spawn } from 'node:child_process';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const ROADMAP_PATH = resolve(__dirname, '../../roadmap.md');
const CANDIDATE_SECTION_HEADER = '## Candidates from external signal';
const MAX_CANDIDATES = parseInt(process.env.RESEARCHER_CANDIDATE_CAP || '5', 10);

// Utility: run shell command and capture stdout
async function runCmd(cmd, args = []) {
  return new Promise((resolve, reject) => {
    const proc = spawn(cmd, args, { stdio: ['ignore', 'pipe', 'pipe'] });
    let out = '';
    proc.stdout.on('data', d => (out += d));
    proc.stderr.on('data', d => (out += d));
    proc.on('close', code => (code === 0 ? resolve(out) : reject(new Error(out))));
  });
}

// Utility: fetch JSON
async function fetchJson(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Failed to fetch ${url}: ${res.status}`);
  return res.json();
}

// Utility: fetch text
async function fetchText(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Failed to fetch ${url}: ${res.status}`);
  return res.text();
}

// Parse roadmap.md for existing candidate IDs
async function getExistingCandidateIds() {
  try {
    await access(ROADMAP_PATH);
    const text = await readFile(ROADMAP_PATH, 'utf8');
    const section = text.split(CANDIDATE_SECTION_HEADER)[1];
    if (!section) return new Set();
    const ids = Array.from(section.matchAll(/\[([\w-]+)] \[([\w\d\-_.]+)]\(/g)).map(m => `${m[1]}:${m[2]}`);
    return new Set(ids);
  } catch {
    return new Set();
  }
}

// Append candidates to roadmap.md
async function appendCandidatesToRoadmap(candidates) {
  let text = '';
  try {
    text = await readFile(ROADMAP_PATH, 'utf8');
  } catch {
    // file missing, create new
    text = '';
  }
  let [before, after] = text.split(CANDIDATE_SECTION_HEADER);
  if (!before) before = text;
  let section = `${CANDIDATE_SECTION_HEADER}\n`;
  for (const c of candidates) {
    section += `- [${c.source}] [${c.id}](${c.url}) — ${c.summary}\n`;
  }
  let newText = before.trimEnd() + '\n\n' + section;
  if (after) newText += after.replace(/^.*$/, '');
  await writeFile(ROADMAP_PATH, newText.trimEnd() + '\n', 'utf8');
}

// --- Source fetchers ---

// arxiv: RSS feed for cs.SE and cs.AI
async function fetchArxiv() {
  const urls = [
    'https://export.arxiv.org/rss/cs.SE',
    'https://export.arxiv.org/rss/cs.AI',
  ];
  let items = [];
  for (const url of urls) {
    const xml = await fetchText(url);
    const matches = Array.from(xml.matchAll(/<item>[\s\S]*?<title>([\s\S]*?)<\/title>[\s\S]*?<link>([\s\S]*?)<\/link>[\s\S]*?<guid.*?>([\s\S]*?)<\/guid>[\s\S]*?<description>([\s\S]*?)<\/description>/g));
    for (const m of matches) {
      items.push({
        source: 'arxiv',
        id: m[3].trim(),
        url: m[2].trim(),
        summary: m[1].trim(),
        raw_text: m[4].trim(),
      });
    }
  }
  return items;
}

// Reddit: /r/LocalLLaMA top day, filter by keywords
async function fetchReddit() {
  const url = 'https://www.reddit.com/r/LocalLLaMA/top.json?t=day';
  const json = await fetchJson(url);
  const keywords = /agent|swarm|chitin/i;
  return (json.data.children || [])
    .map(p => p.data)
    .filter(p => keywords.test(p.title) || keywords.test(p.selftext))
    .map(p => ({
      source: 'reddit',
      id: p.id,
      url: 'https://reddit.com' + p.permalink,
      summary: p.title,
      raw_text: p.selftext,
    }));
}

// HN: Algolia API, last 24h
async function fetchHN() {
  const url = 'https://hn.algolia.com/api/v1/search_by_date?query=AI+coding+agent';
  const json = await fetchJson(url);
  const now = Date.now();
  return (json.hits || [])
    .filter(h => now - new Date(h.created_at).getTime() < 24 * 3600 * 1000)
    .map(h => ({
      source: 'hn',
      id: h.objectID,
      url: h.url || `https://news.ycombinator.com/item?id=${h.objectID}`,
      summary: h.title,
      raw_text: h.story_text || '',
    }));
}

// openclaw: releases + issues
async function fetchOpenclaw() {
  const releases = JSON.parse(await runCmd('gh', ['api', 'repos/openclaw/openclaw/releases?per_page=5']));
  const since = new Date(Date.now() - 24 * 3600 * 1000).toISOString();
  const issues = JSON.parse(await runCmd('gh', ['api', `repos/openclaw/openclaw/issues?state=open&since=${since}`]));
  return [
    ...releases.map(r => ({
      source: 'openclaw',
      id: `rel-${r.id}`,
      url: r.html_url,
      summary: r.name || r.tag_name,
      raw_text: r.body || '',
    })),
    ...issues.map(i => ({
      source: 'openclaw',
      id: `issue-${i.id}`,
      url: i.html_url,
      summary: i.title,
      raw_text: i.body || '',
    })),
  ];
}

// ollama: releases
async function fetchOllama() {
  const releases = JSON.parse(await runCmd('gh', ['api', 'repos/ollama/ollama/releases?per_page=5']));
  return releases.map(r => ({
    source: 'ollama',
    id: `rel-${r.id}`,
    url: r.html_url,
    summary: r.name || r.tag_name,
    raw_text: r.body || '',
  }));
}

// awesome-openclaw-agents: commits (template additions)
async function fetchAwesomeOpenclawAgents() {
  const since = new Date(Date.now() - 24 * 3600 * 1000).toISOString();
  const commits = JSON.parse(await runCmd('gh', ['api', `repos/mergisi/awesome-openclaw-agents/commits?since=${since}`]));
  return commits.map(c => ({
    source: 'awesome-openclaw-agents',
    id: c.sha,
    url: c.html_url || '',
    summary: c.commit.message.split('\n')[0],
    raw_text: c.commit.message,
  }));
}

// Main logic
async function main() {
  const existingIds = await getExistingCandidateIds();
  const allCandidates = [];
  let sourcesScanned = 0;
  for (const fetcher of [fetchArxiv, fetchReddit, fetchHN, fetchOpenclaw, fetchOllama, fetchAwesomeOpenclawAgents]) {
    try {
      const items = await fetcher();
      sourcesScanned++;
      for (const c of items) {
        if (!existingIds.has(`${c.source}:${c.id}`)) {
          allCandidates.push(c);
        }
      }
    } catch (e) {
      // skip source on error
    }
  }
  const newCandidates = allCandidates.slice(0, MAX_CANDIDATES);
  await appendCandidatesToRoadmap(newCandidates);
  // Telemetry log
  console.log(JSON.stringify({
    component: 'researcher',
    candidates_opened: newCandidates.length,
    sources_scanned: sourcesScanned,
  }));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main();
}
