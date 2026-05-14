#!/usr/bin/env python3
"""Post-PR judge for the swarm ELO ledger."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from datetime import datetime

sys.path.insert(0, os.path.dirname(__file__))
# Runtime deployments may still keep the shared library in ~/.openclaw/workflows.
sys.path.append(os.path.expanduser("~/.openclaw/workflows"))
import _swarm_elo as elo  # noqa: E402


DEFAULT_JUDGE = "clawta-gpt-5.5"
TASK_CLASS_HEURISTICS = [
    (r"refactor|cleanup|reorganize|consolidat", "refactor"),
    (r"\b(bug|fix|broken|fails?|crash|error)\b", "bugfix"),
    (r"\b(test|coverage|regression|spec)\b", "test"),
    (r"\b(doc|docs|readme|comment)\b", "docs"),
    (r"\b(feat|feature|implement|add|new)\b", "feature"),
    (r"\b(research|investigat|explore|spike)\b", "research"),
]
CAPABILITY_HEURISTICS = [
    (r"\bapi\b|endpoint|handler|route|graphql", "api"),
    (r"\bui\b|frontend|css|react|component|layout", "frontend"),
    (r"sqlite|schema|query|db|database|migration", "database"),
    (r"\btest|pytest|vitest|coverage|regression", "testing"),
    (r"\bci\b|github actions|workflow|pipeline", "ci"),
    (r"doc|readme|comment", "docs"),
    (r"\bgo\b|kernel|policy|governance", "governance"),
    (r"python|script|workflow|swarm", "automation"),
]


def infer_task_class(ticket_title: str, ticket_body: str) -> str:
    text = (ticket_title + " " + ticket_body).lower()
    for pattern, cls in TASK_CLASS_HEURISTICS:
        if re.search(pattern, text, re.IGNORECASE):
            return cls
    return "unknown"


def infer_capabilities(*parts: str) -> list[str]:
    text = " ".join(parts).lower()
    found = []
    for pattern, label in CAPABILITY_HEURISTICS:
        if re.search(pattern, text, re.IGNORECASE):
            found.append(label)
    if not found:
        found.append("general")
    return sorted(set(found))


def complexity_bucket(additions: int, deletions: int, changed_files: int, commit_count: int) -> str:
    churn = additions + deletions
    score = changed_files * 2 + commit_count + (churn // 120)
    if score >= 20 or churn >= 1200 or changed_files >= 20:
        return "large"
    if score >= 8 or churn >= 250 or changed_files >= 6:
        return "medium"
    return "small"


def parse_ts(value: str | None) -> int | None:
    if not value:
        return None
    try:
        return int(datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp())
    except ValueError:
        return None


def fetch_ticket(ticket_id: str) -> dict:
    """Pull ticket JSON from hermes kanban."""
    try:
        result = subprocess.run(
            ["hermes", "kanban", "--board", "chitin", "show", ticket_id, "--json"],
            capture_output=True,
            text=True,
            timeout=15,
        )
        if result.returncode != 0:
            return {"title": "", "body": "", "error": result.stderr.strip()[:200]}
        return json.loads(result.stdout)
    except (subprocess.TimeoutExpired, json.JSONDecodeError, FileNotFoundError) as e:
        return {"title": "", "body": "", "error": str(e)[:200]}


def fetch_pr_summary(pr_url: str) -> dict:
    """Fetch PR metadata, diff stats, check rollup, and commit msgs via gh CLI."""
    pr_id = pr_url.rstrip("/").rsplit("/", 1)[-1]
    if not pr_id.isdigit():
        return {"title": "", "body": "", "diff_stat": "", "commits": [], "error": "bad pr url"}
    try:
        result = subprocess.run(
            [
                "gh", "pr", "view", pr_id,
                "--json",
                "title,body,additions,deletions,changedFiles,commits,state,isDraft,"
                "mergeStateStatus,reviewDecision,createdAt,updatedAt,mergedAt,headRefName,"
                "statusCheckRollup",
            ],
            capture_output=True,
            text=True,
            timeout=15,
        )
        if result.returncode != 0:
            return {"title": "", "body": "", "commits": [], "error": result.stderr.strip()[:200]}
        data = json.loads(result.stdout)
        commits = [
            {"sha": c.get("oid", "")[:8], "msg": (c.get("messageHeadline") or "")[:120]}
            for c in (data.get("commits") or [])[:10]
        ]
        return {
            "title": data.get("title", ""),
            "body": (data.get("body", "") or "")[:1500],
            "additions": data.get("additions", 0),
            "deletions": data.get("deletions", 0),
            "changed_files": data.get("changedFiles", 0),
            "commits": commits,
            "state": data.get("state", ""),
            "is_draft": bool(data.get("isDraft")),
            "merge_state_status": data.get("mergeStateStatus", ""),
            "review_decision": data.get("reviewDecision", ""),
            "created_at": data.get("createdAt"),
            "updated_at": data.get("updatedAt"),
            "merged_at": data.get("mergedAt"),
            "head_ref_name": data.get("headRefName", ""),
            "status_check_rollup": data.get("statusCheckRollup") or [],
        }
    except (subprocess.TimeoutExpired, json.JSONDecodeError, FileNotFoundError) as e:
        return {"title": "", "body": "", "commits": [], "error": str(e)[:200]}


def fetch_pr_diff(pr_url: str, max_chars: int = 3500) -> str:
    """Fetch the PR diff (bounded). Truncates head of the diff for the judge."""
    pr_id = pr_url.rstrip("/").rsplit("/", 1)[-1]
    if not pr_id.isdigit():
        return ""
    try:
        result = subprocess.run(
            ["gh", "pr", "diff", pr_id],
            capture_output=True,
            text=True,
            timeout=20,
        )
        if result.returncode != 0:
            return ""
        diff = result.stdout
        if len(diff) > max_chars:
            return diff[:max_chars] + f"\n\n[diff truncated; full size: {len(diff)} chars]"
        return diff
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return ""


def classify_pr_outcome(pr_summary: dict) -> str:
    state = str(pr_summary.get("state") or "").upper()
    if pr_summary.get("is_draft"):
        return "draft"
    if state == "MERGED" or pr_summary.get("merged_at"):
        return "merged"
    if state == "CLOSED":
        return "closed"
    if state == "OPEN":
        merge_state = str(pr_summary.get("merge_state_status") or "").upper()
        if merge_state in {"DIRTY", "CONFLICTING"}:
            return "conflicted"
        return "open"
    return "unknown"


def classify_ci_outcome(pr_summary: dict) -> str:
    checks = pr_summary.get("status_check_rollup") or []
    if not checks:
        return "none"
    states = {
        str(item.get("conclusion") or item.get("status") or item.get("state") or "").upper()
        for item in checks
    }
    if any(state in {"FAILURE", "FAILED", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED"} for state in states):
        return "failed"
    if any(state in {"PENDING", "QUEUED", "IN_PROGRESS", "WAITING", "REQUESTED"} for state in states):
        return "pending"
    if all(state in {"SUCCESS", "SKIPPED", "NEUTRAL"} for state in states if state):
        return "passed"
    return "unknown"


def classify_review_outcome(pr_summary: dict) -> str:
    decision = str(pr_summary.get("review_decision") or "").upper()
    mapping = {
        "APPROVED": "approved",
        "CHANGES_REQUESTED": "changes_requested",
        "REVIEW_REQUIRED": "pending",
        "COMMENTED": "commented",
    }
    return mapping.get(decision, "pending" if classify_pr_outcome(pr_summary) == "open" else "unknown")


def build_outcome_metadata(ticket: dict, pr_summary: dict, pr_diff: str, role: str, task_class: str) -> dict:
    task = ticket.get("task") or ticket
    title = task.get("title", "")
    body = task.get("body") or task.get("description") or ""
    capabilities = infer_capabilities(title, body, pr_summary.get("title", ""), pr_summary.get("body", ""), pr_diff)
    return {
        "role": role,
        "task_class": task_class,
        "complexity_bucket": complexity_bucket(
            int(pr_summary.get("additions", 0)),
            int(pr_summary.get("deletions", 0)),
            int(pr_summary.get("changed_files", 0)),
            len(pr_summary.get("commits", [])),
        ),
        "capabilities": capabilities,
        "pr_outcome": classify_pr_outcome(pr_summary),
        "ci_outcome": classify_ci_outcome(pr_summary),
        "review_outcome": classify_review_outcome(pr_summary),
        "pr_created_at": parse_ts(pr_summary.get("created_at")),
        "pr_updated_at": parse_ts(pr_summary.get("updated_at")),
        "pr_merged_at": parse_ts(pr_summary.get("merged_at")),
    }


def build_judge_prompt(
    ticket_id: str,
    ticket: dict,
    pr_url: str,
    pr_summary: dict,
    pr_diff: str,
    driver: str,
    model: str,
    task_class: str,
) -> str:
    task = ticket.get("task") or ticket
    title = task.get("title", "")
    body = (task.get("body") or task.get("description") or "")[:1500]

    return f"""You are the post-PR judge for the chitin swarm. Score the work {driver}/{model} produced for ticket {ticket_id} on a 5-dimension rubric. Each dimension scores 1-5 (1=poor, 3=adequate, 5=excellent). Total score in [5, 25].

