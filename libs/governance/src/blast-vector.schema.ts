import { z } from 'zod';

// Blast vector — 4-axis description of an action's potential effect.
// Spec: docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md §2.
//
// Treated as a vector, not a scalar, because folding into a single
// "risk score" loses information policy needs. Two examples that read
// the same as a scalar but have very different blast vectors:
//
//   `Bash rm -rf node_modules`
//     reversibility=irreversible   (the deleted bytes can't come back)
//     scope=local                  (only this machine)
//     visibility=silent            (no one else observes it)
//     counterparties=self          (no one else is affected)
//   → Scary verb, but the BUSINESS consequence is small because every
//     other axis is trivial and the deleted artifact (node_modules) is
//     regenerable. Reversibility describes the operation, not the
//     consequence.
//
//   `slack.delete_channel`
//     reversibility=irreversible   (channel + history gone)
//     scope=external               (Slack is external to this project)
//     visibility=public_broadcast  (channel members all observe)
//     counterparties=team          (everyone in the channel is affected)
//   → Trivial call, but huge blast — three of four axes are at maximum.
//
// A scalar "risk score" cannot distinguish these. A vector can.

export const ReversibilitySchema = z.enum([
  'reversible',
  'reversible_with_effort',
  'irreversible',
]);

export const ScopeSchema = z.enum([
  'local',
  'project',
  'cross_project',
  'external',
]);

export const VisibilitySchema = z.enum([
  'silent',
  'logged',
  'observable',
  'public_broadcast',
]);

export const CounterpartiesSchema = z.enum([
  'self',
  'team',
  'external_users',
  'public',
]);

export const BlastVectorSchema = z.object({
  reversibility: ReversibilitySchema,
  scope: ScopeSchema,
  visibility: VisibilitySchema,
  counterparties: CounterpartiesSchema,
});

export type Reversibility = z.infer<typeof ReversibilitySchema>;
export type Scope = z.infer<typeof ScopeSchema>;
export type Visibility = z.infer<typeof VisibilitySchema>;
export type Counterparties = z.infer<typeof CounterpartiesSchema>;
export type BlastVector = z.infer<typeof BlastVectorSchema>;
