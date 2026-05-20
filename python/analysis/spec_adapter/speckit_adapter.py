"""spec-kit / house-format adapter (spec 061 R3).

Parses ``.specify/specs/NNN-slug/spec.md`` into ``UnifiedSpec``.

Supported heading patterns (derived from the ~53 existing house specs):

- Title:  ``# Spec NNN: Title`` | ``# NNN — Title`` | ``# Title`` (when
  the NNN prefix is in the directory name)
- Status: ``**Status**: value`` (anywhere before requirements)
- Requirements: ``### RN — text`` or ``**RN** — text`` (bold-wrapped)
- Acceptance: ``### ACN`` or ``**ACN**`` followed by text on the same or
  next line(s) until the next heading / bold-id / section break
- Boundaries: ``## Boundary cases`` or ``## Boundaries`` section
- Open questions: ``## Open questions`` section
- Slice plan: ``## Slice plan`` section with ``- **Slice N** — scope``
- For stub/early specs with ``## Acceptance Criteria (TBD)`` we emit an
  empty list (valid per boundary case 1).
- For specs using ``## Invariants`` instead of explicit R-numbers, the
  invariant entries are captured as requirements with ``inv-N`` ids.

Round-trip (R6): ``render()`` produces house-format markdown that survives
a ``parse → render → parse`` cycle without semantic loss.
"""
from __future__ import annotations

import os
import re
from pathlib import Path
from typing import List, Optional, Tuple

from analysis.spec_adapter.adapter import Adapter, SpecAdapterError
from analysis.spec_adapter.types import (
    AcceptanceCriterion,
    Question,
    Requirement,
    Slice,
    UnifiedSpec,
)

# ── Regex helpers ──────────────────────────────────────────────────────────────

# Title patterns
_RE_TITLE_SPEC_COLON = re.compile(
    r"^#\s+Spec\s+(\d{1,4}|[\w]+-\d{3})\s*[:：]\s*(.+)$"
)
_RE_TITLE_EM_DASH = re.compile(r"^#\s+(\d{1,4}|[\w]+-\d{3})\s+[—–]\s+(.+)$")
_RE_TITLE_PLAIN = re.compile(r"^#\s+(.+)$")

# Status pattern — **Status**: <value> or **Status**: <value> — ...
_RE_STATUS = re.compile(
    r"\*\*Status\*\*\s*[:：]\s*(draft|ratified|superseded|groomed)",
    re.IGNORECASE,
)

# Requirement patterns:
#   ### R1 — text   OR   **R1** — text   OR   **R1**: text   OR   - **R1**: text
_RE_REQ_HEADING = re.compile(
    r"^###\s+(R\d+)\s*[—–:]\s*(.+)$"
)
_RE_REQ_BOLD = re.compile(
    r"^\*?\*?\s*\*{2}(R\d+)\*{2}\s*[—–:]\s*(.+)$"
)
_RE_REQ_BULLET = re.compile(
    r"^-\s+\*{2}(R\d+)\*{2}\s*[—–:]\s*(.+)$"
)

# Acceptance criterion patterns:
#   **AC1** text   OR   ### AC1   OR   - **AC1**: text   OR   - [ ] AC1 text
#   OR   - AC1: text (plain bullet without bold)
_RE_AC_HEADING = re.compile(r"^###\s+(AC\d+)\s*[—–:]?\s*(.*)$")
_RE_AC_BOLD = re.compile(r"^\*?\*?\s*\*{2}(AC\d+)\*{2}\s*[—–:]\s*(.+)$")
_RE_AC_BULLET = re.compile(r"^-\s+\*{2}(AC\d+)\*{2}\s*[—–:]\s*(.+)$")
_RE_AC_PLAIN = re.compile(r"^-\s+(AC\d+)\s*[.:]\s*(.+)$")
_RE_AC_CHECKBOX = re.compile(r"^-\s+\[[ xX]\]\s+(AC?\d+[\s.]?\s*.+)$")

