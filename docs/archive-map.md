# Archive Map — what v1 had, where it went

All v1 repos live at `chitinhq/<name>-archive` (or on the archive list for non-chitin repos). Tagged final release: `v1.0.0`.

## Packages in `chitinhq/chitin-archive`

### Extracted to v2 (as reference reimplementations)

| v1 path | v2 destination | Notes |
|---------|----------------|-------|
| `canon/` | `go/execution-kernel/internal/canon/` | Minimum viable subset: types, parse, normalize, digest. `compress.go` and `sequence.go` not ported (doom-loop detection is Phase 2+). |
| `internal/normalize/` | `go/execution-kernel/internal/normalize/` | Full port — `Normalize()` + `classify()` + 6-class `ActionType`. |
| `internal/flow/` (event emission pattern) | `go/execution-kernel/internal/emit/` | Reimplemented, not ported. Richer schema in v2. |
| `internal/hook/protocol.go` (read side) | `go/execution-kernel/internal/hook/protocol.go` | Reads stdin JSON + env-var fallback; no Allow/Block in v2 Phase 1 (monitor-only). |
| `SPEC/chitin-protocol.md` | `docs/event-model.md` | Seed only — v2 schema is broader and surface-neutral. |

### Parked for Phase 2 (governance)

`internal/policy`, `internal/invariant`, `internal/drift`, `internal/gate`, `internal/research`, `internal/mcp`, `internal/explain`, `internal/attribution`. Will be reimplemented inside `libs/governance` (TS) and `go/execution-kernel/internal/governance/` (Go) when governance begins.

### Left behind (not coming forward)

`internal/doctor`, `internal/init`, `internal/session`, `internal/soul`, `internal/status`, `internal/trust`, `internal/audit`, `.github/workflows/claude.yml`, `.github/workflows/clawta-dispatch.yml`. These are swarm/orchestration/ceremony that doesn't match the observability-first reset.

## Other archived repos

`clawta, octi, sentinel, shellforge, llmint, homebrew-tap, wiki, atlas, quest, bench, soulforge, souls-lab, workspace, ganglia, hypothalamus` — all frozen read-only. Re-incarnations (if any) will be new libs inside this monorepo.
