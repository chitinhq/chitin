// kanban-card-to-request — translate a hermes kanban card into an
// ExecutionRequest the runner can dispatch.
//
// Reads `hermes kanban show <id> --json`, extracts the YAML frontmatter
// the kanban-mirror writes into the card body, reconstructs a
// BacklogEntry-shape object, and builds an ExecutionRequest.
//
// Always-start-T0 policy: the card may carry the entry's groomed
// `tier:` (T1..T4) but we override to T0 unconditionally. The kernel's
// router/advisor will escalate the runner's loop as needed (see
// runWithEscalation in execute-request.ts). Backlog grooming's tier
// becomes a hint, not an assignment — the swarm collapses left over
// time as T0/T1 prove they can handle more.

import { execFileSync } from 'node:child_process';
import { ExecutionRequestSchema, type ExecutionRequest, type Tier } from '@chitin/contracts';
import type { BacklogEntry } from './grooming/parse-backlog.ts';
import { buildPromptForEntry, resolveEntryRole } from './role-prompts.ts';

const HERMES = process.env.HERMES_BIN ?? `${process.env.HOME ?? ''}/.local/bin/hermes`;
const BOARD = process.env.HERMES_KANBAN_BOARD ?? 'chitin';

interface KanbanShowResponse {
  task: {
    id: string;
    title: string;
    body: string | null;
    assignee: string | null;
    status: string;
    priority: number;
  };
}

/** Fetch a kanban card via subprocess. */
function fetchCard(taskId: string): KanbanShowResponse {
  const out = execFileSync(
    HERMES,
    ['kanban', '--board', BOARD, 'show', taskId, '--json'],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] },
  );
  return JSON.parse(out) as KanbanShowResponse;
}

/**
 * Extract the YAML frontmatter from a kanban-mirror-shaped card body
 * (` ```yaml ... ``` ` followed by the entry description) and parse
 * it into a flat key→value map.
 *
 * Mirrors parseSection in grooming/parse-backlog.ts but works on a
 * card body (no leading `### ` heading). Inlined rather than imported
 * because parse-backlog's helpers are not exported (they shouldn't
 * grow public-API responsibility for this side use).
 */
function parseCardBody(body: string): { fields: Record<string, string>; rawFrontmatter: string; description: string } {
  const yamlMatch = body.match(/```yaml\n([\s\S]*?)\n```/);
  if (!yamlMatch) {
    // No frontmatter — treat the whole body as description and let
    // the runner build a generic prompt. Most cards from the mirror
    // will have frontmatter; ad-hoc cards (operator-created via the
    // hermes UI) might not.
    return { fields: {}, rawFrontmatter: '', description: body.trim() };
  }
  const rawFrontmatter = yamlMatch[1];
  const afterYaml = body.slice((yamlMatch.index ?? 0) + yamlMatch[0].length);
  const description = afterYaml.replace(/^\s*---\s*\n/, '').trim();

  const fields: Record<string, string> = {};
  for (const rawLine of rawFrontmatter.split('\n')) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    const colonIdx = line.indexOf(':');
    if (colonIdx <= 0) continue;
    const key = line.slice(0, colonIdx).trim();
    let val = line.slice(colonIdx + 1).trim();
    if (val.startsWith('"') && val.endsWith('"')) val = val.slice(1, -1);
    fields[key] = val;
  }
  return { fields, rawFrontmatter, description };
}

/**
 * Reconstruct a BacklogEntry from a parsed card. The shape needs to
 * be enough that buildPromptForEntry / resolveEntryRole work — they
 * read `id`, `description`, `role`, `file`, `tier` (no other fields).
 */
function cardToEntry(taskId: string, title: string, parsed: ReturnType<typeof parseCardBody>): BacklogEntry {
  return {
    id: parsed.fields.id ?? title,
    status: 'ready',  // by definition — the card is ready in kanban or we wouldn't be running it
    tier: parsed.fields.tier,
    estimatedLoc: parsed.fields.estimated_loc,
    blocks: undefined,
    file: parsed.fields.file,
    referencesIssue: parsed.fields.references_issue,
    referencesFinding: parsed.fields.references_finding,
    referencesSpec: parsed.fields.references_spec,
    role: parsed.fields.role,
    rawFrontmatter: parsed.rawFrontmatter,
    description: parsed.description,
    rawSection: '',
  };
}

/**
 * Build an ExecutionRequest from a kanban card id. Always sets
 * tier=T0 (always-start-T0 policy); the kernel's escalation signal
 * will bump it through the runner's loop as needed.
 *
 * Driver selection at T0: the runner uses whatever the agent CLI
 * resolves to for T0. Today that's openclaw-glm-flash on the 3090
 * (free); future = Hermes-4-14B in Ollama on the same hardware.
 */
export function buildRequestFromKanbanCard(taskId: string): ExecutionRequest {
  const resp = fetchCard(taskId);
  const parsed = parseCardBody(resp.task.body ?? '');
  const entry = cardToEntry(resp.task.id, resp.task.title, parsed);

  const { role } = resolveEntryRole(entry);
  const prompt = buildPromptForEntry(entry);

  // workflow_id derived from the card id so chain telemetry can
  // correlate kanban-driven runs to their cards. `swarm-` prefix
  // preserves the existing dispatcher convention so the
  // listRunningExecuteRequestsFromDisk dedup keeps working.
  const workflowId = `swarm-${entry.id}-${taskId}`;
  // Random run_id per dispatch (the kernel writes per-run telemetry
  // files keyed on run_id; reusing it across attempts collapses them).
  const runId = `${workflowId}-attempt-${Date.now()}`;

  return ExecutionRequestSchema.parse({
    schema_version: '1',
    workflow_id: workflowId,
    run_id: runId,
    repo: 'chitinhq/chitin',
    // 'refactor' matches the dispatcher's existing classification —
    // most backlog work touches existing files with explicit scope.
    task_class: 'refactor',
    risk_level: 'low',
    // Always start at T0; runner's escalation loop bumps from here.
    // T0's allowed driver is the local 3090 model (openclaw-glm-flash
    // today; Hermes-4-14B on Ollama in the planned migration).
    allowed_drivers: ['openclaw-glm-flash'],
    network_policy: 'allowlist',
    write_policy: 'worktree',
    bounds: {
      max_tool_calls: 50,
      max_cost_usd: 0,
      wall_timeout_s: 1800,
    },
    prompt,
    role,
    tier: 'T0' as Tier,
    base_ref: 'main',
  });
}

// Exported for test injection and CLI integration in execute-request.ts.
export const __test__ = { parseCardBody, cardToEntry };
