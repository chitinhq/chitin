# Spec 042: Octi agent-bus identity contract

**Status**: DRAFT 2026-05-18 — awaiting operator ratification.

## Summary

Every agent in the chitin swarm MUST present a verifiable identity to
the agent-bus.  Without identity there is no accountability and no
way to enforce per-agent policy.

## Requirements

### R1 — the identity contract

An agent identity is a tuple of (agent_id, public_key, display_name,
lane).  The bus validates the tuple at connect time.

### R2 — identity anchors Discord threads

Every Discord notification from an agent carries an identity anchor
so the operator can reply in-thread and route the response back to
the originating agent.

## Boundary cases

1. **Agent connects without identity** → bus rejects the connection.
2. **Stale identity (agent decommissioned)** → bus allows connect
   but flags the identity as stale after a configurable window.
3. **Name collision** → first-registered wins; second is rejected.

## Open questions

- **Q1 — key rotation.** How does an agent rotate its key?  Proposed:
  re-connect with a signed identity update.
- **Q2 — lane enumeration.** Is the set of lanes fixed or extensible?
  Proposed: fixed for v1.

## Acceptance criteria

- **AC1** — agent-bus rejects connections without a valid identity.
- **AC2** — every Discord notification includes an identity anchor.

## Slice plan

- **Slice 1** — identity contract + bus validation (R1, R2).