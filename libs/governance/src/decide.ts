import type { ToolCallRequest } from './tool-call-request.schema.js';
import type { Decision } from './decision.schema.js';

// decide() — synchronous policy evaluation.
// Spec §1, §3 (decision space, policy engine).
//
// Slice 1 implements three rules + a default:
//   1. shell_exec, command matches `curl|sh` pattern    → redirect
//   2. shell_exec, command starts with `npm install`    → rewrite to pnpm
//   3. action_class = unclassified                      → deny (fail-safe)
//   default                                             → allow
//
// Subsequent slices will replace this body with a rule-table lookup
// driven by the SDK's `policies` array. Slice 1 keeps it inline so the
// tests can pin the contract; the rule-table abstraction lands in
// Slice 1.5 alongside the SDK shape.
//
// Knuth-style invariant: for every ToolCallRequest, decide() returns
// exactly one Decision whose policy_name names the rule that fired.
// No request returns null; no request fires two rules at once. Test:
// libs/governance/tests/decide.test.ts asserts this on every fixture.

export const POLICY_VERSION = 'p1-2026-05-03';

export async function decide(request: ToolCallRequest): Promise<Decision> {
  // Fail-safe: unclassified actions are denied. This is the escalation
  // hook for later slices — Slice 4's advisor consultation hangs off
  // unclassified-but-low-blast cases. Slice 1 just denies.
  if (request.semantic_envelope.action_class === 'unclassified') {
    return {
      kind: 'deny',
      policy_name: 'unclassified-fail-safe',
      policy_version: POLICY_VERSION,
      reason:
        'The classifier could not assign an action class to this tool call. ' +
        'Slice-1 policy denies unclassified actions until a later slice adds ' +
        'advisor consultation for novel patterns.',
      alternatives: [
        'Use a tool the classifier already covers (Bash, shell.exec)',
        'Ask the user to extend the classifier table for this pattern',
      ],
    };
  }

  // shell_exec rules. Pattern matching here is deliberately on the
  // *args*, not the envelope, because the envelope already says
  // action_class=shell_exec and the rule is about the specific shape.
  // Future slices can hoist these into envelope.target / envelope.artifact_type
  // matches instead — but until those fields are populated meaningfully
  // by the classifier (Slice 2), pattern matching is honest.
  if (request.semantic_envelope.action_class === 'shell_exec') {
    const command = typeof request.tool_args.command === 'string'
      ? request.tool_args.command
      : typeof request.tool_args.cmd === 'string'
      ? request.tool_args.cmd
      : null;

    // Fail-safe: a shell_exec envelope without a recognizable command
    // string indicates an ingress-adapter shape drift OR a malicious
    // attempt to bypass shell_exec rules by stripping the command field.
    // Either way: treat as unclassified-equivalent and deny. This
    // closes a bypass Copilot flagged in Slice-1's first review.
    if (command === null) {
      return {
        kind: 'deny',
        policy_name: 'shell-exec-missing-command-fail-safe',
        policy_version: POLICY_VERSION,
        reason:
          'A shell_exec tool call was received without a recognizable ' +
          'command string in tool_args.command or tool_args.cmd. The ' +
          'classifier shape and the decide-path shape are out of sync, ' +
          'or the call is shaped to bypass the shell_exec rules. ' +
          'Slice-1 fail-safe: deny.',
        alternatives: [
          'Re-emit the call through the documented ingress shape ' +
            '(claude_code_pretooluse + Bash + tool_args.command, ' +
            'or openclaw_before_tool_call + shell.exec + tool_args.cmd)',
          'Ask the user to extend the classifier for a new ingress shape',
        ],
      };
    }

    const trimmed = command.trim();

    // Rule 1: curl|sh — fetches and executes unverified content. Redirect
    // to a safer alternative.
    if (/\b(curl|wget)\b[^|]*\|\s*(sh|bash|zsh)\b/.test(trimmed)) {
      return {
        kind: 'redirect',
        policy_name: 'no-curl-pipe-sh',
        policy_version: POLICY_VERSION,
        reason:
          'Piping fetched content directly into a shell executes unverified ' +
          'bytes. The fetched script could change between fetch and execution, ' +
          'or be a typosquat of an expected URL.',
        alternatives: [
          'Save the script to a file, audit it, then run with --dry-run first',
          'Install the tool from a package manager (apt, brew, pnpm, etc.)',
          'Use a verified package mirror for the source',
        ],
      };
    }

    // Rule 2: npm install → pnpm install. This repo uses pnpm.
    // Demonstrates rewrite as a productive policy decision (vs. denial).
    if (/^npm\s+(install|i|add)\b/.test(trimmed)) {
      const rewritten = trimmed.replace(/^npm\s+(install|i|add)\b/, (match) =>
        match.replace(/^npm/, 'pnpm'),
      );
      return {
        kind: 'rewrite',
        policy_name: 'pnpm-not-npm',
        policy_version: POLICY_VERSION,
        reason:
          'This repo uses pnpm; npm would create a separate node_modules tree ' +
          'and not respect the workspace lockfile.',
        rewrite_args: { command: rewritten },
      };
    }
  }

  // Default: allow.
  return {
    kind: 'allow',
    policy_name: 'default-allow',
    policy_version: POLICY_VERSION,
    reason: 'No rule matched; default policy allows.',
  };
}
