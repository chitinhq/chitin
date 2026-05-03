import { z } from 'zod';

// Semantic envelope — derived from a raw tool call by the classifier.
// Spec: docs/superpowers/specs/2026-05-03-predictive-execution-policy-design.md §2.
//
// Policy matches against the envelope, not the raw call. Pattern-matching
// raw bash args is brittle the way regex denylists are; a policy expressed
// as "deny irreversible × public_broadcast × external unless approved"
// applies regardless of which tool surfaced the action. That's the
// abstraction win.

export const ActionClassSchema = z.enum([
  'shell_exec',          // arbitrary shell command (Bash, Cmd, etc.)
  'file_write',          // creating or modifying files
  'file_read',           // reading files (low blast in most cases, recorded for completeness)
  'network_egress',      // outbound network call (HTTP, DNS, etc.)
  'network_download',    // network call that pulls a fetched artifact
  'git_op',              // git read/write at the local repo level
  'pr_op',               // PR-level operations (open, merge, close)
  'external_message',    // messages sent to humans / channels (Slack, email)
  'database_write',      // writes to a DB
  'memory_write',        // writes to chitin's memory store or similar
  'subprocess_spawn',    // spawning a subprocess (which may then do anything)
  'unclassified',        // classifier could not assign — escalation trigger
]);

export const TargetKindSchema = z.enum([
  'path',     // a filesystem path
  'host',     // a network hostname
  'channel',  // a messaging channel (Slack, Discord, etc.)
  'repo',     // a git repository ref
  'process',  // a process or subprocess identifier
  'unknown',
]);

export const TargetSchema = z.object({
  kind: TargetKindSchema,
  value: z.string(),
});

export const ArtifactTypeSchema = z.enum([
  'shell_script',
  'binary',
  'source',
  'config',
  'data',
  'document',
  'unknown',
]);

export const TrustAssertionSchema = z.enum([
  'agent_owned',           // agent created it in this session
  'user_owned',            // produced by the user / invoking environment
  'external_unverified',   // came from outside (e.g., fetched URL) without a trust signal
  'external_verified',     // came from outside but with a trust signal (signed binary, allowlisted host, etc.)
]);

export const SemanticEnvelopeSchema = z.object({
  action_class: ActionClassSchema,
  target: TargetSchema,
  artifact_type: ArtifactTypeSchema,
  side_effect: z.boolean(),
  trust_assertion: TrustAssertionSchema,
});

export type ActionClass = z.infer<typeof ActionClassSchema>;
export type TargetKind = z.infer<typeof TargetKindSchema>;
export type Target = z.infer<typeof TargetSchema>;
export type ArtifactType = z.infer<typeof ArtifactTypeSchema>;
export type TrustAssertion = z.infer<typeof TrustAssertionSchema>;
export type SemanticEnvelope = z.infer<typeof SemanticEnvelopeSchema>;