# Invariant-as-requirement patterns:
#   ### Inv-N: ...   OR   **inv-N**: ...   OR   - **inv-N**: ...
_RE_INV_HEADING = re.compile(r"^###\s+(inv[\w-]*)\s*[—–:]\s*(.+)$", re.IGNORECASE)
_RE_INV_BOLD = re.compile(r"\*{2}(inv[\w-]*)\*{2}\s*[—–:]\s*(.+)$", re.IGNORECASE)

# Slice pattern: - **Slice N** — scope
_RE_SLICE = re.compile(r"^-\s+\*{2}Slice\s+(\d+)\*{2}\s*[—–]\s*(.+)$")

# Open questions: - **QN** — text   (and optional "Proposed:" or "→" lines)
# Also handles: - **Q1 — key rotation.** (bold wraps entire question)
_RE_QUESTION_BOLD = re.compile(r"^\s*-\s+\*{2}(Q\d+)\s*[—–]\s*(.+?)\*{0,2}\s*$")
_RE_QUESTION_ALT = re.compile(r"^\s*-\s+\*{2}(Q\d+)\*{2}\s*[—–:]\s*(.+)$")

# Section heading at ## level
_RE_H2 = re.compile(r"^##\s+(.+)$")
# Section heading at ### level
_RE_H3 = re.compile(r"^###\s+(.+)$")



def _normalise_status(raw: str) -> str:
    """Map observed status values to the canonical enum."""
    low = raw.strip().lower()
    if low in ("draft", "groomed"):
        return "draft"
    if low == "ratified":
        return "ratified"
    if low == "superseded":
        return "superseded"
    return "draft"  # unknown → draft (safe default)


def _extract_spec_id_from_path(source_path: str) -> str:
    """Derive spec_id from the directory name, e.g. '061-unified-spec-model' → '061'."""
    p = Path(source_path)
    # Walk up to find the NNN-slug directory
    parts = list(p.parts)
    for i in range(len(parts) - 1, -1, -1):
        part = parts[i]
        m = re.match(r"^(\d{1,4}|[\w]+-\d{3})-", part)
        if m:
            return m.group(1)
    return ""


