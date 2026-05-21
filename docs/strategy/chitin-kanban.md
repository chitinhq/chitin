# Chitin Kanban Ticket Spec

Date: 2026-05-14
Derived from: [`chitin-roadmap.md`](./chitin-roadmap.md). Cross-reference: [`chitin-moat-audit.md`](./chitin-moat-audit.md).

This is the executable form of the roadmap. Every ticket carries enough context that any reasonably-credentialed contributor can pick it up cold.

Ticket-ID convention: `CHIT-<NNN>` (sequential). Horizon column maps to H1 (0-30d), H2 (31-90d), H3 (91-180d).

---

## 1. Ticket index

| ID | Title | Priority | Horizon | Type | Size | Group |
|---|---|---|---|---|---|---|
| CHIT-001 | EU AI Act Article 12 mapping doc | P0 | H1 | Docs / Research | M | Ready |
| CHIT-002 | `chitin-kernel compliance article-12 verify` subcommand | P0 | H1 | Engineering | M | Ready |
| CHIT-003 | Gate-latency + chain-throughput benchmark | P0 | H1 | Infra / Engineering | S | Ready |
| CHIT-004 | Chitin Protocol Spec v0.1 | P0 | H1 | Docs / Engineering | M | Ready |
| CHIT-005 | v0.1.0 signed release pipeline | P0 | H1 | Infra | M | Ready |
| CHIT-006 | Landing page + thesis blog post | P0 | H1 | GTM / Docs | M | Ready |
| CHIT-007 | OWASP Agentic-AI Top 10 mapping doc | P0 | H1 | Docs / Security | S | Ready |
| CHIT-008 | Cross-driver coverage comparison table in README | P0 | H1 | Docs | S | Ready |
| CHIT-009 | Maintainer playbook + co-maintainer call-for-contributions | P1 | H1 | Docs / GTM | S | Ready |
| CHIT-010 | Adversarial gate-bypass test suite v0 (≥ 20 cases) | P0 | H1 | Security / Engineering | M | Ready |
| CHIT-011 | Telemetry-uplink design doc (opt-in, anonymized) | P1 | H1 | Design / Research | M | Validation/Research |
| CHIT-012 | Policy-pack registry + `install-pack` command | P0 | H2 | Engineering / Product | L | Backlog |
| CHIT-013 | Chitin Dashboard v1 (primary surface) | P0 | H2 | Product / Engineering | L | Backlog |
| CHIT-014 | First external pilot operator | P0 | H2 | GTM | XL | Validation/Research |
| CHIT-015 | Normalizer-author SDK + first 3rd-party driver | P0 | H2 | Engineering / Ecosystem | M | Backlog |
| CHIT-016 | OpenTelemetry SIG submission: `gen_ai.gate.*` | P1 | H2 | Engineering / GTM | M | Backlog |
| CHIT-017 | Chain integrity continuous fuzz in CI | P0 | H2 | Security / Engineering | M | Backlog |
| CHIT-018 | OWASP Top 10 → executable policy pack | P0 | H2 | Engineering / Security | M | Backlog |
| CHIT-019 | Chitin Protocol conformance suite | P1 | H2 | Engineering / Docs | M | Backlog |
| CHIT-020 | Cross-vendor gate comparison matrix (evergreen) | P1 | H2 | Docs / Research | S | Backlog |
| CHIT-021 | Co-maintainer onboarding (3 PRs → merge bit) | P0 | H2 | GTM / Engineering | XL | Validation/Research |
| CHIT-022 | 3-5 reference operators across 4+ drivers | P0 | H3 | GTM | XL | Blocked/Needs decision |
| CHIT-023 | Opt-in policy-intelligence aggregator (beta) | P1 | H3 | Engineering / Product / Privacy | XL | Blocked/Needs decision |
| CHIT-024 | External security audit + threat model publication | P0 | H3 | Security | L | Backlog |
| CHIT-025 | Policy-pack marketplace UX + 10 community packs | P1 | H3 | Product | L | Backlog |
| CHIT-026 | Production-scale benchmark (10M+ events) | P1 | H3 | Engineering | L | Backlog |
| CHIT-027 | Named cross-vendor case study | P0 | H3 | GTM | XL | Blocked/Needs decision |
| CHIT-028 | MCP-native gateway alignment | P1 | H3 | Engineering / Ecosystem | L | Backlog |
| CHIT-029 | Commercial vehicle decision + first design partner | P0 | H3 | Strategy / GTM | M | Blocked/Needs decision |
| CHIT-030 | Demote souls library positioning in operating-model | P2 | H1 | Docs | S | Ready |
| CHIT-031 | AGENTS.md governance section authoring | P2 | H2 | Docs / GTM | S | Backlog |
| CHIT-032 | macOS / Windows install posture decision | P2 | H2 | Strategy / Engineering | S | Validation/Research |
| CHIT-033 | Sigstore-shape chain snapshot attestation | P2 | H2 | Engineering / Security | M | Backlog |

---

## 2. Tickets by priority

### P0 (must-do for the 180-day moat-ceiling outcome)
CHIT-001, CHIT-002, CHIT-003, CHIT-004, CHIT-005, CHIT-006, CHIT-007, CHIT-008, CHIT-010, CHIT-012, CHIT-013, CHIT-014, CHIT-015, CHIT-017, CHIT-018, CHIT-021, CHIT-022, CHIT-024, CHIT-027, CHIT-029.

### P1 (high-value, defers don't kill the strategy)
CHIT-009, CHIT-011, CHIT-016, CHIT-019, CHIT-020, CHIT-023, CHIT-025, CHIT-026, CHIT-028.

### P2 (nice-to-have / hygiene)
CHIT-030, CHIT-031, CHIT-032, CHIT-033.

---

## 3. Tickets by horizon

| Horizon | Tickets |
|---|---|
| **H1 (0-30d)** | CHIT-001, CHIT-002, CHIT-003, CHIT-004, CHIT-005, CHIT-006, CHIT-007, CHIT-008, CHIT-009, CHIT-010, CHIT-011, CHIT-030 |
| **H2 (31-90d)** | CHIT-012, CHIT-013, CHIT-014, CHIT-015, CHIT-016, CHIT-017, CHIT-018, CHIT-019, CHIT-020, CHIT-021, CHIT-031, CHIT-032, CHIT-033 |
| **H3 (91-180d)** | CHIT-022, CHIT-023, CHIT-024, CHIT-025, CHIT-026, CHIT-027, CHIT-028, CHIT-029 |

---

## 4. Tickets by dependency

Direct upstream → downstream edges:

