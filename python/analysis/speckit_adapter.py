"""Spec-kit/house format adapter (R3, spec 061).

Parses .specify/specs/NNN-slug/spec.md files into UnifiedSpec objects.
Handles the observed variation in spec.md format:

- Title line: "# Spec NNN: Title", "# NNN — Title",
  "# Feature Specification: Title — subtitle", etc.
- Requirements: "### RN — …" or "**RN —** …" headings.
- Acceptance criteria: "**ACN** …" or "### ACN" patterns.
- Boundary cases: "## Boundary cases" section.
- Open questions: "## Open questions" section with "- **QN — …**".
- Slices: "## Slice plan" section with "- **Slice N** — …".
"""
from __future__ import annotations

import os
import re
from pathlib import Path
from typing import Optional

from analysis.unified_spec import (
    AcceptanceCriterion,
    Question,
    Requirement,
    Slice,
    SourceFramework,
    SpecStatus,
    UnifiedSpec,
)


class SpecKitParseError(Exception):
    """Typed error for spec-kit parse failures (boundary case 3, spec 061).

    Carries the file path and the section that failed so the caller can
    surface a meaningful error. Never return a half-populated model.
    """

    def __init__(self, path: str, section: str, detail: str) -> None:
        self.path = path
        self.section = section
        self.detail = detail
        super().__init__(f"Parse error in {path} (section {section}): {detail}")


class SpecKitDuplicateIDError(Exception):
    """Raised when two spec directories share the same spec_id prefix.

    Satisfies boundary case 2 (spec 061): the adapter surfaces the collision
    as an error, never silently picks one.
    """

    def __init__(self, id: str, paths: list[str]) -> None:
        self.id = id
        self.paths = paths
        super().__init__(f"Duplicate spec_id {id!r} found in: {paths}")


# ---------------------------------------------------------------------------
# Regex patterns for parsing spec.md
# ---------------------------------------------------------------------------

_SPEC_ID_RE = re.compile(r"^(\d{3,}|ic-\d+|sw-\d+)")

_TITLE_PATTERNS = [
    re.compile(r"^#\s+Spec\s+\S+\s*:\s*(.+)$"),           # "# Spec NNN: Title"
    re.compile(r"^#\s+\S+\s+—\s+(.+)$"),                   # "# NNN — Title"
    re.compile(r"^#\s+Feature\s+Specification:\s+(.+)$"),   # "# Feature Specification: ..."
    re.compile(r"^#\s+Scripts\s+Classification:\s+(.+)$"),  # "# Scripts Classification: ..."
    re.compile(r"^#\s+\w+:\s+(.+)$"),                     # "# Dispatch: Title"
    re.compile(r"^#\s+(.+)$"),                             # Fallback
]

# Requirements: "### R1 — text" or "**R1 — text**" or "### R1 — text" with bold
_REQUIRE_HEADING_RE = re.compile(
    r"^(?:###\s+)?\*{0,2}([RCQ]\d+)\s*[—\-]+\s*(.+?)\*{0,2}$"
)

# Acceptance: "**AC1** — text" or "### AC1 — text"
_ACCEPTANCE_HEADING_RE = re.compile(
    r"^\*{0,2}(AC\d+)\*{0,2}[\s:]*[—\-]?\s*(.+?)\*{0,2}$"
)

# Slices: "- **Slice 1** — scope" (matched after stripping bullet)
_SLICE_HEADING_RE = re.compile(
    r"^\*{0,2}(Slice\s+\d+)\*{0,2}\s*[—\-]+\s*(.+)$"
)

# Questions: "- **Q1 — text**" (matched on the full line)
_QUESTION_RE = re.compile(
    r"^\s*[-*]\s+\*{0,2}(Q\d+)\s*[—\-]+\s*(.+?)\*{0,2}(?:\s*[—\-]+\s*(.+?))?\s*$"
)
_QUESTION_HEADING_RE = re.compile(
    r"^###\s+(Q\d+)\s*[—\-]+\s*(.+)$"
)


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def detect(path: str | Path) -> bool:
    """Return True if *path* looks like a spec-kit directory.

    A spec-kit directory has the form ``NNN-slug`` and contains ``spec.md``.
    """
    path = Path(path)
    name = path.name
    if not _SPEC_ID_RE.match(name):
        return False
    spec_file = path / "spec.md"
    return spec_file.is_file()


