export interface DecisionRecord {
  readonly eventId: string;
  readonly ts: string;
  readonly agent: string;
  readonly driver: string;
  readonly actionType: string;
  readonly actionTarget: string;
  readonly decision: 'allow' | 'deny' | 'guide' | string;
  readonly ruleId: string;
  readonly reason: string;
  readonly suggestion: string;
  readonly correctedCommand: string;
  readonly escalation: string;
  readonly workflowId: string;
  readonly raw: Record<string, unknown>;
}

export interface DecisionSnapshot {
  readonly recent: readonly DecisionRecord[];
  readonly lastBlocked: DecisionRecord | null;
  readonly lockdown: boolean;
}

interface ChainEventShape {
  readonly event_type?: unknown;
  readonly ts?: unknown;
  readonly labels?: unknown;
  readonly payload?: unknown;
  readonly agent_instance_id?: unknown;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function payloadRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value === 'string') {
    try {
      return asRecord(JSON.parse(value));
    } catch {
      return null;
    }
  }
  return asRecord(value);
}

export function parseDecisionEventLine(line: string): DecisionRecord | null {
  let parsed: ChainEventShape;
  try {
    parsed = JSON.parse(line) as ChainEventShape;
  } catch {
    return null;
  }
  if (parsed.event_type !== 'decision') {
    return null;
  }

  const payload = payloadRecord(parsed.payload);
  if (!payload) {
    return null;
  }

  const eventId = asString(payload.event_id);
  const ts = asString(parsed.ts);
  if (!eventId || !ts) {
    return null;
  }

  const labels = asRecord(parsed.labels) ?? {};
  const payloadAgent = asString(payload.agent);
  const labelAgent = asString(labels.agent);
  const agentInstanceId = asString(parsed.agent_instance_id);

  return {
    eventId,
    ts,
    agent: payloadAgent || labelAgent || agentInstanceId,
    driver: asString(payload.driver) || asString(labels.driver),
    actionType: asString(payload.action_type),
    actionTarget: asString(payload.action_target),
    decision: asString(payload.decision) || 'deny',
    ruleId: asString(payload.rule_id),
    reason: asString(payload.reason),
    suggestion: asString(payload.suggestion),
    correctedCommand: asString(payload.corrected_command),
    escalation: asString(payload.escalation),
    workflowId: asString(payload.workflow_id) || asString(labels.workflow_id),
    raw: payload,
  };
}
