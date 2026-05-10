# Agent Fingerprinting V2

## Objective

Make agent identity useful for cross-driver audit and analytics without
turning Chitin into an orchestrator. Chitin should record enough identity
context to answer: which runtime profile attempted this action, under which
driver, model, role, prompt/tool configuration, and authority class?

## Scope

This slice extends the existing decision identity fields. It does not route
models, schedule work, approve actions, or manage agent sessions.

## Canonical Dimensions

- `agent_instance_id`: one live worker/session/run.
- `agent_fingerprint`: stable profile fingerprint from dispatcher/contracts.
- `driver`: tool-call surface such as `claude-code`, `codex`, `gemini`,
  `hermes`, `copilot`, or `openclaw`.
- `model`: model identifier as supplied by the dispatching runtime.
- `role`: workload role such as `worker`, `reviewer`, `architect`,
  `researcher`, `system`, or `external`.
- `station_prompt_hash`: hash of the governing prompt/context bundle.
- `skills_tools_hash`: hash of enabled skills/toolsets.
- `soul_lens`: optional cognitive/persona lens, or `none`.
- `claimed_authority`: authority claimed by the runtime.
- `authority`: effective trusted authority resolved by kernel policy.
- `workflow_id`: parent workflow or task id.

## Environment Contract

Each dimension reads `CHITIN_<DIM>` first, then
`CHITIN_DISPATCH_<DIM>`. `soul_lens` also falls back to
`CHITIN_ACTIVE_SOUL` to match the TypeScript contracts helper.

Absent dimensions remain omitted from decision JSON. `role` defaults to
`external` only when no explicit role is supplied, preserving the existing
bucket for ad-hoc hooks and operator CLI calls.

## Boundaries

Always:

- Keep allow/deny authority in the Go kernel.
- Treat identity fields as audit metadata unless policy grants explicitly
  trust a stable selector.
- Preserve the legacy `agent` display field and `fingerprint` alias.

Never:

- Let a claimed env authority become effective authority by itself.
- Spawn or consult an LLM from the kernel.
- Write lab/research state into the Chitin repo.

## Success Criteria

- Decision JSONL rows can carry prompt/tool/lens dimensions.
- Canonical decision events include raw identity metadata in payload and
  labels.
- The event envelope keeps a 64-character compatibility fingerprint even
  when the dispatch fingerprint is the 12-character contracts form.
- Missing/partial identity remains backward compatible.