# Ticket {ticket_id} ({task_class})
Title: {title}
Body: {body}

# PR {pr_url}
Title: {pr_summary.get("title", "")}
Body: {pr_summary.get("body", "")}
Diff stats: +{pr_summary.get("additions", 0)} / -{pr_summary.get("deletions", 0)}, {pr_summary.get("changed_files", 0)} files
Commits:
{chr(10).join(f"  {c['sha']}  {c['msg']}" for c in pr_summary.get("commits", []))}

# PR diff (truncated)
```
{pr_diff[:3500]}
```

# Rubric
- code_quality       Clarity, idioms, no dead code, sensible structure
- test_coverage      New tests added, exercise the change, edge cases
- scope_adherence    Matches the ticket; no scope creep
- efficiency         Time-to-PR, no thrashing, no over-iteration (judge from commit history)
- review_friendliness Small diff, good commit messages, well-bounded

# Output
Reply with ONLY a JSON object (no prose, no markdown):
{{
  "code_quality": <1-5>,
  "test_coverage": <1-5>,
  "scope_adherence": <1-5>,
  "efficiency": <1-5>,
  "review_friendliness": <1-5>,
  "reasoning": "<one-paragraph rationale citing specific files / lines / commits>"
}}"""


def call_judge(prompt: str, judge_model: str, timeout_seconds: int = 240) -> dict | None:
    """Call the judge LLM. Returns parsed dict or None on failure."""
    prompt_path = None
    try:
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", prefix="clawta-judge-", suffix=".txt", delete=False) as fh:
            fh.write(prompt)
            prompt_path = fh.name
        message = (
            "Read the complete post-PR judge prompt from "
            f"{prompt_path}. Reply with ONLY the JSON object requested in that file."
        )
        result = subprocess.run(
            ["clawta", "--text", message],
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except (subprocess.TimeoutExpired, FileNotFoundError) as e:
        print(f"judge: clawta call failed: {e}", file=sys.stderr)
        return None
    finally:
        if prompt_path:
            try:
                os.unlink(prompt_path)
            except OSError:
                pass

    if result.returncode != 0:
        print(f"judge: clawta returned {result.returncode}", file=sys.stderr)
        return None

    body = result.stdout or ""
    match = re.search(r"\{[^{}]*\"code_quality\"[^{}]*\}", body, re.DOTALL)
    if not match:
        match = re.search(r"\{.*\"code_quality\".*\}", body, re.DOTALL)
    if not match:
        print(f"judge: no JSON in clawta reply: {body[:200]!r}", file=sys.stderr)
        return None

    try:
        parsed = json.loads(match.group(0))
    except json.JSONDecodeError as e:
        print(f"judge: JSON parse failed: {e}", file=sys.stderr)
        return None

    required = {"code_quality", "test_coverage", "scope_adherence", "efficiency", "review_friendliness"}
    if not required.issubset(parsed.keys()):
        print(f"judge: missing required fields. Got: {sorted(parsed.keys())}", file=sys.stderr)
        return None

    return parsed


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--ticket", required=True, help="Kanban ticket id (e.g., t_XXXXXX)")
    ap.add_argument("--pr-url", required=True, help="GitHub PR url")
    ap.add_argument("--driver", required=True, help="Agent that did the work (e.g., codex)")
    ap.add_argument("--model", required=True, help="Model that did the work (e.g., gpt-5.5)")
    ap.add_argument("--role", default="programmer", help="Worker role that produced the PR")
    ap.add_argument("--task-class", default=None, help="Override task class (auto-inferred if absent)")
    ap.add_argument("--judge-model", default=DEFAULT_JUDGE, help=f"Judge LLM (default: {DEFAULT_JUDGE})")
    ap.add_argument("--inferred", action="store_true", help="Mark dispatch metadata as inferred/backfilled")
    ap.add_argument("--dry-run", action="store_true", help="Print prompt + scores; do not write to DB")
    args = ap.parse_args()

    ticket = fetch_ticket(args.ticket)
    if ticket.get("error"):
        print(f"judge: ticket fetch failed: {ticket['error']}", file=sys.stderr)
        return 2

    task = ticket.get("task") or ticket
    title = task.get("title", "")
    body = task.get("body") or task.get("description") or ""
    task_class = args.task_class or infer_task_class(title, body)

    pr_summary = fetch_pr_summary(args.pr_url)
    pr_diff = fetch_pr_diff(args.pr_url)
    metadata = build_outcome_metadata(ticket, pr_summary, pr_diff, args.role, task_class)

    prompt = build_judge_prompt(
        args.ticket, ticket, args.pr_url, pr_summary, pr_diff,
        args.driver, args.model, task_class,
    )

    if args.dry_run:
        print(prompt)
        return 0

    scores = call_judge(prompt, args.judge_model)
    if scores is None:
        print("judge: failed to obtain scores", file=sys.stderr)
        return 3

    reasoning = scores.pop("reasoning", "")
    total = sum(int(scores[k]) for k in (
        "code_quality", "test_coverage", "scope_adherence",
        "efficiency", "review_friendliness",
    ))

    conn = elo.open_db()
    score_id = elo.record_score(
        conn, args.ticket, args.pr_url, args.driver, args.model,
        task_class, scores, args.judge_model, reasoning,
        role=metadata["role"],
        complexity_bucket=metadata["complexity_bucket"],
        capabilities=metadata["capabilities"],
        pr_outcome=metadata["pr_outcome"],
        ci_outcome=metadata["ci_outcome"],
        review_outcome=metadata["review_outcome"],
        pr_created_at=metadata["pr_created_at"],
        pr_updated_at=metadata["pr_updated_at"],
        pr_merged_at=metadata["pr_merged_at"],
        inferred=args.inferred,
    )
    new_elo = elo.update_elo(
        conn, args.driver, args.model, task_class, total,
        last_dispatch_id=args.ticket,
        role=metadata["role"],
        complexity_bucket=metadata["complexity_bucket"],
        capabilities=metadata["capabilities"],
        pr_outcome=metadata["pr_outcome"],
        ci_outcome=metadata["ci_outcome"],
        review_outcome=metadata["review_outcome"],
    )

    print(json.dumps({
        "score_id": score_id,
        "ticket": args.ticket,
        "driver": args.driver,
        "model": args.model,
        "role": metadata["role"],
        "task_class": task_class,
        "complexity_bucket": metadata["complexity_bucket"],
        "capabilities": metadata["capabilities"],
        "pr_outcome": metadata["pr_outcome"],
        "ci_outcome": metadata["ci_outcome"],
        "review_outcome": metadata["review_outcome"],
        "scores": scores,
        "total": total,
        "new_elo": round(new_elo, 1),
        "reasoning": reasoning,
        "inferred": bool(args.inferred),
    }, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
