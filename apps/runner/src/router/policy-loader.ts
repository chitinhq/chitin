// Reads the router policy from chitin.yaml. The policy file is the
// operator-facing surface for the router — heuristic thresholds,
// advisor triggers, chain depth, model selection.
//
// MVP: minimal YAML parsing of the `router:` section only. Falls
// back to DEFAULT_ROUTER_POLICY if the section is missing OR
// chitin.yaml is unreadable. Never fails open silently — the
// caller always gets a usable policy.

import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { DEFAULT_ROUTER_POLICY, type RouterPolicy } from './types.ts';

/**
 * Locate chitin.yaml relative to the agent's cwd. The file lives
 * at the repo root by convention (chitin/chitin.yaml). Walks up
 * parents until found or filesystem root.
 */
function findChitinYaml(startCwd: string): string | null {
  let dir = resolve(startCwd);
  while (dir !== '/') {
    const candidate = resolve(dir, 'chitin.yaml');
    if (existsSync(candidate)) return candidate;
    const parent = resolve(dir, '..');
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

/** Pure: extract the `router:` section's text from a chitin.yaml body. */
export function extractRouterSection(yaml: string): string | null {
  const lines = yaml.split('\n');
  const startIdx = lines.findIndex((l) => /^router:\s*$/.test(l));
  if (startIdx < 0) return null;
  const out: string[] = [lines[startIdx]];
  for (let i = startIdx + 1; i < lines.length; i++) {
    const line = lines[i] ?? '';
    // End of section: a non-indented, non-empty line at column 0
    if (line && !line.startsWith(' ') && !line.startsWith('\t') && !line.startsWith('#')) break;
    out.push(line);
  }
  return out.join('\n');
}

/**
 * Pure: parse the router section's YAML body into a RouterPolicy.
 * MVP shape — handles the specific keys we declare in
 * DEFAULT_ROUTER_POLICY. Unknown keys ignored. Type errors fall
 * back to defaults so a malformed config doesn't brick the router.
 */
export function parseRouterSection(routerYaml: string): RouterPolicy {
  // Lightweight key extraction. Each section parsed independently.
  const policy: RouterPolicy = JSON.parse(JSON.stringify(DEFAULT_ROUTER_POLICY));
  const lines = routerYaml.split('\n');

  // Top-level enabled flag
  const enabledMatch = lines.find((l) => /^\s+enabled:\s+(true|false)/.test(l));
  if (enabledMatch) {
    policy.enabled = /enabled:\s+true/.test(enabledMatch);
  }

  // heuristics.<name>.threshold / .enabled / .max_*
  const setNum = (path: string[], val: number) => {
    let cur: any = policy;
    for (const p of path.slice(0, -1)) cur = cur?.[p];
    if (cur) cur[path[path.length - 1]] = val;
  };
  const setBool = (path: string[], val: boolean) => {
    let cur: any = policy;
    for (const p of path.slice(0, -1)) cur = cur?.[p];
    if (cur) cur[path[path.length - 1]] = val;
  };

  // Match "      <key>: <value>" inside specific sections
  let section = '';
  let subsection = '';
  for (const rawLine of lines) {
    if (/^\s{2}\w/.test(rawLine)) {
      section = rawLine.trim().replace(/:\s*$/, '');
      subsection = '';
      continue;
    }
    if (/^\s{4}\w/.test(rawLine) && /:\s*$/.test(rawLine)) {
      subsection = rawLine.trim().replace(/:\s*$/, '');
      continue;
    }
    // key: value
    const m = rawLine.match(/^\s+([a-z_]+):\s+(.+)$/);
    if (!m) continue;
    const key = m[1];
    const value = m[2].trim();

    if (section === 'heuristics' && subsection) {
      if (key === 'enabled') {
        setBool(['heuristics', subsection, 'enabled'], value === 'true');
      } else if (key === 'threshold' || key === 'max_loop_count' || key === 'max_stall_seconds') {
        const n = Number(value);
        if (!Number.isNaN(n)) setNum(['heuristics', subsection, key], n);
      }
    } else if (section === 'advisor') {
      if (key === 'enabled') {
        policy.advisor.enabled = value === 'true';
      } else if (key === 'model') {
        policy.advisor.model = value.replace(/^["']|["']$/g, '');
      } else if (key === 'when' && value.startsWith('[')) {
        const inside = value.slice(1, -1);
        policy.advisor.when = inside
          .split(',')
          .map((s) => s.trim().replace(/^["']|["']$/g, ''))
          .filter(Boolean) as RouterPolicy['advisor']['when'];
      } else if (subsection === 'chain') {
        if (key === 'max_depth') {
          const n = Number(value);
          if (!Number.isNaN(n)) policy.advisor.chain.max_depth = n;
        } else if (key === 'tier_steps' && value.startsWith('[')) {
          const inside = value.slice(1, -1);
          policy.advisor.chain.tier_steps = inside
            .split(',')
            .map((s) => s.trim().replace(/^["']|["']$/g, ''))
            .filter(Boolean);
        }
      }
    }
  }

  return policy;
}

/**
 * I/O wrapper: locate chitin.yaml from a starting cwd, parse the
 * router section if present, return policy. Falls back to default
 * on any failure.
 */
export function loadRouterPolicy(cwd: string): RouterPolicy {
  const path = findChitinYaml(cwd);
  if (!path) return DEFAULT_ROUTER_POLICY;
  let yaml: string;
  try {
    yaml = readFileSync(path, 'utf8');
  } catch {
    return DEFAULT_ROUTER_POLICY;
  }
  const section = extractRouterSection(yaml);
  if (!section) return DEFAULT_ROUTER_POLICY;
  return parseRouterSection(section);
}
