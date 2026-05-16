# Feature Specification: Analyzer Cron — periodic LLM analysis of governance sessions

**Feature Branch**: `feat/analyzer-cron`

**Created**: 2026-05-16

**Status**: Draft

**Refs**: t_3e13b0d5, docs/superpowers/specs/2026-05-12-chitin-dashboard.md § Slice 5

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Cron runs daily and produces at least one actionable suggestion (Priority: P1)

Every 24 hours, `analyzer-cron.lobster` reads recent chitin governance sessions, calls a frontier LLM with a structured analysis rubric, and emits suggestions to the `analyzer_suggestions` SQLite table. At least one suggestion is actionable (specific target + diff/rationale, not vague).

**Why this priority**: This is the core value — the cron must produce usable suggestions, not noise. If the rubric produces zero actionable items over 24h of real sessions, the analysis prompt needs calibration.

**Independent Test**: Run the analyzer over a 24h sample of governance sessions. Verify it writes at least one row to `analyzer_suggestions` with a non-empty `diff`, `rationale`, and `target` fields.

**Acceptance Scenarios**:

1. **Given** 24h of governance sessions with at least 50 decisions, **When** `analyzer-cron` runs, **Then** it produces at least one suggestion with `type`, `target`, `diff`, `rationale`, and `applied=false`.
2. **Given** zero governance sessions in the analysis window, **When** `analyzer-cron` runs, **Then** it exits cleanly with no suggestions written (not an error condition).
3. **Given** a session where the same rule denies the same agent 3+ times in 24h, **When** `analyzer-cron` runs, **Then** it emits at least one `policy_rule` suggestion targeting that rule.

### User Story 2 — Dashboard surfacing suggestions by type and target (Priority: P2)

The operator visits `/suggestions` on the chitin dashboard and sees a filterable list of analyzer suggestions — sortable by type, target, and creation date. Each suggestion shows its rationale and proposed diff.

**Why this priority**: Suggestions that can't be reviewed are useless. The dashboard is the review surface.

**Independent Test**: Run the analyzer to produce 5+ suggestions. Navigate to `/suggestions`. Verify the list renders with type/target/date filters and each suggestion shows its rationale.

**Acceptance Scenarios**:

1. **Given** 10 suggestions in `analyzer_suggestions`, **When** the operator visits `/suggestions`, **Then** they see 10 rows filterable by `type` (prompt_edit, new_skill, policy_rule, route_tweak, drop) and `target`.
2. **Given** a suggestion of type `policy_rule` targeting rule `deny-write-to-prod`, **When** the operator filters by `type=policy_rule`, **Then** only policy_rule suggestions appear, and the targeting rule is visible.
3. **Given** a suggestion with `applied=true`, **When** the operator views `/suggestions`, **Then** the suggestion is visually distinguished (strikethrough or "applied" badge).

### User Story 3 — Analysis rubric detects the five signal categories (Priority: P1)

The analyzer's rubric covers five signal categories: wasted denials, cost outliers, tool thrashing, routing failures, and stale rules. Each category has a clear threshold and produces a distinct suggestion type.

**Why this priority**: Without the rubric, the LLM produces unstructured commentary. The rubric constrains the output to actionable, category-tagged suggestions.

**Independent Test**: Feed the analyzer sessions that trigger each of the five categories. Verify each produces a suggestion of the correct type.

**Acceptance Scenarios**:

1. **Given** a session where `deny-write-to-prod` fires 4 times for `hermes` in 24h, **When** the analyzer runs, **Then** it produces a `policy_rule` suggestion targeting `deny-write-to-prod`.
2. **Given** a session whose cost is >2× the median for its task class, **When** the analyzer runs, **Then** it produces a `prompt_edit` or `route_tweak` suggestion identifying the cost outlier.
3. **Given** a session with 6+ similar tool calls (tool thrashing), **When** the analyzer runs, **Then** it produces a `new_skill` or `prompt_edit` suggestion addressing the thrashing pattern.
4. **Given** a session where `claude-code` was dispatched to a task that `codex` would have handled better (routing failure), **When** the analyzer runs, **Then** it produces a `route_tweak` suggestion.
5. **Given** a rule that has not fired in 30 days, **When** the analyzer runs, **Then** it produces a `drop` suggestion targeting that rule.
6. **Given** a session that triggers none of the five categories, **When** the analyzer runs, **Then** it writes no suggestion for that session (no false positives).

### User Story 4 — Model escalation on high-disagreement signals (Priority: P3)

When the rubric produces a suggestion with low confidence (the analysis LLM rates it <0.7 confidence or the suggestion contradicts an existing rule), the analyzer escalates to a more capable model (Opus) for a second pass before emitting the suggestion.

**Why this priority**: This prevents noisy suggestions from reaching the dashboard. Low-confidence output gets a second opinion before it's stored.

**Independent Test**: Produce a suggestion with confidence <0.7. Verify the analyzer re-runs analysis with the escalation model before storing the suggestion.

**Acceptance Scenarios**:

