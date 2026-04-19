# Souls

Cognitive-lens archetypes used as the `soul_id` + `soul_hash` inputs in the
Phase 1.5 event contract (`session_start.payload.soul_id`,
`session_start.payload.soul_hash` — see
`docs/superpowers/specs/2026-04-19-observability-chain-contract-design.md`).

Two tiers:

- **`canonical/`** — promoted souls (8). Stable content, carry
  `status: promoted` frontmatter. These are the ones agents should prefer
  by default, and the ones captured events most often reference.
- **`experimental/`** — personal / lab / in-flight souls (7). Carry
  `status: provisional` frontmatter. Expected to churn; promotion moves a
  soul into `canonical/` with a `promoted_at` date added.

Two events with the same `soul_id` but different `soul_hash` mean the soul
configuration was edited between runs — forensically meaningful. The
envelope's `agent_fingerprint` composites `soul_hash` with `model`,
`system_prompt_hash`, and `tool_allowlist_hash`, so soul edits change
fingerprints deterministically.

## Current library

**Canonical (promoted):**
- `curie.md` — empirical rigor, measurement-first
- `davinci.md` — cross-domain observation, sketch-and-ship
- `knuth.md` — algorithmic correctness, literate programming
- `lovelace.md` — declarative pattern, abstraction over mechanism
- `shannon.md` — emitter/receiver discipline, channel thinking
- `socrates.md` — question-driven inquiry, assumption surfacing
- `sun-tzu.md` — decisive action, terrain before tactics
- `turing.md` — computational substrate, machine semantics

**Experimental (provisional):**
- `dijkstra.md` — correctness by construction, illegal states unrepresentable
- `feynman.md` — teach it to understand it, first principles
- `hamilton.md` — assume partial failure, design for the mistake
- `hopper.md` — compiler-author mindset, make the machine meet the human
- `jared_pleva.md` — personal archetype
- `jobs.md` — taste arbitration, reductive product sense
- `jokic.md` — orchestration, court vision, make teammates better

## Computing `soul_hash`

The `soul_hash` in an event envelope is SHA-256 of the soul file contents,
UTF-8 bytes, no normalization:

```bash
sha256sum souls/canonical/shannon.md | cut -d' ' -f1
```

Anyone reading a captured event can verify the soul configuration from
this repo's history.

## Sourcing

Canonical souls originate from `chitinhq/soulforge` (archived). Experimental
souls originate from `chitinhq/souls-lab` (archived). Both repos are
private archives; this directory is the live, in-repo copy going forward.

## Contributing

Adding a new soul: put it in `experimental/`, provisional status, with the
frontmatter pattern shown in any existing file. Promote by moving to
`canonical/`, adding `status: promoted` and `promoted_at: YYYY-MM-DD` to
the frontmatter.
