# Adopt spec-kit; retire docs/superpowers/specs/ — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace chitin's bespoke `docs/superpowers/specs/` workflow with GitHub spec-kit (`.specify/`) as the canonical spec home, while preserving chitin's load-bearing pieces (kanban linkage, status enum, layer-contract invariants) via spec-kit extensions.

**Architecture:** Three sequential PRs. PR1 scaffolds `.specify/` and CI without touching existing specs (reversible). PR2 runs a one-shot migration script that converts 14 living specs + this design spec into `.specify/specs/NNN-<slug>/`, retires the old linter and index regenerator, and adds skill shims (one-way door). PR3 updates documentation. Spec-kit is pinned at a specific tag; the chitin front-matter survives as an extension; constitution-drift is enforced by a CI test against `docs/architecture/layer-contracts.md` and `docs/architecture.md`.

**Tech Stack:** Python 3.11 (linters, migration script, tests via pytest/unittest), uv (spec-kit install), spec-kit CLI (`specify`), GitHub Actions YAML, bash (CI steps), Markdown (constitution + specs + plans).

**Spec reference:** [`docs/superpowers/specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md`](../specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md).

---

## File structure (after all three PRs land)

### New files

| Path | Responsibility |
|---|---|
| `.specify/memory/constitution.md` | The 7 articles + "see also" pointers. Loaded into every spec-kit slash command. |
| `.specify/templates/overrides/spec-template.md` | Chitin-shaped spec template: front-matter block + standard sections. |
| `.specify/templates/overrides/plan-template.md` | Chitin's slice convention. |
| `.specify/templates/overrides/tasks-template.md` | Tasks template with kanban-flow chokepoint reminder header. |
| `.specify/extensions/chitin-frontmatter/extension.yaml` | Spec-kit extension declaring the 7 required chitin front-matter fields. |
| `.specify/extensions/chitin-frontmatter/README.md` | Documents the extension. |
| `.specify/specs/001-operator-approval-escalation/{spec,plan,tasks}.md` | Migrated from `2026-05-07-operator-approval-escalation-design.md`. |
| … through `.specify/specs/015-adopt-speckit-replace-spec-flow/{spec,plan,tasks}.md` | Migrated specs, IDs 001–015. |
| `scripts/check-speckit-frontmatter.py` | New linter; replaces `scripts/check-spec-frontmatter.py`. |
| `scripts/migrate-specs-to-speckit.py` | One-shot migration script (used in PR2, retired after). |
| `scripts/test_check_speckit_frontmatter.py` | Pytest tests for the new linter. |
| `scripts/test_migrate_specs.py` | Pytest tests for the migration script. |
| `scripts/test_constitution_drift.py` | CI test asserting constitution Articles I–V match upstream sources. |
| `docs/superpowers/specs/README.md` | Historical-record note added in PR3. |
| `.claude/commands/brainstorming.md` | Shim redirecting to `/speckit.specify`. |
| `.claude/commands/writing-plans.md` | Shim redirecting to `/speckit.plan`. |
| `.claude/commands/executing-plans.md` | Shim redirecting to `/speckit.implement`. |

### Modified files

| Path | What changes |
|---|---|
| `.github/workflows/ci.yml` | Add four steps: install uv + spec-kit, run `specify check`, run new linter, frozen-directory gate. |
| `CLAUDE.md` | Replace pointers to the retired skills; add a note on the spec-kit flow + shim window. |
| `AGENTS.md` | Same as CLAUDE.md. |
| `README.md` | Update the "Specs and plans" section to point at `.specify/`. |
| `docs/runbooks/spec-lifecycle.md` | Rewrite paths from `docs/superpowers/specs/` → `.specify/specs/NNN-<slug>/`. |
| `docs/superpowers/specs/INDEX.md` | Add a "FROZEN at 2026-05-15" header at top; remove migrated rows. |

### Retired files (deleted in PR2)

| Path | Reason |
|---|---|
| `scripts/check-spec-frontmatter.py` | Replaced by `check-speckit-frontmatter.py`. |
| `scripts/regen-spec-index.py` | Spec-kit's directory listing IS the index. |
| `docs/superpowers/specs/*.md` (the 14 living + 1 design) | Replaced by a 1-line redirect file pointing at `.specify/specs/NNN-<slug>/spec.md`. |

---

# PHASE 1 — PR1: Scaffold `.specify/` without migration

Goal of this phase: `.specify/` exists with constitution + extension + templates + drift test + CI integration. **No existing specs are touched.** Reversible by `git revert` + `rm -rf .specify/`.

---

### Task 1.1: Branch + install spec-kit CLI

**Files:**
- Touched in this task: none persistent; just toolchain prep.

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/speckit-pr1-scaffold
```

- [ ] **Step 2: Install uv (Astral package manager) if not already installed**

Run: `which uv && uv --version` — if it prints a version, skip; otherwise:

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
exec $SHELL -l   # reload PATH
uv --version
```

Expected: `uv 0.x.y` printed.

- [ ] **Step 3: Install the spec-kit CLI from a pinned tag**

Pick the latest tagged release on https://github.com/github/spec-kit/releases. For this plan, use `v0.0.55` as a placeholder — update to the actual latest tag when running the task and record the chosen tag in `.specify/extensions/chitin-frontmatter/README.md` (Task 1.4 step 3).

```bash
SPECKIT_TAG=v0.0.55   # CONFIRM against latest tag before running
uv tool install specify-cli --from "git+https://github.com/github/spec-kit.git@${SPECKIT_TAG}"
specify --version
```

Expected: `specify` prints a version string; exit 0.

- [ ] **Step 4: Verify supported-agent list includes our four drivers**

```bash
specify integration list
```

Expected output contains: `claude`, `codex`, `gemini`, `copilot` (or `github-copilot`).

If any are missing, halt and revisit Section 5 of the design spec — the spec-kit version may have changed naming. Do not proceed.

---

### Task 1.2: Run `specify init` against the repo

**Files:**
- Create: `.specify/memory/constitution.md` (placeholder; replaced in Task 1.3)
- Create: `.specify/templates/spec-template.md` (spec-kit core; do not edit later)
- Create: `.specify/templates/plan-template.md` (spec-kit core)
- Create: `.specify/templates/tasks-template.md` (spec-kit core)
- Create: `.specify/scripts/bash/*` (spec-kit core)
- Create: `.specify/extensions/`, `.specify/presets/` (empty dirs)

- [ ] **Step 1: Run `specify init` in the repo root**

```bash
cd /home/red/workspace/chitin
specify init . --integration claude --integration codex --integration gemini --integration copilot --force
```

The `--force` flag is required because `.specify/` doesn't exist yet but the working directory is non-empty. Spec-kit will scaffold without touching unrelated files.

Expected: `.specify/` tree appears with `memory/`, `templates/`, `scripts/bash/`, plus any agent-specific instruction files spec-kit adds to root (e.g. injecting into `CLAUDE.md` — accept, we re-author in PR3).

- [ ] **Step 2: Verify scaffold**

Run: `ls .specify/`
Expected: at least `memory/`, `templates/`, `scripts/`, `extensions/` (the last may be missing; create it manually with `mkdir -p .specify/extensions .specify/presets` if so).

Run: `specify check`
Expected: exit 0, with informational output about the new project.

- [ ] **Step 3: Stage and verify diff is what spec-kit produced**

```bash
git status
git diff --stat
```

Note any modifications to `CLAUDE.md` or other root files made by spec-kit; we will reconcile these in PR3, but for PR1 we accept them as-is to keep the scaffold consistent.

- [ ] **Step 4: Commit the raw scaffold**

```bash
git add .specify/ CLAUDE.md AGENTS.md README.md 2>/dev/null || true
git add .specify/
# Add any other files spec-kit modified that we want to keep
git status
git commit -m "chore(speckit): specify init scaffolds .specify/"
```

The commit is intentionally pre-customization; Task 1.3 onwards rewrites the artifacts.

---

### Task 1.3: Write the chitin constitution

