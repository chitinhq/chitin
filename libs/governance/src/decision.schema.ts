import { z } from 'zod';

// Decision — output of decide(). Spec §1.
//
// Seven decision kinds, distinguished by what they do to the call and
// what they leave behind. Slice 1 implements `allow`, `deny`, `rewrite`,
// and `redirect`. The remaining three (`allow_with_auto_undo`,
// `allow_observed`, `stage`) need supporting infrastructure (undo
// primitives, observation queue, sandbox runner) that lands in later
// slices and is intentionally not part of Slice 1.

export const DecisionKindSchema = z.enum([
  'allow',
  'allow_with_auto_undo',
  'allow_observed',
  'deny',
  'rewrite',
  'redirect',
  'stage',
]);

export const DecisionSchema = z
  .object({
    kind: DecisionKindSchema,
    policy_name: z
      .string()
      .min(1)
      .describe('Which policy rule produced this decision. "default" for the fall-through.'),
    policy_version: z
      .string()
      .min(1)
      .describe(
        'Version of the policy library at decision time. Used by audit + ' +
        'counterfactual replay to re-evaluate against historical policies.',
      ),
    reason: z
      .string()
      .describe('Human-readable explanation of the decision; surfaces to the agent.'),
    rewrite_args: z
      .record(z.string(), z.unknown())
      .optional()
      .describe('Required for kind=rewrite. The replacement tool args.'),
    alternatives: z
      .array(z.string())
      .optional()
      .describe(
        'For kind=redirect or kind=deny. Suggested alternative paths the agent ' +
        'can take to make progress without crossing the policy.',
      ),
  })
  .superRefine((decision, ctx) => {
    if (decision.kind === 'rewrite' && !decision.rewrite_args) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['rewrite_args'],
        message: 'rewrite_args is required when kind=rewrite',
      });
    }
    if (decision.kind === 'redirect' && (!decision.alternatives || decision.alternatives.length === 0)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['alternatives'],
        message: 'redirect must surface at least one alternative path',
      });
    }
  });

export type DecisionKind = z.infer<typeof DecisionKindSchema>;
export type Decision = z.infer<typeof DecisionSchema>;