- CHIT-001 → CHIT-002, CHIT-018
- CHIT-004 → CHIT-005, CHIT-012, CHIT-015, CHIT-016, CHIT-019, CHIT-028
- CHIT-005 → CHIT-006, CHIT-014
- CHIT-006 → CHIT-014, CHIT-022
- CHIT-007 → CHIT-018
- CHIT-010 → CHIT-017, CHIT-024
- CHIT-011 → CHIT-023
- CHIT-012 → CHIT-018, CHIT-025
- CHIT-013 → CHIT-014, CHIT-022, CHIT-027
- CHIT-014 → CHIT-022
- CHIT-015 → CHIT-021, CHIT-028
- CHIT-022 → CHIT-023, CHIT-025, CHIT-027, CHIT-029

Tickets with no upstream dependencies (can start today): CHIT-001, CHIT-003, CHIT-004, CHIT-007, CHIT-008, CHIT-009, CHIT-010, CHIT-011, CHIT-020, CHIT-030, CHIT-031, CHIT-032.

---

## 5. Tickets by group

### Backlog (waiting for sequencing)
CHIT-012, CHIT-013, CHIT-015, CHIT-016, CHIT-017, CHIT-018, CHIT-019, CHIT-020, CHIT-024, CHIT-025, CHIT-026, CHIT-028, CHIT-031, CHIT-033.

### Ready (work can begin immediately)
CHIT-001, CHIT-002, CHIT-003, CHIT-004, CHIT-005, CHIT-006, CHIT-007, CHIT-008, CHIT-009, CHIT-010, CHIT-030.

### In Progress candidates (start tomorrow)
CHIT-001, CHIT-004, CHIT-008. (Documentation-led tickets with no engineering dependency.)

### Validation / Research (must validate a hypothesis before committing engineering)
CHIT-011 (design before code), CHIT-014 (need to find a willing operator), CHIT-021 (need a willing contributor), CHIT-032 (need OS-cost data).

### Blocked / Needs decision
CHIT-022 (depends on H2.3 success), CHIT-023 (founder decision on data-policy posture), CHIT-027 (vendor approval), CHIT-029 (founder commercial-vehicle decision).

---

## 6. Full ticket specs

> Field order per ticket: Title • Priority • Type • Objective • Background • Acceptance criteria • Implementation notes • Dependencies • Size • Risk • Owner role • Success metric • Evidence source • Moat impact • Failure mode • Follow-up.

### CHIT-001 — EU AI Act Article 12 mapping doc
- **Priority:** P0 — **Type:** Docs / Research — **Size:** M — **Risk:** Medium
- **Objective:** Produce `docs/compliance/eu-ai-act-article-12.md` that maps each Article 12 obligation to a specific chitin control, citing repo file:line evidence.
- **Background:** EU AI Act high-risk obligations enforce 2026-08-02 with penalties up to €15M / 3% global turnover. Article 12 requires automatic event logging, ≥ 6-month retention, traceability, and tamper-evidence. Chitin's hash-linked chain + daily-partitioned `gov-decisions-YYYY-MM-DD.jsonl` + canonical-JSON SHA-256 + signed-policy CI already satisfy the technical bar.
- **Acceptance criteria:**
  1. Doc enumerates each Article 12 paragraph and points to the chitin control that satisfies it (file path + symbol).
  2. Gaps (if any) are listed explicitly with a remediation plan.
  3. Doc includes a "how to extract a 30-day attestation" recipe.
  4. Doc is linked from README under "Compliance."
  5. Reviewed by one privacy-engineering peer (need not be a lawyer).
- **Implementation notes:** Don't make legal claims; make technical-control claims. Use the language "evidence of compliance with Article 12 obligations" not "Article 12 certified." Cite primary sources (EU AI Act text) directly.
- **Dependencies:** None — start immediately.
- **Owner role:** Founder / principal engineer (with privacy-engineering review).
- **Success metric:** First conformance bundle generates without error; one external citation within 60 days.
- **Evidence source:** `[web H]` helpnetsecurity, raconteur, securityboulevard, dev.to/veritaschain; `[repo H]` `internal/canon`, `internal/chain`.
- **Moat impact:** Compliance (+5), Brand (+3), Switching costs (+2).
- **Failure mode:** Doc reads as legal advice → reframe as technical-control mapping; commission legal review separately if needed.
- **Follow-up:** CHIT-002.

### CHIT-002 — `chitin-kernel compliance article-12 verify` subcommand
- **Priority:** P0 — **Type:** Engineering — **Size:** M — **Risk:** Medium
- **Objective:** New subcommand emitting a signed JSON attestation over a chain window: covered period, event count by type, chain-integrity proof, retention metadata, policy hash, kernel build SHA. Output suitable as "evidence of Article 12 compliance."
- **Background:** The chain already supports this; bundling the projection + signature is operator-facing UX.
- **Acceptance criteria:**
  1. `chitin-kernel compliance article-12 verify --window 30d` exits 0 iff every event in window verifies (`prev_hash` continuity, canonical-hash match).
  2. Emits `~/.chitin/attestations/article-12-YYYYMMDD-YYYYMMDD.json` signed with the policy key (or a dedicated attestation key).
  3. `chitin-kernel compliance article-12 show` pretty-prints the bundle.
  4. Failures (missing event, hash mismatch) produce a structured remediation hint.
  5. Test coverage: golden-file tests on a fixture chain.
- **Implementation notes:** Reuse `internal/chain` verify path; the signature scheme should match the existing signed-policy minisign scheme to avoid key sprawl. Do NOT call this "certified" anywhere in the codebase.
- **Dependencies:** CHIT-001.
- **Owner role:** Kernel engineer.
- **Success metric:** Bundle output verified by re-running `verify` against the emitted JSON.
- **Evidence source:** `[repo H]` `internal/chain`, `internal/canon`, signed-policy CI step in `ci.yml`.
- **Moat impact:** Compliance (+5), Technical (+1), Distribution (+2 via demo path).
- **Failure mode:** Hidden chain-integrity bug surfaces — feed into CHIT-017 fuzz harness as a regression case.
- **Follow-up:** CHIT-018 (consume attestation as an installable policy pack).

### CHIT-003 — Gate-latency + chain-throughput benchmark
- **Priority:** P0 — **Type:** Infra / Engineering — **Size:** S — **Risk:** Low
- **Objective:** Publish `bench/perf/RESULTS.md` with gate-latency p50/p99/p999, chain-emit ops/sec, 1M-event replay wall-clock, and a reproducible `bench/perf/run.sh`. Compare to public Microsoft Toolkit "sub-millisecond" claim.
- **Background:** "Deterministic gating" needs numbers. Microsoft's claim is the floor we must publicly match or beat.
- **Acceptance criteria:**
  1. p50 ≤ 1 ms, p99 ≤ 5 ms gate evaluation against representative `chitin.yaml`.
  2. Chain emit ≥ 10K events/sec sustained.
  3. 1M-event replay ≤ 60 s on a reference machine.
  4. Benchmark script reproducible from a clean checkout with `make bench-perf`.
  5. Results doc linked from README.