def parse(path: str | Path) -> UnifiedSpec:
    """Parse a spec-kit spec.md at *path* and return a UnifiedSpec.

    Raises SpecKitParseError on malformed input (never returns a
    half-populated model).
    """
    path = Path(path).resolve()

    # If path points to spec.md directly, use its parent dir
    if path.name == "spec.md":
        spec_dir = path.parent
    else:
        spec_dir = path

    spec_file = spec_dir / "spec.md"
    if not spec_file.is_file():
        raise SpecKitParseError(str(spec_file), "file-read", f"not found: {spec_file}")

    try:
        doc = spec_file.read_text(encoding="utf-8")
    except OSError as exc:
        raise SpecKitParseError(str(spec_file), "file-read", str(exc)) from exc

    # Extract spec_id from directory name
    dir_name = spec_dir.name
    sid_match = _SPEC_ID_RE.match(dir_name)
    if not sid_match:
        raise SpecKitParseError(
            str(spec_file),
            "spec_id",
            f"directory {dir_name!r} does not match spec-id pattern",
        )
    spec_id = sid_match.group(1)

    # Parse the document sections
    title = _extract_title(doc)
    status = _extract_status(doc)
    requirements = _extract_requirements(doc)
    acceptance = _extract_acceptance(doc)
    boundaries = _extract_boundaries(doc)
    slices = _extract_slices(doc, requirements)
    questions = _extract_questions(doc)

    return UnifiedSpec(
        spec_id=spec_id,
        title=title,
        status=status,
        source_framework=SourceFramework.SPEC_KIT,
        source_path=str(spec_file),
        requirements=requirements,
        acceptance=acceptance,
        boundaries=boundaries,
        slices=slices,
        open_questions=questions,
    )


def parse_tree(specs_dir: str | Path) -> list[UnifiedSpec]:
    """Parse all spec-kit specs under ``.specify/specs/``.

    Raises SpecKitDuplicateIDError if two directories share the same
    spec_id prefix (boundary case 2).
    """
    specs_dir = Path(specs_dir)
    if not specs_dir.is_dir():
        return []

    seen: dict[str, list[str]] = {}
    results: list[UnifiedSpec] = []

    for entry in sorted(specs_dir.iterdir()):
        if not entry.is_dir():
            continue
        if not detect(entry):
            continue
        uspec = parse(entry)
        # Check for duplicates
        if uspec.spec_id in seen:
            seen[uspec.spec_id].append(uspec.source_path)
            raise SpecKitDuplicateIDError(uspec.spec_id, seen[uspec.spec_id])
        seen[uspec.spec_id] = [uspec.source_path]
        results.append(uspec)

    return results


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

def _extract_title(doc: str) -> str:
    """Extract spec title from the first H1 line."""
    for line in doc.splitlines():
        line = line.strip()
        if not line.startswith("# "):
            continue
        for pat in _TITLE_PATTERNS:
            m = pat.match(line)
            if m:
                return m.group(1).strip()
        # Pure fallback: everything after "# "
        return line[2:].strip()
    return ""


def _extract_status(doc: str) -> SpecStatus:
    """Infer spec status from common markers in the document."""
    lower = doc.lower()
    if "**status**: shipped" in lower or ("**status**:" in lower and "shipped" in lower):
        return SpecStatus.RATIFIED
    if "**status**: ratified" in lower:
        return SpecStatus.RATIFIED
    if "**status**: superseded" in lower:
        return SpecStatus.SUPERSEDED
    if "**status**: draft" in lower:
        return SpecStatus.DRAFT
    if "ratified" in lower:
        return SpecStatus.RATIFIED
    # Default to draft
    return SpecStatus.DRAFT


def _extract_requirements(doc: str) -> list[Requirement]:
    """Parse ### RN — text and **RN —** text headings."""
    reqs: list[Requirement] = []
    for line in doc.splitlines():
        stripped = line.strip()
        # Remove leading ### if present
        target = stripped
        if target.startswith("### "):
            target = target[4:]
        m = _REQUIRE_HEADING_RE.match(target)
        if m:
            rid = m.group(1)
            text = _clean_markdown(m.group(2))
            if rid and text:
                # Only accept R-prefixed IDs for requirements
                if rid.startswith("R"):
                    reqs.append(Requirement(id=rid, text=text))
    return reqs


