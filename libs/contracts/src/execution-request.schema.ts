import { z } from 'zod';

export const TaskClassSchema = z.enum([
  'refactor',
  'test_writing',
  'doc_update',
  'bug_fix',
  'scaffolding',
  'exploration',
]);

export const RiskLevelSchema = z.enum(['low', 'medium', 'high', 'irreversible']);

// Anthropic ToS forbids Claude Code as a worker driver — interactive CLI / /schedule only.
// Schema is the contract boundary; reject at parse, not at dispatch.
export const DriverIdSchema = z.enum([
  'copilot',
  'local-qwen',
  'local-glm',
  'local-deepseek',
]);

export const NetworkPolicySchema = z.enum(['none', 'allowlist', 'open']);

export const WritePolicySchema = z.enum(['none', 'worktree', 'branch', 'main']);

// 24h cap on wall_timeout_s. setTimeout truncates timeouts > ~2^31 ms (~24.85 days)
// to 1ms in Node, which would SIGKILL the activity child immediately. 24h is
// a sensible upper bound for any single agent turn — anything longer is a
// modeling problem, not a timeout problem.
const WALL_TIMEOUT_MAX_S = 24 * 60 * 60;

export const BoundsSchema = z.object({
  max_tool_calls: z.number().int().positive(),
  max_cost_usd: z.number().nonnegative(),
  wall_timeout_s: z.number().int().positive().max(WALL_TIMEOUT_MAX_S),
});

const TemporalIdSchema = z.string().regex(/^[a-zA-Z0-9_\-:.]{1,128}$/);

export const ExecutionRequestSchema = z
  .object({
    schema_version: z.literal('1'),
    workflow_id: TemporalIdSchema,
    run_id: TemporalIdSchema,
    repo: z.string().regex(/^[^/\s]+\/[^/\s]+$/, 'must be <owner>/<name>'),
    files: z.array(z.string().min(1)).optional(),
    task_class: TaskClassSchema,
    risk_level: RiskLevelSchema,
    allowed_drivers: z.array(DriverIdSchema).min(1),
    network_policy: NetworkPolicySchema,
    write_policy: WritePolicySchema,
    bounds: BoundsSchema,
    prompt: z.string().min(1),
  })
  .superRefine((req, ctx) => {
    if (req.network_policy === 'open' && (req.risk_level === 'high' || req.risk_level === 'irreversible')) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['network_policy'],
        message: `network_policy='open' is not allowed at risk_level='${req.risk_level}'`,
      });
    }
    if (req.write_policy === 'main') {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['write_policy'],
        message: `write_policy='main' is reserved; slice 1 never authorizes direct main writes`,
      });
    }
  });

export type ExecutionRequest = z.infer<typeof ExecutionRequestSchema>;
export type TaskClass = z.infer<typeof TaskClassSchema>;
export type RiskLevel = z.infer<typeof RiskLevelSchema>;
export type DriverId = z.infer<typeof DriverIdSchema>;
export type NetworkPolicy = z.infer<typeof NetworkPolicySchema>;
export type WritePolicy = z.infer<typeof WritePolicySchema>;
export type Bounds = z.infer<typeof BoundsSchema>;
