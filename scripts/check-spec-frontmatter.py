#!/usr/bin/env python3
"""Validate YAML front-matter on every spec file.

Implements the contract from
`docs/superpowers/specs/2026-05-13-spec-lifecycle-metadata.md`.

Every `.md` file under `docs/superpowers/specs/` and
`docs/superpowers/superseded/` (recursively) must:

1. Start with a `---\\n` front-matter block.
2. Contain all six required fields: status, owner, kanban,
   implementation_pr, superseded_by, effective_from, effective_to.
3. Use a valid status enum value.
4. If status == implemented, implementation_pr must be non-null.
5. If status == superseded, superseded_by must point to an existing
   spec path under docs/superpowers/.

Exit 0 on success, 1 on validation failure. INDEX.md and README.md
files are intentionally exempt — they aren't specs.
"""

from __future__ import annotations

import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.stderr.write(
        "check-spec-frontmatter: PyYAML is required.\n"
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

EXEMPT_NAMES = {"INDEX.md", "README.md"}

ROOTS = (
    Path("docs/superpowers/specs"),
    Path("docs/superpowers/superseded"),
)


def parse_frontmatter(path: Path) -> tuple[dict | None, str | None]:
    """Return (data, error). data is the parsed YAML dict or None on error."""
    text = path.read_text(encoding="utf-8")
    if not text.startswith("---\n"):
        return None, "missing front-matter (file must start with `---`)"

    end = text.find("\n---", 4)
    if end == -1:
        return None, "unterminated front-matter (no closing `---`)"

    block = text[4:end]
    try:
        data = yaml.safe_load(block)
    except yaml.YAMLError as e:
        return None, f"YAML parse error: {e}"

    if not isinstance(data, dict):
        return None, "front-matter must be a YAML mapping"

    return data, None


def validate(path: Path) -> list[str]:
    """Return a list of error messages for the given spec path."""
    errors: list[str] = []
    data, err = parse_frontmatter(path)
    if err:
        errors.append(err)
        return errors

    missing = [f for f in REQUIRED_FIELDS if f not in data]
    if missing:
        errors.append(f"missing required fields: {', '.join(missing)}")
        return errors

    status = data.get("status")
    if status not in VALID_STATUSES:
        errors.append(
            f"invalid status {status!r}; must be one of {', '.join(VALID_STATUSES)}"
        )

    if status == "implemented" and data.get("implementation_pr") in (None, ""):
        errors.append(
            "status=implemented requires non-null implementation_pr"
        )

    if status == "superseded":
        target = data.get("superseded_by")
        if not target:
            errors.append("status=superseded requires non-null superseded_by")
        else:
            # Allow path relative to repo root.
            target_path = Path(str(target))
            if not target_path.exists():
                errors.append(
                    f"superseded_by points to missing file: {target}"
                )

    return errors


def iter_specs() -> list[Path]:
    paths: list[Path] = []
    for root in ROOTS:
        if not root.exists():
            continue
        for p in sorted(root.rglob("*.md")):
            if p.name in EXEMPT_NAMES:
                continue
            paths.append(p)
    return paths


def main() -> int:
    specs = iter_specs()
    if not specs:
        sys.stderr.write(
            "check-spec-frontmatter: no spec files found under "
            f"{[str(r) for r in ROOTS]}\n"
        )
        return 1

    failures = 0
    for path in specs:
        errs = validate(path)
        if errs:
            failures += 1
            for e in errs:
                print(f"{path}: {e}")

    if failures:
        print(
            f"\ncheck-spec-frontmatter: {failures}/{len(specs)} file(s) failed",
            file=sys.stderr,
        )
        print(
            "  See docs/superpowers/specs/2026-05-13-spec-lifecycle-metadata.md",
            file=sys.stderr,
        )
        print(
            "  and docs/runbooks/spec-lifecycle.md for the schema.",
            file=sys.stderr,
        )
        return 1

    print(f"check-spec-frontmatter: {len(specs)} spec(s) OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
