# Feature Specification: Wiki Pipeline — LLM-compiled knowledge base with ingest, compile, lint, and ask

**Feature Branch**: `feat/wiki-pipeline`

**Created**: 2026-05-16

**Status**: shipped (960fdf3, #700)

**Refs**: t_25cd184e

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Operator asks a question and gets a sourced answer (Priority: P1)

The operator runs `chitin wiki ask "how does the dispatch gate evaluate branch protection?"` and receives a concise answer sourced from the compiled knowledge base, with citations linking to the original docs.

**Why this priority**: This is the entire value proposition. Without `ask`, the pipeline is just a build step. `ask` is what makes the wiki queryable.

**Independent Test**: Ingest at least 5 source docs, compile, run `chitin wiki ask <question>` about content that appears in those sources. Verify the answer contains relevant information and cites at least one source.

**Acceptance Scenarios**:

1. **Given** a compiled wiki KB with at least 5 ingested sources, **When** the operator runs `chitin wiki ask "what does the dispatch gate check?"`, **Then** the answer references spec-kit entries and/or governance docs, and includes at least one citation.
2. **Given** an empty wiki KB (no compiled content), **When** the operator runs `chitin wiki ask <anything>`, **Then** the command returns a clear "no compiled knowledge base available" message (not an error or hallucination).

### User Story 2 — Docs are ingested from raw sources into structured content (Priority: P1)

Running `chitin wiki ingest` pulls content from configured source directories, extracts structure (headings, sections, code blocks), and stores it as raw content in the pipeline workspace. Sources include `docs/`, `.specify/specs/`, `docs/decisions/`, and any additional paths configured.

**Why this priority**: Ingest is the pipeline's input stage. Without it, there's nothing to compile or query.

**Independent Test**: Run `chitin wiki ingest` on a repo with `docs/` and `.specify/specs/` content. Verify that raw structured content appears in the pipeline workspace with correct source paths and extraction metadata.

**Acceptance Scenarios**:

1. **Given** a repo with `docs/` containing 10 markdown files and `.specify/specs/` containing 3 spec entries, **When** `chitin wiki ingest` runs, **Then** all 13 files are processed and their structured content is stored with correct source paths and section metadata.
2. **Given** a source file with nested headings, code blocks, and tables, **When** ingest processes it, **Then** the structured output preserves heading hierarchy, code block content, and table structure.
3. **Given** a previously ingested source that hasn't changed, **When** `chitin wiki ingest` runs again, **Then** that source is skipped (incremental, not full rebuild).

### User Story 3 — Ingested content compiles to a structured knowledge base (Priority: P1)

Running `chitin wiki compile` transforms raw ingested content into a searchable knowledge base optimized for `ask` queries. The compiled KB includes embeddings, section indexes, and cross-references between related topics.

**Why this priority**: Compilation is the transform that makes raw content queryable. Without it, `ask` has no structured index to search.

**Independent Test**: After ingest, run `chitin wiki compile`. Verify the compiled KB contains section indexes, cross-references, and is sized comparably to v2's 963KB platform KB (within order of magnitude).

**Acceptance Scenarios**:

1. **Given** an ingested wiki workspace with 10+ sources, **When** `chitin wiki compile` runs, **Then** a compiled KB is produced with section indexes and cross-references.
2. **Given** a compiled KB, **When** its size is measured, **Then** it is within 0.5× to 2× of v2's 963KB platform KB (order-of-magnitude coverage parity).
3. **Given** a compile step that fails on one source, **When** `chitin wiki compile` runs, **Then** it logs the failure for that source and continues with remaining sources (partial success, not total failure).

### User Story 4 — Lint validates compiled output quality (Priority: P2)

Running `chitin wiki lint` checks the compiled KB for quality issues: broken cross-references, orphaned sections, stale content indicators, and coverage gaps.

**Why this priority**: Lint catches degradation before it reaches the operator. It's a safety net, not the primary path.

**Independent Test**: After compile, run `chitin wiki lint`. Verify it reports quality metrics (section count, cross-reference integrity, coverage percentage) and flags any issues.

**Acceptance Scenarios**:

1. **Given** a compiled KB, **When** `chitin wiki lint` runs, **Then** it outputs a summary with total sections, cross-reference count, and coverage percentage.
2. **Given** a compiled KB with a cross-reference pointing to a nonexistent section, **When** lint runs, **Then** it flags that reference as broken with file and line information.
3. **Given** a compiled KB with a source doc that hasn't been updated in 90+ days, **When** lint runs, **Then** it flags that source as potentially stale.

### User Story 5 — Pipeline is re-runnable when sources update (Priority: P2)

When source content changes (docs updated, specs added, decisions revised), re-running `chitin wiki ingest && chitin wiki compile` produces an updated KB without manual cleanup.

**Why this priority**: The wiki is a living artifact. If it can't be refreshed, it rots.

**Independent Test**: Run ingest+compile. Edit one source file. Run ingest+compile again. Verify the compiled KB reflects the edit without requiring a clean rebuild.

**Acceptance Scenarios**:

1. **Given** an existing compiled KB, **When** a source doc is edited and `chitin wiki ingest && chitin wiki compile` is re-run, **Then** the compiled KB reflects the edit.
2. **Given** an existing compiled KB, **When** a new spec is added and the pipeline is re-run, **Then** the compiled KB includes the new spec without dropping existing content.
3. **Given** an existing compiled KB, **When** a source doc is deleted and the pipeline is re-run, **Then** the compiled KB removes that source's content on the next full rebuild.

## Edge Cases

- **Empty repo (no docs/)**: Ingest produces zero sources; compile produces an empty KB; `ask` returns "no compiled knowledge base available."
- **Binary files in docs/**: Ingest skips non-text files with a warning. The compiled KB contains no binary content.
- **Very large source (>10MB single file)**: Ingest chunks the file into sections; compile indexes each section independently. No single file blows up the KB.
- **Concurrent ingest+compile**: The pipeline should not corrupt the KB if two runs overlap. Use file locking or atomic writes.
- **LLM unavailable for compile**: If the compilation LLM call fails, the previous compiled KB remains valid. Compile logs the failure and exits non-zero.
- **Circular cross-references**: Compile detects and breaks cycles, logging a warning.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `chitin wiki ingest` MUST accept configurable source paths (default: `docs/`, `.specify/specs/`, `docs/decisions/`) and store structured content in the pipeline workspace.
- **FR-002**: `chitin wiki compile` MUST transform ingested content into a searchable KB with section indexes and cross-references.
- **FR-003**: `chitin wiki lint` MUST validate compiled output for broken references, stale content, and coverage gaps.
- **FR-004**: `chitin wiki ask <question>` MUST query the compiled KB and return an answer with citations to source documents.
- **FR-005**: All four subcommands MUST be idempotent and re-runnable without manual cleanup.
- **FR-006**: Pipeline MUST handle partial failures gracefully — a single source failure does not block the rest of the pipeline.
- **FR-007**: The compiled KB SHOULD achieve coverage comparable to v2's 963KB platform KB (order-of-magnitude parity, not byte-exact).
- **FR-008**: Pipeline configuration (source paths, compilation model, lint thresholds) MUST live in `chitin.yaml` or a dedicated wiki config file, not in environment variables.

### Key Entities

- **Pipeline workspace**: Directory holding raw ingested content and compiled KB (default: `~/.chitin/wiki/`).
- **Raw content**: Structured representation of source docs (headings, sections, code blocks, metadata).
- **Compiled KB**: Searchable index with embeddings and cross-references, optimized for `ask` queries.
- **Lint report**: Quality summary with section count, cross-reference integrity, coverage metrics, and issue flags.
- **Source config**: List of paths and glob patterns for ingestion, with optional metadata (category, priority).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `chitin wiki ask` returns a relevant answer with citation for questions about content in the ingested sources, in under 5 seconds locally.
- **SC-002**: Full pipeline (`ingest → compile → lint`) on the chitin repo completes in under 2 minutes.
- **SC-003**: Compiled KB size is within 0.5×–2× of v2's 963KB (order-of-magnitude coverage parity).
- **SC-004**: Re-running `ingest → compile` after a single source edit completes in under 30 seconds (incremental, not full rebuild).
- **SC-005**: `lint` on a healthy compiled KB exits 0 with zero broken references.

## Assumptions

- The pipeline is a local CLI tool, not a service. It runs on the operator's machine.
- Source content is markdown. Non-text files are skipped.
- The v2 wiki+atlas pipeline (chitinhq/wiki, chitinhq/atlas) provides reference implementations for ingest and compile logic. This spec ports their approach, not their code.
- The frontier LLM used for compilation and `ask` is operator-configurable (default: whatever `chitin.yaml` specifies).
- No UI beyond the CLI `ask` command. Console integration is a separate spec.

## Phased Delivery

- **Phase 1 (this PR)**: `ingest` + `compile` + `ask` — minimum viable query pipeline. No lint.
- **Phase 2**: `lint` subcommand with quality checks and coverage metrics.
- **Phase 3**: Incremental re-ingest (skip unchanged sources) and incremental re-compile.
- **Phase 4**: Console UI integration (`/wiki` route in chitin-console).

Each phase ships as its own tracking-epic kanban ticket linked back to this spec.

## Out of scope

- Modifying v3's existing plain markdown source files.
- Building any UI beyond the CLI `ask` command.
- Migrating content from knowledge bases outside wiki+atlas.
- Real-time watch mode (pipeline runs on-demand or via cron, not filesystem watch).
- Multi-repo wiki merge (each repo has its own wiki; cross-repo search is a followup).