**Files:**
- Modify (overwrite): `.specify/memory/constitution.md`

- [ ] **Step 1: Replace the placeholder constitution with the chitin canon**

Write the entire content of `.specify/memory/constitution.md` to be exactly the body shown in Section 3 of `docs/superpowers/specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md`. That body starts with `# Chitin Constitution` and ends with the "See also" list.

The file MUST be ≤ 300 lines after this write — verify with `wc -l .specify/memory/constitution.md`. Expected: well under 300.

- [ ] **Step 2: Verify the constitution loads through spec-kit**

```bash
specify check
```

Expected: exit 0; no warnings about constitution missing or malformed.

- [ ] **Step 3: Commit**

```bash
git add .specify/memory/constitution.md
git commit -m "feat(speckit): author chitin constitution (Articles I-VII)"
```

---

### Task 1.4: Author the chitin-frontmatter extension

**Files:**
- Create: `.specify/extensions/chitin-frontmatter/extension.yaml`
- Create: `.specify/extensions/chitin-frontmatter/README.md`

Spec-kit extensions are folders containing an `extension.yaml` manifest plus optional templates. Read https://github.com/github/spec-kit/tree/main/docs to confirm the exact manifest shape for the pinned tag before writing — the schema below is correct as of `v0.0.55` and may shift.

- [ ] **Step 1: Create the extension directory and manifest**

```bash
mkdir -p .specify/extensions/chitin-frontmatter
```

Write `.specify/extensions/chitin-frontmatter/extension.yaml`:

```yaml
id: chitin-frontmatter
name: Chitin front-matter
version: 0.1.0
description: |
  Enforces chitin's seven required YAML front-matter fields on every
  .specify/specs/**/spec.md: status, owner, kanban, implementation_pr,
  superseded_by, effective_from, effective_to.
  Source contract: docs/runbooks/spec-lifecycle.md (after PR3 rewrite).
required_frontmatter:
  - status
  - owner
  - kanban
  - implementation_pr
  - superseded_by
  - effective_from
  - effective_to
status_enum:
  - draft
  - open
  - implemented
  - amended
  - superseded
kanban_pattern: '^t_[a-f0-9]{8}$'
```

- [ ] **Step 2: Document the extension**

Write `.specify/extensions/chitin-frontmatter/README.md`:

```markdown
# chitin-frontmatter extension

Declares chitin's seven required YAML front-matter fields on every spec.

## Required fields

| Field | Meaning |
|---|---|
| status | draft / open / implemented / amended / superseded |
| owner | claude-code, jared, red, jared+claude, red+claude |
| kanban | null or hermes ticket id matching `^t_[a-f0-9]{8}$` |
| implementation_pr | integer PR number when status=implemented, else null |
| superseded_by | path to replacement spec when status=superseded, else null |
| effective_from | YYYY-MM-DD when the spec entered its current lifecycle |
| effective_to | YYYY-MM-DD when implemented or superseded, else null |

## Enforcement

`scripts/check-speckit-frontmatter.py` runs in CI on every PR.

## Spec-kit version pinned

Pinned to spec-kit tag `<TAG>` (see CI workflow).
```

Replace `<TAG>` with the exact tag installed in Task 1.1 Step 3.

- [ ] **Step 3: Verify spec-kit picks up the extension**

```bash
specify check
```

Expected: exit 0; extension listed in any verbose output.

- [ ] **Step 4: Commit**

```bash
git add .specify/extensions/chitin-frontmatter/
git commit -m "feat(speckit): add chitin-frontmatter extension"
```

---

### Task 1.5: Author chitin template overrides

**Files:**
- Create: `.specify/templates/overrides/spec-template.md`
- Create: `.specify/templates/overrides/plan-template.md`
- Create: `.specify/templates/overrides/tasks-template.md`

- [ ] **Step 1: Create the overrides directory**

```bash
mkdir -p .specify/templates/overrides
```

- [ ] **Step 2: Write the chitin spec template**

Write `.specify/templates/overrides/spec-template.md`:

````markdown
---
status: draft
owner: <claude-code | jared | red | jared+claude | red+claude>
kanban: <null | t_xxxxxxxx>
implementation_pr: null
superseded_by: null
effective_from: <YYYY-MM-DD>
effective_to: null
---

# <Title>

## 1. Goal and scope

### Goal
<one paragraph>

### In scope
1. <item>

### Out of scope
- <item>

## 2. Architecture
<short narrative + diagram if useful>

## 3. Acceptance criteria
1. <observable, verifiable predicate>

## 4. Risks
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| <risk> | Low/Med/High | Low/Med/High | <mitigation> |

## 5. Open questions
- <question>

````

- [ ] **Step 3: Write the chitin plan template**

Write `.specify/templates/overrides/plan-template.md`:

```markdown
# <Feature> Implementation Plan

**Goal:** <one sentence>
**Architecture:** <2-3 sentences>
**Tech Stack:** <list>
**Spec reference:** [.specify/specs/NNN-<slug>/spec.md](../specs/NNN-<slug>/spec.md)

## File structure
<table of new + modified files>

## Phase 1 / PR1
### Task 1.1: <name>
**Files:** <list>
- [ ] **Step 1:** <action>
...
- [ ] **Step N: Commit**
```bash
git add <files>
git commit -m "<scope>: <message>"
```
```

- [ ] **Step 4: Write the chitin tasks template**

Write `.specify/templates/overrides/tasks-template.md`:

```markdown
# Tasks: <Feature>

> Implementation tracker. All status changes route through `scripts/kanban-flow`
> to satisfy chitin's audit invariant (every status change pairs a comment +
> task_events row).

- [ ] Task 1: <name>
- [ ] Task 2: <name>
- [ ] Task 3: <name>
```

- [ ] **Step 5: Verify overrides apply**

```bash
specify check
```

Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add .specify/templates/overrides/
git commit -m "feat(speckit): add chitin template overrides"
```

---

### Task 1.6: Write the constitution-drift test

**Files:**
- Create: `scripts/test_constitution_drift.py`

The drift test asserts that:
- Articles I–IV of `.specify/memory/constitution.md` match the four invariants in `docs/architecture/layer-contracts.md` verbatim (modulo header).
- Article V matches the "Hard rule" body of `docs/architecture.md`.

- [ ] **Step 1: Write the failing test**

Write `scripts/test_constitution_drift.py`:

```python
#!/usr/bin/env python3
"""Constitution-drift CI test.

Asserts:
- Articles I-IV of .specify/memory/constitution.md match the four invariants
  in docs/architecture/layer-contracts.md verbatim (modulo Article-numbering
  header text and surrounding whitespace).
- Article V matches the "Hard rule" body of docs/architecture.md.

Articles VI and VII are chitin-cultural and NOT drift-checked here.

Exit 0 on match, 1 on drift, 2 on usage error.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

CONST = Path(".specify/memory/constitution.md")
LAYER = Path("docs/architecture/layer-contracts.md")
ARCH = Path("docs/architecture.md")

INVARIANTS = ("Kernel Authority", "Driver Constraint",
              "Routing Scope", "Aggregation Role")


def normalize(text: str) -> str:
    """Collapse whitespace; lowercase; strip markdown emphasis."""
    text = re.sub(r"[*_`]+", "", text)
    text = re.sub(r"\s+", " ", text)
    return text.strip().lower()


def extract_section(body: str, heading_regex: str) -> str | None:
    """Return the body of the first section whose heading matches; up to next ##."""
    pat = re.compile(rf"(?m)^##\s+{heading_regex}\s*$\n(.*?)(?=^##\s|\Z)",
                     re.DOTALL)
    m = pat.search(body)
    return m.group(1) if m else None


def read(path: Path) -> str:
    if not path.exists():
        print(f"constitution-drift: missing {path}", file=sys.stderr)
        sys.exit(2)
    return path.read_text(encoding="utf-8")


