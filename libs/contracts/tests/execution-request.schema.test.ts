import { describe, expect, it } from 'vitest';
import { ExecutionRequestSchema, type ExecutionRequest } from '../src/execution-request.schema';

const validRequest: ExecutionRequest = {
  schema_version: '1',
  workflow_id: 'wf-2026-04-30-001',
  run_id: 'run-2026-04-30-001-attempt-1',
  repo: 'chitinhq/chitin',
  files: ['libs/contracts/src/index.ts'],
  task_class: 'refactor',
  risk_level: 'low',
  allowed_drivers: ['copilot'],
  network_policy: 'allowlist',
  write_policy: 'worktree',
  bounds: {
    max_tool_calls: 50,
    max_cost_usd: 0.5,
    wall_timeout_s: 600,
  },
  prompt: 'Rename FooBar to BarBaz across libs/contracts.',
};

describe('ExecutionRequestSchema', () => {
  it('round-trips a valid request', () => {
    const parsed = ExecutionRequestSchema.parse(validRequest);
    expect(parsed).toEqual(validRequest);
  });

  it('accepts files omitted (scope hint is optional)', () => {
    const { files: _files, ...rest } = validRequest;
    expect(() => ExecutionRequestSchema.parse(rest)).not.toThrow();
  });

  it('rejects empty allowed_drivers (policy output must be non-empty)', () => {
    const bad = { ...validRequest, allowed_drivers: [] };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('rejects unknown driver id in allowed_drivers', () => {
    const bad = { ...validRequest, allowed_drivers: ['gpt-5' as never] };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('rejects claude-code as a worker driver (Anthropic ToS — interactive only)', () => {
    const bad = { ...validRequest, allowed_drivers: ['claude-code' as never] };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('rejects malformed repo (missing owner)', () => {
    const bad = { ...validRequest, repo: 'chitin' };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('rejects malformed repo (whitespace)', () => {
    const bad = { ...validRequest, repo: 'chitin hq/chitin' };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('accepts max_cost_usd = 0 (T0-only / no-cloud is legal)', () => {
    const ok = { ...validRequest, bounds: { ...validRequest.bounds, max_cost_usd: 0 } };
    expect(() => ExecutionRequestSchema.parse(ok)).not.toThrow();
  });

  it('rejects wall_timeout_s = 0 (zero = instant timeout = nonsense)', () => {
    const bad = { ...validRequest, bounds: { ...validRequest.bounds, wall_timeout_s: 0 } };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('rejects wall_timeout_s > 24h (setTimeout truncates beyond 2^31 ms)', () => {
    const bad = { ...validRequest, bounds: { ...validRequest.bounds, wall_timeout_s: 24 * 60 * 60 + 1 } };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('accepts wall_timeout_s = 24h (boundary)', () => {
    const ok = { ...validRequest, bounds: { ...validRequest.bounds, wall_timeout_s: 24 * 60 * 60 } };
    expect(() => ExecutionRequestSchema.parse(ok)).not.toThrow();
  });

  it('rejects max_tool_calls = 0', () => {
    const bad = { ...validRequest, bounds: { ...validRequest.bounds, max_tool_calls: 0 } };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('accepts max_tool_calls = 1 (single-call task is legal)', () => {
    const ok = { ...validRequest, bounds: { ...validRequest.bounds, max_tool_calls: 1 } };
    expect(() => ExecutionRequestSchema.parse(ok)).not.toThrow();
  });

  it('rejects empty prompt', () => {
    const bad = { ...validRequest, prompt: '' };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it("rejects network_policy='open' at risk_level='high'", () => {
    const bad = { ...validRequest, network_policy: 'open' as const, risk_level: 'high' as const };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow(/network_policy='open' is not allowed/);
  });

  it("rejects network_policy='open' at risk_level='irreversible'", () => {
    const bad = { ...validRequest, network_policy: 'open' as const, risk_level: 'irreversible' as const };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it("accepts network_policy='open' at risk_level='medium' (boundary)", () => {
    const ok = { ...validRequest, network_policy: 'open' as const, risk_level: 'medium' as const };
    expect(() => ExecutionRequestSchema.parse(ok)).not.toThrow();
  });

  it("accepts network_policy='open' at risk_level='low'", () => {
    const ok = { ...validRequest, network_policy: 'open' as const, risk_level: 'low' as const };
    expect(() => ExecutionRequestSchema.parse(ok)).not.toThrow();
  });

  it("rejects write_policy='main' (slice 1 never authorizes direct main writes)", () => {
    const bad = { ...validRequest, write_policy: 'main' as const };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow(/write_policy='main' is reserved/);
  });

  it('rejects schema_version other than 1', () => {
    const bad = { ...validRequest, schema_version: '2' as never };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });

  it('rejects workflow_id with disallowed characters', () => {
    const bad = { ...validRequest, workflow_id: 'wf with spaces' };
    expect(() => ExecutionRequestSchema.parse(bad)).toThrow();
  });
});
