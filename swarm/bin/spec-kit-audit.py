#!/usr/bin/env python3
"""Audit spec-kit entries and infer their implementation status.

The script is intentionally read-only. It scans `.specify/specs/*/spec.md`,
extracts ticket ids and explicit status markers, then uses git history to infer
whether each spec is Draft, Accepted, Implemented, or Superseded.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Iterable

STATUS_VALUES = ("Draft", "Accepted", "Implemented", "Superseded")
TICKET_RE = re.compile(r"(?<![A-Za-z0-9_])t_[a-f0-9]{8}(?![A-Za-z0-9_])")
STATUS_RE = re.compile(r"(?im)^\s*(?:\*\*)?Status(?:\*\*)?\s*[:\-]\s*`?(Draft|Accepted|Implemented|Superseded)`?\b")
IMPLEMENTED_RE = re.compile(r"(?im)^\s*(?:\*\*)?(?:Implemented|Implementation|Merged|Landed)(?:\*\*)?\s*[:\-]")
SUPERSEDED_RE = re.compile(r"(?i)\b(superseded|obsolete|replaced by|closed superseded)\b")


@dataclass(frozen=True)
class SpecAudit:
    slug: str
    path: str
    status: str
    tickets: list[str]
    commits: int
    evidence: str


def run_git(repo: Path, args: list[str]) -> str:
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if result.returncode != 0:
        return ""
    return result.stdout


def explicit_status(text: str) -> str | None:
    match = STATUS_RE.search(text)
    return match.group(1) if match else None


def extract_tickets(text: str) -> list[str]:
    return sorted(set(TICKET_RE.findall(text)))


def commit_count_for_path(repo: Path, relpath: str) -> int:
    out = run_git(repo, ["log", "--follow", "--format=%H", "--", relpath])
    return len([line for line in out.splitlines() if line.strip()])


def implementation_refs(repo: Path, tickets: Iterable[str], relpath: str) -> list[str]:
    refs: list[str] = []
    for ticket in tickets:
        out = run_git(repo, ["log", "--format=%h %s", "--grep", ticket, "--all", "--", ":(exclude)" + relpath])
        refs.extend(line.strip() for line in out.splitlines() if line.strip())
    return refs


def infer_status(repo: Path, spec_path: Path) -> SpecAudit:
    relpath = spec_path.relative_to(repo).as_posix()
    text = spec_path.read_text(errors="ignore")
    tickets = extract_tickets(text)
    commits = commit_count_for_path(repo, relpath)

    marker = explicit_status(text)
    if marker in {"Implemented", "Superseded"}:
        return SpecAudit(spec_path.parent.name, relpath, marker, tickets, commits, f"explicit status marker: {marker}")

    if SUPERSEDED_RE.search(text):
        return SpecAudit(spec_path.parent.name, relpath, "Superseded", tickets, commits, "superseded language in spec")

    if IMPLEMENTED_RE.search(text):
        return SpecAudit(spec_path.parent.name, relpath, "Implemented", tickets, commits, "implementation marker in spec")

    refs = implementation_refs(repo, tickets, relpath) if tickets else []
    if refs:
        return SpecAudit(spec_path.parent.name, relpath, "Implemented", tickets, commits, refs[0])

    if commits > 0:
        evidence = "tracked in git history"
        if marker == "Draft":
            evidence = "tracked in git history (explicit Draft marker is stale)"
        return SpecAudit(spec_path.parent.name, relpath, "Accepted", tickets, commits, evidence)

    if marker == "Accepted":
        return SpecAudit(spec_path.parent.name, relpath, "Accepted", tickets, commits, "explicit status marker: Accepted")

    return SpecAudit(spec_path.parent.name, relpath, "Draft", tickets, commits, "no git history for spec path")


def scan(repo: Path, specs_root: Path | None = None) -> list[SpecAudit]:
    specs_root = specs_root or (repo / ".specify" / "specs")
    if not specs_root.exists():
        return []
    return [infer_status(repo, path) for path in sorted(specs_root.glob("*/spec.md"))]


def format_text(items: list[SpecAudit]) -> str:
    lines = ["slug\tstatus\ttickets\tevidence"]
    for item in items:
        lines.append(f"{item.slug}\t{item.status}\t{','.join(item.tickets) or '-'}\t{item.evidence}")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Audit spec-kit entries and inferred status")
    parser.add_argument("--repo", default=".", help="repository root (default: cwd)")
    parser.add_argument("--json", action="store_true", help="emit JSON instead of tab-separated text")
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    items = scan(repo)
    if args.json:
        print(json.dumps([asdict(item) for item in items], indent=2, sort_keys=True))
    else:
        print(format_text(items))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