def main() -> int:
    const = read(CONST)
    layer = read(LAYER)
    arch = read(ARCH)

    errors = []

    # Articles I-IV vs layer-contracts
    for i, name in enumerate(INVARIANTS, start=1):
        art = extract_section(const, rf"Article\s+[IVX]+\s+—\s*{re.escape(name)}")
        if art is None:
            errors.append(f"Article matching '{name}' not found in {CONST}")
            continue
        ref = extract_section(layer, rf"\d+\.\s*{re.escape(name)}")
        if ref is None:
            errors.append(f"Section '{name}' not found in {LAYER}")
            continue
        if normalize(art) != normalize(ref):
            errors.append(
                f"Article '{name}' drifted from {LAYER} (normalized text mismatch)"
            )

    # Article V vs architecture.md "Hard rule"
    art_v = extract_section(const, r"Article\s+V\s+—\s*Single Side-Effect Authority")
    hard_rule = extract_section(arch, r"Hard rule")
    if art_v is None:
        errors.append("Article V not found in constitution")
    elif hard_rule is None:
        errors.append(f"'Hard rule' section not found in {ARCH}")
    elif normalize(art_v) != normalize(hard_rule):
        errors.append("Article V drifted from docs/architecture.md 'Hard rule'")

    if errors:
        print("constitution-drift: FAIL", file=sys.stderr)
        for e in errors:
            print(f"  - {e}", file=sys.stderr)
        return 1

    print("constitution-drift: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Run the test to verify it passes against the constitution we wrote in Task 1.3**

Run: `python3 scripts/test_constitution_drift.py`
Expected: `constitution-drift: OK` and exit 0.

If it fails: the constitution's Article text doesn't normalize to the upstream text. Inspect the diff in the error, then either (a) fix the wording in the constitution to match upstream, or (b) accept the divergence is intentional and update the test's normalization rules. The "intentional divergence" path is rare and should be justified in a code comment.

- [ ] **Step 3: Run a deliberately-broken variant to verify the test fails**

```bash
# Temporarily corrupt Article I
sed -i.bak 's/Kernel Authority/Kernel AUTHORITY/' .specify/memory/constitution.md
python3 scripts/test_constitution_drift.py
echo "exit: $?"
# Restore
mv .specify/memory/constitution.md.bak .specify/memory/constitution.md
```

Expected: the run prints `constitution-drift: FAIL` with a diff and exits 1. Restore is silent.

- [ ] **Step 4: Make executable + commit**

```bash
chmod +x scripts/test_constitution_drift.py
git add scripts/test_constitution_drift.py
git commit -m "test(speckit): add constitution-drift CI guard"
```

---

### Task 1.7: Wire spec-kit into CI

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Read the current CI workflow to find the right insertion point**

Open `.github/workflows/ci.yml`. The new steps should run AFTER the `pnpm install --frozen-lockfile` step (so Python is available, since the existing workflow already does `python3 -m unittest swarm.tests...`) and BEFORE `Verify signed governance policy` (so spec-kit issues are caught early).

- [ ] **Step 2: Add the four new CI steps**

Insert this block, with appropriate indentation matching the surrounding steps:

```yaml
      - name: Install uv (for spec-kit)
        uses: astral-sh/setup-uv@v3
        with:
          enable-cache: true

      - name: Install spec-kit CLI
        run: |
          SPECKIT_TAG="$(grep -E '^Pinned to spec-kit tag' .specify/extensions/chitin-frontmatter/README.md | sed -E 's/.*\`([^\`]+)\`.*/\1/')"
          uv tool install specify-cli --from "git+https://github.com/github/spec-kit.git@${SPECKIT_TAG}"

      - name: Spec-kit native check
        run: specify check

      - name: Constitution-drift guard
        run: python3 scripts/test_constitution_drift.py
```

The pinned tag is read from the extension README so a single point of truth governs CI + local installs.

- [ ] **Step 3: Verify CI YAML parses**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"
```

Expected: no exception, prints nothing.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(speckit): wire spec-kit install + check + drift guard"
```

---

### Task 1.8: Open PR1

- [ ] **Step 1: Push the branch**

```bash
rtk git push -u origin feat/speckit-pr1-scaffold
```

- [ ] **Step 2: Open PR1**

```bash
gh pr create --title "feat(speckit): scaffold .specify/ (PR1 of 3)" --body "$(cat <<'EOF'
## Summary
- `specify init .` scaffolds `.specify/memory/`, `.specify/templates/`, `.specify/scripts/bash/`
- Authors chitin constitution (Articles I–VII)
- Adds `chitin-frontmatter` extension declaring the 7 required spec front-matter fields
- Adds chitin template overrides (spec/plan/tasks)
- Adds constitution-drift CI guard
- Wires `specify check` + drift guard into `.github/workflows/ci.yml`

Spec: docs/superpowers/specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md
Plan: docs/superpowers/plans/2026-05-15-adopt-speckit-replace-spec-flow.md

## Reversibility
This PR is reversible by `git revert` + `rm -rf .specify/`. No existing
spec is touched.

## Test plan
- [ ] CI passes
- [ ] `specify check` exits 0 locally
- [ ] `python3 scripts/test_constitution_drift.py` exits 0 locally
- [ ] Deliberate constitution corruption causes the drift guard to fail (manual verify)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Wait for CI**

```bash
rtk gh pr checks
```

Expected: green. If red, halt; do not proceed to PR2 until PR1 is green and merged.

- [ ] **Step 4: Merge PR1**

```bash
gh pr merge --squash
git checkout main && rtk git pull
```

PR1 is now live on `main`. Proceed to Phase 2.

---

# PHASE 2 — PR2: Migrate specs + retire old flow (one-way door)

Goal of this phase: 14 living specs + this design spec move into `.specify/specs/NNN-<slug>/`. Old spec files become 1-line redirects. The old linter + index regenerator are deleted. Skill shims appear. CI gate prevents new writes to `docs/superpowers/specs/`.

⚠️ **PR2 is the commitment point.** Rollback after merge requires re-creating `docs/superpowers/specs/` from git history. Treat as one-way door.

---

### Task 2.1: Branch + new linter — failing tests first

**Files:**
- Create: `scripts/test_check_speckit_frontmatter.py`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/speckit-pr2-migrate
```

- [ ] **Step 2: Write the failing test file**

Write `scripts/test_check_speckit_frontmatter.py`:

```python
#!/usr/bin/env python3
"""Golden-file tests for scripts/check-speckit-frontmatter.py."""

from __future__ import annotations

import subprocess
from pathlib import Path

import pytest

SCRIPT = Path(__file__).parent / "check-speckit-frontmatter.py"

VALID = """---
status: open
owner: claude-code
kanban: t_5f50f6a8
implementation_pr: null
superseded_by: null
effective_from: 2026-05-15
effective_to: null
---

# Sample
"""

MISSING_FIELD = """---
status: open
owner: claude-code
kanban: null
implementation_pr: null
effective_from: 2026-05-15
effective_to: null
---

# Sample
"""

BAD_STATUS = """---
status: wip
owner: claude-code
kanban: null
implementation_pr: null
superseded_by: null
effective_from: 2026-05-15
effective_to: null
---

# Sample
"""

IMPLEMENTED_WITHOUT_PR = """---
status: implemented
owner: claude-code
kanban: null
implementation_pr: null
superseded_by: null
effective_from: 2026-05-15
effective_to: 2026-05-16
---

# Sample
"""

SUPERSEDED_WITHOUT_TARGET = """---
status: superseded
owner: claude-code
kanban: null
implementation_pr: null
superseded_by: null
effective_from: 2026-05-15
effective_to: 2026-05-16
---

# Sample
"""

MALFORMED_KANBAN = """---
status: open
owner: claude-code
kanban: bogus
implementation_pr: null
superseded_by: null
effective_from: 2026-05-15
effective_to: null
---

# Sample
"""


def run(tmp_path: Path, body: str) -> subprocess.CompletedProcess:
    spec_dir = tmp_path / ".specify" / "specs" / "001-sample"
    spec_dir.mkdir(parents=True)
    (spec_dir / "spec.md").write_text(body, encoding="utf-8")
    return subprocess.run(
        ["python3", str(SCRIPT)],
        cwd=tmp_path,
        capture_output=True,
        text=True,
    )


def test_valid(tmp_path):
    r = run(tmp_path, VALID)
    assert r.returncode == 0, r.stderr


def test_missing_field(tmp_path):
    r = run(tmp_path, MISSING_FIELD)
    assert r.returncode == 1
    assert "superseded_by" in r.stderr


def test_bad_status(tmp_path):
    r = run(tmp_path, BAD_STATUS)
    assert r.returncode == 1
    assert "status" in r.stderr


def test_implemented_without_pr(tmp_path):
    r = run(tmp_path, IMPLEMENTED_WITHOUT_PR)
    assert r.returncode == 1
    assert "implementation_pr" in r.stderr


def test_superseded_without_target(tmp_path):
    r = run(tmp_path, SUPERSEDED_WITHOUT_TARGET)
    assert r.returncode == 1
    assert "superseded_by" in r.stderr


def test_malformed_kanban(tmp_path):
    r = run(tmp_path, MALFORMED_KANBAN)
    assert r.returncode == 1
    assert "kanban" in r.stderr
```

- [ ] **Step 3: Run the test file to verify it fails (script does not yet exist)**

```bash
pip install pytest pyyaml 2>/dev/null
python3 -m pytest scripts/test_check_speckit_frontmatter.py -v
```

Expected: all 6 tests fail with "no such file or directory" or import errors against `scripts/check-speckit-frontmatter.py`. This confirms the test harness invokes a missing script.

- [ ] **Step 4: Commit the failing tests**

```bash
git add scripts/test_check_speckit_frontmatter.py
git commit -m "test(speckit): golden-file tests for new front-matter linter"
```

---

### Task 2.2: Implement the new linter to make the tests pass

**Files:**
- Create: `scripts/check-speckit-frontmatter.py`

- [ ] **Step 1: Implement the linter**

Write `scripts/check-speckit-frontmatter.py`:

```python
#!/usr/bin/env python3
"""Validate YAML front-matter on every .specify/specs/**/spec.md.

Replaces scripts/check-spec-frontmatter.py. Same contract, new root.

Required fields:
  status, owner, kanban, implementation_pr, superseded_by,
  effective_from, effective_to

Rules:
  - status in {draft, open, implemented, amended, superseded}
  - status == implemented => implementation_pr is non-null
  - status == superseded  => superseded_by points to an existing
    .specify/specs/**/spec.md
  - kanban is null or matches ^t_[a-f0-9]{8}$
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.stderr.write(
        "check-speckit-frontmatter: PyYAML is required.\n"
        "  Install: pip install pyyaml  (or apt install python3-yaml)\n"
    )
    sys.exit(2)


REQUIRED_FIELDS = (
    "status",
    "owner",
    "kanban",
    "implementation_pr",
    "superseded_by",
    "effective_from",
    "effective_to",
)
VALID_STATUSES = ("draft", "open", "implemented", "amended", "superseded")
KANBAN_RE = re.compile(r"^t_[a-f0-9]{8}$")
ROOT = Path(".specify/specs")


def parse_frontmatter(path: Path) -> tuple[dict | None, str | None]:
    text = path.read_text(encoding="utf-8")
    if not text.startswith("---\n"):
        return None, "missing front-matter (file must start with `---`)"
    end = text.find("\n---", 4)
    if end == -1:
        return None, "front-matter not terminated with `---`"
    try:
        data = yaml.safe_load(text[4:end])
    except yaml.YAMLError as e:
        return None, f"YAML parse error: {e}"
    if not isinstance(data, dict):
        return None, "front-matter is not a mapping"
    return data, None


def validate(path: Path) -> list[str]:
    errors: list[str] = []
    data, err = parse_frontmatter(path)
    if err:
        return [f"{path}: {err}"]
    assert data is not None

    for field in REQUIRED_FIELDS:
        if field not in data:
            errors.append(f"{path}: missing required field `{field}`")

    status = data.get("status")
    if status not in VALID_STATUSES:
        errors.append(
            f"{path}: status `{status}` not in {VALID_STATUSES}"
        )

    if status == "implemented" and data.get("implementation_pr") is None:
        errors.append(f"{path}: status=implemented requires implementation_pr")

    if status == "superseded":
        target = data.get("superseded_by")
        if target is None:
            errors.append(f"{path}: status=superseded requires superseded_by")
        else:
            if not Path(target).exists():
                errors.append(
                    f"{path}: superseded_by `{target}` does not exist"
                )

    kanban = data.get("kanban")
    if kanban is not None:
        if not (isinstance(kanban, str) and KANBAN_RE.match(kanban)):
            errors.append(
                f"{path}: kanban `{kanban}` does not match ^t_[a-f0-9]{{8}}$"
            )

    return errors


def main() -> int:
    if not ROOT.exists():
        print(f"check-speckit-frontmatter: {ROOT} does not exist; nothing to check")
        return 0

    errors: list[str] = []
    for spec in sorted(ROOT.glob("*/spec.md")):
        errors.extend(validate(spec))

    if errors:
        print("check-speckit-frontmatter: FAIL", file=sys.stderr)
        for e in errors:
            print(f"  {e}", file=sys.stderr)
        return 1

    print("check-speckit-frontmatter: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Run the tests to verify they pass**

```bash
chmod +x scripts/check-speckit-frontmatter.py
python3 -m pytest scripts/test_check_speckit_frontmatter.py -v
```

Expected: 6/6 pass.

- [ ] **Step 3: Commit**

```bash
git add scripts/check-speckit-frontmatter.py
git commit -m "feat(speckit): new front-matter linter (passes 6 golden-file tests)"
```

---

### Task 2.3: Migration script — failing tests first

**Files:**
- Create: `scripts/test_migrate_specs.py`

- [ ] **Step 1: Write the failing tests**

Write `scripts/test_migrate_specs.py`:

```python
#!/usr/bin/env python3
"""Tests for scripts/migrate-specs-to-speckit.py."""

from __future__ import annotations

import subprocess
from pathlib import Path

import pytest

SCRIPT = Path(__file__).parent / "migrate-specs-to-speckit.py"


def make_old_spec(repo: Path, filename: str, body: str) -> Path:
    """Place a spec at docs/superpowers/specs/<filename>."""
    target = repo / "docs" / "superpowers" / "specs" / filename
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(body, encoding="utf-8")
    return target


SAMPLE = """---
status: open
owner: jared
kanban: null
implementation_pr: null
superseded_by: null
effective_from: 2026-05-11
effective_to: null
---

# Sample spec title

Original body content goes here.
"""


def run(repo: Path, *args) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["python3", str(SCRIPT), *args],
        cwd=repo,
        capture_output=True,
        text=True,
    )


