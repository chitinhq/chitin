# Copilot SDK Feasibility Spike

2-day time-boxed probe of the GitHub Copilot Go SDK to determine whether
chitin can integrate Copilot CLI with inline governance for a live demo
on 2026-05-07.

The spike completed: the Copilot CLI integration ships as
`chitin-kernel drive copilot` (in-kernel SDK wrapper). The original spike
spec + plan have been retired now that the work shipped.

## Directory layout

- `rung1-auth/` — SDK install + Enterprise auth probe
- `rung2-observe/` — JSON-RPC stream observation probe
- `rung3-intercept/` — Pre-execution intercept probe
- `rung4-gate/` — End-to-end gate + decision log probe

Each directory has its own `main.go`, `go.mod`, `README.md`, and
`RESULT.md` (evidence).

The findings have been folded into `chitin-kernel drive copilot`; the
findings report has been retired post-ship.

## Running a rung locally

    cd scratch/copilot-spike/rung<N>-<name>/
    go mod tidy
    go run main.go

Credentials expected in the operator's existing gh-auth config. Do not
commit secrets to this directory.
