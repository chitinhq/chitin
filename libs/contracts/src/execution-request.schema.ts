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

// Slice 6c: tier hint for the dispatcher. The grooming agent assigns a
// tier per backlog entry (T0 mechanical, T4 strongest programmatic, T5
// human-only). Activity dispatch uses the tier to pick a model per
// driver — T0 → cheapest available, T4 → strongest. Optional because
// not every workflow comes from the grooming pipeline yet (manual
// dispatches exist); when absent, drivers fall back to their default
// model. T5 is intentionally absent from the enum: that tier is human-
// only, no programmatic driver should ever receive a T5 ExecutionRequest.
export const TierSchema = z.enum(['T0', 'T1', 'T2', 'T3', 'T4']);

// Phase 1 of the swarm-as-software-factory design (see
// docs/design/2026-05-02-swarm-as-software-factory.md §3-4): each
// workflow plays one role on the assembly line. The dispatcher uses
// the role to pick the prompt template + tier defaults; the
// gov-decisions chain records the role on each decision so audit can
// reconstruct who did what at each station.
//
// Roles intentionally describe AGENTS, not work-shapes. A `programmer`
// agent might be doing a refactor or a feature; a `reviewer` might be
// looking at either. Work-shape (refactor / fix / doc) lives on
// `task_class` — a separate concern.
//
// Absent = generic programmer (the slice-7b dispatcher's pre-Phase-1
// behavior). Existing manual dispatches keep working.
export const RoleSchema = z.enum([
  'researcher',         // Pull external signals (arxiv, Reddit, openclaw, ollama)
  'product',            // Turn signals into 1-paragraph problem statements
  'groomer',            // Tier-classify, size, identify file scope, mark blockers
  'architect',          // Write design docs / ADRs
  'programmer',         // Read entry's file:, edit, commit, push (the current swarm)
  'reviewer',           // Tier-escalating review (R0-R3, see design §5)
  'peer-reviewer',      // Independent second-opinion per PR, parallel to Copilot R0
  'comment-responder',  // Read PR review comments, evaluate, push fix commits
  'qa',                 // Generate / run E2E tests; smoke-test
  'gatekeeper',         // CI + reviews + telemetry → merge or escalate
  'tech-writer',        // Update wiki + ADRs + runbooks; lessons-learned
  'analyst',            // Author analysis-lib queries; explain telemetry
  'refactorer',         // Find duplication / dead code / hot-path debt
  'debt-curator',       // Maintain debt-ledger; surface debt that blocks other work
]);

// Driver tiers for the swarm. The 2026-04-30 framing that excluded
// `claude-code` was based on a misread of Anthropic's terms — verified
// 2026-05-02 against code.claude.com/docs/en/headless that headless mode
// (`claude -p ... --dangerously-skip-permissions`) is officially supported
// for unattended/CI/cron use. The interactive CLI surface is a separate
// thing and not represented in this enum (you don't programmatically
// dispatch interactive sessions; they're always Jared-driven).
//
// claude-code-headless joins as the strongest programmatic tier (T4 in
// swarm-backlog.md), distinct from `copilot` (T1-T3 depending on Copilot
// model) and the local-* drivers (T0/T2).
export const DriverIdSchema = z.enum([
  'copilot',
  'claude-code-headless',
  'local-qwen',
  // `local-glm` historically routed to glm-5.1:cloud for reasoning
  // delegation; kept for that role. `local-glm-flash` is the local
  // mechanical-tier variant added 2026-05-03 — glm-4.7-flash:latest
  // on the 3090, T0 default. Distinct driver because the cost/latency
  // profile is fundamentally different (flash = local + fast,
  // glm-5.1 = cloud + slower + smarter).
  'local-glm',
  'local-glm-flash',
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

// Git ref: branch or commit-ish. Restrictive enough that what we pass to
// `git worktree add ... <ref>` is shell-safe and doesn't try to be a path.
const GitRefSchema = z
  .string()
  .regex(/^[a-zA-Z0-9_\-./]{1,128}$/, 'must be a simple git ref (branch, tag, or sha)')
  .refine((s) => !s.startsWith('-'), 'git ref cannot start with hyphen (flag-injection guard)');

export const ExecutionRequestSchema = z
  .object({
    schema_version: z.literal('1'),
    workflow_id: TemporalIdSchema,
    run_id: TemporalIdSchema,
    repo: z.string().regex(/^[\w][\w.-]*\/[\w][\w.-]*$/, 'must be <owner>/<name>'),
    files: z.array(z.string().min(1)).optional(),
    task_class: TaskClassSchema,
    risk_level: RiskLevelSchema,
    allowed_drivers: z.array(DriverIdSchema).min(1),
    network_policy: NetworkPolicySchema,
    write_policy: WritePolicySchema,
    bounds: BoundsSchema,
    prompt: z.string().min(1),
    // Slice 5: optional. When set, the activity creates a git worktree
    // from this ref at ~/.cache/chitin/swarm-worktrees/<workflow_id>/ and
    // spawns the agent there. When absent (slice 1-4 behavior), the
    // activity runs in a tempdir and any agent edits are discarded.
    base_ref: GitRefSchema.optional(),
    // Slice 6c: optional tier hint. When set, dispatch resolves a
    // tier-appropriate model for the chosen driver (e.g., T0 →
    // claude-haiku for claude-code-headless). Absent = driver default.
    tier: TierSchema.optional(),
    // Phase 1 (factory design §3-4): which role this workflow plays.
    // Picks the prompt template + per-role tier defaults. Absent =
    // generic programmer (current pre-Phase-1 behavior).
    role: RoleSchema.optional(),
    // Phase 1 (factory design §4): when this workflow was spawned by
    // a parent in a multi-step flow (e.g., reviewer spawned by a
    // programmer that just opened a PR), the parent's workflow_id
    // links them in the chain. Absent = top-level dispatch.
    parent_workflow_id: TemporalIdSchema.optional(),
    // Phase 1 (factory design §4): step index within a multi-step
    // flow, 0-based. Lets the flow cap iterations (Lobster's
    // loop.maxIterations equivalent — chitin caps at 3 per the
    // design doc to prevent runaway chains). Absent = 0.
    step_index: z.number().int().nonnegative().max(3).optional(),
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
export type Tier = z.infer<typeof TierSchema>;
export type Role = z.infer<typeof RoleSchema>;
