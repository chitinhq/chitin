// advisor.ts
// Slice 4: Predictive-execution-policy advisor consultation contract and escalation logic
// See: predictive-execution-policy spec, swarm tier-router with advisor consultation

import { ToolCallRequest, ExecutionRequest, Policy, ChainEvent } from './types';
import { getAdvisorTier } from './advisor-route';

// --- Types ---

export interface AdvisorRequest {
  executionRequest: ExecutionRequest;
  policy: Policy;
  recentChainEvents: ChainEvent[];
}

export interface AdvisorResponse {
  recommendation: string; // e.g., 'allow', 'deny', 'escalate', 'defer'
  reason: string;
  agent_guidance?: string;
  artifacts?: AdvisorArtifact[];
}

export type AdvisorArtifact = PolicyDiffArtifact;

export interface PolicyDiffArtifact {
  type: 'policy_diff';
  diff: string; // Markdown diff
  metadata: Record<string, any>;
}

// --- Escalation Heuristic ---

export interface EscalationContext {
  actionClass: string;
  blastVector: number;
  classifierConfidence: number; // 0..1
  policyMatched: boolean;
  consecutiveDenies: number;
}

export function shouldEscalate(ctx: EscalationContext): boolean {
  // Deterministic escalation heuristic
  if (ctx.classifierConfidence < 0.5) return true;
  if (!ctx.policyMatched) return true;
  if (ctx.blastVector > 0) return true;
  if (ctx.consecutiveDenies >= 2) return true;
  return false;
}

// --- Advisor Dispatch (stub) ---
// Implementation will call into the swarm's tier-driver infra
export async function dispatchAdvisor(
  req: AdvisorRequest,
  tier: number
): Promise<AdvisorResponse> {
  // Placeholder: actual implementation will call tier-driver
  return {
    recommendation: 'allow',
    reason: 'stub',
    agent_guidance: undefined,
    artifacts: [],
  };
}

// --- Chain-event emission (stub) ---
export function emitAdvisorConsultationEvent(
  tier: number,
  req: AdvisorRequest,
  res: AdvisorResponse,
  latencyMs: number
): void {
  // Placeholder: extend F4 OTEL emit logic
}

// --- Policy diff queueing (stub) ---
export function queuePolicyDiff(artifact: PolicyDiffArtifact): void {
  // Placeholder: write markdown sidecar to docs/policy-diffs/
}
