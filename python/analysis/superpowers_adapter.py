"""Superpowers markdown adapter (R5, spec 061).

Superpowers documents under docs/superpowers/ are often implementation plans
or design notes, not strict specs. This adapter normalizes only structure the
source actually exposes and leaves missing fields empty.
"""
from __future__ import annotations

import re
from pathlib import Path

from analysis.unified_spec import (
    AcceptanceCriterion,
    Question,
    Requirement,
    Slice,
    SourceFramework,
    SpecStatus,
    UnifiedSpec,
)


class SuperpowersParseError(Exception):
    """Typed error for Superpowers parse failures."""

    def __init__(self, path: str, section: str, detail: str) -> None:
        self.path = path
        self.section = section
        self.detail = detail
        super().__init__(f"Parse error in {path} (section {section}): {detail}")


_DATE_STEM_RE = re.compile(r"^\d{4}-\d{2}-\d{2}-.+")
_EXPLICIT_REQ_RE = re.compile(r"^(?:###\s+)?\*{0,2}(R\d+)\s*[—\-]+\s*(.+?)\*{0,2}$")
_EXPLICIT_AC_RE = re.compile(r"^\*{0,2}(AC\d+)\*{0,2}[\s:]*[—\-]?\s*(.+?)\*{0,2}$")
_SLICE_HEADING_RE = re.compile(r"^#{2,3}\s+(Slice\s+\d+|Milestone\s+M\d+|M\d+)\s*[—:-]\s*(.+)$", re.IGNORECASE)
_QUESTION_RE = re.compile(r"^(?:(?:[-*]|\d+\.)\s+)?\*{0,2}(?:Q(\d+)\s*[—\-])?\s*(.+?)\*{0,2}$")

_REQUIREMENT_SECTIONS = {
    "goal",
    "goals",
    "hard rules",
    "invariants",
    "invariant",
    "decision",
    "in scope",
    "scope",
}
_ACCEPTANCE_SECTIONS = {
    "acceptance",
    "success criteria",
    "done-condition",
    "done condition",
    "verification",
}
_BOUNDARY_SECTIONS = {
    "non-goals",
    "non goals",
    "out of scope",
    "edge cases",
    "risks",
}


def detect(path: str | Path) -> bool:
    """Return True for Superpowers markdown documents."""
    p = Path(path)
    if p.is_dir():
        return _is_superpowers_dir(p) and ((p / "plan.md").is_file() or (p / "spec.md").is_file())
    if p.suffix.lower() != ".md":
        return False
    return _is_superpowers_markdown(p)


def parse(path: str | Path) -> UnifiedSpec:
    """Parse a Superpowers markdown document into UnifiedSpec."""
    source = _resolve_source(path)
    try:
        doc = source.read_text(encoding="utf-8")
    except OSError as exc:
        raise SuperpowersParseError(str(source), "file-read", str(exc)) from exc

    title = _extract_title(doc)
    requirements = _extract_requirements(doc)
    acceptance = _extract_acceptance(doc)

    return UnifiedSpec(
        spec_id=_spec_id_for_path(path, source),
        title=title,
        status=_extract_status(doc),
        source_framework=SourceFramework.SUPERPOWERS,
        source_path=str(source.resolve()),
        requirements=requirements,
        acceptance=acceptance,
        boundaries=_extract_boundaries(doc),
        slices=_extract_slices(doc, requirements),
        open_questions=_extract_questions(doc),
    )


def _resolve_source(path: str | Path) -> Path:
    p = Path(path)
    if p.is_dir():
        for name in ("spec.md", "plan.md"):
            candidate = p / name
            if candidate.is_file():
                return candidate
        raise SuperpowersParseError(str(p), "file-read", "directory has no spec.md or plan.md")
    if not p.is_file():
        raise SuperpowersParseError(str(p), "file-read", "not found")
    if p.suffix.lower() != ".md":
        raise SuperpowersParseError(str(p), "file-read", "expected markdown file")
    return p


def _spec_id_for_path(path: str | Path, source: Path) -> str:
    original = Path(path)
    if original.is_dir():
        return original.name
    return source.stem


def _is_superpowers_dir(path: Path) -> bool:
    return _has_superpowers_segment(path) and _DATE_STEM_RE.match(path.name) is not None


def _is_superpowers_markdown(path: Path) -> bool:
    return _has_superpowers_segment(path) and _DATE_STEM_RE.match(path.stem) is not None


def _has_superpowers_segment(path: Path) -> bool:
    parts = path.parts
    return any(parts[i] == "docs" and i + 1 < len(parts) and parts[i + 1] == "superpowers" for i in range(len(parts)))


def _extract_title(doc: str) -> str:
    for line in doc.splitlines():
        stripped = line.strip()
        if stripped.startswith("# "):
            return _clean_markdown(stripped[2:])
    return ""


def _extract_status(doc: str) -> SpecStatus:
    lower = doc.lower()
    status_line = next((ln.lower() for ln in doc.splitlines() if "status:" in ln.lower()), "")
    status_text = status_line or lower[:2000]
    if "superseded" in status_text:
        return SpecStatus.SUPERSEDED
    if any(token in status_text for token in ("implemented", "shipped", "ratified", "amended", "open")):
        return SpecStatus.RATIFIED
    return SpecStatus.DRAFT


