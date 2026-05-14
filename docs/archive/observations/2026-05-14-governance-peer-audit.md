---
date: 2026-05-14
status: observation
purpose: Skim governance peers against chitin's kernel to identify invariant gaps and candidate enforcement mechanisms worth adapting.
---

# Governance peer audit (2026-05-14)

## Question

What do `microsoft/agent-governance-toolkit`, `cisco-ai-defense/defenseclaw`,
and `edictum-ai/edictum` enforce that chitin's kernel does not, and is any of
that worth turning into kernel work rather than substrate work?

## Answer

The meaningful kernel-side gap is not "approvals" or "LLM judges" because
those were already culled to Hermes by decision
`docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`. The real gap is
that chitin still treats outbound network and MCP access as coarse default-allow
surfaces (`http.request`, `mcp.call`) while both DefenseClaw and AGT push much
harder on pre-exec trust boundaries around external capabilities. A follow-up
spec is warranted for typed egress and MCP trust policy, but not for identity
mesh, sandbox transport, or hosted approval flows because those either do not
fit chitin's local-only boundary or belong in substrates.

## Chitin baseline

Chitin's current kernel strengths are the ones the repo already claims:

- Cross-driver typed action vocabulary in
  `go/execution-kernel/internal/gov/action.go`
- Declarative typed policy and bounds in `chitin.yaml`
- Tamper-evident decision chain and heuristic signal stamping in
  `go/execution-kernel/internal/gov/policy.go` and
  `docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md`
- Kernel-only enforcement boundary in
  `docs/decisions/2026-05-06-execution-governance-runtime-positioning.md`

Evidence in-repo:

- `go/execution-kernel/internal/gov/action.go:13-80` defines the closed action
  enum and explicitly models `http.request`, `mcp.call`, and git/GitHub actions.
- `chitin.yaml:265-273` currently default-allows both `http.request` and
  `mcp.call`.
- `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md:47-62` says
  approvals stay in Hermes while chitin keeps typed policy, bounds, and chain.
- `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md:30-46` excludes
  orchestration and approval-system ownership from chitin's remit.

## Peer gaps vs chitin

### 1. Microsoft AGT

AGT's README claims full coverage for the OWASP Agentic Top 10, including
zero-trust identity, execution sandboxing, content policies, memory integrity,
encrypted inter-agent trust gates, circuit breakers, and kill-switch handling:
<https://github.com/microsoft/agent-governance-toolkit>.

Relevant gaps:

- **Identity and inter-agent trust.** Chitin stamps identity dimensions onto
  decisions (`go/execution-kernel/internal/gov/policy.go:194-217`) but does not
  authenticate agents with signed credentials or trust scores the way AGT
  describes.
  Assessment: low priority for the current local-only product boundary.
- **Execution sandboxing / privilege rings.** Chitin denies or allows actions
  before execution, but it does not provide OS/container isolation like AGT's
  execution rings.
  Assessment: matters for defense-in-depth, but belongs in the substrate or host
  runtime rather than in the kernel.
- **Output validation, memory integrity, circuit breakers, anomaly-driven rogue
  agent controls.** Chitin explicitly is not a prompt/content moderation tool
  and culled in-gate LLM adjudication. It does have heuristic signals for blast,
  floundering, and drift, but not full output validation or runtime kill-switch
  workflows.
  Assessment: mostly outside scope, except that typed downstream actions based
  on outputs may become relevant later.

### 2. Cisco DefenseClaw

DefenseClaw focuses on admission scanning, runtime prompt/completion inspection,
static code checks, registry ingestion with SSRF guards, sandbox integration,
and rich audit sinks:
<https://github.com/cisco-ai-defense/defenseclaw>.

Relevant gaps:

- **Capability admission before runtime.** Chitin gates tool calls, but does not
  scan skills, MCP servers, or plugins before installation/use.
  Assessment: this matters. It fits chitin's moat if implemented as typed policy
  over external capability surfaces, not as another scanner bundle.
- **Registry trust / SSRF guarded catalog ingestion.** Chitin currently has
  canonical `http.request` and `mcp.call` actions, but no first-class trust
  policy over allowed hosts, registries, or MCP provenance beyond coarse allow.
  Assessment: highest-value follow-up.
- **Prompt/completion inspection and optional LLM judge.** Chitin intentionally
  does not own this.
  Assessment: no kernel action. That would recreate the cull.
- **Sandbox transport.** Useful operationally, but not chitin-owned.
  Assessment: document as composition guidance, not kernel code.

### 3. Edictum

Edictum's framing is "declared agency profiles become executable runtime
boundaries": read scope, write scope, tool authority, approval requirements, and
process obligations, plus audit events and self-protection:
<https://github.com/edictum-ai/edictum>.

Relevant gaps:

- **First-class agent profiles / contracts.** Chitin has the raw materials
  (typed actions, path predicates, bounds, role/authority dimensions on
  decisions) but not a first-class profile layer that says "low/medium/high
  agency" or "ticket-bound edit bot".
  Assessment: worth borrowing as a policy authoring format, not as a hosted
  control plane.
- **Process obligations / evidence requirements.** Edictum attaches approval and
  workflow obligations to profiles. Chitin's kernel records chain facts but does
  not express "this action requires prior evidence artifact X" as a typed rule.
  Assessment: interesting, but second priority after network/MCP trust.
- **Audit redaction.** Edictum advertises redacted audit sinks. Chitin already
  masks sensitive values in normalized arguments and tests for redaction in the
  sidecar path (`go/execution-kernel/internal/canon/parse.go:242-273`,
  `go/execution-kernel/cmd/chitin-kernel/gate_hook_test.go:323-370`).
  Assessment: no action.

## Mechanisms worth borrowing

### Borrow

- **Profile-style policy presets from Edictum.**
  Chitin should consider a thin profile layer that compiles to its existing
  typed rules and bounds. Example shape: `profile: low|medium|high` plus scoped
  overrides for `http.request`, `mcp.call`, `git.push`, and protected paths.
- **Typed egress and MCP trust policy inspired by DefenseClaw's admission model.**
  The kernel already normalizes `http.request` and `mcp.call`; the missing part
  is policy fields such as allowed hostnames, schemes, registries, and MCP
  provenance/trust classes. This deepens chitin's typed-policy moat.

### Do not borrow into the kernel

- **Hosted approvals / human-in-the-loop loops.**
  Already culled to Hermes by decision.
- **In-gate LLM judges on prompts or tool calls.**
  Already culled; downstream consumers can react to chain signals instead.
- **Sandbox runtime transport.**
  Valuable, but better owned by OpenClaw/Hermes/host runtime because chitin is
  not the execution substrate.
- **Identity mesh / PKI as a kernel requirement.**
  Strong conceptually, but mismatched with chitin's current local-only product
  boundary.

## Recommendation

Open one follow-up spec ticket:

- **Spec: typed outbound network and MCP trust policy for chitin kernel**

Rationale:

- It addresses the clearest real gap shown by peers.
- It strengthens chitin's asymmetric value: cross-driver canonical actions plus
  typed enforcement, not regex shell filtering.
- It avoids reintroducing culled substrate-parallel features.

No additional action is needed on approvals, sandbox transport, or LLM-judged
runtime inspection inside the kernel.

## Sources

- Microsoft AGT README:
  <https://github.com/microsoft/agent-governance-toolkit>
- Cisco DefenseClaw README:
  <https://github.com/cisco-ai-defense/defenseclaw>
- Edictum README:
  <https://github.com/edictum-ai/edictum>
