#!/usr/bin/env node
/**
 * @chitin/advisor — chain-consumer scaffold.
 *
 * Reads heuristic signal metadata stamped on
 * `~/.chitin/gov-decisions-<utc-date>.jsonl` rows by the kernel's
 * router-hook (predicted_blast, floundering_score, drift_score,
 * routing_decision) and decides what to do about it: ask another
 * model for a second opinion, file a kanban ticket, page an
 * operator, dispatch a peer at a higher tier, etc.
 *
 * The implementation is the OPERATOR's choice and lives in this
 * directory. The kernel deliberately does NOT spawn LLMs in the
 * gate hot path — see
 * docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md
 * for the rationale (Sun Tzu lens: in-gate `claude -p` was a
 * symmetric duplicate of hermes' approvals.mode: smart).
 *
 * Boundary contract:
 *   - Reads chain rows.
 *   - Reads policy + routes config (chitin.yaml + chitin-routes.yaml).
 *   - Calls whatever it likes downstream — LLMs, ticket systems,
 *     pagers, gateway DMs, peer-spawn mechanisms.
 *   - DOES NOT mutate chain rows in-place.
 *   - DOES NOT gate tool calls (the kernel already returned its
 *     verdict by the time this runs).
 *
 * See `apps/advisor/README.md` for wiring shapes.
 */

const args = process.argv.slice(2);

if (args.includes('-h') || args.includes('--help')) {
  console.log(
    [
      'chitin-advisor — chain-consumer scaffold (NOT YET IMPLEMENTED)',
      '',
      'Reads heuristic signals from ~/.chitin/gov-decisions-*.jsonl and',
      'routes follow-up actions (LLM second opinion, kanban ticket,',
      'operator page, peer spawn). The kernel emits the signals; this',
      'app decides what to do with them.',
      '',
      'See apps/advisor/README.md for the contract and wiring shapes.',
    ].join('\n'),
  );
  process.exit(0);
}

console.error(
  'chitin-advisor: not yet implemented; see apps/advisor/README.md.\n' +
    'Scaffolded 2026-05-08 alongside the audit Tier 6 cull that moved\n' +
    'the LLM advisor out of the kernel hot path.',
);
process.exit(64);
