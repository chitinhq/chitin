// researcher.ts
// Cron'd researcher-role agent for external signal collection
// See backlog entry for full requirements and sources

import fs from 'fs/promises';
import path from 'path';
import fetch from 'node-fetch';

const ROADMAP_PATH = path.resolve(__dirname, '../../roadmap.md');
const MAX_CANDIDATES = 5;

// Utility: Read roadmap.md and extract existing candidate entries
async function getExistingCandidates(): Promise<Set<string>> {
  try {
    const content = await fs.readFile(ROADMAP_PATH, 'utf8');
    const matches = content.matchAll(/- \[candidate\] \[(.*?)\]/g);
    return new Set(Array.from(matches, m => m[1]));
  } catch (e) {
    return new Set();
  }
}

// Utility: Append new candidates to roadmap.md
async function appendCandidatesToRoadmap(entries: {source: string, id: string, summary: string, why: string}[]) {
  if (!entries.length) return;
  let content = await fs.readFile(ROADMAP_PATH, 'utf8');
  const sectionHeader = '## Candidates from external signal';
  let idx = content.indexOf(sectionHeader);
  if (idx === -1) {
    content += `\n\n${sectionHeader}\n`;
    idx = content.length;
  }
  const insertIdx = content.indexOf('\n', idx) + 1;
  const newEntries = entries.map(e => `- [candidate] [${e.id}] (${e.source}): ${e.summary} — ${e.why}`).join('\n');
  content = content.slice(0, insertIdx) + newEntries + '\n' + content.slice(insertIdx);
  await fs.writeFile(ROADMAP_PATH, content, 'utf8');
}

// Fetchers for each source (stubs for now)
async function fetchArxiv(since: Date): Promise<{id: string, summary: string, why: string}[]> {
  // TODO: Implement real fetch
  return [];
}
async function fetchReddit(since: Date): Promise<{id: string, summary: string, why: string}[]> {
  // TODO: Implement real fetch
  return [];
}
async function fetchHN(since: Date): Promise<{id: string, summary: string, why: string}[]> {
  // TODO: Implement real fetch
  return [];
}
async function fetchX(since: Date): Promise<{id: string, summary: string, why: string}[]> {
  // TODO: Implement real fetch
  return [];
}
async function fetchOpenClaw(since: Date): Promise<{id: string, summary: string, why: string}[]> {
  // TODO: Implement real fetch
  return [];
}
async function fetchOllama(since: Date): Promise<{id: string, summary: string, why: string}[]> {
  // TODO: Implement real fetch
  return [];
}
async function fetchAwesomeOpenClawAgents(since: Date): Promise<{id: string, summary: string, why: string}[]> {
  // TODO: Implement real fetch
  return [];
}

// Main entrypoint
export async function runResearcher(sinceWindowHours = 4) {
  const since = new Date(Date.now() - sinceWindowHours * 60 * 60 * 1000);
  const existing = await getExistingCandidates();
  const sources = [
    {name: 'arxiv', fetcher: fetchArxiv},
    {name: 'reddit', fetcher: fetchReddit},
    {name: 'hn', fetcher: fetchHN},
    {name: 'x', fetcher: fetchX},
    {name: 'openclaw', fetcher: fetchOpenClaw},
    {name: 'ollama', fetcher: fetchOllama},
    {name: 'awesome-openclaw-agents', fetcher: fetchAwesomeOpenClawAgents},
  ];
  let candidates: {source: string, id: string, summary: string, why: string}[] = [];
  for (const src of sources) {
    const found = await src.fetcher(since);
    for (const entry of found) {
      if (!existing.has(entry.id)) {
        candidates.push({...entry, source: src.name});
      }
    }
  }
  // Cap to max N per run
  candidates = candidates.slice(0, MAX_CANDIDATES);
  await appendCandidatesToRoadmap(candidates);
  // TODO: Telemetry: increment candidate-entries-opened-per-run
}

// If run directly
if (require.main === module) {
  runResearcher().catch(e => {
    console.error('Researcher failed:', e);
    process.exit(1);
  });
}