- **Implementation notes:** Use `go test -bench` for gate, custom harness for chain emit and replay. Don't fabricate numbers; if they don't hit thresholds, fix root cause or revise the public claim. Run on the dogfood box + an x86-64 cloud box; publish both.
- **Dependencies:** None.
- **Owner role:** Kernel engineer.
- **Success metric:** Benchmark merged and reproducible by a non-author.
- **Evidence source:** `[repo H]` Go kernel; `[web]` Microsoft Toolkit claim.
- **Moat impact:** Technical (+2), Brand (+2).
- **Failure mode:** Numbers are bad → must triage before publishing; that itself is valuable.
- **Follow-up:** CHIT-026 (10M scale).

### CHIT-004 — Chitin Protocol Spec v0.1
- **Priority:** P0 — **Type:** Docs / Engineering — **Size:** M — **Risk:** Medium
- **Objective:** Promote `docs/event-model.md` + the action enum + the gate-decision shape into `docs/spec/chitin-protocol-v0.1.md`: a versioned spec with semver stability promise, change-policy, and conformance-vector references.
- **Background:** Without a spec, the protocol is implementation-defined. A spec anchors community contributions, third-party normalizers, and OpenTelemetry SIG conversation.
- **Acceptance criteria:**
  1. Spec covers: chain envelope v2 (`schema_version=2`), canonical action enum, decision payload, hash-chain rules, OTEL projection mapping.
  2. Semver stability promise: v0.x = breaking-OK with 30-day notice; v1.x = SemVer.
  3. Change policy: RFC PR + 1 maintainer + 1 day open before merge.
  4. Conformance-vector subdirectory referenced (vectors land in CHIT-019).
  5. Spec linked from README as the primary docs entry point.
  6. Trademark research done (separate decision before naming).
- **Implementation notes:** Use IETF-style "MUST / SHOULD / MAY." Reuse existing zod schema as normative reference. State explicitly: chain is canonical; OTEL is projection.
- **Dependencies:** None (the substance is already in `docs/event-model.md`).
- **Owner role:** Founder / principal engineer.
- **Success metric:** Spec published; first external citation within 90 days.
- **Evidence source:** `[repo H]` `docs/event-model.md`, `docs/architecture/layer-contracts.md`, `libs/contracts/`.
- **Moat impact:** Technical (+3), Ecosystem (+3), Brand (+2).
- **Failure mode:** Lock the shape too early → mitigate via v0.x explicit breaking-allowed window.
- **Follow-up:** CHIT-019 (conformance suite), CHIT-016 (OTel SIG submission).

### CHIT-005 — v0.1.0 signed release pipeline
- **Priority:** P0 — **Type:** Infra — **Size:** M — **Risk:** Low
- **Objective:** Cut the first signed release: `linux-x86_64`, `linux-arm64`, `darwin-arm64`, `darwin-x86_64`. Tag `v0.1.0`. Publish to GH Releases with SHA-256 + minisign signatures + checksums file. Optional: a homebrew tap.
- **Background:** Today the install path is `go build`. No external operator will install from source by default.
- **Acceptance criteria:**
  1. `gh release view v0.1.0` returns 4 binaries + checksums + minisign sig + release notes.
  2. `install.sh` one-liner downloads, verifies signature, drops to `~/.local/bin/`.
  3. Release notes summarize since-genesis features + known limitations.
  4. Tag is annotated and signed.
  5. CI workflow `release.yml` builds artifacts on tag push.
- **Implementation notes:** Use `goreleaser` or hand-roll. Sign with the same minisign key family as policy signing (separate sub-key). Document key fingerprint publicly.
- **Dependencies:** CHIT-004 (so the spec version anchors the kernel version).
- **Owner role:** Kernel engineer.
- **Success metric:** One non-author successfully installs via `install.sh` (validates with CHIT-014 pilot).
- **Evidence source:** `[repo H]` `scripts/install-kernel.sh`, package.json scripts.
- **Moat impact:** Distribution (+5), Brand (+2).
- **Failure mode:** Cross-platform build breakage → cross-compile via Go toolchain, test in CI.
- **Follow-up:** Homebrew tap, AUR, nix flake (P2 each).

### CHIT-006 — Landing page + thesis blog post
- **Priority:** P0 — **Type:** GTM / Docs — **Size:** M — **Risk:** Low
- **Objective:** Publish a static landing page (GH Pages at `chitinhq.github.io` or `chitin.dev`) summarizing the thesis + a single 1500-word blog post: "Chitin: cross-driver execution governance for AI coding agents."
- **Background:** No public surface exists today. The "governance toolkit" and "EU AI Act" narratives are crowding the channel; chitin needs an entry point.
- **Acceptance criteria:**
  1. Landing page: hero + 3 differentiators (cross-driver, hash-linked chain, local-only) + install command + GH link.
  2. Blog post: covers thesis, six drivers, Article 12 mapping, and how to try chitin in 5 minutes.
  3. Page has structured-data tags for SEO (OpenGraph, schema.org SoftwareApplication).
  4. Loads in < 1 s; works without JS.
- **Implementation notes:** Keep it static. No tracking pixels (consistent with the privacy posture). Use the same `docs/` markdown via mdBook or Hugo if natural; otherwise hand-roll.
- **Dependencies:** CHIT-005 (so the page has a real install command), CHIT-001 (so the compliance line is real).
- **Owner role:** Founder.
- **Success metric:** 100 unique IPs in first 30 days; Google indexing within 14 days.
- **Evidence source:** `[repo H]` `README.md`, `docs/thesis.md`; `[web H]` crowded category.
- **Moat impact:** Brand (+4), Distribution (+4).
- **Failure mode:** Premature commercial framing → keep the page OSS-neutral; no "Talk to Sales" buttons.
- **Follow-up:** CHIT-014 (use page as pilot-recruiting CTA).

### CHIT-007 — OWASP Agentic-AI Top 10 mapping doc
- **Priority:** P0 — **Type:** Docs / Security — **Size:** S — **Risk:** Low
- **Objective:** `docs/compliance/owasp-agentic-top10.md` mapping each OWASP Agentic-AI Top 10 risk to a chitin control or a documented gap.
- **Background:** Microsoft Agent Governance Toolkit's anchor narrative is OWASP coverage. Chitin must publish its mapping to be in the same conversation.
- **Acceptance criteria:**
  1. Each of the 10 risks has: description / chitin control / gap (if any) / mitigation status.
  2. Doc linked from README under "Compliance."
  3. At least 2 mapped controls have an associated test in `bench/` or `internal/gov/`.
- **Dependencies:** None.
- **Owner role:** Founder + security-minded reviewer.
- **Success metric:** Doc merged; cited once externally within 60 days.
- **Evidence source:** `[web H]` Microsoft Toolkit claim; OWASP Agentic-AI Top 10.
- **Moat impact:** Compliance (+3), Brand (+2).
- **Failure mode:** Map is overly aspirational → state gaps honestly.
- **Follow-up:** CHIT-018 (turn into an installable policy pack).