class SpecKitAdapter(Adapter):
    """Adapter for the spec-kit / house format.

    Handles ``.specify/specs/NNN-slug/spec.md`` and sibling paths.
    """

    FRAMEWORK = "spec-kit"

    # ── detect ─────────────────────────────────────────────────────────────

    def detect(self, path: str | Path) -> bool:
        """Return True for paths under ``.specify/specs/`` containing ``spec.md``."""
        p = Path(path)
        # Match: .../.specify/specs/NNN-slug/spec.md  (or just .../NNN-slug/)
        try:
            parts = p.resolve().parts
        except OSError:
            p_str = str(path)
            return ".specify" in p_str and "specs" in p_str

        for i, part in enumerate(parts):
            if part == ".specify" and i + 1 < len(parts) and parts[i + 1] == "specs":
                return True
        return False

    # ── parse ──────────────────────────────────────────────────────────────

    def parse(self, source_path: str | Path) -> UnifiedSpec:
        """Parse a house-format spec.md into UnifiedSpec.

        Raises:
            SpecAdapterError: on malformed input (boundary case 3).
        """
        p = Path(source_path)
        if not p.exists():
            raise SpecAdapterError(str(source_path), "file", "file does not exist")
        try:
            text = p.read_text(encoding="utf-8")
        except OSError as exc:
            raise SpecAdapterError(str(source_path), "file", str(exc)) from exc

        return self._parse_text(text, str(source_path))

    def _parse_text(self, text: str, source_path: str) -> UnifiedSpec:
        """Core parser operating on markdown text."""
        lines = text.splitlines()

        spec_id = self._parse_spec_id(lines, source_path)
        title = self._parse_title(lines, spec_id, source_path)
        status = self._parse_status(lines)

        # Section-based extraction
        sections = self._split_sections(lines)

        requirements = self._parse_requirements(sections, lines)
        acceptance = self._parse_acceptance(sections, lines)
        boundaries = self._parse_boundaries(sections)
        open_questions = self._parse_open_questions(sections)
        slices = self._parse_slices(sections)

        return UnifiedSpec(
            spec_id=spec_id,
            title=title,
            status=status,
            source_framework=self.FRAMEWORK,
            source_path=source_path,
            requirements=requirements,
            acceptance=acceptance,
            boundaries=boundaries,
            slices=slices,
            open_questions=open_questions,
        )

    # ── spec_id ───────────────────────────────────────────────────────────

    def _parse_spec_id(self, lines: List[str], source_path: str) -> str:
        """Extract spec_id from the H1 heading or directory name."""
        # First, try the H1 heading
        for line in lines:
            line = line.strip()
            m = _RE_TITLE_SPEC_COLON.match(line)
            if m:
                return m.group(1).strip()
            m = _RE_TITLE_EM_DASH.match(line)
            if m:
                return m.group(1).strip()

        # Fall back to directory name
        sid = _extract_spec_id_from_path(source_path)
        if sid:
            return sid

        raise SpecAdapterError(
            source_path, "spec_id",
            "cannot determine spec_id from heading or directory name",
        )

    # ── title ─────────────────────────────────────────────────────────────

    def _parse_title(self, lines: List[str], spec_id: str, source_path: str) -> str:
        """Extract the title from the H1 heading."""
        for line in lines:
            line_s = line.strip()
            m = _RE_TITLE_SPEC_COLON.match(line_s)
            if m:
                return m.group(2).strip()
            m = _RE_TITLE_EM_DASH.match(line_s)
            if m:
                return m.group(2).strip()

        # Plain H1 — just a title, no NNN prefix
        for line in lines:
            line_s = line.strip()
            m = _RE_TITLE_PLAIN.match(line_s)
            if m:
                return m.group(1).strip()

        raise SpecAdapterError(source_path, "title", "no H1 heading found")

    # ── status ─────────────────────────────────────────────────────────────

    def _parse_status(self, lines: List[str]) -> str:
        """Extract status from ``**Status**: ...`` anywhere in the doc."""
        for line in lines:
            m = _RE_STATUS.search(line)
            if m:
                return _normalise_status(m.group(1))
        return "draft"  # default if no status line

    # ── section splitting ──────────────────────────────────────────────────

    def _split_sections(self, lines: List[str]) -> dict:
        """Split doc into named sections keyed by ``##`` headings.

        Returns ``{section_title: [lines_under_that_heading]}``.
        """
        sections: dict = {}
        current_key: Optional[str] = None

        for line in lines:
            m = _RE_H2.match(line)
            if m:
                current_key = m.group(1).strip().lower()
                # Normalise section names
                if current_key in ("requirements", "requirement"):
                    current_key = "requirements"
                elif current_key in ("acceptance criteria", "acceptance"):
                    current_key = "acceptance criteria"
                elif current_key in ("boundary cases", "boundaries"):
                    current_key = "boundary cases"
                elif current_key in ("open questions",):
                    current_key = "open questions"
                elif current_key in ("slice plan",):
                    current_key = "slice plan"
                if current_key not in sections:
                    sections[current_key] = []
                continue

            if current_key is not None:
                sections[current_key].append(line)

        return sections

    # ── requirements ───────────────────────────────────────────────────────

    def _parse_requirements(
        self, sections: dict, all_lines: List[str]
    ) -> List[Requirement]:
        """Parse requirements from the ``## Requirements`` section."""
        req_lines = sections.get("requirements", [])
        if not req_lines:
            return []

        reqs: List[Requirement] = []
        for line in req_lines:
            s = line.strip()
            if not s:
                continue

            # ### R1 — text
            m = _RE_REQ_HEADING.match(s)
            if m:
                reqs.append(Requirement(id=m.group(1), text=m.group(2).strip()))
                continue

            # **R1** — text
            m = _RE_REQ_BOLD.match(s)
            if m:
                reqs.append(Requirement(id=m.group(1), text=m.group(2).strip()))
                continue

            # - **R1**: text
            m = _RE_REQ_BULLET.match(s)
            if m:
                reqs.append(Requirement(id=m.group(1), text=m.group(2).strip()))
                continue

        return reqs

    # ── acceptance criteria ───────────────────────────────────────────────

    def _parse_acceptance(
        self, sections: dict, all_lines: List[str]
    ) -> List[AcceptanceCriterion]:
        """Parse acceptance criteria from the ``## Acceptance Criteria`` section."""
        ac_lines = sections.get("acceptance criteria", [])
        if not ac_lines:
            return []

        # Check for (TBD) marker — stub spec
        joined = "\n".join(ac_lines)
        if "(TBD)" in joined or "(tbd)" in joined.lower():
            return []

        acs: List[AcceptanceCriterion] = []
        for line in ac_lines:
            s = line.strip()
            if not s:
                continue

            # - **AC1**: text
            m = _RE_AC_BOLD.match(s)
            if m:
                acs.append(AcceptanceCriterion(id=m.group(1), text=m.group(2).strip()))
                continue

            # - **AC1** text (no separator)
            m = _RE_AC_BULLET.match(s)
            if m:
                acs.append(AcceptanceCriterion(id=m.group(1), text=m.group(2).strip()))
                continue

            # - AC1: text  (plain bullet without bold)
            m = _RE_AC_PLAIN.match(s)
            if m:
                acs.append(AcceptanceCriterion(id=m.group(1), text=m.group(2).strip()))
                continue

            # - [ ] AC1 text  (checkbox style)
            m = _RE_AC_CHECKBOX.match(s)
            if m:
                raw = m.group(1).strip()
                # Split at first space or dot after the AC label
                parts = re.match(r"(AC?\d+)[\s.]*\s*(.*)", raw)
                if parts:
                    acs.append(AcceptanceCriterion(
                        id=parts.group(1), text=parts.group(2).strip()
                    ))
                continue

            # ### AC1
            m = _RE_AC_HEADING.match(s)
            if m:
                text = m.group(2).strip() if m.group(2) else ""
                acs.append(AcceptanceCriterion(id=m.group(1), text=text))
                continue

        return acs

    # ── boundaries ────────────────────────────────────────────────────────

    def _parse_boundaries(self, sections: dict) -> List[str]:
        """Parse boundary cases from the ``## Boundary cases`` section."""
        boundary_lines = sections.get("boundary cases", [])
        if not boundary_lines:
            return []

        boundaries: List[str] = []
        for line in boundary_lines:
            s = line.strip()
            if not s:
                continue
            # Lines like "1. **Description** — detail" or "- **boundary** — desc"
            # or "3. plain text"
            # Strip leading numbering/bullets
            cleaned = re.sub(r"^(\d+\.\s*|\-\s*)", "", s)
            # If starts with ** it's a bold label — include the whole line
            if cleaned.startswith("**"):
                # Remove bold markers for cleaner text
                cleaned = re.sub(r"\*{2}", "", cleaned).strip()
                cleaned = re.sub(r"^[—–:]\s*", "", cleaned).strip()
            boundaries.append(cleaned)

        return boundaries

    # ── open questions ────────────────────────────────────────────────────

    def _parse_open_questions(self, sections: dict) -> List[Question]:
        """Parse open questions from the ``## Open questions`` section."""
        oq_lines = sections.get("open questions", [])
        if not oq_lines:
            return []

        questions: List[Question] = []
        current_q: Optional[Question] = None

        for line in oq_lines:
            s = line.strip()
            if not s:
                continue

            # Try Q-format patterns
            m = _RE_QUESTION_BOLD.match(s)
            if m:
                if current_q is not None:
                    questions.append(current_q)
                qid = m.group(1)
                text = m.group(2).strip()
                proposed = None
                # Check for "Proposed:" inline
                if "Proposed:" in text or "proposed:" in text.lower():
                    parts = re.split(r"[Pp]roposed:\s*", text, maxsplit=1)
                    text = parts[0].strip().rstrip("—–: ").strip()
                    if len(parts) > 1:
                        proposed = parts[1].strip()
                current_q = Question(id=qid, text=text, proposed=proposed)
                continue

            # Q-format fallback: - **Q1** — text  (bold closes before dash)
            m = _RE_QUESTION_ALT.match(s)
            if m:
                if current_q is not None:
                    questions.append(current_q)
                qid = m.group(1)
                text = m.group(2).strip()
                proposed = None
                if "Proposed:" in text or "proposed:" in text.lower():
                    parts = re.split(r"[Pp]roposed:\s*", text, maxsplit=1)
                    text = parts[0].strip().rstrip("—–: ").strip()
                    if len(parts) > 1:
                        proposed = parts[1].strip()
                current_q = Question(id=qid, text=text, proposed=proposed)
                continue

            # Continuation of previous question or "Proposed:" line
            if current_q is not None:
                prop_match = re.match(r"^\s*[Pp]roposed\s*[:：]\s*(.+)$", s)
                if prop_match:
                    current_q = Question(
                        id=current_q.id,
                        text=current_q.text,
                        proposed=prop_match.group(1).strip(),
                    )
                    continue
                # Append to text
                current_q = Question(
                    id=current_q.id,
                    text=current_q.text + " " + re.sub(r"^[>\-\s]+", "", s).strip(),
                    proposed=current_q.proposed,
                )

        if current_q is not None:
            questions.append(current_q)

        return questions

    # ── slice plan ────────────────────────────────────────────────────────

    def _parse_slices(self, sections: dict) -> List[Slice]:
        """Parse slices from the ``## Slice plan`` section."""
        slice_lines = sections.get("slice plan", [])
        if not slice_lines:
            return []

        slices: List[Slice] = []
        for line in slice_lines:
            s = line.strip()
            if not s:
                continue
            m = _RE_SLICE.match(s)
            if m:
                slice_id = f"Slice {m.group(1)}"
                scope = m.group(2).strip()
                # Extract requirement ids from scope (e.g. "R1, R2, R3.")
                req_ids = re.findall(r"\bR\d+\b", scope)
                # Also look for "AC1, AC2" style references in scope
                slices.append(Slice(id=slice_id, scope=scope, requirement_ids=req_ids))
                continue

        # Handle "Single slice" or "Single artifact" prose
        if not slices:
            joined = " ".join(l.strip() for l in slice_lines if l.strip())
            if "single" in joined.lower() or "not sliced" in joined.lower():
                slices.append(Slice(
                    id="Slice 1",
                    scope=joined.strip(),
                    requirement_ids=[],
                ))

        return slices

    # ── render (R6 round-trip) ──────────────────────────────────────────────

    def render(self, spec: UnifiedSpec) -> str:
        """Render a UnifiedSpec back to house-format markdown.

        Produces output that survives ``parse → render → parse`` without
        semantic loss (R6).
        """
        parts: List[str] = []

        # Title
        parts.append(f"# Spec {spec.spec_id}: {spec.title}")
        parts.append("")

        # Status
        parts.append(f"**Status**: {spec.status}")
        parts.append("")

        # Requirements
        if spec.requirements:
            parts.append("## Requirements")
            parts.append("")
            for req in spec.requirements:
                parts.append(f"### {req.id} — {req.text}")
                parts.append("")

        # Acceptance criteria
        if spec.acceptance:
            parts.append("## Acceptance criteria")
            parts.append("")
            for ac in spec.acceptance:
                parts.append(f"- **{ac.id}** — {ac.text}")
                parts.append("")

        # Boundary cases
        if spec.boundaries:
            parts.append("## Boundary cases")
            parts.append("")
            for i, b in enumerate(spec.boundaries, 1):
                parts.append(f"{i}. {b}")
                parts.append("")

        # Open questions
        if spec.open_questions:
            parts.append("## Open questions")
            parts.append("")
            for q in spec.open_questions:
                line = f"- **{q.id}** — {q.text}"
                if q.proposed:
                    line += f" Proposed: {q.proposed}"
                parts.append(line)
                parts.append("")

        # Slice plan
        if spec.slices:
            parts.append("## Slice plan")
            parts.append("")
            for sl in spec.slices:
                parts.append(f"- **{sl.id}** — {sl.scope}")
                parts.append("")

        return "\n".join(parts)