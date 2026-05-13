# Regression-gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `scripts/regression-gate.sh` (an aggregator that runs every registered `scripts/check-*.{sh,py}` invariant against the current tree), wire it into CI as a required check, extend `swarm/bin/clawta-pr-lifecycle` to rerun it on the PR's merged-state tree before auto-merge, adopt the existing front-matter + INDEX-drift checks into the registry, and ship a runbook for invariant authors and operators.

**Architecture:** A pure-shell aggregator discovers invariant scripts by filename glob (`check-*.{sh,py}` = gating, `warn-*.{sh,py}` = informational) at the top level of `scripts/`. Each invariant is a standalone executable with a thin contract: exit `0` = preserved, `1` = broken, `≥2` = tool error; stdout is the diagnostic. The aggregator runs every gate (no short-circuit) under a 30s per-invariant `timeout`, then prints a summary. CI runs the aggregator inside the existing `test` job; `clawta-pr-lifecycle` reruns it against the PR's merged-state tree to close the stale-CI race, posting a comment + reassigning the kanban ticket on failure. False positives go through per-invariant `<name>.allow` files — no per-PR `/bypass` magic.

**Tech Stack:** bash 5, python3 (stdlib `unittest`), GitHub Actions, gh CLI, git worktrees.

**Spec:** `docs/superpowers/specs/2026-05-13-regression-gate.md` (merged via PR #584, commit `c3be51c`).

**Kanban:** `t_ac6da121` (triage / red / P60).

---

## File structure

| File | Action | Role |
|---|---|---|
| `scripts/regression-gate.sh` | NEW | The aggregator runner. |
| `scripts/check-spec-index-sync.sh` | NEW | Thin wrapper around `python3 scripts/regen-spec-index.py --check` so the existing INDEX drift check joins the registry uniformly. |
| `swarm/tests/test_regression_gate.py` | NEW | Aggregator behavior tests (9 cases). |
| `swarm/tests/test_clawta_pr_lifecycle_regression_gate.py` | NEW | Tests for the new `run_regression_gate` helper + `classify()` integration. |
| `swarm/bin/clawta-pr-lifecycle` | MODIFY | Add `run_regression_gate()` helper; extend `classify()` to call it and set `gate_status` + `gate_diagnostic` fields; post comment + reassign ticket on failure inside `run_once()`. |
| `.github/workflows/ci.yml` | MODIFY | Remove the PR-#581 standalone steps for `check-spec-frontmatter.py` and `regen-spec-index.py --check`; add a single `Regression gate` step that runs the aggregator. |
| `docs/runbooks/regression-gate.md` | NEW | Operator runbook — invariant-author contract, override workflow, one-time branch-protection setup. |

**Naming note for the implementer:** the spec text refers to "`swarm/bin/clawta-swarm-pr-owner`" (the SDLC architecture diagram's label). The actual file is **`swarm/bin/clawta-pr-lifecycle`**. The diagram label and the filename diverged; the file is the source of truth.

---

## Tasks

### Task 1: Scaffold aggregator + empty-registry test

**Files:**
- Create: `scripts/regression-gate.sh`
- Create: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Write the failing test for empty-registry behavior**

Create `swarm/tests/test_regression_gate.py`:

```python
#!/usr/bin/env python3
"""Behavior tests for scripts/regression-gate.sh."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
AGGREGATOR_SRC = REPO_ROOT / "scripts" / "regression-gate.sh"


def make_sandbox() -> Path:
    """Build a throwaway tree with a `scripts/` dir + a copy of the
    aggregator, then return the tree root. Callers add stub invariants
    into <tree>/scripts/ before running the aggregator."""
    tmp = Path(tempfile.mkdtemp(prefix="regression-gate-test-"))
    (tmp / "scripts").mkdir()
    shutil.copy(AGGREGATOR_SRC, tmp / "scripts" / "regression-gate.sh")
    (tmp / "scripts" / "regression-gate.sh").chmod(0o755)
    return tmp


def run_aggregator(sandbox: Path, timeout: int = 60) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["bash", "scripts/regression-gate.sh"],
        cwd=sandbox,
        capture_output=True,
        text=True,
        timeout=timeout,
    )


class EmptyRegistryTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def test_empty_registry_exits_zero(self) -> None:
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("All 0 invariants preserved", result.stdout)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest swarm.tests.test_regression_gate -v` from repo root.
Expected: FAIL — `scripts/regression-gate.sh` does not exist (`FileNotFoundError` or `bash: scripts/regression-gate.sh: No such file or directory`).

- [ ] **Step 3: Write the minimal aggregator**

Create `scripts/regression-gate.sh`:

```bash
#!/usr/bin/env bash
# regression-gate — run every registered invariant against the current tree.
# Exit 0 iff every check-*.{sh,py} passes; exit 1 on any failure.
# warn-*.{sh,py} run informationally and never affect the exit code.
#
# Spec: docs/superpowers/specs/2026-05-13-regression-gate.md
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mapfile -t gates < <(find scripts -maxdepth 1 -type f \
    \( -name 'check-*.sh' -o -name 'check-*.py' \) | sort)
mapfile -t warns < <(find scripts -maxdepth 1 -type f \
    \( -name 'warn-*.sh'  -o -name 'warn-*.py'  \) | sort)

PER_INVARIANT_TIMEOUT="${REGRESSION_GATE_TIMEOUT:-30}"

run_one() {
    local s="$1"
    echo "── $s ──"
    case "$s" in
        *.py) timeout "$PER_INVARIANT_TIMEOUT" python3 "$s" ;;
        *)    timeout "$PER_INVARIANT_TIMEOUT" bash    "$s" ;;
    esac
}

declare -A rc
fails=0
for s in "${gates[@]}"; do
    run_one "$s"; r=$?
    rc["$s"]=$r
    [ "$r" -eq 0 ] || fails=$((fails+1))
done
for s in "${warns[@]}"; do run_one "$s" || true; done

echo
echo "═══ regression-gate summary ═══"
for s in "${gates[@]}"; do
    [ "${rc[$s]}" -eq 0 ] && tag=PASS || tag=FAIL
    printf "  %-5s  %s\n" "$tag" "$s"
done

if [ "$fails" -gt 0 ]; then
    echo
    echo "$fails/${#gates[@]} invariant(s) broken."
    echo "False positive? Add an entry to scripts/<name>.allow with a # reason."
    echo "Spec: docs/superpowers/specs/2026-05-13-regression-gate.md"
    exit 1
fi
echo "All ${#gates[@]} invariants preserved."
exit 0
```

Make executable:

```bash
chmod +x scripts/regression-gate.sh
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest swarm.tests.test_regression_gate -v` from repo root.
Expected: PASS — `test_empty_registry_exits_zero`.

- [ ] **Step 5: Commit**

```bash
git add scripts/regression-gate.sh swarm/tests/test_regression_gate.py
git commit -m "feat(regression-gate): scaffold aggregator with empty-registry behavior"
```

---

### Task 2: Aggregator handles a passing invariant

**Files:**
- Modify: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Add the passing-invariant test**

Append a new test class to `swarm/tests/test_regression_gate.py` (after `EmptyRegistryTests`):

```python
class PassingInvariantTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_single_passing_invariant(self) -> None:
        self._write_check("ok", "#!/usr/bin/env bash\nexit 0\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("PASS", result.stdout)
        self.assertIn("check-ok.sh", result.stdout)
        self.assertIn("All 1 invariants preserved", result.stdout)
```

- [ ] **Step 2: Run test**

Run: `python3 -m unittest swarm.tests.test_regression_gate.PassingInvariantTests -v`
Expected: PASS — the Task-1 aggregator already handles this correctly.

- [ ] **Step 3: Commit**

```bash
git add swarm/tests/test_regression_gate.py
git commit -m "test(regression-gate): single passing invariant"
```

---

### Task 3: Aggregator surfaces a failing invariant + exits 1

**Files:**
- Modify: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Add the failing-invariant test**

Append a new test class:

```python
class FailingInvariantTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_single_failing_invariant(self) -> None:
        self._write_check("broken",
            "#!/usr/bin/env bash\necho 'violation: thing X broke'\nexit 1\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 1, msg=result.stdout + result.stderr)
        self.assertIn("FAIL", result.stdout)
        self.assertIn("check-broken.sh", result.stdout)
        self.assertIn("1/1 invariant(s) broken", result.stdout)
        # Diagnostic visible in output:
        self.assertIn("violation: thing X broke", result.stdout)
```

- [ ] **Step 2: Run test**

Run: `python3 -m unittest swarm.tests.test_regression_gate.FailingInvariantTests -v`
Expected: PASS — the Task-1 aggregator handles this correctly (exit 1, FAIL marker, summary line).

- [ ] **Step 3: Commit**

```bash
git add swarm/tests/test_regression_gate.py
git commit -m "test(regression-gate): single failing invariant"
```

---

### Task 4: Aggregator runs every invariant — no short-circuit

**Files:**
- Modify: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Add the no-short-circuit test**

Append a new test class:

```python
class NoShortCircuitTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_mixed_pass_fail_pass_all_run(self) -> None:
        # Three invariants alphabetically: a-pass, b-fail, c-pass.
        # If aggregator short-circuits on b, c-pass never runs.
        self._write_check("a-pass", "#!/usr/bin/env bash\necho 'a ran'\nexit 0\n")
        self._write_check("b-fail", "#!/usr/bin/env bash\necho 'b ran'\nexit 1\n")
        self._write_check("c-pass", "#!/usr/bin/env bash\necho 'c ran'\nexit 0\n")

        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 1)
        # All three ran:
        self.assertIn("a ran", result.stdout)
        self.assertIn("b ran", result.stdout)
        self.assertIn("c ran", result.stdout)
        # Summary lists all three with correct tags:
        self.assertIn("PASS", result.stdout)
        self.assertIn("FAIL", result.stdout)
        self.assertIn("1/3 invariant(s) broken", result.stdout)
```

- [ ] **Step 2: Run test**

Run: `python3 -m unittest swarm.tests.test_regression_gate.NoShortCircuitTests -v`
Expected: PASS — the aggregator iterates over the gate list and accumulates without exiting early.

- [ ] **Step 3: Commit**

```bash
git add swarm/tests/test_regression_gate.py
git commit -m "test(regression-gate): no short-circuit on first failure"
```

---

### Task 5: Tool-error exit codes (≥2) count as failure

**Files:**
- Modify: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Add the tool-error test**

Append a new test class:

```python
class ToolErrorTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_tool_error_counts_as_failure(self) -> None:
        # Exit code >= 2 indicates a tool error (script crashed); aggregator
        # must treat this as a failure, not silently pass.
        self._write_check("crash",
            "#!/usr/bin/env bash\necho 'crash'\nexit 2\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 1, msg=result.stdout + result.stderr)
        self.assertIn("FAIL", result.stdout)
        self.assertIn("check-crash.sh", result.stdout)
```

- [ ] **Step 2: Run test**

Run: `python3 -m unittest swarm.tests.test_regression_gate.ToolErrorTests -v`
Expected: PASS — the aggregator's `[ "$r" -eq 0 ] || fails=$((fails+1))` treats any non-zero exit as failure.

- [ ] **Step 3: Commit**

```bash
git add swarm/tests/test_regression_gate.py
git commit -m "test(regression-gate): tool-error exit codes (>=2) count as failure"
```

---

### Task 6: 30-second per-invariant timeout

**Files:**
- Modify: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Add the timeout test**

Append a new test class:

```python
class TimeoutTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_timeout_kills_hung_invariant(self) -> None:
        # Use a 2s override so the test itself runs fast.
        self._write_check("hang",
            "#!/usr/bin/env bash\nsleep 5\nexit 0\n")
        env = dict(os.environ, REGRESSION_GATE_TIMEOUT="2")
        result = subprocess.run(
            ["bash", "scripts/regression-gate.sh"],
            cwd=self.sandbox,
            capture_output=True, text=True,
            env=env,
            timeout=30,
        )
        self.assertEqual(result.returncode, 1, msg=result.stdout + result.stderr)
        self.assertIn("FAIL", result.stdout)
        self.assertIn("check-hang.sh", result.stdout)
```

- [ ] **Step 2: Run test**

Run: `python3 -m unittest swarm.tests.test_regression_gate.TimeoutTests -v`
Expected: PASS — `timeout 2 bash check-hang.sh` exits 124 (killed), which counts as failure.

- [ ] **Step 3: Commit**

```bash
git add swarm/tests/test_regression_gate.py
git commit -m "test(regression-gate): per-invariant timeout kills hung scripts"
```

---

### Task 7: warn-* scripts run but never fail the gate

**Files:**
- Modify: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Add the warn-only test**

Append a new test class:

```python
class WarnOnlyTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write(self, prefix: str, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"{prefix}-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_warn_exit_one_does_not_fail_aggregator(self) -> None:
        # warn-drift exits 1 (legacy worktrees still present); aggregator
        # surfaces the output but exit code stays 0.
        self._write("warn", "drift",
            "#!/usr/bin/env bash\necho 'WARN: 8 legacy worktrees still in tree'\nexit 1\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("WARN: 8 legacy worktrees still in tree", result.stdout)

    def test_warn_does_not_appear_in_gate_summary(self) -> None:
        # Summary table lists check-* gates only, not warn-*.
        self._write("check", "ok", "#!/usr/bin/env bash\nexit 0\n")
        self._write("warn",  "info", "#!/usr/bin/env bash\necho 'info'\nexit 0\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0)
        # 'check-ok.sh' appears in the summary; 'warn-info.sh' should NOT.
        summary_section = result.stdout.split("═══ regression-gate summary ═══", 1)[-1]
        self.assertIn("check-ok.sh", summary_section)
        self.assertNotIn("warn-info.sh", summary_section)
```

- [ ] **Step 2: Run test**

Run: `python3 -m unittest swarm.tests.test_regression_gate.WarnOnlyTests -v`
Expected: PASS — aggregator runs warn-* with `|| true`, summary loop iterates only over `gates[]`.

- [ ] **Step 3: Commit**

```bash
git add swarm/tests/test_regression_gate.py
git commit -m "test(regression-gate): warn-* informational, excluded from summary"
```

---

### Task 8: Glob discovery — only `check-*.{sh,py}` and `warn-*.{sh,py}` at top level

**Files:**
- Modify: `swarm/tests/test_regression_gate.py`

- [ ] **Step 1: Add the discovery hygiene test**

Append a new test class:

```python
class DiscoveryHygieneTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write(self, relpath: str, body: str) -> None:
        full = self.sandbox / relpath
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(body)
        full.chmod(0o755)

    def test_wrong_extension_not_invoked(self) -> None:
        # check-foo.txt is NOT a .sh / .py file; must not be invoked.
        self._write("scripts/check-foo.txt",
            "#!/usr/bin/env bash\necho 'WAS_RUN'\nexit 1\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0)  # empty registry
        self.assertNotIn("WAS_RUN", result.stdout)

    def test_subdirectory_not_recursed(self) -> None:
        # Only top-level scripts/ matches; subdirectories are NOT recursed.
        self._write("scripts/nested/check-foo.sh",
            "#!/usr/bin/env bash\necho 'WAS_RUN'\nexit 1\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0)
        self.assertNotIn("WAS_RUN", result.stdout)

    def test_allow_file_not_invoked_as_invariant(self) -> None:
        # foo.allow is allowlist data, not an invariant — must not be invoked.
        self._write("scripts/governance-boundary.allow",
            "some/path # reason here\n")
        # An actual gate is needed to give the aggregator something to do:
        self._write("scripts/check-real.sh", "#!/usr/bin/env bash\nexit 0\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0)
        self.assertNotIn("governance-boundary.allow", result.stdout)
        self.assertIn("check-real.sh", result.stdout)
```

- [ ] **Step 2: Run test**

Run: `python3 -m unittest swarm.tests.test_regression_gate.DiscoveryHygieneTests -v`
Expected: PASS — `find scripts -maxdepth 1 -type f \( -name 'check-*.sh' -o -name 'check-*.py' \)` excludes wrong extensions, subdirs, and `.allow` files.

- [ ] **Step 3: Commit**

```bash
git add swarm/tests/test_regression_gate.py
git commit -m "test(regression-gate): discovery scope (top-level .sh/.py only)"
```

---

### Task 9: Inaugural registry — `check-spec-index-sync.sh` wrapper

**Files:**
- Create: `scripts/check-spec-index-sync.sh`

- [ ] **Step 1: Create the wrapper**

```bash
cat > scripts/check-spec-index-sync.sh <<'EOF'
#!/usr/bin/env bash
# check-spec-index-sync — invariant: docs/superpowers/specs/INDEX.md is
# in sync with the regen-spec-index.py generator output.
#
# Part of the regression-gate registry — exit 0 = preserved, 1 = drift.
exec python3 "$(dirname "$0")/regen-spec-index.py" --check
EOF
chmod +x scripts/check-spec-index-sync.sh
```

- [ ] **Step 2: Verify on the current tree**

Run: `bash scripts/check-spec-index-sync.sh`
Expected: exit 0 with `regen-spec-index --check: docs/superpowers/specs/INDEX.md is up to date (N specs)`.

- [ ] **Step 3: Verify discovered by the aggregator on the real tree**

Run: `bash scripts/regression-gate.sh`
Expected: exit 0; summary lists `PASS  scripts/check-spec-frontmatter.py` and `PASS  scripts/check-spec-index-sync.sh`; total `All 2 invariants preserved.`

- [ ] **Step 4: Commit**

```bash
git add scripts/check-spec-index-sync.sh
git commit -m "feat(regression-gate): inaugural registry — check-spec-index-sync wrapper"
```

---

### Task 10: CI wiring — remove PR-#581 standalone steps, add `Regression gate`

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Locate the PR-#581 block**

Run: `grep -n "Spec front-matter\|check-spec-frontmatter\|regen-spec-index" .github/workflows/ci.yml`
Expected: lines around 140-149 (a single `Spec front-matter + INDEX.md drift check` step that runs both Python scripts).

- [ ] **Step 2: Replace the block**

Edit `.github/workflows/ci.yml` to **remove** this block (lines 140-149 in the merged tree):

```yaml
      - name: Spec front-matter + INDEX.md drift check
        run: |
          # Enforces the lifecycle schema from
          # docs/superpowers/specs/2026-05-13-spec-lifecycle-metadata.md:
          # every spec has a 6-field YAML front-matter block, status is
          # in the closed enum, and the committed INDEX.md matches what
          # the generator produces. See docs/runbooks/spec-lifecycle.md.
          python3 -m pip install --quiet --user pyyaml
          python3 scripts/check-spec-frontmatter.py
          python3 scripts/regen-spec-index.py --check
```

And **replace** with this block in the same location:

```yaml
      - name: Regression gate
        run: |
          # Runs every registered invariant in scripts/check-*.{sh,py}
          # against the merged-state tree. Hard-block: failure = CI failure.
          # See docs/superpowers/specs/2026-05-13-regression-gate.md
          # and docs/runbooks/regression-gate.md.
          python3 -m pip install --quiet --user pyyaml
          bash scripts/regression-gate.sh
```

The `pip install pyyaml` line stays — `check-spec-frontmatter.py` and `regen-spec-index.py` still need it, and they run via the aggregator now.

- [ ] **Step 3: Verify locally**

Run: `bash scripts/regression-gate.sh` from repo root.
Expected: exit 0, summary lists both inaugural invariants as PASS.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(regression-gate): single chokepoint replaces PR #581 standalone steps"
```

---

### Task 11: Lifecycle helper — `run_regression_gate(pr_number, head)`

**Files:**
- Modify: `swarm/bin/clawta-pr-lifecycle`

This task adds the helper function but does NOT yet call it from `classify()` — that's Task 12. Two-task split keeps each test surface small.

- [ ] **Step 1: Read the existing imports + run helpers**

Read `swarm/bin/clawta-pr-lifecycle` lines 1-60 to confirm what's imported (`subprocess`, `tempfile`, `pathlib`, etc.) and the shape of the existing `run()` helper at line 56.

- [ ] **Step 2: Add `tempfile` + `shutil` imports if missing**

If not already imported near the top of the file, add:

```python
import shutil
import tempfile
```

- [ ] **Step 3: Add the helper function**

Insert AFTER the existing `run()` helper (around line 65, before `gh_json`):

```python
GATE_PER_INVARIANT_TIMEOUT = 30
GATE_TOTAL_TIMEOUT = 180  # All invariants combined.


def run_regression_gate(pr_number: int, head: str) -> tuple[int, str]:
    """Fetch the PR's merged-state tree into a temp worktree and run
    scripts/regression-gate.sh against it.

    Returns:
        (rc, stdout_tail) where:
          rc = 0    → all invariants passed
          rc = 1    → at least one invariant broken (invariant fail)
          rc >= 2   → aggregator/tool error (fail-closed; do not merge)

        stdout_tail is the last ~40 lines of aggregator output, suitable
        for posting as a kanban comment.
    """
    workdir = tempfile.mkdtemp(prefix=f"regression-gate-pr-{pr_number}-")
    try:
        # Worktree the PR's merge ref into the temp dir. --detach because
        # we don't need a branch and want to avoid name collisions.
        wt = run(
            ["git", "worktree", "add", "--detach", workdir,
             f"refs/pull/{pr_number}/merge"],
            timeout=60,
        )
        if wt.returncode != 0:
            return (2, f"git worktree add failed:\n{wt.stderr or wt.stdout}")

        # Run the aggregator against the merged-state tree.
        result = subprocess.run(
            ["bash", "scripts/regression-gate.sh"],
            cwd=workdir,
            capture_output=True,
            text=True,
            timeout=GATE_TOTAL_TIMEOUT,
            env=dict(
                os.environ,
                REGRESSION_GATE_TIMEOUT=str(GATE_PER_INVARIANT_TIMEOUT),
            ),
        )
        tail = "\n".join(result.stdout.splitlines()[-40:])
        return (result.returncode, tail)
    except subprocess.TimeoutExpired as e:
        return (2, f"aggregator total timeout ({GATE_TOTAL_TIMEOUT}s):\n{e}")
    except Exception as e:  # noqa: BLE001 — fail-closed on any orchestration error
        return (2, f"regression-gate orchestration error:\n{e}")
    finally:
        # Always tear down the worktree; ignore errors (best-effort cleanup).
        subprocess.run(
            ["git", "worktree", "remove", "--force", workdir],
            capture_output=True, timeout=30,
        )
        shutil.rmtree(workdir, ignore_errors=True)
```

The `import os` needs to be present near the top of the file; if not already imported, add it.

- [ ] **Step 4: Smoke-run the helper from a Python REPL**

Run from repo root:

```bash
python3 -c "
import importlib.machinery, importlib.util
loader = importlib.machinery.SourceFileLoader('clawta_pr_lifecycle', 'swarm/bin/clawta-pr-lifecycle')
spec = importlib.util.spec_from_loader('clawta_pr_lifecycle', loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)
# Quick smoke: the function should exist and be callable.
print('helper exists:', callable(mod.run_regression_gate))
"
```

Expected: prints `helper exists: True`. No exceptions during module load (which would indicate a syntax error in the file).

- [ ] **Step 5: Commit**

```bash
git add swarm/bin/clawta-pr-lifecycle
git commit -m "feat(clawta-pr-lifecycle): add run_regression_gate helper"
```

---

### Task 12: Lifecycle integration — extend `classify()` with the gate

**Files:**
- Modify: `swarm/bin/clawta-pr-lifecycle`

This task wires the helper into `classify()` and the `run_once()` action loop so a failing gate (a) blocks `auto_merge_ready` and (b) leaves a kanban comment + reassigns the ticket on invariant failure (and just a comment on tool error).

- [ ] **Step 1: Read the current `classify()` body**

Open `swarm/bin/clawta-pr-lifecycle` and read lines 191-244 (the `classify()` function). Note in particular:

- Line 207-215: the `auto_merge_ready` boolean composition (the current gates a–e).
- Line 216-225: the `action` string assignment (`ready-to-merge`, `needs-rebase`, `needs-fix`, `wait`).
- Line 226+: the return dict.

- [ ] **Step 2: Modify `classify()` to call the gate**

In `swarm/bin/clawta-pr-lifecycle`, replace the existing `auto_merge_ready = (...)` block (around line 207) and the existing `action = ...` block (around line 216) with the following.

Find this:

```python
    auto_merge_ready = (
        base_ready
        and check_state == "pass"
        and bool(ticket)
        and status == "in_progress"
        and bool(review)
        and review.get("verdict") == "APPROVE"
        and is_review_nonblocking(review, head, require_current_head=True)
    )
    if merged:
        action = "mark-done" if ticket and status != "done" else "merged-noop"
    elif base_ready:
        action = "ready-to-merge"
    elif pr.get("mergeable") == "CONFLICTING":
        action = "needs-rebase"
    elif review and review.get("verdict") == "REQUEST_CHANGES":
        action = "needs-fix"
    else:
        action = "wait"
```

Replace with:

```python
    # Gates a–e: existing shape checks (cheap; no subprocess).
    gates_a_through_e = (
        base_ready
        and check_state == "pass"
        and bool(ticket)
        and status == "in_progress"
        and bool(review)
        and review.get("verdict") == "APPROVE"
        and is_review_nonblocking(review, head, require_current_head=True)
    )

    # Gate f: regression-gate. Only invoked if (a–e) all pass — running the
    # gate is expensive (~5-30s worktree + subprocess), so short-circuit.
    gate_status: str = "skipped"
    gate_diagnostic: str = ""
    if gates_a_through_e and not merged:
        gate_rc, gate_diagnostic = run_regression_gate(int(pr["number"]), head)
        if gate_rc == 0:
            gate_status = "pass"
        elif gate_rc == 1:
            gate_status = "fail-invariant"
        else:
            gate_status = "fail-tool"

    auto_merge_ready = gates_a_through_e and gate_status == "pass"

    if merged:
        action = "mark-done" if ticket and status != "done" else "merged-noop"
    elif gate_status == "fail-invariant":
        action = "regression-gate-fail"
    elif gate_status == "fail-tool":
        action = "regression-gate-error"
    elif base_ready:
        action = "ready-to-merge"
    elif pr.get("mergeable") == "CONFLICTING":
        action = "needs-rebase"
    elif review and review.get("verdict") == "REQUEST_CHANGES":
        action = "needs-fix"
    else:
        action = "wait"
```

- [ ] **Step 3: Add gate fields to the `classify()` return dict**

Find the existing return dict (lines 226-243). Add the two new fields:

```python
    return {
        "pr": str(pr["number"]),
        "url": pr.get("url", ""),
        "head": head,
        "branch": pr.get("headRefName", ""),
        "checks": check_state,
        "mergeable": pr.get("mergeable"),
        "review": review.get("verdict") if review else None,
        "ticket": ticket,
        "ticket_status": status,
        "auto_merge_ready": auto_merge_ready,
        "gate_status": gate_status,          # NEW
        "gate_diagnostic": gate_diagnostic,  # NEW
        "action": action,
        "state": pr.get("state"),
        "merged_at": pr.get("mergedAt"),
        "updated_at": pr.get("updatedAt"),
        "merged": merged,
        "is_draft": bool(pr.get("isDraft")),
    }
```

- [ ] **Step 4: Handle the new actions in `run_once()`**

Open `swarm/bin/clawta-pr-lifecycle` and read `run_once()` (around line 329). Locate the per-item action dispatch.

Locate this stretch (it iterates over classified items and acts on each):

```python
    for item in items:
        action = item["action"]
        if action == "mark-done":
            mark_done(item, apply)
        elif action == "ready-to-merge" and auto_merge:
            merge_pr(item, apply)
        # ... existing cases
```

(Exact code may vary; use the `action` field as the discriminator.)

Add two new branches for the gate-fail actions, **before** the existing fall-throughs:

```python
        elif action == "regression-gate-fail":
            # Invariant broken on this head. Comment + reassign ticket; no merge.
            post_regression_gate_comment(item, apply, request_changes=True)
            assign_ticket_to_operator(item, "red", apply)
        elif action == "regression-gate-error":
            # Tool error (aggregator crashed / worktree fetch failed / timeout).
            # Comment only — do NOT reassign; operator must investigate.
            post_regression_gate_comment(item, apply, request_changes=False)
```

- [ ] **Step 5: Add the comment + reassign helpers**

Insert after `escalate_to_operator()` (around line 312):

```python
def post_regression_gate_comment(item: dict[str, Any], apply: bool,
                                 *, request_changes: bool) -> None:
    """Post a comment on the PR explaining the gate result. If
    request_changes is True, the comment is shaped as a request-for-changes
    (broken invariant); otherwise as an evaluation-failure note (tool error)."""
    if not apply:
        return
    tag = "broken invariant" if request_changes else "could not evaluate"
    body = (
        f"<!-- clawta-pr-lifecycle:regression-gate head={item['head']} -->\n"
        f"**Regression gate: {tag}** "
        f"(`gate_status={item.get('gate_status')}`).\n\n"
        f"```\n{item.get('gate_diagnostic', '(no diagnostic)')}\n```\n\n"
        f"See docs/runbooks/regression-gate.md for override and triage."
    )
    run([
        "gh", "pr", "comment", item["pr"], "--repo", REPO,
        "--body", body,
    ], timeout=60, check=False)


def assign_ticket_to_operator(item: dict[str, Any], operator: str,
                              apply: bool) -> None:
    """Reassign the PR's kanban ticket to the operator after a gate failure."""
    ticket = item.get("ticket")
    if not ticket or not apply:
        return
    run([
        "hermes", "kanban", "--board", "chitin",
        "reassign", ticket, operator,
        "--reclaim",
        "--reason", "regression-gate fail-invariant on PR "
                    f"#{item['pr']} head={item['head']}",
    ], timeout=60, check=False)
```

`REPO` is already defined elsewhere in this file (a module-level constant such as `chitinhq/chitin`); use the existing constant.

- [ ] **Step 6: Update `needs_operator_escalation()` to not double-escalate**

The existing function (line 282) treats `auto_merge_ready=False` as a candidate for operator escalation. Now that `regression-gate-fail` posts its own comment + reassignment, exclude it from the generic escalation path. Find this stretch:

```python
def needs_operator_escalation(item: dict[str, Any]) -> bool:
    if not item.get("ticket"):
        return False
    if item.get("state") != "OPEN":
        return False
    if item.get("ticket_status") in {"done", None}:
        return False
    if item.get("auto_merge_ready"):
        return False
    # ... existing fall-through logic
```

Add the two new exclusions before `# existing fall-through logic`:

```python
    if item.get("action") == "regression-gate-fail":
        # Already handled by post_regression_gate_comment + reassign.
        return False
    if item.get("action") == "regression-gate-error":
        # Tool error — operator must investigate, but skip the generic
        # escalation path (it's a tooling issue, not a PR-content issue).
        return False
```

- [ ] **Step 7: Smoke-run module load**

Run from repo root:

```bash
python3 -c "
import importlib.machinery, importlib.util
loader = importlib.machinery.SourceFileLoader('m', 'swarm/bin/clawta-pr-lifecycle')
spec = importlib.util.spec_from_loader('m', loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)
print('loaded OK; new helpers:',
      callable(mod.run_regression_gate),
      callable(mod.post_regression_gate_comment),
      callable(mod.assign_ticket_to_operator))
"
```

Expected: `loaded OK; new helpers: True True True`. Any `SyntaxError` or `NameError` → fix before continuing.

- [ ] **Step 8: Commit**

```bash
git add swarm/bin/clawta-pr-lifecycle
git commit -m "feat(clawta-pr-lifecycle): regression-gate as 6th auto-merge gate

Wires run_regression_gate() into classify() (short-circuited on a-e
failures), adds two new actions (regression-gate-fail /
regression-gate-error), and posts a structured comment + reassigns
the ticket on invariant break. Tool errors only comment, never reassign."
```

---

### Task 13: Tests for the lifecycle integration

**Files:**
- Create: `swarm/tests/test_clawta_pr_lifecycle_regression_gate.py`

- [ ] **Step 1: Write the test file**

Create `swarm/tests/test_clawta_pr_lifecycle_regression_gate.py`:

```python
#!/usr/bin/env python3
"""Tests for the regression-gate integration in clawta-pr-lifecycle."""

from __future__ import annotations

import importlib.machinery
import importlib.util
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "bin" / "clawta-pr-lifecycle"


def load_module():
    loader = importlib.machinery.SourceFileLoader("clawta_pr_lifecycle", str(SCRIPT))
    spec = importlib.util.spec_from_loader("clawta_pr_lifecycle", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


def base_pr(**overrides) -> dict:
    """A canonical 'ready-to-merge' PR dict from gh pr list JSON output."""
    pr = {
        "number": 999,
        "url": "https://github.com/chitinhq/chitin/pull/999",
        "title": "test pr",
        "headRefName": "feature/x",
        "headRefOid": "abc123",
        "baseRefName": "main",
        "state": "OPEN",
        "mergeable": "MERGEABLE",
        "mergedAt": None,
        "body": "",
        "isDraft": False,
        "updatedAt": "2026-05-13T20:00:00Z",
    }
    pr.update(overrides)
    return pr


def approve_comment(head: str = "abc123") -> dict:
    """A canonical APPROVE review comment from gh pr comments."""
    return {
        "user": {"login": "jpleva91"},
        "body": f"<!-- clawta-reviewer:v1 head={head} -->\n**Verdict:** APPROVE",
    }


class GateShortCircuitTests(unittest.TestCase):
    def test_gate_skipped_when_base_gates_fail(self) -> None:
        """If (a-e) fail, gate is NOT invoked (expensive subprocess avoided)."""
        m = load_module()
        # PR is draft → base_ready false → gate skipped.
        pr = base_pr(isDraft=True)
        with mock.patch.object(m, "run_regression_gate") as gate, \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value={"id": "t_x", "status": "in_progress"}):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "skipped")
        self.assertFalse(result["auto_merge_ready"])
        gate.assert_not_called()


class GatePassTests(unittest.TestCase):
    def test_gate_pass_sets_auto_merge_ready(self) -> None:
        m = load_module()
        pr = base_pr()
        with mock.patch.object(m, "run_regression_gate",
                               return_value=(0, "All 2 invariants preserved.")) as gate, \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value={"id": "t_x", "status": "in_progress"}):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "pass")
        self.assertTrue(result["auto_merge_ready"])
        self.assertEqual(result["action"], "ready-to-merge")
        gate.assert_called_once()


class GateFailInvariantTests(unittest.TestCase):
    def test_gate_fail_invariant_sets_action_and_diagnostic(self) -> None:
        m = load_module()
        pr = base_pr()
        diagnostic = "1/2 invariant(s) broken.\nFAIL  scripts/check-foo.sh"
        with mock.patch.object(m, "run_regression_gate",
                               return_value=(1, diagnostic)), \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value={"id": "t_x", "status": "in_progress"}):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "fail-invariant")
        self.assertEqual(result["gate_diagnostic"], diagnostic)
        self.assertFalse(result["auto_merge_ready"])
        self.assertEqual(result["action"], "regression-gate-fail")


class GateFailToolTests(unittest.TestCase):
    def test_gate_fail_tool_sets_separate_action(self) -> None:
        m = load_module()
        pr = base_pr()
        with mock.patch.object(m, "run_regression_gate",
                               return_value=(2, "git worktree add failed: ...")), \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value={"id": "t_x", "status": "in_progress"}):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "fail-tool")
        self.assertFalse(result["auto_merge_ready"])
        self.assertEqual(result["action"], "regression-gate-error")


class EscalationExclusionTests(unittest.TestCase):
    def test_regression_gate_fail_not_escalated_generically(self) -> None:
        """needs_operator_escalation must skip regression-gate-fail items
        because post_regression_gate_comment already handles them."""
        m = load_module()
        item = {
            "ticket": "t_x",
            "state": "OPEN",
            "ticket_status": "in_progress",
            "auto_merge_ready": False,
            "action": "regression-gate-fail",
        }
        self.assertFalse(m.needs_operator_escalation(item))

    def test_regression_gate_error_not_escalated_generically(self) -> None:
        m = load_module()
        item = {
            "ticket": "t_x",
            "state": "OPEN",
            "ticket_status": "in_progress",
            "auto_merge_ready": False,
            "action": "regression-gate-error",
        }
        self.assertFalse(m.needs_operator_escalation(item))


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run the tests**

Run: `python3 -m unittest swarm.tests.test_clawta_pr_lifecycle_regression_gate -v`
Expected: all 6 tests PASS.

- [ ] **Step 3: Run the whole test suite to ensure no regressions**

Run: `python3 -m unittest discover -s swarm/tests -v`
Expected: every test (existing + new) passes.

- [ ] **Step 4: Commit**

```bash
git add swarm/tests/test_clawta_pr_lifecycle_regression_gate.py
git commit -m "test(clawta-pr-lifecycle): regression-gate integration tests"
```

---

### Task 14: Operator runbook

**Files:**
- Create: `docs/runbooks/regression-gate.md`

- [ ] **Step 1: Write the runbook**

Create `docs/runbooks/regression-gate.md`:

```markdown
# Regression-gate runbook

How the regression-gate works, how to author an invariant, how to
override a false positive, and the one-time operator setup.

Schema and design in
[`docs/superpowers/specs/2026-05-13-regression-gate.md`](../superpowers/specs/2026-05-13-regression-gate.md).

## What the gate does

For every PR, every invariant in `scripts/check-*.{sh,py}` runs against
the PR's merged-state tree. If any invariant exits non-zero, the gate
fails and the PR cannot auto-merge until the failure is resolved.

The gate runs in two places:

1. **CI** — as part of the `test` job in `.github/workflows/ci.yml`.
   This is the binding gate visible to humans on the PR page.
2. **`swarm/bin/clawta-pr-lifecycle`** — reruns the gate against the
   PR's merge-ref tree as the 6th auto-merge check, after CI passes,
   to close the stale-CI race (commit pushed after last CI run).

Both venues run the same `scripts/regression-gate.sh` script.

## Invariant author contract

A file under `scripts/` is an invariant iff it matches the glob
`scripts/check-*.{sh,py}` (top level only — no recursion). To add a
new invariant:

1. Create `scripts/check-<your-invariant>.sh` (or `.py`).
2. Make it executable: `chmod +x scripts/check-<name>.sh`.
3. Follow the contract:

   | Concern | Contract |
   |---|---|
   | Exit code | `0` = preserved · `1` = broken · `≥ 2` = tool error (treated as broken) |
   | Stdout | One human-readable line per violation; ideally `path:line  reason` |
   | Stderr | Tool errors only |
   | Input | Runs from the repo root; assume merged-state tree is checked out |
   | Allowlist | Optional `scripts/<name>.allow` next to the script; format `<path-or-pattern> # <reason>`; load it inside your script |
   | Timeout | 30 seconds per invariant (configurable via `REGRESSION_GATE_TIMEOUT` env var for local debugging) |

4. **Test it locally** by running:

   ```bash
   bash scripts/regression-gate.sh
   ```

   Your new check should appear in the summary.

### warn-* — invariants in a soak period

If your invariant is not yet ready to be gating (e.g., legacy data
still violates it during a migration window), name it
`scripts/warn-<name>.{sh,py}` instead. Warn scripts follow the same
contract but the aggregator **ignores** their exit code — their
output surfaces but never fails the gate. Promote `warn-` → `check-`
by renaming the file when ready.

## Overriding a false positive

The gate does not have a per-PR override mechanism. False positives
are addressed by editing the invariant's allowlist file:

1. Open `scripts/<invariant-name>.allow`. Create it if it doesn't
   exist.
2. Add a line: `<path-or-pattern> # <reason>`. The reason is
   mandatory — invariant linters typically reject entries missing
   it.
3. Commit the allowlist change in the same PR (or a follow-up PR).
4. Push; CI reruns; the gate passes.

This is by design — bypasses are auditable in git history and force
a written reason.

## When the gate fails in clawta-pr-lifecycle

The `clawta-pr-lifecycle` cron reruns the gate against the PR's
merge ref. Two failure modes:

| Aggregator exit | clawta action |
|---|---|
| `1` — invariant broken | Posts a structured comment on the PR; reassigns the kanban ticket to `red`. PR stays open; no merge. |
| `≥ 2` — tool error (aggregator crashed, worktree fetch failed, timeout) | Posts a comment flagging "could not evaluate"; **does NOT reassign**. Operator must investigate (it's a tooling issue, not a PR-content issue). |

## One-time operator setup (branch protection)

The `Regression gate` step rides inside the existing `test` job in
`.github/workflows/ci.yml`. Make sure your branch protection rule
on `main` includes:

1. Repo settings → Branches → `main` rule → Required status checks.
2. Add or confirm `test` is in the required list.
3. Enable **"Require branches to be up to date before merging"** so
   that a stale PR re-runs CI (and the gate) against the latest
   `main`.

This is a one-time settings change, not a code change.

## Inaugural registry (Day-0)

When this runbook ships, the registry contains two invariants:

| Script | Enforces |
|---|---|
| `scripts/check-spec-frontmatter.py` | Every spec under `docs/superpowers/specs/**` has a valid 6-field YAML front-matter block. See `docs/runbooks/spec-lifecycle.md`. |
| `scripts/check-spec-index-sync.sh` | `docs/superpowers/specs/INDEX.md` matches what `regen-spec-index.py` produces. |

The five already-specced audit-response invariants (governance
boundary, kanban-isolation, scripts-classification,
worktree-status, worktree-naming) join the registry as their
implementation PRs land — no coordination required; the aggregator
picks them up by filename match.

## Followups (file as kanban tickets if needed)

- Promote `regression-gate` to its own CI job if check-list
  legibility on the PR page becomes a debugging cost.
- Reimplement the aggregator as `chitin-kernel regression-gate` if
  a dashboard or audit surface needs typed metadata.
- Chain-event logging of gate decisions for the analyzer cron in
  `docs/superpowers/specs/2026-05-12-chitin-dashboard.md` Slice 5.
```

- [ ] **Step 2: Verify it links correctly**

Run: `grep -E "regression-gate|spec-lifecycle" docs/superpowers/README.md`

If `docs/superpowers/README.md` exists and has a runbooks section, optionally add a link:
- Edit `docs/superpowers/README.md` and add a line under the runbook links pointing to `../runbooks/regression-gate.md`.

This step is optional — the runbook is reachable by path; the README link is for discoverability.

- [ ] **Step 3: Commit**

```bash
git add docs/runbooks/regression-gate.md
git commit -m "docs(regression-gate): operator + invariant-author runbook"
```

---

### Task 15: Verify, push, open PR

**Files:**
- No new file changes.

- [ ] **Step 1: Run the full test suite**

Run: `python3 -m unittest discover -s swarm/tests -v`
Expected: every test passes (regression-gate aggregator + lifecycle integration + the two pre-existing files).

- [ ] **Step 2: Run the regression-gate aggregator on the real tree**

Run: `bash scripts/regression-gate.sh`
Expected: exit 0; summary lists both inaugural invariants as PASS.

- [ ] **Step 3: Push the branch**

```bash
git push -u origin <current-branch>
```

(Branch name: whatever the worktree is on, e.g. `worktree-impl-regression-gate`.)

- [ ] **Step 4: Open the PR**

Run:

```bash
gh pr create --repo chitinhq/chitin --base main --head <current-branch> \
  --title "feat(regression-gate): aggregator + clawta-pr-lifecycle integration (t_ac6da121)" \
  --body "$(cat <<'EOF'
## Summary

Implements `docs/superpowers/specs/2026-05-13-regression-gate.md` (PR #584). Closes `t_ac6da121`.

- `scripts/regression-gate.sh` — pure-shell aggregator; discovers `scripts/check-*.{sh,py}` at top level; per-invariant `timeout 30`; exit 0 iff every gate passes.
- `scripts/check-spec-index-sync.sh` — thin wrapper around `regen-spec-index.py --check`, joining the registry uniformly.
- `swarm/bin/clawta-pr-lifecycle` — adds `run_regression_gate()` helper; extends `classify()` to call the gate after (a-e) pass; new actions `regression-gate-fail` and `regression-gate-error` with the structured-comment + reassign-ticket flow.
- `.github/workflows/ci.yml` — single `Regression gate` step replaces the PR #581 standalone steps (single chokepoint).
- `docs/runbooks/regression-gate.md` — operator + invariant-author runbook.
- Day-0 registry: 2 invariants (`check-spec-frontmatter.py` + `check-spec-index-sync.sh`). The five already-specced audit-response invariants will join as their PRs land — zero coordination.

## Tests

- Aggregator behavior tests in `swarm/tests/test_regression_gate.py` (empty registry, passing, failing, no-short-circuit, tool error, timeout, warn-only, discovery hygiene).
- Lifecycle integration tests in `swarm/tests/test_clawta_pr_lifecycle_regression_gate.py` (gate skipped on a-e fail, gate pass → auto-merge, fail-invariant → action + diagnostic, fail-tool → separate action, escalation exclusions).

## Operator action required after merge

Update branch protection on `main`: confirm `test` is in required status checks, and enable "Require branches to be up to date before merging." See `docs/runbooks/regression-gate.md`.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 5: Link PR back to the ticket**

```bash
hermes kanban --board chitin comment t_ac6da121 \
  "Implementation PR opened: <PR_URL>. Ready for review."
```

- [ ] **Step 6: Update the spec's front-matter with the implementation PR**

Once the PR merges, edit `docs/superpowers/specs/2026-05-13-regression-gate.md`:

```yaml
status: implemented
implementation_pr: <PR-number>
effective_to: '<merge-date-YYYY-MM-DD>'
```

Regenerate INDEX.md (`python3 scripts/regen-spec-index.py`) and commit on a follow-up PR. The `check-spec-index-sync` invariant will catch any drift.

- [ ] **Step 7: Close the kanban ticket after merge**

```bash
scripts/kanban-flow done t_ac6da121 \
  --result "PR <#> merged. Aggregator + clawta-pr-lifecycle integration + runbook landed." \
  --author red
```

---

## Spec coverage map

Every section / requirement from `docs/superpowers/specs/2026-05-13-regression-gate.md`:

| Spec section / item | Implemented by |
|---|---|
| `scripts/regression-gate.sh` aggregator | Tasks 1-8 |
| Per-invariant 30s timeout | Task 1 (implementation) + Task 6 (test) |
| `check-*.{sh,py}` discovery, top-level only | Task 1 (impl) + Task 8 (test) |
| `warn-*.{sh,py}` informational, excluded from summary | Task 1 (impl) + Task 7 (test) |
| No-short-circuit (every gate runs even after a failure) | Task 1 (impl) + Task 4 (test) |
| Exit-≥2 = tool error, treated as failure | Task 1 (impl) + Task 5 (test) |
| `scripts/check-spec-index-sync.sh` wrapper | Task 9 |
| CI step + PR #581 redundant-step removal | Task 10 |
| `run_regression_gate(pr_number, head)` helper | Task 11 |
| `classify()` integration + short-circuit on (a-e) fail | Task 12 |
| `regression-gate-fail` action + reassign-ticket | Task 12 + 13 (test) |
| `regression-gate-error` action + comment-only | Task 12 + 13 (test) |
| Operator runbook | Task 14 |
| Branch protection setup | Task 14 (documented as operator action, not a code task) |
| Done-condition checklist verification | Task 15 |

## Out of scope (per spec, deferred to followups)

- Per-PR `/bypass-invariant` comments.
- Promoting `regression-gate` to a separate named CI job.
- `chitin-kernel regression-gate` Go reimplementation.
- Chain-event logging of gate decisions.
- Retrofitting non-spec'd scripts into the `check-*` namespace.

## Effort estimate

S — approximately one day (matches the spec). Distribution:

- Tasks 1-8 (aggregator + tests): ~3 hr.
- Task 9 (wrapper): ~0.1 hr.
- Task 10 (CI rewire): ~0.3 hr.
- Tasks 11-13 (lifecycle integration + tests): ~3-4 hr.
- Task 14 (runbook): ~1 hr.
- Task 15 (verify + PR): ~0.5 hr.