### CHIT-008 — Cross-driver coverage comparison table in README
- **Priority:** P0 — **Type:** Docs — **Size:** S — **Risk:** Low
- **Objective:** Add a "Chitin vs native vendor gates" table to README: rows are Claude Code, Codex, Gemini, Copilot, OpenClaw, Hermes; columns are native vendor gate status / chitin gate status / cross-vendor canonical action.
- **Background:** [RFC #45427](https://github.com/anthropics/claude-code/issues/45427) makes "native deterministic gate" a live narrative. Chitin's cross-vendor scope is the differentiated answer.
- **Acceptance criteria:**
  1. Table merged in README above the "Moat" section.
  2. Each row cites the live driver normalizer.
  3. A footnote tracks the date of last verification against vendor surface.
- **Dependencies:** None.
- **Owner role:** Founder.
- **Success metric:** Cited in CHIT-006 blog post.
- **Evidence source:** `[repo H]` `docs/driver-conformance.md`, `internal/driver/*/normalize.go`.
- **Moat impact:** Brand (+2), Positioning (+3).
- **Failure mode:** Table goes stale → cron-style reminder in `docs/driver-conformance.md` to re-verify quarterly.
- **Follow-up:** CHIT-020 (evergreen comparison matrix).

### CHIT-009 — Maintainer playbook + co-maintainer call
- **Priority:** P1 — **Type:** Docs / GTM — **Size:** S — **Risk:** Low
- **Objective:** Publish `docs/maintaining.md` covering the kernel's invariants, the audit-driven cull pattern, the layer contracts, and the merge bar. Open `chitinhq/chitin#XX` titled "Wanted: co-maintainer with kernel-engineering experience."
- **Background:** Bus factor of 1 is the load-bearing risk on every audit.
- **Acceptance criteria:**
  1. Doc covers: build, test, release, signing, ADR process.
  2. Issue lists the merge bar (CI green + 1 reviewer + invariant check).
  3. Linked from README.
- **Dependencies:** None.
- **Owner role:** Founder.
- **Success metric:** ≥ 1 substantive inbound by D90.
- **Evidence source:** `[repo H]` single-author signals; `[inference H]` from velocity.
- **Moat impact:** Execution velocity (+2), Brand (+1).
- **Failure mode:** No takers → re-evaluate funding / second-maintainer compensation strategy.
- **Follow-up:** CHIT-021.

### CHIT-010 — Adversarial gate-bypass test suite v0
- **Priority:** P0 — **Type:** Security / Engineering — **Size:** M — **Risk:** Medium
- **Objective:** Add at least 20 known shell + normalizer bypass attempts as `*_test.go` cases that assert the gate denies them. Examples: UTF-8 bidi tricks, `$IFS` and `$'\t'` argv assembly, here-doc concatenation, subshell-chain escapes, `python -c "import os; os.system('rm -rf …')"` patterns, base64-decoded payloads piped to bash, env-var-rewritten commands, glob-expansion abuse, here-string trickery, `eval $(curl …)`, prefix-bypass to `git config`, alias redefinition, encoded path traversal, symlink-redirect, `sudo`-emulation via privilege re-acquisition tools.
- **Background:** The kernel's strongest public claim is "deterministic gate." A negative-test suite that fails on any allowed-bypass is the strongest defense against regression and the strongest evidence for buyers/auditors.
- **Acceptance criteria:**
  1. ≥ 20 cases added under `go/execution-kernel/internal/gov/bypass_test.go` (or equivalent location).
  2. Each case includes: pattern name, raw input, expected decision, citation if from a real CVE / blog.
  3. All currently pass.
  4. CI fails on any regression.
  5. Doc in `docs/security/adversarial-tests.md` indexes the case set with citations.
- **Implementation notes:** Some cases will probably fail today — that's the point. Triage: fix kernel for true bypasses; mark documented intentional-allow-with-warning for edge cases.
- **Dependencies:** None.
- **Owner role:** Kernel engineer with security mindset.
- **Success metric:** All 20 cases green; ≥ 3 real bugs caught and fixed in the process.
- **Evidence source:** `[repo H]` `gov.Gate`, `chitin.yaml` rules (e.g. `no-destructive-rm-via-execute-code` already in `chitin.yaml`); `[web]` shell-bypass literature, OWASP Top 10.
- **Moat impact:** Technical (+3), Defensibility (+3), Compliance (+2).
- **Failure mode:** Some cases pass that shouldn't → fix root cause, file a CVE-style advisory if pre-release.
- **Follow-up:** CHIT-017 (continuous fuzz), CHIT-024 (external audit).

### CHIT-011 — Telemetry-uplink design doc (opt-in, anonymized)
- **Priority:** P1 — **Type:** Design / Research — **Size:** M — **Risk:** Low (design only)
- **Objective:** `docs/design/policy-intelligence-uplink.md` — schema, opt-in flow, consent UX, anonymization, retention, deletion. **No code in this ticket.**
- **Background:** Local-only is a moat. Some operators will opt in to anonymized policy intelligence in exchange for cross-operator insights. The privacy contract must be designed before any code is written.
- **Acceptance criteria:**
  1. Doc covers what is uploaded (counts/histograms, no payloads, no paths, no command strings) and what is not.
  2. Default = off. Per-event consent unrealistic; consent at install + revocation via a single flag.
  3. Endpoint, key rotation, deletion request flow defined.
  4. Privacy threat model addresses: re-identification, network operator inspection, server compromise.
  5. Reviewed by one privacy-engineering peer.
- **Dependencies:** None.
- **Owner role:** Founder + privacy reviewer.
- **Success metric:** Doc merged; one external privacy-engineering review.
- **Evidence source:** `[inference H]` chitin local-only thesis; `[web H]` data-moat patterns.
- **Moat impact:** Data network effects (+3 if eventually implemented; design alone +0 but unblocks H3).
- **Failure mode:** Privacy review finds blocker → revise; better than discovering it after launch.
- **Follow-up:** CHIT-023.

### CHIT-012 — Policy-pack registry + `install-pack` command
- **Priority:** P0 — **Type:** Engineering / Product — **Size:** L — **Risk:** Medium
- **Objective:** A typed pack bundle format + a hosted registry index (static JSON over GH Pages) + `chitin-kernel install-pack <name>` that downloads, verifies signature, merges into `chitin.yaml` via an explicit `imports:` block.
- **Background:** Policy packs are the compounding primitive — every operator's policy investment becomes shareable. `examples/policy-packs/` exists already, but the registry + install command turn it into an ecosystem.
- **Acceptance criteria:**
  1. Pack format documented: `pack.yaml` with `id, version, rules, bounds_overrides, signature, depends_on`.
  2. Registry index at `https://chitinhq.github.io/policy-packs/index.json` lists 3+ packs (start with EU AI Act starter, OWASP starter, "no-net-egress").
  3. `install-pack`: downloads, verifies minisign signature against a chitin-trusted-keys file, dry-runs against current `chitin.yaml`, prompts before applying.
  4. `uninstall-pack` reverses cleanly.
  5. Conformance tests assert the registry's signed packs pass policy validation.
- **Implementation notes:** Use minisign keys; ship the trusted-keys-list in the binary; allow operator override. Keep the registry as a static GH Pages site for simplicity.
- **Dependencies:** CHIT-004 (spec stability for pack schema).
- **Owner role:** Kernel engineer + designer.
- **Success metric:** Operators install 1+ pack each from registry; ≥ 1 third-party PR-contributed pack within 90 days.
- **Evidence source:** `[repo H]` `examples/policy-packs/`; `[inference H]` compounding primitive logic.
- **Moat impact:** Switching costs (+3), Ecosystem (+3), Brand (+2).
- **Failure mode:** Pack-quality control becomes a problem → add a vetted-vs-community badge.
- **Follow-up:** CHIT-025.

### CHIT-013 — Chitin Dashboard v1 (primary surface)
- **Priority:** P0 — **Type:** Product / Engineering — **Size:** L — **Risk:** Medium
- **Objective:** Promote `apps/chitin-dashboard/` from MVP to default surface. Panels: per-driver decisions, severity-counter heatmap, chain inspector (filter / pivot / replay-single-event), OTEL projection status, attestation viewer (CHIT-002 output).
- **Background:** A kernel without a console is invisible to non-engineer buyers. Today's dashboard is a built-into-`dist` artifact mostly verified via CI checking the dist matches source.
- **Acceptance criteria:**
  1. Runs against live `~/.chitin/` state via the kernel's read CLI surface — never reaches into `gov.db` directly.
  2. All 6 drivers display real data.
  3. Chain inspector handles 1M-event chains without UI freeze (virtualized list).
  4. Attestation viewer surfaces validate-pass/fail.
  5. Bundled with `chitin-kernel` releases as a static asset.
- **Implementation notes:** Single static SPA; no auth; local-only. The dashboard never writes — read-only contract. The data API is the existing CLI subcommands shelled out from a thin local server.
- **Dependencies:** CHIT-005 (so the dashboard ships with the release), CHIT-002 (attestation viewer).
- **Owner role:** Front-end engineer or founder.
- **Success metric:** Used as the demo surface in CHIT-006 blog and CHIT-027 case study.
- **Evidence source:** `[repo M]` `apps/chitin-dashboard/`.
- **Moat impact:** Product completeness (+4), Brand (+3).
- **Failure mode:** Scope creep into multi-tenant SaaS → hold the local-only line.
- **Follow-up:** CHIT-027 demo.

### CHIT-014 — First external pilot operator
- **Priority:** P0 — **Type:** GTM — **Size:** XL — **Risk:** Medium
- **Objective:** Recruit one credibly-named external operator. Ship them through: install, policy authoring, first denial incident, first replay, first compliance bundle.
- **Background:** The single biggest score-mover for the moat. Until one external operator runs chitin in anger, every technical claim is implementation-defined.
- **Acceptance criteria:**
  1. Operator runs chitin for ≥ 14 continuous days with ≥ 2 drivers gated.
  2. Operator publishes (or co-authors) at least a short writeup / testimonial.
  3. Founder captures all friction points in `docs/observations/pilot-1.md` and feeds back into roadmap.
  4. Operator's chain hits ≥ 10K events.
- **Implementation notes:** Pilot operator should be technical and patient. Source from author's network or from inbound after CHIT-006 lands.
- **Dependencies:** CHIT-001 (compliance hook), CHIT-005 (install path), CHIT-006 (landing page).
- **Owner role:** Founder.
- **Success metric:** Public testimonial; ≥ 3 specific feedback items.
- **Evidence source:** None today `[repo H]`.
- **Moat impact:** Brand (+5), Switching costs (+4), Distribution (+3).
- **Failure mode:** Pilot abandons → triage friction and recruit another.
- **Follow-up:** CHIT-022.

### CHIT-015 — Normalizer-author SDK + first third-party driver
- **Priority:** P0 — **Type:** Engineering / Ecosystem — **Size:** M — **Risk:** Medium
- **Objective:** Publish a typed Go interface + harness for writing `normalize.go`; partner with a community member or fork-author of a popular coding CLI (Cursor, Continue, Aider, Warp) to ship one new normalizer.
- **Background:** Chitin's six-driver coverage matters because it spans vendors. The category is fragmenting (30+ tools tracked publicly); chitin should be a substrate for new-driver work, not its only author.
- **Acceptance criteria:**
  1. `internal/driver/sdk/` package with the interface contract.
  2. Doc `docs/developing/normalizer-author-guide.md`.
  3. Conformance-vector pack lets a new normalizer run against known inputs and produce expected canonical outputs.
  4. One driver normalizer PR'd from outside the operator's network.
- **Dependencies:** CHIT-004 (spec stability).
- **Owner role:** Kernel engineer + community-relations.
- **Success metric:** Third-party PR merged within 90 days of SDK publication.
- **Evidence source:** `[repo H]` `internal/driver/`; `[web H]` CLI proliferation.
- **Moat impact:** Ecosystem (+4), Distribution (+2).
- **Failure mode:** Nobody PRs → recruit one specifically (treat as paid task if necessary).
- **Follow-up:** CHIT-021, CHIT-028.

### CHIT-016 — OpenTelemetry SIG submission: `gen_ai.gate.*`
- **Priority:** P1 — **Type:** Engineering / GTM — **Size:** M — **Risk:** Medium
- **Objective:** Submit chitin's gate-decision attributes to the OTel `gen_ai` SIG for inclusion in semantic conventions.
- **Background:** Anchors chitin's chain shape in an industry standard. OTel `gen_ai.*` is the right vehicle; the work doubles as positioning.
- **Acceptance criteria:**
  1. Issue / PR opened in `open-telemetry/semantic-conventions`.
  2. Proposal includes: `gate.decision`, `gate.reason`, `gate.rule_id`, `action.type`, `action.target`.
  3. One SIG review comment received.
- **Dependencies:** CHIT-004.
- **Owner role:** Founder.
- **Success metric:** Adopted (eventually) or formally tracked as candidate.
- **Evidence source:** `[repo H]` `internal/emit/otel.go`; `[web H]` OTel SIG processes.
- **Moat impact:** Ecosystem (+3), Brand (+2), Compliance (+2).
- **Failure mode:** SIG declines → reverts to chitin-proprietary attributes, no harm done.
- **Follow-up:** Continue engagement until adopted.

### CHIT-017 — Chain integrity continuous fuzz in CI
- **Priority:** P0 — **Type:** Security / Engineering — **Size:** M — **Risk:** Low
- **Objective:** Property-based fuzz that generates synthetic event chains, tampers (mutate, reorder, drop, replay), and asserts the kernel rejects.
- **Background:** "Tamper-evident" is foundational. Continuous evidence > one-time test.
- **Acceptance criteria:**
  1. Fuzz runs nightly in CI for ≥ 30 min wall-clock.
  2. ≥ 100K cases generated per run.
  3. Zero false-accepts in 30 days; any regression fails CI.
  4. Corpus checked in under `bench/integrity-corpus/`.
- **Implementation notes:** Use `testing.F` (Go 1.18+ native fuzz). Persist failing seeds.
- **Dependencies:** CHIT-010 (clean baseline first).
- **Owner role:** Kernel engineer.
- **Success metric:** Fuzz green continuously.
- **Evidence source:** `[repo H]` `canon`, `chain`.
- **Moat impact:** Technical (+2), Defensibility (+3), Compliance (+3).
- **Failure mode:** False positive bugs → triage and either fix or document.
- **Follow-up:** CHIT-024.

### CHIT-018 — OWASP Top 10 → executable policy pack
- **Priority:** P0 — **Type:** Engineering / Security — **Size:** M — **Risk:** Low
- **Objective:** Convert CHIT-007 doc into an installable `owasp-agentic-top10.pack.yaml` enforcing concrete rules per row. Demonstrate dry-run on fixture chain.
- **Background:** Make the OWASP claim executable, not paper.
- **Acceptance criteria:**
  1. Pack ships in CHIT-012 registry.
  2. Each enabled rule maps to one OWASP risk row.
  3. `chitin-kernel install-pack owasp-agentic-top10` + dry-run produces expected decisions on fixture chain.
  4. Coverage of risks is tracked: enforce / guide / monitor / not-applicable.
- **Dependencies:** CHIT-007, CHIT-012.
- **Owner role:** Security-minded engineer.
- **Success metric:** Pack installed by ≥ 5 operators within 90 days of release.
- **Evidence source:** OWASP Agentic-AI Top 10; `[repo H]` rule shape.
- **Moat impact:** Compliance (+3), Switching costs (+2).
- **Failure mode:** Over-enforcement breaks normal operator workflow → use `guide` mode by default, `enforce` mode opt-in.
- **Follow-up:** CHIT-025.

### CHIT-019 — Chitin Protocol conformance suite
- **Priority:** P1 — **Type:** Engineering / Docs — **Size:** M — **Risk:** Low
- **Objective:** A test-vector pack a third party can run to claim "chitin-protocol compatible." Covers canonical-JSON hashing, chain `prev_hash` chaining, action-enum mapping, decision payload shape.
- **Background:** Spec without conformance suite is documentation, not a standard.
- **Acceptance criteria:**
  1. Vectors under `spec/conformance-vectors/`.
  2. `chitin-kernel spec-conformance run` passes 100%.
  3. README provides instructions for third-party implementers.
  4. One out-of-tree repo passes the suite (the third-party driver from CHIT-015 should).
- **Dependencies:** CHIT-004.
- **Owner role:** Kernel engineer.
- **Success metric:** Cited in CHIT-016 OTel SIG proposal.
- **Evidence source:** `[repo H]` `internal/canon`, `internal/chain`.
- **Moat impact:** Ecosystem (+2), Technical (+2), Distribution (+2).
- **Failure mode:** Vectors are too brittle → version them; allow tolerable normalization.
- **Follow-up:** CHIT-016, CHIT-015.

### CHIT-020 — Cross-vendor gate comparison matrix (evergreen)
- **Priority:** P1 — **Type:** Docs / Research — **Size:** S — **Risk:** Low
- **Objective:** Living doc + landing-page section comparing chitin coverage vs Anthropic / OpenAI / Google / Microsoft / GitHub native gates as they ship.
- **Background:** RFC #45427 will resolve one way or the other. Other vendors will follow. Chitin needs an evergreen answer.
- **Acceptance criteria:**
  1. Doc updated monthly with date of last check.
  2. Each row cites the vendor's documentation page.
  3. Cross-linked from README and landing page.
- **Dependencies:** CHIT-008 (as the seed table).
- **Owner role:** Founder.
- **Success metric:** Updated 6 times in 6 months.
- **Evidence source:** `[web H]` vendor docs.
- **Moat impact:** Brand (+2), Positioning (+2).
- **Failure mode:** Falls behind → set a calendar reminder.
- **Follow-up:** None.

### CHIT-021 — Co-maintainer onboarding (3 PRs → merge bit)
- **Priority:** P0 — **Type:** GTM / Engineering — **Size:** XL — **Risk:** Medium
- **Objective:** Mentor one external contributor through 3 substantive PRs (a driver normalizer, a policy pack, a dashboard panel). Offer merge bit if quality + judgment criteria met.
- **Background:** Bus factor of 1 is the load-bearing risk.
- **Acceptance criteria:**
  1. 3 PRs merged from one contributor.
  2. Contributor demonstrates: invariant respect, audit-driven cull pattern, layer-contract awareness.
  3. Merge bit granted (after contributor agrees to maintenance expectations).
- **Dependencies:** CHIT-015 (SDK gives the first PR's surface), CHIT-009 (playbook).
- **Owner role:** Founder.
- **Success metric:** Merge bit granted.
- **Evidence source:** `[repo H]`.
- **Moat impact:** Execution velocity (+3), Brand (+2).
- **Failure mode:** No contributor materializes → revisit funding/compensation strategy.
- **Follow-up:** Continued mentorship; team-of-2 goals.

### CHIT-022 — 3-5 reference operators across 4+ drivers
- **Priority:** P0 — **Type:** GTM — **Size:** XL — **Risk:** Medium
- **Objective:** Recruit 2-4 more external operators after CHIT-014's pilot. Each runs ≥ 4 drivers gated; chain ≥ 100K events; ideally one or more publish a writeup.
- **Background:** Switching costs only materialize after sustained external use across multiple operators.
- **Acceptance criteria:**
  1. ≥ 3 named external operators active.
  2. Aggregate chain volume ≥ 500K events.
  3. ≥ 1 case-study-grade testimonial.
- **Dependencies:** CHIT-014, CHIT-006, CHIT-013.
- **Owner role:** Founder.
- **Success metric:** 3 operators publicly listed.
- **Evidence source:** `[inference H]`.
- **Moat impact:** Switching costs (+3), Brand (+3), Distribution (+2).
- **Failure mode:** No recruits → reassess pitch / positioning.
- **Follow-up:** CHIT-027.

### CHIT-023 — Opt-in policy-intelligence aggregator (beta)
- **Priority:** P1 — **Type:** Engineering / Product / Privacy — **Size:** XL — **Risk:** High
- **Objective:** Implement CHIT-011's design behind explicit opt-in. Anonymized policy-decision counts + denied-action histograms uploaded to a single endpoint; aggregator emits cross-operator visualizations.
- **Background:** Without cross-operator data, policy-pack quality plateaus. This is the wedge for an eventual commercial path.
- **Acceptance criteria:**
  1. Default off. Operator runs explicit `chitin-kernel intel opt-in` with prompt confirming privacy contract.
  2. Schema: counts/histograms only — no payloads, no paths, no commands, no machine IDs.
  3. Server-side: WORM-immutable storage; SOC2 Lite controls.
  4. Per-operator delete-my-data flow.
  5. Aggregator dashboard renders cross-operator counters for opt-in operators only.
  6. Privacy audit completed by an outside reviewer.
- **Implementation notes:** Use the same minisign/key infrastructure for identifying opt-in operators without identifying their humans. Server can be tiny — a single Cloud Run / Fly.io endpoint.
- **Dependencies:** CHIT-011, CHIT-022.
- **Owner role:** Founder + privacy reviewer.
- **Success metric:** ≥ 3 operators opt in.
- **Evidence source:** `[inference H]`; CHIT-011 design doc.
- **Moat impact:** Data network effects (+3), Brand (+2 or -3 depending on execution).
- **Failure mode:** Privacy mistake → high reputation cost; the threat-model + audit is non-negotiable.
- **Follow-up:** Reaches into commercial path (CHIT-029).

### CHIT-024 — External security audit + threat model publication
- **Priority:** P0 — **Type:** Security — **Size:** L — **Risk:** Medium
- **Objective:** Commission a paid external security audit of the kernel; publish the threat model and findings (after remediation).
- **Background:** The "deterministic gate" claim needs external attestation. The audit is the brand-positive moment.
- **Acceptance criteria:**
  1. Audit completed by a recognizable security firm.
  2. Threat model published in `docs/security/threat-model.md`.
  3. Findings tracked to remediation, status published.
- **Implementation notes:** Budget $15-40K. Trail of Bits or NCC Group or boutique like Doyensec.
- **Dependencies:** CHIT-010, CHIT-017.
- **Owner role:** Founder.
- **Success metric:** Audit completed; remediation rate ≥ 80% on high/critical.
- **Evidence source:** `[inference H]`.
- **Moat impact:** Defensibility (+5), Brand (+3), Technical (+1).
- **Failure mode:** Audit finds critical issue → triage and disclose responsibly.
- **Follow-up:** Cite in CHIT-027.

### CHIT-025 — Policy-pack marketplace UX + 10 community packs
- **Priority:** P1 — **Type:** Product — **Size:** L — **Risk:** Medium
- **Objective:** Polish CHIT-012's registry into a marketplace UX: list, install, fork, contribute, signed-pack badges, popularity counts. Recruit 10 packs from ≥ 5 contributors.
- **Background:** Compounding economics — each pack multiplies operator value.
- **Acceptance criteria:**
  1. Marketplace site lists ≥ 10 packs from ≥ 5 distinct contributors.
  2. Vetted / community / experimental badges.
  3. Pack download counts (privacy-respecting).
  4. Featured "starter packs" curated.
- **Dependencies:** CHIT-012, CHIT-022.
- **Owner role:** Front-end engineer + community manager.
- **Success metric:** 10 packs by D180.
- **Evidence source:** `[inference H]`.
- **Moat impact:** Network (+3), Ecosystem (+3).
- **Failure mode:** Supply problem → seed with paid contractor packs.
- **Follow-up:** None — ongoing.

### CHIT-026 — Production-scale benchmark (10M+ events)
- **Priority:** P1 — **Type:** Engineering — **Size:** L — **Risk:** Medium
- **Objective:** Move benchmark from synthetic to realistic scale. 10M-event chain with realistic distribution of decision/event types. Multi-driver concurrent emit. Publish numbers + scaling envelope.
- **Background:** Current dogfood is single-box, low volume. Forecloses "doesn't scale" objections from buyers.
- **Acceptance criteria:**
  1. 10M-event run completes; replay ≤ 10 min.
  2. SQLite tuning notes published.
  3. Tail-latency at 10M-row index ≤ 10× of 1M baseline.
  4. Concurrent-driver emit (6 drivers, 1K req/s aggregate) passes without WAL contention.
- **Dependencies:** CHIT-003.
- **Owner role:** Kernel engineer.
- **Success metric:** Numbers cited in CHIT-027 case study.
- **Evidence source:** `[repo M]` current `gov.db` WAL story.
- **Moat impact:** Scalability (+3), Technical (+2).
- **Failure mode:** Scaling cliff hit → published honestly with a fix plan.
- **Follow-up:** Address scaling fixes as separate tickets.

### CHIT-027 — Named cross-vendor case study
- **Priority:** P0 — **Type:** GTM — **Size:** XL — **Risk:** High
- **Objective:** One published case study where chitin gates Claude Code + Codex + Gemini in the same workflow at a named operator, ideally with a vendor blog cross-post.
- **Background:** Most repeatable distribution beat; converts technical evidence into brand.
- **Acceptance criteria:**
  1. Case study published with operator name + workflow.
  2. Cross-vendor coverage demonstrated.
  3. Cross-posted on at least one vendor blog (Anthropic, OpenAI, Google, GitHub) or industry blog (e.g. ClickHouse for Langfuse-class).
- **Dependencies:** CHIT-022, CHIT-013.
- **Owner role:** Founder.
- **Success metric:** Publication; SEO + recruiting tailwind.
- **Evidence source:** `[inference H]`.
- **Moat impact:** Brand (+5), Distribution (+4).
- **Failure mode:** Vendor blog declines → self-publish + cite operator approval.
- **Follow-up:** Series of case studies.

### CHIT-028 — MCP-native gateway alignment
- **Priority:** P1 — **Type:** Engineering / Ecosystem — **Size:** L — **Risk:** Medium
- **Objective:** Add MCP-first observation and policy paths so chitin can sit in front of MCP gateways (not only behind parent drivers).
- **Background:** MCP is the consolidating contract surface. Failing to align cedes that channel as it grows.
- **Acceptance criteria:**
  1. MCP gateway normalizer in `internal/driver/mcp/`.
  2. One MCP server demoed under chitin.
  3. Documentation: how to wire chitin in front of an MCP server vs behind a parent driver.
- **Dependencies:** CHIT-004, CHIT-016.
- **Owner role:** Kernel engineer.
- **Success metric:** First MCP-only operator pilot.
- **Evidence source:** `[web H]` MCP momentum.
- **Moat impact:** Workflow (+3), Ecosystem (+3), Technical (+1).
- **Failure mode:** MCP spec churns → version against a specific MCP spec rev.
- **Follow-up:** Track MCP spec evolution.

### CHIT-029 — Commercial vehicle decision + first design partner
- **Priority:** P0 — **Type:** Strategy / GTM — **Size:** M — **Risk:** High
- **Objective:** Founder publishes a decision: open-core (chitin OSS + chitin-cloud aggregator) vs paid-support contract vs policy-pack marketplace vs pure-OSS-no-revenue. Acquire first design partner if commercial path chosen.
- **Background:** Business model can no longer stay undeclared at month 4-6; it gates hiring + capital + recruiting + GTM.
- **Acceptance criteria:**
  1. Public commercial-option page published.
  2. If commercial: ≥ 1 design partner contracted.
  3. If pure-OSS: clear "no commercial vehicle planned" statement so operators don't anticipate.
- **Implementation notes:** The healthy default for chitin's posture is open-core: chitin OSS + chitin-cloud aggregator (CHIT-023). Support contract is a fallback; marketplace is downstream of CHIT-025.
- **Dependencies:** CHIT-022, CHIT-023.
- **Owner role:** Founder.
- **Success metric:** Decision published; recruiting and capital convos unlocked.
- **Evidence source:** `[inference H]`.
- **Moat impact:** Business model (+4), Brand (+2 or -2 depending on choice).
- **Failure mode:** Wrong choice → reversible (with cost).
- **Follow-up:** Whatever the choice implies.

### CHIT-030 — Demote souls library positioning in operating-model
- **Priority:** P2 — **Type:** Docs — **Size:** S — **Risk:** Low
- **Objective:** Reconcile the souls-library positioning. CLAUDE.md surfaces it as a major construct; `docs/operating-model.md` calls it "historical analytics/reference artifact; not a kernel runtime surface." Pick one stance; demote everywhere else.
- **Background:** External readers will be confused.
- **Acceptance criteria:**
  1. Single canonical stance documented.
  2. Other docs aligned (or cross-linked to canonical).
- **Dependencies:** None.
- **Owner role:** Founder.
- **Success metric:** Documentation consistency.
- **Evidence source:** `[repo H]` `docs/operating-model.md`, `CLAUDE.md`.
- **Moat impact:** Brand (+1).
- **Failure mode:** None.
- **Follow-up:** None.

### CHIT-031 — AGENTS.md governance section authoring
- **Priority:** P2 — **Type:** Docs / GTM — **Size:** S — **Risk:** Low
- **Objective:** Author an AGENTS.md "Governance" section template that operators of AGENTS.md-aware CLIs (Codex, Claude Code, Cursor, Copilot, Gemini, etc.) can drop in to reference chitin for runtime enforcement.
- **Background:** AGENTS.md is becoming the cross-tool instruction standard `[web]`. Aligning is positioning gold.
- **Acceptance criteria:**
  1. Template doc published.
  2. PR contributed to Linux Foundation Agentic-AI Foundation reference repos where applicable.
- **Dependencies:** None.
- **Owner role:** Founder.
- **Success metric:** Adoption signal — ≥ 1 third-party AGENTS.md referencing chitin.
- **Evidence source:** `[web H]`.
- **Moat impact:** Ecosystem (+2), Brand (+1).
- **Failure mode:** Foundation declines → self-host the template.
- **Follow-up:** None.

### CHIT-032 — macOS / Windows install posture decision
- **Priority:** P2 — **Type:** Strategy / Engineering — **Size:** S — **Risk:** Low
- **Objective:** Decide and document the macOS + Windows posture. Today everything assumes Linux (bubblewrap, systemd, `~/.chitin/`).
- **Background:** A meaningful share of developer-machine operators are on macOS. Windows usage in regulated industries is non-trivial.
- **Acceptance criteria:**
  1. Decision documented: "macOS supported for kernel + chain (no systemd, no bubblewrap); Windows is best-effort via WSL2; native Windows on roadmap with no commitment."
  2. README + thesis updated.
- **Dependencies:** None.
- **Owner role:** Founder.
- **Success metric:** Decision documented.
- **Evidence source:** `[repo H]` `infra/systemd/`, plugin sandbox via bubblewrap.
- **Moat impact:** Distribution (+1).
- **Failure mode:** None.
- **Follow-up:** macOS-native plugin sandbox investigation (P3).

### CHIT-033 — Sigstore-shape chain snapshot attestation
- **Priority:** P2 — **Type:** Engineering / Security — **Size:** M — **Risk:** Medium
- **Objective:** Promote the `chain snapshot` subcommand to produce sigstore-compatible attestations (in-toto predicates + signature) so chitin attestations can be consumed by SLSA-aware tooling.
- **Background:** `chain snapshot` already emits a hash-linked session export; aligning with sigstore shape unlocks supply-chain governance use cases.
- **Acceptance criteria:**
  1. Snapshot output is a valid in-toto attestation with `predicateType: chitin-protocol/chain-snapshot/v0.1`.
  2. Verifiable with `cosign verify` against the chitin signing key.
  3. Doc cross-links to SLSA + sigstore.
- **Dependencies:** CHIT-002 (shared signing pattern), CHIT-004 (predicate type stability).
- **Owner role:** Kernel engineer.
- **Success metric:** Snapshot consumable by one CI tool unaware of chitin.
- **Evidence source:** `[repo H]` `chain snapshot`; `[web]` sigstore / in-toto specs.
- **Moat impact:** Compliance (+2), Ecosystem (+2).
- **Failure mode:** None significant.
- **Follow-up:** SLSA L2/L3 conformance.

---

## 7. Suggested execution order

**Week 1-2 (start tomorrow, parallel-safe):**
- CHIT-001 (compliance doc) — founder
- CHIT-004 (spec) — founder
- CHIT-008 (README table) — founder
- CHIT-030 (souls demote) — founder

**Week 2-3:**
- CHIT-002 (CLI subcommand) — kernel
- CHIT-003 (benchmark) — kernel
- CHIT-007 (OWASP doc) — founder
- CHIT-009 (maintainer playbook) — founder

**Week 3-4:**
- CHIT-005 (release pipeline) — kernel
- CHIT-006 (landing page + blog) — founder
- CHIT-010 (adversarial suite) — kernel
- CHIT-011 (telemetry design doc) — founder

**Month 2 (parallel with H2 sequencing):**
- CHIT-012 (pack registry) — engineer
- CHIT-013 (dashboard) — engineer
- CHIT-017 (fuzz CI) — kernel
- CHIT-014 (pilot recruiting) — founder
- CHIT-018 (OWASP pack) — security engineer

**Month 3:**
- CHIT-015 (normalizer SDK)
- CHIT-016 (OTel SIG submission)
- CHIT-019 (conformance suite)
- CHIT-020 (cross-vendor matrix)
- CHIT-021 (co-maintainer onboarding) — kicks off

**Months 4-6 (H3):**
- CHIT-022 (3-5 ops)
- CHIT-023 (intel aggregator beta)
- CHIT-024 (audit)
- CHIT-025 (marketplace UX)
- CHIT-026 (10M bench)
- CHIT-027 (case study)
- CHIT-028 (MCP align)
- CHIT-029 (commercial decision)

Hygiene tickets (CHIT-031, 032, 033) slotted into engineer downtime.

---

## 8. Definition of done — universal

A ticket is "done" only when:
1. PR is merged and CI is green.
2. The acceptance criteria are independently verifiable from the merged code.
3. Any doc changes are linked from README (or another canonical entry point).
4. The success metric is measurable from public surface (where applicable).
5. Follow-up tickets, if any, are filed.

A ticket is "in review" — not "done" — at PR-open.
A ticket without acceptance criteria is **not ready** and must be groomed first.