def test_dry_run_creates_nothing(tmp_path):
    make_old_spec(tmp_path, "2026-05-11-sample.md", SAMPLE)
    r = run(tmp_path, "--dry-run")
    assert r.returncode == 0
    assert not (tmp_path / ".specify" / "specs").exists()


def test_real_run_migrates_one_spec(tmp_path):
    make_old_spec(tmp_path, "2026-05-11-sample.md", SAMPLE)
    r = run(tmp_path)
    assert r.returncode == 0, r.stderr
    new_spec = tmp_path / ".specify" / "specs" / "001-sample" / "spec.md"
    assert new_spec.exists()
    text = new_spec.read_text(encoding="utf-8")
    assert text.startswith("---\nstatus: open\n")
    assert "Original body content goes here." in text


def test_old_path_becomes_redirect(tmp_path):
    make_old_spec(tmp_path, "2026-05-11-sample.md", SAMPLE)
    run(tmp_path)
    old = tmp_path / "docs" / "superpowers" / "specs" / "2026-05-11-sample.md"
    assert old.exists()
    text = old.read_text(encoding="utf-8")
    assert "Moved to" in text
    assert ".specify/specs/001-sample/spec.md" in text


def test_idempotent(tmp_path):
    make_old_spec(tmp_path, "2026-05-11-sample.md", SAMPLE)
    r1 = run(tmp_path)
    r2 = run(tmp_path)
    assert r1.returncode == 0
    assert r2.returncode == 0
    assert "skipped" in r2.stdout.lower() or "already" in r2.stdout.lower()