1. **Given** a rubric signal with confidence 0.5, **When** the analyzer processes it, **Then** it escalates to the configured escalation model (default: Opus) and re-evaluates before storing.
2. **Given** a rubric signal with confidence 0.85, **When** the analyzer processes it, **Then** it stores the suggestion without escalation.
3. **Given** the escalation model is unavailable, **When** a low-confidence signal is detected, **Then** the analyzer stores the suggestion with a `needs_review` flag instead of dropping it.

## Edge Cases

- **Empty decision log**: Analyzer runs, finds zero sessions in the window, exits cleanly with no suggestions.
- **Very large decision log (100k+ decisions)**: Analyzer processes in chunks or limits to the most recent N sessions to stay within LLM context windows.
- **LLM API failure mid-analysis**: Analyzer logs the failure, writes no suggestion for that session, continues with remaining sessions. Does NOT crash.
- **Duplicate suggestions (same type+target)**: Analyzer checks for existing suggestions with the same type and target within the last 7 days and does not re-emit duplicates.
- **Suggestion with `applied=true` is re-triggered**: Analyzer does not re-emit applied suggestions for the same target within 30 days.
- **Rubric produces only `drop` suggestions (counter-bias)**: The drop type exists as a counter-bias — when every signal suggests adding rules, at least one should suggest removing unused ones.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `analyzer-cron.lobster` MUST run daily and on-demand (via `/analyze` dashboard button).
- **FR-002**: A new SQLite table `analyzer_suggestions(id, type, target, diff, rationale, applied, created_at)` MUST be created to store suggestions.
- **FR-003**: Suggestion types MUST be one of: `prompt_edit`, `new_skill`, `policy_rule`, `route_tweak`, `drop`.
- **FR-004**: The analysis rubric MUST cover five signal categories: wasted denials (same rule, same agent, 3+ in N hours), cost outliers (>2× median for task class), tool thrashing (5+ similar tool calls), routing failures (wrong driver for task type), stale rules (no fire in 30 days).
- **FR-005**: Default analysis model MUST be Sonnet; escalation to Opus on low-confidence signals (<0.7 confidence or contradictory suggestion).
- **FR-006**: The analyzer MUST be registered as a cron job (`openclaw cron` or `hermes cron`).
- **FR-007**: `/suggestions` route on the dashboard MUST display suggestions filterable by type and target, sortable by `created_at`.
- **FR-008**: The `drop` suggestion type MUST exist as a counter-bias mechanism — at least one `drop` suggestion should be produced if stale rules are detected, even when other signals suggest additions.

### Key Entities

- **analyzer_suggestions table**: `id` (auto), `type` (enum of 5), `target` (string: rule name, prompt path, skill name, etc.), `diff` (proposed change), `rationale` (why), `applied` (boolean, default false), `created_at` (timestamp).
- **Analysis rubric**: Structured prompt template fed to the LLM with session data and signal thresholds.
- **analyzer-cron.lobster**: Lobster workflow that reads governance sessions, runs the rubric, and writes suggestions.
- **/suggestions dashboard route**: Angular component rendering the suggestions table with filters.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: At least 1 actionable suggestion (non-empty type+target+diff) produced over a 24h sample of real governance sessions (manual calibration allowed).
- **SC-002**: Analyzer cron completes a full run over 24h of sessions in under 10 minutes (including LLM calls).
- **SC-003**: Zero false-positive `drop` suggestions for rules that fired within the last 30 days.
- **SC-004**: /suggestions dashboard renders 50 suggestions in under 500ms.
- **SC-005**: Duplicate suppression works — no two suggestions with same type+target within 7 days.

## Assumptions

- The analyzer reads from `gov-decisions-*.jsonl` and `chain_index.sqlite` (the chitin governance store, same data the kernel writes to).
- The dashboard already exists (Slices 1-4 of the dashboard spec). This slice adds one route.
- The frontier LLM is available via the same API used by other chitin tools. Model selection is operator-configurable.
- Manual rubric calibration is expected in the first week. The "at least 1 actionable suggestion" success criterion accounts for this.
- Auto-applying suggestions is explicitly out of scope (Slice 6 owns adoption).

## Phased Delivery

- **Phase 1 (this PR)**: `analyzer_suggestions` table + `analyzer-cron.lobster` workflow + analysis rubric + cron registration. No dashboard. Suggestions are queryable via `sqlite3 ~/.chitin/analyzer_suggestions.db`.
- **Phase 2**: `/suggestions` dashboard route with filtering and sorting.
- **Phase 3**: Model escalation on low-confidence signals.
- **Phase 4**: Duplicate suppression and `applied` tracking integration with Slice 6.

Each phase ships as its own tracking-epic kanban ticket linked back to this spec.

## Out of scope

- Auto-applying suggestions (Slice 6 owns this).
- Live cross-session correlation (future enhancement).
- Building the dashboard from scratch (this slice adds one route to an existing dashboard).
- Replacing the governance decision store (analyzer reads, never writes to gov-decisions).
- Multi-model comparison (more than one escalation model).