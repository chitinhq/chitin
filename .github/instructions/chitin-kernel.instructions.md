---
applyTo: "go/execution-kernel/**,chitin.yaml,chitin-routes.example.yaml,bin/chitin-router-hook,scripts/install-*hook.sh"
---

Read `AGENTS.md` before touching these files. The Go kernel is the
canonical execution-governance runtime; do not add orchestration,
approval prompting, in-gate LLM calls, model routing, or MCP hosting here.

When changing a driver normalizer, keep the canonical action vocabulary in
`go/execution-kernel/internal/gov/action.go` closed and deliberate. Unknown
vendor tools must fail closed as `unknown` until a fixture and test justify
the mapping.

Every driver mapping change needs a focused normalizer test in the same
driver package. Shell-shaped tools should route through `gov.Normalize`
so git, infra, npm, test, and remote-code-exec classifications stay shared.

Router heuristics are advisory signal rows. Do not reintroduce `claude -p`,
advisor takeover, peer spawning, or approval flows in the kernel hot path.
