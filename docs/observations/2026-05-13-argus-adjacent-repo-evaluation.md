# Argus Adjacent Repo Evaluation

Date: 2026-05-13
Ticket: t_d9b109e1

This evaluates three adjacent projects for the Argus observatory spec in
`docs/superpowers/specs/2026-05-12-argus-observatory.md`, especially Slice 4
agent memory mining.

## Verdict

| Repo                     |                             HEAD evaluated | Classification                                               | Recommendation                                                                                                                                 |
| ------------------------ | -----------------------------------------: | ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `vectorize-io/hindsight` | `51ea9aa2864bbc77065c763f450691f4311a5a9f` | Source, not implementation substrate                         | Add an optional Hindsight read adapter for Slice 4 if the operator runs it. Do not fork or build Argus on it.                                  |
| `Agent-Field/agentfield` | `7334d019fc8168637633ef70c7f09013324aaf76` | Source, not implementation substrate                         | Copy the execution-log envelope ideas into Argus's source contracts; ingest AgentField output only when AgentField appears in the local fleet. |
| `iii-hq/iii`             | `54c841aea8ce1646686468d8ad923eaf6b19fbc3` | Source and primitive reference, not implementation substrate | Treat iii OTLP traces, stream state, and console-visible logs as possible Slice 3 inputs. Do not base Argus on the iii engine.                 |

## Hindsight vs Argus Slice 4

Hindsight overlaps with Slice 4's "what each agent believes" model, but at a
different layer. Argus wants a read-only, cross-source belief index:
`beliefs(agent, subject, claim, ts_recorded, source_path)`, then drift detectors
compare those beliefs against chain, kanban, git, and logs. Hindsight is an
active memory system for agents, centered on memory banks and `retain`,
`recall`, and `reflect`. Its README describes automatic memory storage through
an LLM wrapper, direct API/SDK access, and memories organized as facts,
experiences, mental models, entities, relationships, and time series.

That makes Hindsight useful as a Slice 4 source when present:

- `world` facts and `experience` facts can normalize into Argus beliefs.
- Mental models and observations are especially close to Argus "belief"
  records because they encode synthesized agent understanding.
- Hindsight bank tags and metadata can preserve per-agent or per-user privacy
  boundaries.
- Its webhooks (`retain.completed`, consolidation events) could eventually
  feed incremental Argus ingestion.

It should not become Argus's implementation substrate:

- Argus is read-only and evidence-first; Hindsight is designed to mutate and
  improve an agent's memory during operation.
- Argus's hard inputs are chitin chain, kanban, git, logs, Hermes/OpenClaw
  memory, wiki, and graph outputs. Hindsight does not replace those adapters.
- Argus must compare belief against external ground truth. Hindsight is one
  possible belief store, not the arbiter of truth.
- Hindsight's server path introduces PostgreSQL/vector indexes and optional
  model-provider calls. Argus's foundation is a local SQLite observatory plus
  bounded qwen narration.

Fork-vs-build verdict for Slice 4: build Argus's canonical belief schema,
adapters, and drift detectors in chitin; do not fork Hindsight. File a later
adapter ticket only if the operator chooses to run Hindsight locally or if a
Hermes/OpenClaw memory migration lands on Hindsight. If that happens, prefer
read-only API export or direct database export over embedding Hindsight in the
Argus daemon.

## AgentField

AgentField is a Go control plane for building and running agents as backend
services. It includes routing, coordination, shared memory, async execution,
cryptographic identity, verifiable credentials, Prometheus metrics, structured
logs, workflow DAGs, execution timelines, and raw node log APIs.

For Argus, this is not a base to build on. It overlaps with Hermes and Clawta's
substrate responsibilities: routing, workflow execution, approvals, harnesses,
and agent fleet management. Pulling it under Argus would violate Argus's
read-only observatory boundary.

It is still worth mining for source-contract design. AgentField's execution
observability RFC separates lifecycle/workflow events, structured
execution-correlated logs, and raw node logs. That maps cleanly onto Argus
Slices 2 and 3:

- Add `source_system`, `workflow_id`, `root_workflow_id`, `node_id`,
  `event_type`, `level`, and `source` as optional normalized fields for log
  events.
- Keep raw process logs secondary; prefer structured execution logs whenever a
  source provides them.
- If an AgentField deployment exists locally, ingest its workflow DAG,
  execution timeline, VC/audit receipts, and structured logs as source data.

## iii

iii reduces services to workers, triggers, and functions. Its engine is Rust,
the console inspects workers/functions/triggers/queues/traces/logs/real-time
state, and the skills/docs describe built-in OpenTelemetry support, W3C trace
propagation, Prometheus scraping, and WebSocket stream primitives.

For Argus, iii is a source and primitive reference. It is attractive for the
"real-time service observation" shape, especially Slice 3 logs and any later
live dashboard, but it should not become Argus's runtime:

- The iii engine is ELv2, while chitin should avoid coupling Argus's core to a
  non-Apache/MIT runtime.
- Worker/trigger/function hosting is a substrate concern. Hermes/OpenClaw
  already own orchestration and runtime hosting.
- Argus should ingest OTLP/log/stream outputs from systems like iii, not run
  inside their engine.

The useful amendment is narrow: when Slice 3 specifies logs ingestion, add OTLP
trace/log ingestion and stream-tail ingestion as optional source classes. The
schema should preserve `trace_id`, `span_id`, worker/function identifiers,
trigger type, and stream name/group where present.

## Spec Amendments To File

1. `t_419b8362` - Add an optional Slice 4 "Hindsight memory-bank adapter"
   ticket. Scope it as read-only normalization from Hindsight facts,
   experiences, observations, and mental models into Argus `beliefs`.
2. `t_3bf25460` - Add a Slice 2/3 "structured execution log envelope"
   ticket. Use AgentField's distinction between lifecycle events, structured
   execution logs, and raw node logs to refine Argus's event schema.
3. `t_db604b40` - Add a Slice 3 stretch ticket for OTLP and real-time stream
   ingestion, using iii as the reference shape for traces, metrics, logs, and
   stream events.

## Sources

- `vectorize-io/hindsight` README and docs at HEAD
  `51ea9aa2864bbc77065c763f450691f4311a5a9f`.
- `Agent-Field/agentfield` README, `docs/ARCHITECTURE.md`,
  `docs/api/AGENT_NODE_LOGS.md`, and
  `docs/design/execution-observability-rfc.md` at HEAD
  `7334d019fc8168637633ef70c7f09013324aaf76`.
- `iii-hq/iii` README, `engine/README.md`, `console/README.md`,
  `skills/iii-observability/SKILL.md`, and
  `skills/iii-realtime-streams/SKILL.md` at HEAD
  `54c841aea8ce1646686468d8ad923eaf6b19fbc3`.
