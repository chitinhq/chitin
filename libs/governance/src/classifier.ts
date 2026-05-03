import type { Ingress } from './tool-call-request.schema.js';
import type { SemanticEnvelope } from './semantic-envelope.schema.js';
import type { BlastVector } from './blast-vector.schema.js';

// C1 deterministic classifier. Spec §2.1.
//
// Slice 1 covers one action class: shell_exec via Bash from Claude Code's
// PreToolUse hook input shape. Coverage extends in Slice 1.5 (other
// ingresses) and Slice 2 (other action classes).
//
// Invariant (Knuth-style): for every (ingress, tool_name) pair the
// classifier handles, classify() returns a SemanticEnvelope where
// envelope.action_class is NOT 'unclassified'. For pairs it doesn't
// handle, classify() returns 'unclassified' with confidence 0; that
// signals the caller to escalate (Slice 4) or use a fallback policy.
//
// The current Slice-1 coverage:
//   ingress=claude_code_pretooluse, tool_name=Bash → shell_exec
//   ingress=openclaw_before_tool_call, tool_name=shell.exec → shell_exec

export const CLASSIFIER_VERSION = 'c1-2026-05-03';

export interface ClassifyInput {
  ingress: Ingress;
  tool_name: string;
  tool_args: Record<string, unknown>;
}

export interface ClassifyOutput {
  envelope: SemanticEnvelope;
  blast_vector: BlastVector;
  confidence: number;
  classifier_version: string;
}

export function classify(input: ClassifyInput): ClassifyOutput {
  // Bash via Claude Code PreToolUse hook. The hook payload shape sets
  // tool_args.command to the literal command string. Reference:
  // docs/observations/2026-04-19-hook-payload-capture.md.
  if (input.ingress === 'claude_code_pretooluse' && input.tool_name === 'Bash') {
    const command = typeof input.tool_args.command === 'string' ? input.tool_args.command : '';
    return classifyShellExec(command);
  }

  // openclaw plugin's before_tool_call — shell.exec with params.cmd.
  // Reference: apps/openclaw-plugin-governance/src/index.mjs.
  if (input.ingress === 'openclaw_before_tool_call' && input.tool_name === 'shell.exec') {
    const cmd = typeof input.tool_args.cmd === 'string' ? input.tool_args.cmd : '';
    return classifyShellExec(cmd);
  }

  // Unclassified — escalation signal for the caller.
  return {
    envelope: {
      action_class: 'unclassified',
      target: { kind: 'unknown', value: '' },
      artifact_type: 'unknown',
      side_effect: true,            // assume side-effect when unsure (fail-safe)
      trust_assertion: 'agent_owned',
    },
    blast_vector: {
      reversibility: 'irreversible', // fail-safe defaults
      scope: 'external',
      visibility: 'observable',
      counterparties: 'team',
    },
    confidence: 0,
    classifier_version: CLASSIFIER_VERSION,
  };
}

// classifyShellExec — extracted because both ingresses converge on the
// same logical analysis once we have the command string. Keeps Slice 1
// honest about the abstraction: ingress should not change the envelope.
function classifyShellExec(command: string): ClassifyOutput {
  // Heuristics over the raw command. Slice-1 conservatism: when unsure,
  // tighten the blast vector (more conservative blast → more likely to
  // escalate to advisor in later slices).
  const trimmed = command.trim();

  // curl|wget piped into a shell — fetches and executes. High blast.
  // Even though "curl X | sh" affects only the local box, the script
  // fetched from an unverified URL is the actual blast surface.
  const isPipeToShell = /\b(curl|wget)\b[^|]*\|\s*(sh|bash|zsh)\b/.test(trimmed);

  // npm/yarn/pnpm install — package manager side-effect on lockfile +
  // node_modules. Recoverable (delete and reinstall).
  const isInstall = /^(npm|yarn|pnpm)\s+(install|add|i)\b/.test(trimmed);

  // git push — observable, partially reversible.
  const isGitPush = /^git\s+push\b/.test(trimmed);

  // gh pr create / merge — observable, network egress, partially reversible.
  const isGhPrOp = /^gh\s+pr\s+(create|merge|close|review)\b/.test(trimmed);

  // rm -rf — destructive, but local. The bytes are irreversibly deleted;
  // whether that's a recoverable consequence depends on what was at the
  // path (e.g., node_modules can be reinstalled). Detect any rm command
  // whose flag block contains both `r` and `f`. Linear-time check on
  // the matched flags string — explicitly avoids the polynomial regex
  // shape GitHub Advanced Security flagged in Slice-1's first review.
  const rmFlagMatch = /^rm\s+-([a-zA-Z]+)\b/.exec(trimmed);
  const isRmRf = rmFlagMatch !== null
    && rmFlagMatch[1].includes('r')
    && rmFlagMatch[1].includes('f');

  let blast: BlastVector;
  let confidence: number;
  let trust: 'agent_owned' | 'external_unverified';

  if (isPipeToShell) {
    blast = {
      reversibility: 'irreversible',  // executed bytes can't be unrun
      scope: 'local',
      visibility: 'silent',
      counterparties: 'self',
    };
    confidence = 0.95;
    trust = 'external_unverified';
  } else if (isGhPrOp) {
    blast = {
      reversibility: 'reversible_with_effort',
      scope: 'external',
      visibility: 'public_broadcast',
      counterparties: 'team',
    };
    confidence = 0.9;
    trust = 'agent_owned';
  } else if (isGitPush) {
    blast = {
      reversibility: 'reversible_with_effort',
      scope: 'external',
      visibility: 'observable',
      counterparties: 'team',
    };
    confidence = 0.85;
    trust = 'agent_owned';
  } else if (isInstall) {
    blast = {
      reversibility: 'reversible',
      scope: 'project',
      visibility: 'logged',
      counterparties: 'self',
    };
    confidence = 0.9;
    trust = 'external_unverified'; // installed packages aren't agent-owned
  } else if (isRmRf) {
    blast = {
      reversibility: 'irreversible',
      scope: 'local',
      visibility: 'silent',
      counterparties: 'self',
    };
    confidence = 0.85;
    trust = 'agent_owned';
  } else {
    // Generic shell — moderate blast, low confidence.
    blast = {
      reversibility: 'reversible_with_effort',
      scope: 'local',
      visibility: 'logged',
      counterparties: 'self',
    };
    confidence = 0.4;
    trust = 'agent_owned';
  }

  return {
    envelope: {
      action_class: 'shell_exec',
      target: { kind: 'process', value: trimmed.split(/\s+/)[0] || 'unknown' },
      artifact_type: 'unknown',
      side_effect: true,
      trust_assertion: trust,
    },
    blast_vector: blast,
    confidence,
    classifier_version: CLASSIFIER_VERSION,
  };
}
