import { z } from 'zod';

// Blast vector — 4-axis description of an action's potential effect.
// Spec: docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md §2.
//
// Treated as a vector, not a scalar, because folding into a single
// "risk score" loses information policy needs. A `Bash rm -rf node_modules`
// has small blast (irreversible × local × silent × self) — scary verb,
// recoverable. A `slack.delete_channel` has huge blast
// (irreversible × external × public_broadcast × team) — trivial call,
// unrecoverable. A scalar can't see the difference.

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