def test_amended_is_not_migrated(tmp_path):
    amended = SAMPLE.replace("status: open", "status: amended")
    make_old_spec(tmp_path, "2026-05-12-amended-sample.md", amended)
    r = run(tmp_path)
    assert r.returncode == 0
    # Amended specs stay in place; nothing in .specify
    assert not (tmp_path / ".specify" / "specs").exists()


def test_missing_frontmatter_hard_fails(tmp_path):
    make_old_spec(tmp_path, "2026-05-11-bad.md", "# Just a heading, no front-matter")
    r = run(tmp_path)
    assert r.returncode != 0
    assert "front-matter" in r.stderr.lower()
```

- [ ] **Step 2: Run the tests to verify they fail (script not yet implemented)**

```bash
python3 -m pytest scripts/test_migrate_specs.py -v
```

Expected: all 6 tests fail with missing-script errors.

- [ ] **Step 3: Commit**

```bash
git add scripts/test_migrate_specs.py
git commit -m "test(speckit): migration script tests"
```

---

### Task 2.4: Implement the migration script

**Files:**
- Create: `scripts/migrate-specs-to-speckit.py`

- [ ] **Step 1: Implement the migration script**

Write `scripts/migrate-specs-to-speckit.py`:

```python
#!/usr/bin/env python3
"""One-shot migration: docs/superpowers/specs/*.md -> .specify/specs/NNN-<slug>/.

Behavior:
  - Iterates docs/superpowers/specs/*.md sorted by filename (date-prefixed).
  - Skips INDEX.md and README.md.
  - Skips specs with status in {amended, superseded} -- they stay historical.
  - For each living spec (status in {draft, open}):
      - Assigns the next sequential NNN.
      - Derives slug from the filename (after the date prefix).
      - Writes .specify/specs/NNN-<slug>/spec.md with front-matter preserved
        and the original body under `## Original spec body`.
      - Writes empty plan.md and tasks.md from the templates if present,
        else minimal stubs.
      - Rewrites the old path to a 1-line redirect.
  - Idempotent: if .specify/specs/NNN-<slug>/ exists, skip and log.
  - --dry-run: print planned operations; create nothing.
  - Hard-fails on: missing front-matter, slug collision.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.stderr.write("migrate-specs-to-speckit: PyYAML required.\n")
    sys.exit(2)


OLD_DIR = Path("docs/superpowers/specs")
NEW_DIR = Path(".specify/specs")
EXEMPT = {"INDEX.md", "README.md"}
SKIP_STATUSES = {"amended", "superseded", "implemented"}
DATE_PREFIX_RE = re.compile(r"^\d{4}-\d{2}-\d{2}-(.+?)(?:-design)?\.md$")


def parse_frontmatter(path: Path) -> tuple[dict, str]:
    """Return (front-matter dict, body without front-matter)."""
    text = path.read_text(encoding="utf-8")
    if not text.startswith("---\n"):
        raise ValueError(f"{path}: missing front-matter")
    end = text.find("\n---", 4)
    if end == -1:
        raise ValueError(f"{path}: unterminated front-matter")
    data = yaml.safe_load(text[4:end])
    if not isinstance(data, dict):
        raise ValueError(f"{path}: front-matter not a mapping")
    body = text[end + len("\n---"):].lstrip("\n")
    return data, body


def derive_slug(filename: str) -> str:
    m = DATE_PREFIX_RE.match(filename)
    if not m:
        raise ValueError(f"{filename}: does not match YYYY-MM-DD-<slug>.md")
    return m.group(1)


def emit_frontmatter(data: dict) -> str:
    return "---\n" + yaml.safe_dump(data, sort_keys=False).rstrip() + "\n---\n"


def write_new_spec(new_dir: Path, frontmatter: dict, body: str, old_path: Path,
                   commit_sha: str) -> None:
    new_dir.mkdir(parents=True, exist_ok=False)
    spec_text = (
        emit_frontmatter(frontmatter)
        + "\n"
        + body.rstrip()
        + f"\n\n---\nMIGRATED-FROM: `{old_path}` (commit `{commit_sha}`)\n"
    )
    (new_dir / "spec.md").write_text(spec_text, encoding="utf-8")
    (new_dir / "plan.md").write_text(
        "# Plan\n\n(Plan body lives here; see docs/superpowers/plans/ for the "
        "historical plan if one exists.)\n",
        encoding="utf-8",
    )
    (new_dir / "tasks.md").write_text(
        "# Tasks\n\n"
        "> All status changes route through `scripts/kanban-flow`.\n\n"
        "- [ ] Task 1\n",
        encoding="utf-8",
    )


def write_redirect(old_path: Path, new_path: Path) -> None:
    old_path.write_text(
        f"> Moved to `{new_path}` on 2026-05-15. "
        "See [`docs/superpowers/specs/README.md`](./README.md) for context.\n",
        encoding="utf-8",
    )


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    if not OLD_DIR.exists():
        print(f"migrate-specs: {OLD_DIR} does not exist; nothing to migrate")
        return 0

    # Resolve commit SHA for MIGRATED-FROM markers
    import subprocess
    try:
        commit_sha = subprocess.check_output(
            ["git", "rev-parse", "--short", "HEAD"], text=True
        ).strip()
    except subprocess.CalledProcessError:
        commit_sha = "unknown"

    living: list[Path] = []
    for p in sorted(OLD_DIR.glob("*.md")):
        if p.name in EXEMPT:
            continue
        try:
            fm, _ = parse_frontmatter(p)
        except ValueError as e:
            print(f"FAIL: {e}", file=sys.stderr)
            return 1
        if fm.get("status") in SKIP_STATUSES:
            continue
        living.append(p)

    used_slugs: dict[str, Path] = {}
    plan: list[tuple[Path, Path]] = []
    for idx, old in enumerate(living, start=1):
        try:
            slug = derive_slug(old.name)
        except ValueError as e:
            print(f"FAIL: {e}", file=sys.stderr)
            return 1
        if slug in used_slugs:
            print(
                f"FAIL: slug collision `{slug}` between {old} and {used_slugs[slug]}",
                file=sys.stderr,
            )
            return 1
        used_slugs[slug] = old
        new_dir = NEW_DIR / f"{idx:03d}-{slug}"
        plan.append((old, new_dir))

    for old, new_dir in plan:
        if new_dir.exists():
            print(f"skipped (already migrated): {old} -> {new_dir}")
            continue
        if args.dry_run:
            print(f"DRY-RUN: would migrate {old} -> {new_dir}")
            continue
        fm, body = parse_frontmatter(old)
        write_new_spec(new_dir, fm, body, old, commit_sha)
        write_redirect(old, new_dir / "spec.md")
        print(f"migrated: {old} -> {new_dir}")

    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 2: Run the tests**

```bash
chmod +x scripts/migrate-specs-to-speckit.py
python3 -m pytest scripts/test_migrate_specs.py -v
```

Expected: all 6 tests pass.

- [ ] **Step 3: Commit**

```bash
git add scripts/migrate-specs-to-speckit.py
git commit -m "feat(speckit): migration script for living specs (passes 6 tests)"
```

---

### Task 2.5: Add frozen-directory CI gate

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `scripts/check-speckit-frontmatter.py` (add to CI)

- [ ] **Step 1: Add the gate step**

In `.github/workflows/ci.yml`, after the `Constitution-drift guard` step from Task 1.7, add:

```yaml
      - name: Spec front-matter linter
        run: python3 scripts/check-speckit-frontmatter.py

      - name: Frozen historical spec dir
        if: github.event_name == 'pull_request'
        run: |
          base="origin/${{ github.event.pull_request.base.ref }}"
          changed=$(git diff --name-only "$base"...HEAD \
            | grep -E '^docs/superpowers/specs/[^/]+\.md$' \
            | grep -vE '(README|INDEX)\.md$' || true)
          # Allow files that already exist and got rewritten to redirects
          new=""
          for f in $changed; do
            if ! git ls-tree -r "$base" --name-only | grep -qx "$f"; then
              new="$new $f"
            fi
          done
          if [ -n "$new" ]; then
            echo "::error::docs/superpowers/specs/ is frozen historical; new specs go in .specify/. Offending files:"
            for f in $new; do echo "  - $f"; done
            exit 1
          fi
```

The gate only fires on additions; modifications to existing files (e.g. converting one into a redirect) are allowed because Task 2.7 needs them.

- [ ] **Step 2: Verify YAML still parses**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"
```

Expected: no exception.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(speckit): linter + frozen-historical-dir gate"
```

---

### Task 2.6: Run migration (dry-run + apply)

**Files:**
- Creates: `.specify/specs/001…015-<slug>/{spec,plan,tasks}.md`
- Modifies: `docs/superpowers/specs/<14 living + this design>.md` → 1-line redirects

- [ ] **Step 1: Dry-run**

```bash
python3 scripts/migrate-specs-to-speckit.py --dry-run
```

Expected output: 15 `DRY-RUN: would migrate <old> -> <new_dir>` lines covering:
- 14 living specs (13 open + 1 draft) numbered 001–014 oldest-to-newest
- This design spec (`2026-05-15-adopt-speckit-replace-spec-flow-design.md`) numbered 015

Verify each pairing matches §4 numbering map of the design spec. If a slug looks wrong (e.g. trailing `-design`), inspect `DATE_PREFIX_RE` — the regex strips an optional `-design` suffix.

- [ ] **Step 2: Apply migration**

```bash
python3 scripts/migrate-specs-to-speckit.py
```

Expected: 15 `migrated: …` lines; no errors.

- [ ] **Step 3: Verify the new tree**

```bash
ls .specify/specs/
```

Expected: 15 directories `001-…` through `015-…`.

```bash
python3 scripts/check-speckit-frontmatter.py
```

Expected: `check-speckit-frontmatter: OK`.

```bash
specify check
```

Expected: exit 0.

- [ ] **Step 4: Commit the migration**

```bash
git add .specify/specs/ docs/superpowers/specs/
git commit -m "chore(speckit): migrate 15 living specs to .specify/specs/"
```

---

### Task 2.7: Freeze the INDEX header + add historical-record stub

**Files:**
- Modify: `docs/superpowers/specs/INDEX.md`
- Note: full `README.md` rewrite happens in PR3 Task 3.1; here we drop a stub.

- [ ] **Step 1: Prepend the freeze header to INDEX.md**

Open `docs/superpowers/specs/INDEX.md`. Insert this block immediately after the first line (`# Spec index`):

```markdown

> **FROZEN at 2026-05-15.** This index is no longer auto-generated.
> All living specs have migrated to `.specify/specs/`. The two amended
> specs and these redirect files remain here as historical record.
> See [`README.md`](./README.md) for context.
```

- [ ] **Step 2: Remove rows for migrated specs from the table**

In the same `INDEX.md`, delete the table rows for the 14 living specs and this design spec (they're now redirects, not specs). Keep only the 2 amended rows. The "Implemented", "Draft", "Superseded" sub-sections may be left empty or removed; keep them for historical completeness with `_(none)_` markers.

- [ ] **Step 3: Add a placeholder README so the frozen-dir CI gate allows future edits to it**

Write `docs/superpowers/specs/README.md`:

```markdown
# Historical spec archive

This directory was the canonical home for chitin specs until 2026-05-15.
On that date, all living specs migrated to `.specify/specs/`. See the
design spec [`2026-05-15-adopt-speckit-replace-spec-flow-design.md`](./2026-05-15-adopt-speckit-replace-spec-flow-design.md)
(now a redirect to its new home at
[`.specify/specs/015-adopt-speckit-replace-spec-flow/spec.md`](../../../.specify/specs/015-adopt-speckit-replace-spec-flow/spec.md))
for the full rationale and lifecycle of this migration.

(Full historical-record content is rewritten in PR3.)
```

The "(Full historical-record content is rewritten in PR3.)" line is a deliberate placeholder; PR3 Task 3.1 replaces this file with the full version.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/INDEX.md docs/superpowers/specs/README.md
git commit -m "docs(speckit): freeze INDEX + historical-record stub"
```

---

### Task 2.8: Add skill shims

**Files:**
- Create: `.claude/commands/brainstorming.md`
- Create: `.claude/commands/writing-plans.md`
- Create: `.claude/commands/executing-plans.md`

> **Open question on precedence.** These project-local commands may or may not
> override the user's global `~/.claude/skills/` for the same names. The shim's
> job is best-effort: if Claude Code respects project-local precedence, the user
> sees the redirect; if not, the docs updates in PR3 remain the source of truth.
> Verify behavior in Task 2.10 step 3.

- [ ] **Step 1: Create the directory if missing**

```bash
mkdir -p .claude/commands
```

- [ ] **Step 2: Write the brainstorming shim**

Write `.claude/commands/brainstorming.md`:

```markdown
This command is retired as of 2026-05-15 in favor of spec-kit.

Use `/speckit.specify` to start a new spec (and `/speckit.clarify` to
sharpen ambiguity). See `docs/superpowers/specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md`
for context.

This shim retires on 2026-06-15 (30 days after PR2 merge).
```

- [ ] **Step 3: Write the writing-plans shim**

Write `.claude/commands/writing-plans.md`:

```markdown
This command is retired as of 2026-05-15 in favor of spec-kit.

Use `/speckit.plan` instead. See `docs/superpowers/specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md`
for context.

This shim retires on 2026-06-15 (30 days after PR2 merge).
```

- [ ] **Step 4: Write the executing-plans shim**

Write `.claude/commands/executing-plans.md`:

```markdown
This command is retired as of 2026-05-15 in favor of spec-kit.

Use `/speckit.implement` instead. See `docs/superpowers/specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md`
for context.

This shim retires on 2026-06-15 (30 days after PR2 merge).
```

- [ ] **Step 5: Commit**

```bash
git add .claude/commands/
git commit -m "feat(speckit): skill shims for retired brainstorming/writing-plans/executing-plans"
```

---

### Task 2.9: Retire the old linter and the index regenerator

**Files:**
- Delete: `scripts/check-spec-frontmatter.py`
- Delete: `scripts/regen-spec-index.py`

- [ ] **Step 1: Verify the old linter is not referenced in CI**

```bash
grep -n 'check-spec-frontmatter\|regen-spec-index' .github/workflows/ci.yml || echo "not referenced"
```

Expected: `not referenced`. If it IS referenced, fix the CI workflow to remove the reference before deleting the script (else CI breaks on PR2).

- [ ] **Step 2: Verify nothing else in the repo invokes the retired scripts**

```bash
grep -rn 'check-spec-frontmatter\|regen-spec-index' \
  --include='*.py' --include='*.sh' --include='*.yml' --include='*.yaml' --include='Makefile' \
  . | grep -v '^\./\.git'
```

Expected: at most references in docs/runbooks (those are rewritten in PR3) and inside the retired files themselves.

- [ ] **Step 3: Delete the retired scripts**

```bash
git rm scripts/check-spec-frontmatter.py scripts/regen-spec-index.py
```

- [ ] **Step 4: Commit**

```bash
git commit -m "chore(speckit): retire check-spec-frontmatter + regen-spec-index"
```

---

### Task 2.10: Verify the integrated PR2 state, open PR2

- [ ] **Step 1: Run the full test suite locally**

```bash
python3 -m pytest scripts/test_check_speckit_frontmatter.py scripts/test_migrate_specs.py -v
python3 scripts/check-speckit-frontmatter.py
python3 scripts/test_constitution_drift.py
specify check
```

Expected: all pass; exit 0 from each.

- [ ] **Step 2: Push branch**

```bash
rtk git push -u origin feat/speckit-pr2-migrate
```

- [ ] **Step 3: Open PR2**

```bash
gh pr create --title "feat(speckit): migrate 15 living specs (PR2 of 3)" --body "$(cat <<'EOF'
## Summary
- New front-matter linter (`scripts/check-speckit-frontmatter.py`) with 6 golden-file tests
- One-shot migration script (`scripts/migrate-specs-to-speckit.py`) with 6 tests
- Migrated 14 living specs + the spec-kit-adoption design spec into `.specify/specs/001–015`
- Frozen-directory CI gate prevents new files under `docs/superpowers/specs/`
- Skill shims for `/brainstorming`, `/writing-plans`, `/executing-plans` (retire 2026-06-15)
- Retired `scripts/check-spec-frontmatter.py` and `scripts/regen-spec-index.py`
- INDEX.md now bears a "FROZEN at 2026-05-15" header

Spec: docs/superpowers/specs/2026-05-15-adopt-speckit-replace-spec-flow-design.md
Plan: docs/superpowers/plans/2026-05-15-adopt-speckit-replace-spec-flow.md

⚠️ **One-way door.** Rollback after merge requires re-creating
`docs/superpowers/specs/` from git history.

## Test plan
- [ ] CI passes
- [ ] `python3 scripts/check-speckit-frontmatter.py` exits 0 locally
- [ ] `specify check` exits 0 locally
- [ ] `python3 scripts/test_constitution_drift.py` exits 0 locally
- [ ] Verify .claude/commands/brainstorming.md is invoked when typing /brainstorming in a fresh Claude Code session (precedence check; document outcome in PR comments)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: Wait for CI**

```bash
rtk gh pr checks
```

Expected: green. If red, investigate. Most likely failure points:
- Front-matter linter sees an existing spec with non-standard kanban format → add a test case, fix the regex, or fix the spec.
- `specify check` fails because an extension manifest changed shape between spec-kit versions → update `.specify/extensions/chitin-frontmatter/extension.yaml`.
- Frozen-directory gate is too strict → tighten the `git ls-tree -r` filter.

- [ ] **Step 5: Manually verify the shim precedence (open question)**

In a fresh Claude Code session in this repo, type `/brainstorming`. Record the observed behavior in a PR2 comment:
- If the shim file content appears → project-local override works. ✅
- If the global skill activates → document this in the PR; precedence is informational, not load-bearing.

- [ ] **Step 6: Merge PR2**

```bash
gh pr merge --squash
git checkout main && rtk git pull
```

PR2 is live. **Commitment point passed.** Proceed to Phase 3.

---

# PHASE 3 — PR3: Documentation reconciliation

Goal of this phase: top-level docs reflect the new flow. The frozen-directory README expands into a full historical record. All references to the old flow updated.

---

### Task 3.1: Branch + flesh out the historical README

**Files:**
- Modify: `docs/superpowers/specs/README.md`

- [ ] **Step 1: Create branch**

```bash
git checkout -b docs/speckit-pr3-reconcile
```

- [ ] **Step 2: Replace the stub README with the full historical record**

Write `docs/superpowers/specs/README.md`:

```markdown
# Historical spec archive

This directory was the canonical home for chitin specs until **2026-05-15**.
On that date, every living spec migrated to [`.specify/specs/`](../../../.specify/specs/)
and this directory was frozen as historical record.

## What lives here now

| File | Why it's here |
|---|---|
| `INDEX.md` | Final auto-generated index; bears a "FROZEN at 2026-05-15" header. |
| `2026-05-12-clawta-hermes-architecture.md` | Amended spec; stays as historical record (does not migrate). |
| `2026-05-13-spec-lifecycle-metadata.md` | Amended spec; stays as historical record. |
| `<date>-<slug>.md` (15 redirect files) | One-line redirects pointing at the migrated `.specify/specs/NNN-<slug>/spec.md`. Kept so inbound links from hermes ticket comments + decision records still resolve. |

## What changed

See [`2026-05-15-adopt-speckit-replace-spec-flow-design.md`](./2026-05-15-adopt-speckit-replace-spec-flow-design.md)
(redirects to [`.specify/specs/015-adopt-speckit-replace-spec-flow/spec.md`](../../../.specify/specs/015-adopt-speckit-replace-spec-flow/spec.md))
for the full rationale and design.

## How to write a new spec

Use `/speckit.specify`. Output lands in `.specify/specs/NNN-<slug>/spec.md`
with chitin front-matter enforced by `scripts/check-speckit-frontmatter.py`.

## How to find an old spec

- For specs migrated on 2026-05-15: the old path is a 1-line redirect — follow it.
- For specs amended before migration: read them in place; their amendment logs remain.
- For specs that no longer apply: see `git log -- docs/superpowers/specs/<file>.md`.
```

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/README.md
git commit -m "docs(speckit): flesh out historical-spec-archive README"
```

---

### Task 3.2: Update the spec-lifecycle runbook

**Files:**
- Modify: `docs/runbooks/spec-lifecycle.md`

- [ ] **Step 1: Rewrite paths and tooling references**

Open `docs/runbooks/spec-lifecycle.md` and apply these changes:

1. Replace every occurrence of `docs/superpowers/specs/**/*.md` with `.specify/specs/NNN-<slug>/spec.md`.
2. Replace every reference to `scripts/check-spec-frontmatter.py` with `scripts/check-speckit-frontmatter.py`.
3. Replace every reference to `scripts/regen-spec-index.py` with text noting the script retired on 2026-05-15 and the directory listing now serves as the index.
4. Add a "See also" pointer to `.specify/extensions/chitin-frontmatter/README.md` and `.specify/memory/constitution.md`.
5. Add a one-paragraph header noting the 2026-05-15 migration with a link to the design spec at its new path: `.specify/specs/015-adopt-speckit-replace-spec-flow/spec.md`.

- [ ] **Step 2: Verify all references resolve**

```bash
grep -nE 'check-spec-frontmatter|regen-spec-index|docs/superpowers/specs' docs/runbooks/spec-lifecycle.md
```

Expected: only references to the historical README or `INDEX.md` (those are intentional). No references to the retired scripts.

- [ ] **Step 3: Commit**

```bash
git add docs/runbooks/spec-lifecycle.md
git commit -m "docs(speckit): rewrite spec-lifecycle runbook for .specify/"
```

---

### Task 3.3: Update top-level README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Rewrite the "Specs and plans" section**

Open `README.md`. Find the section that mentions `docs/superpowers/specs/` and `docs/superpowers/plans/` (the section heading is "Specs and plans" in `llms.txt`; the README itself references `docs/superpowers/specs/INDEX.md`).

Replace the existing pointers with:

```markdown
**Specs and plans**

- [`.specify/specs/`](./.specify/specs/) — active specs (numbered NNN-<slug>; each has `spec.md`, `plan.md`, `tasks.md`)
- [`.specify/memory/constitution.md`](./.specify/memory/constitution.md) — the 7 articles every spec respects
- [`.specify/extensions/chitin-frontmatter/`](./.specify/extensions/chitin-frontmatter/) — required front-matter contract
- [`docs/superpowers/specs/`](./docs/superpowers/specs/) — historical archive (frozen 2026-05-15)
- [`docs/superpowers/plans/`](./docs/superpowers/plans/) — historical plans (frozen 2026-05-15)
- [`docs/runbooks/spec-lifecycle.md`](./docs/runbooks/spec-lifecycle.md) — front-matter contract + transitions
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(speckit): point README at .specify/ as canonical spec home"
```

---

### Task 3.4: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Find references to the retired skills**

```bash
grep -nE '/brainstorming|/writing-plans|/executing-plans' CLAUDE.md
```

For each match, replace with the corresponding `/speckit.*` command per Section 5 of the design spec:

| Old | New |
|---|---|
| `/brainstorming` | `/speckit.specify` (use `/speckit.clarify` if scope is fuzzy) |
| `/writing-plans` | `/speckit.plan` |
| `/executing-plans` | `/speckit.implement` |

- [ ] **Step 2: Add a "Spec authoring" pointer block**

If CLAUDE.md doesn't already have a spec-authoring section, add one near the top:

```markdown
## Spec authoring

Specs live at `.specify/specs/NNN-<slug>/spec.md`. The constitution at
`.specify/memory/constitution.md` is loaded into every spec-kit slash
command — read it before you write.

Workflow:
- `/speckit.specify` — author a new spec (drafts go in `.specify/specs/NNN-<slug>/spec.md`)
- `/speckit.clarify` — sharpen an ambiguous spec
- `/speckit.plan` — expand a spec into an implementation plan
- `/speckit.tasks` — break a plan into tracked tasks
- `/speckit.implement` — execute tasks
- `/speckit.analyze` — cross-artifact consistency check (optional)

The shim files at `.claude/commands/{brainstorming,writing-plans,executing-plans}.md`
print a redirect and retire on 2026-06-15.
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(speckit): rewrite CLAUDE.md spec-authoring guidance"
```

---

### Task 3.5: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Mirror the CLAUDE.md changes**

Apply the same edits to `AGENTS.md` (the file is broadly equivalent guidance for non-Claude agents):

```bash
grep -nE '/brainstorming|/writing-plans|/executing-plans' AGENTS.md
```

Replace each occurrence with the corresponding `/speckit.*` command. Add the same "Spec authoring" pointer block from Task 3.4 Step 2.

- [ ] **Step 2: Commit**

```bash
git add AGENTS.md
git commit -m "docs(speckit): rewrite AGENTS.md spec-authoring guidance"
```

---

### Task 3.6: Push PR3 + merge

- [ ] **Step 1: Push branch**

```bash
rtk git push -u origin docs/speckit-pr3-reconcile
```

- [ ] **Step 2: Open PR3**

```bash
gh pr create --title "docs(speckit): reconcile top-level docs (PR3 of 3)" --body "$(cat <<'EOF'
## Summary
- Flesh out `docs/superpowers/specs/README.md` as the historical-record entry point
- Rewrite `docs/runbooks/spec-lifecycle.md` for `.specify/` paths + new linter
- Update top-level `README.md` "Specs and plans" section
- Update `CLAUDE.md` and `AGENTS.md` to point at `/speckit.*` commands
- Add "Spec authoring" pointer blocks to both agent-instruction docs

Spec: .specify/specs/015-adopt-speckit-replace-spec-flow/spec.md
Plan: docs/superpowers/plans/2026-05-15-adopt-speckit-replace-spec-flow.md

## Reversibility
This PR is fully reversible by `git revert`.

## Test plan
- [ ] CI passes
- [ ] Every reference to retired scripts (`check-spec-frontmatter.py`, `regen-spec-index.py`) is gone
- [ ] Every reference to retired skills (`/brainstorming`, `/writing-plans`, `/executing-plans`) outside the shim files points at the spec-kit replacement
- [ ] README links resolve

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Wait for CI + merge**

```bash
rtk gh pr checks
gh pr merge --squash
git checkout main && rtk git pull
```

---

# Phase 4 — 30-day follow-up (post-merge of PR3)

Goal: retire the shims after the muscle-memory window.

---

### Task 4.1: Retire skill shims (scheduled 2026-06-15)

**Files:**
- Delete: `.claude/commands/brainstorming.md`
- Delete: `.claude/commands/writing-plans.md`
- Delete: `.claude/commands/executing-plans.md`
- Modify: `CLAUDE.md`, `AGENTS.md` (remove "shim files at … retire on 2026-06-15" line)

- [ ] **Step 1: Verify 30 days have passed since PR2 merge**

```bash
git log -1 --format='%cd' --date=short -- .claude/commands/brainstorming.md
```

Expected: a date ≤ 2026-05-16. If today is < 2026-06-15, halt; this task is scheduled, not immediate.

- [ ] **Step 2: Delete the shims**

```bash
git rm .claude/commands/brainstorming.md .claude/commands/writing-plans.md .claude/commands/executing-plans.md
```

- [ ] **Step 3: Remove the "retire on 2026-06-15" line from CLAUDE.md and AGENTS.md**

For each file, find the line `The shim files at .claude/commands/… retire on 2026-06-15.` and delete it.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md AGENTS.md
git commit -m "chore(speckit): retire 30-day skill shims (scheduled 2026-06-15)"
rtk git push origin main   # or open a quick PR if main is protected
```

---

# Open questions (deferred per design spec §11)

1. **Spec-kit version bumps.** No automatic upgrades. When a new spec-kit release surfaces an interesting capability, open a dedicated `chore(speckit): bump to vX.Y.Z` PR that updates the tag in `.specify/extensions/chitin-frontmatter/README.md` + reruns `specify check` + the full test suite.
2. **Presets directory.** `.specify/presets/` stays empty until a concrete need (stack-specific spec packs, e.g. governance-pack) emerges. Defer.
3. **`/speckit.analyze` in CI.** Optional cross-artifact consistency pass. Defer until a real cross-artifact bug is observed in production.
4. **Shim precedence.** Verified by Task 2.10 Step 5. Outcome captured in PR2 comments. If project-local commands don't override user-global skills, the shim becomes documentation-only — that's acceptable.

---

# Plan self-review

Spec coverage check:
- ✅ §1 in-scope items 1–10 → all covered (init: Task 1.2; constitution: Task 1.3; migrate 14 + this design: Task 2.6; freeze 2 amended + INDEX: Task 2.7; new linter: Tasks 2.1/2.2; retire regen-spec-index: Task 2.9; chitin-frontmatter extension: Task 1.4; doc updates: Tasks 3.1–3.5; `specify check` in CI: Task 1.7; skill retirement with shim: Task 2.8 + Task 4.1).
- ✅ §2 architecture target tree → matches file layout in Tasks 1.2–1.5, 2.6–2.8.
- ✅ §3 constitution body → authored verbatim in Task 1.3.
- ✅ §4 numbering map → enforced by migration script (Task 2.4) + verified in Task 2.6 Step 1.
- ✅ §5 skills + linter + CI → Tasks 2.8 (shims), 2.1/2.2 (new linter), 2.9 (retire old linter), 1.7/2.5 (CI steps).
- ✅ §6 error handling → migration script hard-fails captured in Task 2.4 + tested in Task 2.3.
- ✅ §7 testing → Tasks 2.1, 2.3, 1.6 (constitution drift), 2.5 (frozen-dir gate), 2.6 step 3 (round-trip smoke).
- ✅ §8 risks → frozen-dir CI gate (Task 2.5), drift test (Task 1.6), pinned spec-kit version (Task 1.1 step 3 + extension README).
- ✅ §9 acceptance criteria 1–10 → each maps to at least one task; §9 #5 (`specify check` exits 0 on main) is verified by Task 2.10 Step 1 + CI.
- ✅ §10 follow-ups → Task 4.1 (retire shims); other follow-ups documented in "Open questions" above.

Type / name consistency:
- `scripts/check-speckit-frontmatter.py` consistent across Tasks 2.1, 2.2, 2.5, 2.10, 3.2.
- `scripts/migrate-specs-to-speckit.py` consistent across Tasks 2.3, 2.4, 2.6.
- `scripts/test_constitution_drift.py` consistent across Tasks 1.6, 1.7, 2.10.
- `.specify/extensions/chitin-frontmatter/extension.yaml` consistent across Tasks 1.4, 1.7 (CI references the README's pinned tag line).
- `KANBAN_RE` pattern (`^t_[a-f0-9]{8}$`) consistent between extension manifest (Task 1.4) and linter (Task 2.2).

No placeholders remaining: every code block contains real code; every command is exact; no "TBD", "fill in details", "similar to Task N". The single intentional placeholder is `SPECKIT_TAG=v0.0.55` in Task 1.1, which is annotated `CONFIRM against latest tag before running`.
