// advisor.ts
// Predictive execution policy: slice 4 — kernel + tiered advisor pattern
// Implements AdvisorRequest/Response types, escalation heuristic, and advisor dispatch contract.

import { ExecutionRequest, ExecutionResponse, ToolCallRequest, Policy, PolicyDiff } from './policy-types';
import { ChainEvent, emitChainEvent } from './chain';
import { routeAdvisorTier } from './advisor-route';
import { v4 as uuidv4 } from 'uuid';

// --- Types ---

export interface AdvisorRequest {
  request_id: string;
  execution_request: ExecutionRequest;
  policy: Policy;
  recent_chain_events: ChainEvent[];
}

export interface AdvisorResponse {
  recommendation: 'allow' | 'deny' | 'escalate';
  reason: string;
  agent_guidance?: string;
  artifacts?: AdvisorArtifact[];
}

export type AdvisorArtifact = PolicyDiff | AdvisorStructuredArtifact;

export interface AdvisorStructuredArtifact {
  type: string;
  data: any;
}

// --- Escalation Heuristic ---

export function shouldEscalate(
  toolCall: ToolCallRequest,
  policy: Policy,
  recentChainEvents: ChainEvent[],
  classifierConfidence: number,
  consecutiveDenies: number
): boolean {
  // Heuristic: escalate if low classifier confidence, no exact policy match, non-trivial blast_vector, or N denies
  const LOW_CONFIDENCE = classifierConfidence < 0.6;
  const NO_POLICY_MATCH = !policy.actions.some(a => a.name === toolCall.action_class);
  const NONTRIVIAL_BLAST = toolCall.blast_vector && toolCall.blast_vector.length > 0 && toolCall.blast_vector.some(x => x !== 0);
  const TOO_MANY_DENIES = consecutiveDenies >= 3;
  return LOW_CONFIDENCE || NO_POLICY_MATCH || NONTRIVIAL_BLAST || TOO_MANY_DENIES;
}

// --- Advisor Dispatch ---

export async function consultAdvisor(
  toolCall: ToolCallRequest,
  executionRequest: ExecutionRequest,
  policy: Policy,
  recentChainEvents: ChainEvent[],
  classifierConfidence: number,
  consecutiveDenies: number
): Promise<AdvisorResponse> {
  const tier = routeAdvisorTier(toolCall.action_class, toolCall.blast_vector);
  const request: AdvisorRequest = {
    request_id: uuidv4(),
    execution_request: executionRequest,
    policy,
    recent_chain_events: recentChainEvents,
  };
  // Call the swarm's tier-driver (mocked here)
  const response = await dispatchToAdvisorTier(tier, request);
  // Emit chain event
  await emitChainEvent({
    type: 'advisor_consultation',
    tier,
    request,
    response,
    latency_ms: 0, // Fill with real latency if available
    timestamp: new Date().toISOString(),
  });
  // Queue policy diffs
  if (response.artifacts) {
    for (const artifact of response.artifacts) {
      if ('diff' in artifact) {
        await queuePolicyDiff(artifact as PolicyDiff, request, response, tier);
      }
    }
  }
  return response;
}

// --- Mocked advisor tier dispatch ---

async function dispatchToAdvisorTier(tier: number, request: AdvisorRequest): Promise<AdvisorResponse> {
  // Replace with real model call
  return {
    recommendation: 'allow',
    reason: 'Mocked: safe to proceed',
    agent_guidance: 'Proceed with caution.',
    artifacts: [],
  };
}

// --- Policy diff queueing ---

import { promises as fs } from 'fs';
import * as path from 'path';

async function queuePolicyDiff(
  diff: PolicyDiff,
  request: AdvisorRequest,
  response: AdvisorResponse,
  tier: number
) {
  const dir = path.join(__dirname, '../../../docs/policy-diffs');
  await fs.mkdir(dir, { recursive: true });
  const filename = `diff-${request.request_id}.md`;
  const filePath = path.join(dir, filename);
  const content = `---\ntier: ${tier}\nrequest_id: ${request.request_id}\ntimestamp: ${new Date().toISOString()}\n---\n\n## Policy Diff\n\n${JSON.stringify(diff, null, 2)}\n\n## Advisor Response\n\n${JSON.stringify(response, null, 2)}\n`;
  await fs.writeFile(filePath, content, 'utf8');
}
