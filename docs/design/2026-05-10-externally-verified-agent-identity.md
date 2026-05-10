# Externally Verified Agent Identity

## Objective

Define how Chitin should compose with substrates that already provide
cryptographic agent identity, such as DID request signatures or verifiable
credentials, without turning Chitin into an identity provider.

The asymmetric Chitin value remains the execution-governance kernel: typed
tool-call policy, bounds, cross-driver audit, escalation, and chain evidence.
Verified identity is useful only when it strengthens those enforcement and
audit surfaces.

## Context

AgentField implements agent identity as a control-plane feature: agents receive
DIDs, sign requests with Ed25519 keys, receive verifiable credentials, and are
authorized through tag policies. That is an identity and orchestration surface.

Chitin should not duplicate that. Chitin can record and later match externally
verified evidence when a trusted driver or substrate has already performed
verification before invoking `chitin-kernel gate`.

## Boundary

Always:

- Keep Chitin local-first and kernel-scoped.
- Treat externally supplied identity claims as untrusted unless a driver marks
  them as verified through a trusted integration path.
- Preserve current runtime-profile fields:
  `agent_instance_id`, `agent_fingerprint`, `driver`, `model`, `role`,
  `station_prompt_hash`, `skills_tools_hash`, `soul_lens`, `authority`, and
  `workflow_id`.
- Use verified identity only as extra audit and policy context.

Never:

- Generate DIDs.
- Store private keys.
- Issue verifiable credentials.
- Host a tag approval workflow.
- Resolve remote DID documents in the gate hot path.
- Add network calls to `Gate.Evaluate`.

## Proposed Optional Fields

Future decision rows and decision events may carry:

- `agent_did`: externally verified agent DID or equivalent subject id.
- `credential_id`: externally verified credential id, receipt id, or VC id.
- `verification_method`: identifier of the key or method used by the verifier.
- `proof_hash`: hash of the proof material, not the full proof.
- `verified_identity`: boolean set only by a trusted driver/substrate verifier.
- `identity_issuer`: issuer or control-plane id that verified the identity.

All fields are optional and `omitempty`. Missing fields preserve current
behavior.

## Trust Rule

Raw env vars may carry identity metadata for audit, but they must not become
trusted authority by themselves.

Only two paths can set `verified_identity=true`:

1. A Chitin-owned driver plugin verifies proof before gate evaluation.
2. A substrate integration passes identity evidence through a Chitin-trusted
   local boundary, such as an installed plugin or signed local envelope.

The Go kernel should enforce this distinction by keeping raw claim fields and
verified fields separate.

## Policy Shape

Policy selectors should match verified identity fields only when
`verified_identity=true`. For example:

```yaml
rules:
  - id: verified-supervisor-can-merge
    action: github.pr.merge
    effect: allow
    authority: supervisor
    verified_identity: true
    identity_issuer: hermes-local
```

If verified identity fields are present but `verified_identity` is false or
missing, selectors that require verified identity must not match.

## Success Criteria

- Chitin can ingest externally verified identity evidence without expanding
  into identity issuance or orchestration.
- Policy can distinguish verified identity from raw caller claims.
- The gate hot path stays local and non-networked.
- Existing fingerprint v2 behavior remains backward compatible.

## Non-Goals

- DID method support.
- Credential storage lifecycle.
- Revocation registry.
- Admin approval UI.
- Agent discovery or routing.
- Cross-agent call authorization.

Those belong in substrates such as Hermes, OpenClaw, AgentField, or other
control planes. Chitin should only gate the proposed execution using local
evidence available at the tool-call boundary.