def _extract_acceptance(doc: str) -> list[AcceptanceCriterion]:
    """Parse **ACN** text and ### ACN patterns."""
    acs: list[AcceptanceCriterion] = []
    for line in doc.splitlines():
        stripped = line.strip()
        m = _ACCEPTANCE_HEADING_RE.match(stripped)
        if m:
            aid = m.group(1)
            text = _clean_markdown(m.group(2))
            if aid and text:
                acs.append(AcceptanceCriterion(id=aid, text=text))
    return acs


def _extract_boundaries(doc: str) -> list[str]:
    """Parse the ## Boundary cases section."""
    lines = doc.splitlines()
    in_bounds = False
    bounds: list[str] = []
    for line in lines:
        trimmed = line.strip()
        if trimmed.startswith("## "):
            heading = trimmed[3:].lower()
            if heading.startswith("boundary"):
                in_bounds = True
                continue
            if in_bounds:
                break
            continue
        if not in_bounds:
            continue
        # Numbered items: "1. **text** …" or "1. text …"
        numbered = re.match(r"^\d+\.\s+(.+)$", trimmed)
        if numbered:
            bounds.append(_clean_list_item(numbered.group(1)))
            continue
        # Bullet items: "- text" or "* text"
        if trimmed.startswith("- ") or trimmed.startswith("* "):
            item = trimmed[2:]
            bounds.append(_clean_list_item(item))
            continue
    return bounds


def _extract_slices(doc: str, reqs: list[Requirement]) -> list[Slice]:
    """Parse the ## Slice plan section."""
    lines = doc.splitlines()
    in_slices = False
    slices: list[Slice] = []
    for line in lines:
        trimmed = line.strip()
        if trimmed.startswith("## "):
            heading = trimmed[3:].lower()
            if heading.startswith("slice"):
                in_slices = True
                continue
            if in_slices:
                break
            continue
        if not in_slices:
            continue
        # Strip bullet prefix: "- **Slice 1** — scope"
        target = trimmed
        if target.startswith("- "):
            target = target[2:]
        if target.startswith("* "):
            target = target[2:]
        m = _SLICE_HEADING_RE.match(target)
        if m:
            sid = m.group(1)
            scope = _clean_markdown(m.group(2))
            # Link to requirement IDs mentioned in the scope text
            req_ids = [r.id for r in reqs if r.id in scope]
            slices.append(Slice(id=sid, scope=scope, requirement_ids=req_ids))
    return slices


def _extract_questions(doc: str) -> list[Question]:
    """Parse the ## Open questions section."""
    lines = doc.splitlines()
    in_questions = False
    questions: list[Question] = []
    for line in lines:
        trimmed = line.strip()
        if trimmed.startswith("## "):
            heading = trimmed[3:].lower()
            if "open question" in heading:
                in_questions = True
                continue
            if in_questions:
                break
            continue
        if not in_questions:
            continue
        # Match "- **Q1 — text** — proposed answer"
        m = _QUESTION_RE.match(trimmed)
        if m:
            qid = m.group(1)
            text = _clean_markdown(m.group(2))
            proposed = _clean_markdown(m.group(3)) if m.group(3) else None
            questions.append(Question(id=qid, text=text, proposed=proposed))
            continue
        # Match "### Q1 — text"
        m = _QUESTION_HEADING_RE.match(trimmed)
        if m:
            qid = m.group(1)
            text = _clean_markdown(m.group(2))
            questions.append(Question(id=qid, text=text))
            continue
    return questions


def _clean_markdown(s: str) -> str:
    """Remove markdown bold/code markers and trim whitespace."""
    s = s.strip()
    # Remove paired bold markers
    s = re.sub(r"\*\*(.+?)\*\*", r"\1", s)
    # Remove paired backtick code markers
    s = re.sub(r"`(.+?)`", r"\1", s)
    return s.strip()


def _clean_list_item(s: str) -> str:
    """Remove markdown bold markers and trim."""
    s = s.strip()
    s = re.sub(r"\*\*(.+?)\*\*", r"\1", s)
    # Remove trailing period only if it's a list-item period
    s = s.rstrip(".")
    return s.strip()