def _extract_requirements(doc: str) -> tuple[Requirement, ...]:
    explicit: list[Requirement] = []
    for line in doc.splitlines():
        target = line.strip()
        if target.startswith("### "):
            target = target[4:]
        match = _EXPLICIT_REQ_RE.match(target)
        if match:
            explicit.append(Requirement(id=match.group(1), text=_clean_markdown(match.group(2))))
    if explicit:
        return tuple(explicit)

    items = _inline_label_items(doc, {"goal"})
    items.extend(_section_items(doc, _REQUIREMENT_SECTIONS))
    return tuple(Requirement(id=f"R{i}", text=item) for i, item in enumerate(items, start=1))


def _extract_acceptance(doc: str) -> tuple[AcceptanceCriterion, ...]:
    explicit: list[AcceptanceCriterion] = []
    for line in doc.splitlines():
        match = _EXPLICIT_AC_RE.match(line.strip())
        if match:
            explicit.append(AcceptanceCriterion(id=match.group(1), text=_clean_markdown(match.group(2))))
    if explicit:
        return tuple(explicit)

    items = _inline_label_following_bullets(doc, {"acceptance"})
    items.extend(_section_items(doc, _ACCEPTANCE_SECTIONS))
    return tuple(AcceptanceCriterion(id=f"AC{i}", text=item) for i, item in enumerate(items, start=1))


def _extract_boundaries(doc: str) -> tuple[str, ...]:
    return tuple(_section_items(doc, _BOUNDARY_SECTIONS))


def _extract_slices(doc: str, reqs: tuple[Requirement, ...]) -> tuple[Slice, ...]:
    slices: list[Slice] = []
    for line in doc.splitlines():
        match = _SLICE_HEADING_RE.match(line.strip())
        if not match:
            continue
        sid = match.group(1)
        scope = _clean_markdown(match.group(2))
        req_ids = tuple(req.id for req in reqs if req.id in scope)
        slices.append(Slice(id=sid, scope=scope, requirement_ids=req_ids))

    if slices:
        return tuple(slices)

    tasks = _checkbox_items(doc)
    return tuple(Slice(id=f"Task {i}", scope=task, requirement_ids=()) for i, task in enumerate(tasks, start=1))


def _extract_questions(doc: str) -> tuple[Question, ...]:
    items = _section_items(doc, {"open questions", "open items"})
    questions: list[Question] = []
    for i, item in enumerate(items, start=1):
        match = _QUESTION_RE.match(item)
        if match and match.group(1):
            questions.append(Question(id=f"Q{match.group(1)}", text=_clean_markdown(match.group(2))))
        else:
            questions.append(Question(id=f"Q{i}", text=_clean_markdown(item)))
    return tuple(questions)


def _section_items(doc: str, headings: set[str]) -> list[str]:
    lines = doc.splitlines()
    active = False
    found: list[str] = []
    paragraph: list[str] = []

    def flush_paragraph() -> None:
        if paragraph:
            text = _clean_markdown(" ".join(paragraph))
            if text:
                found.append(text)
            paragraph.clear()

    for line in lines:
        stripped = line.strip()
        if stripped.startswith("## "):
            flush_paragraph()
            heading = _clean_heading(stripped[3:])
            active = heading in headings
            continue
        if stripped.startswith("#"):
            flush_paragraph()
            if active:
                active = False
            continue
        if not active:
            continue
        if not stripped:
            flush_paragraph()
            continue
        item = _list_item_text(stripped)
        if item is not None:
            flush_paragraph()
            found.append(item)
            continue
        paragraph.append(stripped)

    flush_paragraph()
    return found


def _inline_label_items(doc: str, labels: set[str]) -> list[str]:
    items: list[str] = []
    pattern = re.compile(r"^\*{0,2}([^:*]+):\*{0,2}\s*(.+)$")
    for line in doc.splitlines():
        match = pattern.match(line.strip())
        if not match:
            continue
        label = _clean_heading(match.group(1))
        if label in labels:
            items.append(_clean_markdown(match.group(2)))
    return items


def _inline_label_following_bullets(doc: str, labels: set[str]) -> list[str]:
    lines = doc.splitlines()
    items: list[str] = []
    active = False
    pattern = re.compile(r"^\*{0,2}([^:*]+):\*{0,2}\s*$")
    for line in lines:
        stripped = line.strip()
        match = pattern.match(stripped)
        if match:
            active = _clean_heading(match.group(1)) in labels
            continue
        if active and stripped.startswith("#"):
            active = False
            continue
        if not active:
            continue
        if not stripped:
            continue
        item = _list_item_text(stripped)
        if item is not None:
            items.append(item)
            continue
        active = False
    return items


def _checkbox_items(doc: str) -> list[str]:
    items: list[str] = []
    for line in doc.splitlines():
        stripped = line.strip()
        if stripped.startswith("- [ ] ") or stripped.startswith("- [x] "):
            items.append(_clean_markdown(stripped[6:]))
    return items


def _list_item_text(line: str) -> str | None:
    numbered = re.match(r"^\d+\.\s+(.+)$", line)
    if numbered:
        return _clean_markdown(numbered.group(1))
    if line.startswith("- ") or line.startswith("* "):
        return _clean_markdown(line[2:])
    return None


def _clean_heading(s: str) -> str:
    s = _clean_markdown(s).lower()
    s = re.sub(r"\s*\([^)]*\)", "", s)
    return s.strip(" :")


def _clean_markdown(s: str) -> str:
    s = s.strip()
    s = re.sub(r"\*\*(.+?)\*\*", r"\1", s)
    s = re.sub(r"`(.+?)`", r"\1", s)
    return s.strip().rstrip(".")
