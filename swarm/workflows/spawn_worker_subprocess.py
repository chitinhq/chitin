#!/usr/bin/env python3
"""
Spawn a frontier-coder worker as a subprocess with output capture.

This replaces the lobster `exec` pattern with subprocess + wait, enabling:
- Output capture
- Result observation
- Proper failure handling
- Parent-child session tracking

Reads config from stdin (JSON):
  {
    "driver": "claude-code",
    "model": "claude-opus-4-7",
    "worktree": "/path/to/worktree",
    "branch": "swarm/claude-code-t_xxx",
    "cmd": "claude",
    "args": ["--model", "...", "-p", "..."],
    "env": {
      "CHITIN_DRIVER": "clawta",
      "CHITIN_BUDGET_ENVELOPE": "..."
    }
  }

Outputs JSON:
  {
    "status": "completed" | "completed_no_commit" | "failed" | "timeout",
    "returncode": <int>,
    "stdout": "<worker stdout>",
    "stderr": "<worker stderr>",
    "error": "<error message if status != completed>",
    "exit_reason": "<machine-readable failure classification>",
    "transcript_tail": "<last lines of stdout/stderr>",
    "commit_count_ahead": <int>
  }
"""

import fnmatch
import json
import os
import re
import subprocess
import sys
import hashlib
from pathlib import Path


TRANSCRIPT_TAIL_LINES = 40
SPEC_KIT_REF_RE = re.compile(
    r"`?(?P<path>(?:[A-Za-z0-9_.~/-]*/)?\.specify/specs/"
    r"(?P<slug>[0-9A-Za-z][0-9A-Za-z_.-]*)/spec\.md)`?"
)
FILE_SCOPE_HEADING_RE = re.compile(r"^##\s+File-system scope\s*$", re.IGNORECASE)
NEXT_H2_RE = re.compile(r"^##\s+")


def prepare_worker_command(config: dict) -> tuple[list[str], str | None]:
    """Build a worker command line while keeping long prompts out of argv.

    Returns (argv, stdin_text). The caller is responsible for feeding
    stdin_text to the subprocess when non-None.
    """
    driver = config.get("driver", "unknown")
    model = config.get("model", "")
    prompt = config.get("prompt", "")
    system_prompt = config.get("system_prompt", "")
    args_template = config.get("args", [])
    if system_prompt and driver == "codex":
        prompt = f"{system_prompt}\n\n{prompt}" if prompt else system_prompt

    argv: list[str] = [config.get("cmd", "")]
    stdin_text: str | None = None

    for arg in args_template:
        if arg == "{model}":
            argv.append(model)
            continue

        if arg == "{prompt}":
            if driver in {"codex", "copilot"}:
                stdin_text = prompt
                continue
            if driver == "gemini":
                # gemini CLI: `-p ""` selects non-interactive mode while
                # leaving the prompt body to be read from stdin. The empty
                # argv slot is the flag value, not the prompt — the real
                # prompt travels via stdin_text so it stays off argv.
                argv.append("")
                stdin_text = prompt
                continue
            argv.append(prompt)
            continue

        argv.append(arg)

    if system_prompt and driver == "copilot":
        argv.extend(["--append-system-prompt", system_prompt])

    if prompt and driver == "gemini" and "{prompt}" not in args_template:
        # Card has no {prompt} placeholder: append `-p ""` ourselves so the
        # gemini CLI still runs non-interactively and consumes stdin_text.
        argv.extend(["-p", ""])
        stdin_text = prompt

    return argv, stdin_text


def materialize_driver_prompt_artifacts(config: dict, cwd: str) -> None:
    system_prompt = str(config.get("system_prompt", "") or "")
    if not system_prompt:
        return
    if config.get("driver") == "claude-code":
        Path(cwd, "CLAUDE.md").write_text(system_prompt + "\n", encoding="utf-8")


def resolve_soul_file(config: dict) -> Path | None:
    soul_id = str(config.get("soul_id", "") or "").strip()
    if not soul_id:
        return None
    override = str(config.get("souls_dir") or os.environ.get("CHITIN_SOULS_DIR", "")).strip()
    if override:
        candidate = Path(override).expanduser() / f"{soul_id}.md"
        if candidate.is_file():
            return candidate
    repo = str(config.get("repo_root") or os.environ.get("CHITIN_REPO", "")).strip()
    if repo:
        for rel in ("souls/canonical", "souls/experimental"):
            candidate = Path(repo).expanduser() / rel / f"{soul_id}.md"
            if candidate.is_file():
                return candidate
    return None


def compute_file_sha256(path: Path | None) -> str | None:
    if path is None or not path.is_file():
        return None
    return hashlib.sha256(path.read_bytes()).hexdigest()


def build_transcript_tail(stdout: str, stderr: str, max_lines: int = TRANSCRIPT_TAIL_LINES) -> str:
    sections: list[str] = []
    if stdout:
        sections.extend(f"[stdout] {line}" for line in stdout.splitlines())
    if stderr:
        sections.extend(f"[stderr] {line}" for line in stderr.splitlines())
    if not sections:
        return ""
    return "\n".join(sections[-max_lines:])


def resolve_base_ref(config: dict | None, cwd: str) -> str:
    """Resolve the remote base ref used to count worker commits.

    Board dispatches are not always based on origin/main: readybench uses
    origin/swarm, and other boards may use origin/develop. Prefer the
    explicit board default passed by the workflow, then env override, then
    origin/HEAD, then the legacy origin/main fallback.
    """
    config = config or {}
    raw = str(
        config.get("default_branch")
        or os.environ.get("CHITIN_DEFAULT_BRANCH")
        or os.environ.get("CHITIN_BASE_BRANCH")
        or ""
    ).strip()
    if raw:
        if raw.startswith("origin/") or raw.startswith("refs/"):
            return raw
        return f"origin/{raw}"

    origin_head = subprocess.run(
        ["git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if origin_head.returncode == 0:
        ref = origin_head.stdout.strip()
        if ref:
            return ref

    return "origin/main"


def commits_ahead_of_base(cwd: str, config: dict | None = None) -> int:
    base_ref = resolve_base_ref(config, cwd)
    merge_base = subprocess.run(
        ["git", "merge-base", base_ref, "HEAD"],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if merge_base.returncode == 0:
        result = subprocess.run(
            ["git", "rev-list", "--count", f"{merge_base.stdout.strip()}..HEAD"],
            cwd=cwd,
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            return int(result.stdout.strip() or "0")

    fallback = subprocess.run(
        ["git", "rev-list", "--count", f"{base_ref}..HEAD"],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if fallback.returncode != 0:
        detail = (fallback.stderr or merge_base.stderr).strip()
        raise RuntimeError(f"unable to count commits ahead of {base_ref}: {detail}".strip())
    return int(fallback.stdout.strip() or "0")


def extract_spec_ref(text: str) -> dict | None:
    match = SPEC_KIT_REF_RE.search(text or "")
    if not match:
        return None
    return {"path": match.group("path"), "slug": match.group("slug")}


def extract_spec_path(text: str) -> str | None:
    ref = extract_spec_ref(text)
    return str(ref["path"]) if ref else None


def _scope_tokens_from_line(line: str) -> list[str]:
    stripped = line.strip()
    if not stripped or stripped.startswith("#"):
        return []
    stripped = re.sub(r"^[-*+]\s+", "", stripped)
    stripped = re.sub(r"^\d+[.)]\s+", "", stripped)
    code_spans = re.findall(r"`([^`]+)`", stripped)
    negated_code_span = bool(code_spans and stripped.split("`", 1)[0].strip().endswith("!"))
    candidates = code_spans or re.split(r"[\s,]+", re.sub(r"^[A-Za-z _-]+:\s*", "", stripped))
    tokens: list[str] = []
    for raw in candidates:
        token = raw.strip().strip("'\"")
        if negated_code_span and not token.startswith("!"):
            token = "!" + token
        if not token:
            continue
        negated = token.startswith("!")
        body = token[1:] if negated else token
        if "/" not in body and not any(ch in body for ch in "*?[]"):
            continue
        if "://" in body:
            continue
        tokens.append(("!" if negated else "") + body.lstrip("./"))
    return tokens


def extract_file_scope_globs(spec_text: str) -> list[str]:
    in_scope = False
    globs: list[str] = []
    for line in (spec_text or "").splitlines():
        if FILE_SCOPE_HEADING_RE.match(line.strip()):
            in_scope = True
            continue
        if in_scope and NEXT_H2_RE.match(line.strip()):
            break
        if in_scope:
            globs.extend(_scope_tokens_from_line(line))
    return globs


def load_file_scope(config: dict | None) -> dict:
    config = config or {}
    explicit = config.get("file_scope")
    if isinstance(explicit, list) and explicit:
        return {"source": "config", "globs": [str(item) for item in explicit if str(item).strip()]}

    ticket_text = str(config.get("ticket_body") or config.get("prompt") or "")
    spec_ref = extract_spec_ref(ticket_text)
    if not spec_ref:
        return {"source": "none", "globs": [], "reason": "no spec-kit path found"}

    candidates: list[Path] = []
    spec_root = str(config.get("spec_root") or os.environ.get("CHITIN_SPEC_ROOT") or "").strip()
    if spec_root:
        candidates.append(Path(spec_root).expanduser() / str(spec_ref["slug"]) / "spec.md")

    workspace_root = str(config.get("workspace_root") or os.environ.get("WORKSPACE_ROOT") or "").strip()
    if workspace_root:
        candidates.append(Path(workspace_root).expanduser() / str(spec_ref["path"]))

    repo_root = Path(str(config.get("repo_root") or os.environ.get("CHITIN_REPO", "") or ".")).expanduser()
    candidates.append(repo_root / str(spec_ref["path"]))

    candidate = next((path for path in candidates if path.is_file()), candidates[0])
    if not candidate.is_file():
        checked = ", ".join(str(path) for path in candidates)
        return {"source": str(spec_ref["path"]), "globs": [], "reason": f"spec file not found; checked: {checked}"}

    globs = extract_file_scope_globs(candidate.read_text(encoding="utf-8"))
    if not globs:
        return {"source": str(candidate), "globs": [], "reason": "missing File-system scope section"}
    return {"source": str(candidate), "globs": globs}


def changed_files_since_base(cwd: str, config: dict | None = None) -> list[str]:
    base_ref = resolve_base_ref(config, cwd)
    merge_base = subprocess.run(
        ["git", "merge-base", base_ref, "HEAD"],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    base = merge_base.stdout.strip() if merge_base.returncode == 0 else base_ref
    diff = subprocess.run(
        ["git", "diff", "--name-only", f"{base}..HEAD"],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if diff.returncode != 0:
        raise RuntimeError((diff.stderr or merge_base.stderr).strip() or f"unable to diff against {base_ref}")
    return [line.strip() for line in diff.stdout.splitlines() if line.strip()]


def _matches_scope(path: str, pattern: str) -> bool:
    path = path.lstrip("./")
    pattern = pattern.lstrip("./")
    if fnmatch.fnmatchcase(path, pattern):
        return True
    if pattern.endswith("/**"):
        prefix = pattern[:-3].rstrip("/") + "/"
        return path.startswith(prefix)
    return False


def validate_file_scope(cwd: str, config: dict | None = None) -> dict:
    scope = load_file_scope(config)
    globs = scope.get("globs") or []
    if not globs:
        reason = str(scope.get("reason") or "")
        has_spec_ref = scope.get("source") not in {None, "none"}
        if has_spec_ref and "missing File-system scope" in reason:
            return {
                "ok": False,
                "enforced": False,
                **scope,
                "changed_files": changed_files_since_base(cwd, config),
                "violations": ["<missing File-system scope>"],
            }
        return {"ok": True, "enforced": False, **scope, "changed_files": [], "violations": []}

    changed = changed_files_since_base(cwd, config)
    allow = [g for g in globs if not g.startswith("!")]
    deny = [g[1:] for g in globs if g.startswith("!")]
    violations: list[str] = []
    for path in changed:
        denied = any(_matches_scope(path, pattern) for pattern in deny)
        allowed = any(_matches_scope(path, pattern) for pattern in allow) if allow else True
        if denied or not allowed:
            violations.append(path)
    return {
        "ok": not violations,
        "enforced": True,
        **scope,
        "changed_files": changed,
        "violations": violations,
    }


def snapshot_event_files(chitin_home: str) -> dict[str, int]:
    root = Path(chitin_home).expanduser()
    if not root.is_dir():
        return {}
    snapshot: dict[str, int] = {}
    for path in root.glob("*events-*.jsonl"):
        try:
            snapshot[str(path)] = path.stat().st_mtime_ns
        except OSError:
            continue
    return snapshot


def changed_files_since_base(cwd: str, config: dict | None = None) -> list[str]:
    """Return paths the worker touched (committed or staged-or-working) relative
    to the resolved base ref. Used by the post-spawn path-scope validator."""
    config = config or {}
    base_ref = resolve_base_ref(config, cwd)
    # Prefer merge-base..HEAD to capture only the worker's own commits.
    merge_base = subprocess.run(
        ["git", "merge-base", base_ref, "HEAD"],
        cwd=cwd, capture_output=True, text=True,
    )
    if merge_base.returncode == 0 and merge_base.stdout.strip():
        diff = subprocess.run(
            ["git", "diff", "--name-only", f"{merge_base.stdout.strip()}..HEAD"],
            cwd=cwd, capture_output=True, text=True,
        )
    else:
        diff = subprocess.run(
            ["git", "diff", "--name-only", f"{base_ref}..HEAD"],
            cwd=cwd, capture_output=True, text=True,
        )
    if diff.returncode != 0:
        return []
    return [line for line in diff.stdout.splitlines() if line.strip()]


def _glob_to_re(pattern: str) -> re.Pattern:
    """Translate a shell-style glob ('apps/portal/**', 'apps/portal/*.json',
    'frontend/**') to a regex. ** matches any path including '/', * matches
    one path segment (no '/'). Anchored at start and end."""
    out = ""
    i = 0
    while i < len(pattern):
        c = pattern[i]
        if c == "*" and i + 1 < len(pattern) and pattern[i + 1] == "*":
            # ** — any chars including /
            out += ".*"
            i += 2
            # Eat trailing slash so 'foo/**' matches 'foo' too
            if i < len(pattern) and pattern[i] == "/":
                i += 1
        elif c == "*":
            # single * — any chars except /
            out += "[^/]*"
            i += 1
        elif c == "?":
            out += "[^/]"
            i += 1
        elif c in ".^$+(){}[]|\\":
            out += "\\" + c
            i += 1
        else:
            out += c
            i += 1
    return re.compile("^" + out + "$")


def validate_path_scope(
    changed_paths: list[str],
    may_globs: list[str],
    must_not_globs: list[str],
) -> tuple[bool, list[str]]:
    """Return (ok, offending_paths). A path is offending if it matches any
    MUST-NOT glob, OR it matches no MAY glob (when MAY globs are supplied).

    Empty changed_paths returns (True, []) — no work to enforce against.
    Empty may_globs + empty must_not_globs returns (True, []) — no scope
    declared, no enforcement (back-compat for specs missing the section).
    """
    if not changed_paths:
        return True, []
    if not may_globs and not must_not_globs:
        return True, []
    may = [_glob_to_re(g) for g in may_globs]
    must_not = [_glob_to_re(g) for g in must_not_globs]
    offending: list[str] = []
    for path in changed_paths:
        if any(rx.match(path) for rx in must_not):
            offending.append(path)
            continue
        if may and not any(rx.match(path) for rx in may):
            offending.append(path)
    return (not offending, offending)


def detect_event_chain(
    chitin_home: str, before: dict[str, int]
) -> tuple[str | None, str | None]:
    after = snapshot_event_files(chitin_home)
    changed: list[tuple[str, int]] = []
    for path, mtime in after.items():
        if path not in before or mtime > before[path]:
            changed.append((path, mtime))
    if not changed:
        return None, None

    chain_file = max(changed, key=lambda item: item[1])[0]
    last_hash: str | None = None
    try:
        for raw in Path(chain_file).read_text().splitlines():
            line = raw.strip()
            if not line:
                continue
            try:
                payload = json.loads(line)
            except json.JSONDecodeError:
                continue
            this_hash = payload.get("this_hash")
            if this_hash:
                last_hash = str(this_hash)
    except OSError:
        return chain_file, None
    return chain_file, last_hash


def summarize_completed_run(
    config: dict,
    returncode: int,
    stdout: str,
    stderr: str,
    cwd: str,
    event_chain_file: str | None = None,
    event_chain_hash: str | None = None,
    soul_hash_mismatch: bool = False,
    observed_soul_hash: str | None = None,
) -> dict:
    transcript_tail = build_transcript_tail(stdout, stderr)
    commit_count_ahead = commits_ahead_of_base(cwd, config)
    driver = config.get("driver", "unknown")
    model = config.get("model", "")

    # Post-spawn path-scope validator (per workspace Constitution §1.1,
    # added 2026-05-17 after portal MVP day-0 retro). If the spec declares
    # MAY-write / MUST-NOT-write globs and the worker touched files outside,
    # mark the run as failed with scope_violation so the lobster wrapper
    # closes the PR + demotes the ticket. Spec authors pass globs through
    # spawn config as `file_system_scope: {may: [...], must_not: [...]}`.
    # If the spec didn't declare scope, this is a no-op (back-compat).
    scope = config.get("file_system_scope") or {}
    may_globs = scope.get("may") or []
    must_not_globs = scope.get("must_not") or []
    if commit_count_ahead > 0 and (may_globs or must_not_globs):
        changed = changed_files_since_base(cwd, config)
        ok, offending = validate_path_scope(changed, may_globs, must_not_globs)
        if not ok:
            offending_str = ", ".join(offending[:10])
            if len(offending) > 10:
                offending_str += f", … +{len(offending) - 10} more"
            return {
                "status": "failed",
                "returncode": returncode,
                "stdout": stdout,
                "stderr": stderr,
                "error": (
                    f"scope_violation: worker touched files outside declared "
                    f"file_system_scope: {offending_str}"
                ),
                "exit_reason": "scope_violation",
                "transcript_tail": transcript_tail,
                "commit_count_ahead": commit_count_ahead,
                "driver": driver,
                "model": model,
                "event_chain_file": event_chain_file,
                "event_chain_hash": event_chain_hash,
                "soul_hash_mismatch": soul_hash_mismatch,
                "observed_soul_hash": observed_soul_hash,
                "audit_flags": (
                    ["scope_violation"]
                    + (["soul_hash_mismatch"] if soul_hash_mismatch else [])
                ),
                "offending_paths": offending,
            }

    if returncode == 0 and commit_count_ahead == 0:
        return {
            "status": "completed_no_commit",
            "returncode": returncode,
            "stdout": stdout,
            "stderr": stderr,
            "error": "Worker session ended without creating any commits",
            "exit_reason": "model-concluded-nothing",
            "transcript_tail": transcript_tail,
            "commit_count_ahead": commit_count_ahead,
            "driver": driver,
            "model": model,
            "event_chain_file": event_chain_file,
            "event_chain_hash": event_chain_hash,
            "soul_hash_mismatch": soul_hash_mismatch,
            "observed_soul_hash": observed_soul_hash,
            "audit_flags": ["soul_hash_mismatch"] if soul_hash_mismatch else [],
        }

    path_scope = validate_file_scope(cwd, config) if returncode == 0 and commit_count_ahead > 0 else {"ok": True, "enforced": False}
    if not path_scope.get("ok", True):
        violations = ", ".join(path_scope.get("violations", []))
        reason = str(path_scope.get("reason") or "")
        missing_scope = "missing File-system scope" in reason
        error = (
            f"Spec is missing required File-system scope section: {path_scope.get('source')}"
            if missing_scope
            else f"Worker changed files outside declared File-system scope: {violations}"
        )
        return {
            "status": "failed",
            "returncode": -1,
            "stdout": stdout,
            "stderr": stderr,
            "error": error,
            "exit_reason": "path-scope-missing" if missing_scope else "path-scope-violation",
            "transcript_tail": transcript_tail,
            "commit_count_ahead": commit_count_ahead,
            "driver": driver,
            "model": model,
            "event_chain_file": event_chain_file,
            "event_chain_hash": event_chain_hash,
            "soul_hash_mismatch": soul_hash_mismatch,
            "observed_soul_hash": observed_soul_hash,
            "path_scope": path_scope,
            "audit_flags": ["soul_hash_mismatch"] if soul_hash_mismatch else [],
        }

    status = "completed" if returncode == 0 else "failed"
    return {
        "status": status,
        "returncode": returncode,
        "stdout": stdout,
        "stderr": stderr,
        "error": None if status == "completed" else f"Worker exited with code {returncode}",
        "exit_reason": None if status == "completed" else "session-error",
        "transcript_tail": transcript_tail,
        "commit_count_ahead": commit_count_ahead,
        "driver": driver,
        "model": model,
        "event_chain_file": event_chain_file,
        "event_chain_hash": event_chain_hash,
        "soul_hash_mismatch": soul_hash_mismatch,
        "observed_soul_hash": observed_soul_hash,
        "path_scope": path_scope,
        "audit_flags": ["soul_hash_mismatch"] if soul_hash_mismatch else [],
    }

def main():
    try:
        # Read config from stdin
        config_json = sys.stdin.read()
        config = json.loads(config_json)

        driver = config.get("driver", "unknown")
        cmd = config.get("cmd", "")
        worktree = config.get("worktree", "")
        env_vars = config.get("env", {})

        if not cmd:
            print(json.dumps({
                "status": "failed",
                "returncode": 1,
                "stdout": "",
                "stderr": "No cmd specified in config",
                "error": "Missing cmd in config"
            }))
            return 1

        # Prepare environment
        env = os.environ.copy()
        env.update(env_vars)
        chitin_home = env.get("CHITIN_HOME", os.path.join(os.path.expanduser("~"), ".chitin"))

        # Prepare working directory
        cwd = worktree if worktree else os.getcwd()
        if not os.path.isdir(cwd):
            print(json.dumps({
                "status": "failed",
                "returncode": 1,
                "stdout": "",
                "stderr": f"Worktree directory not found: {cwd}",
                "error": f"Worktree missing: {cwd}"
            }))
            return 1

        # Spawn worker process. Driver-specific prompt handling keeps
        # large ticket bodies off argv where possible.
        full_cmd, stdin_text = prepare_worker_command(config)
        materialize_driver_prompt_artifacts(config, cwd)
        soul_file = resolve_soul_file(config)
        initial_soul_hash = compute_file_sha256(soul_file)
        event_files_before = snapshot_event_files(chitin_home)
        try:
            result = subprocess.run(
                full_cmd,
                cwd=cwd,
                env=env,
                capture_output=True,
                text=True,
                input=stdin_text,
                timeout=3600  # 1 hour timeout, same as before
            )
            event_chain_file, event_chain_hash = detect_event_chain(chitin_home, event_files_before)
            observed_soul_hash = compute_file_sha256(soul_file) or initial_soul_hash
            expected_soul_hash = str(config.get("soul_hash", "") or "").strip() or initial_soul_hash
            soul_hash_mismatch = bool(expected_soul_hash and observed_soul_hash and expected_soul_hash != observed_soul_hash)

            output = summarize_completed_run(
                config,
                result.returncode,
                result.stdout,
                result.stderr,
                cwd,
                event_chain_file=event_chain_file,
                event_chain_hash=event_chain_hash,
                soul_hash_mismatch=soul_hash_mismatch,
                observed_soul_hash=observed_soul_hash,
            )
            if output["status"] == "completed_no_commit":
                print(
                    json.dumps(
                        {
                            "exit_reason": output["exit_reason"],
                            "model": output["model"],
                            "driver": output["driver"],
                            "commit_count_ahead": output["commit_count_ahead"],
                            "transcript_tail": output["transcript_tail"],
                        }
                    ),
                    file=sys.stderr,
                )
            print(json.dumps(output))
            return 0

        except subprocess.TimeoutExpired:
            # A timed-out worker may still have written Chitin events before
            # it was killed — detect the chain so the run ledger keeps the
            # link instead of recording a null hash.
            timeout_chain_file, timeout_chain_hash = detect_event_chain(
                chitin_home, event_files_before
            )
            print(json.dumps({
                "status": "timeout",
                "returncode": -1,
                "stdout": "",
                "stderr": "Worker process timed out after 3600 seconds",
                "error": "Worker timeout",
                "exit_reason": "session-timeout",
                "transcript_tail": "",
                "commit_count_ahead": 0,
                "driver": driver,
                "model": config.get("model", ""),
                "event_chain_file": timeout_chain_file,
                "event_chain_hash": timeout_chain_hash,
            }))
            return 1

        except Exception as e:
            print(json.dumps({
                "status": "failed",
                "returncode": -1,
                "stdout": "",
                "stderr": str(e),
                "error": f"Failed to spawn worker: {str(e)}",
                "exit_reason": "session-error",
                "transcript_tail": "",
                "commit_count_ahead": 0,
                "driver": driver,
                "model": config.get("model", ""),
                "event_chain_file": None,
                "event_chain_hash": None,
            }))
            return 1

    except Exception as e:
        print(json.dumps({
            "status": "failed",
            "returncode": -1,
            "stdout": "",
            "stderr": str(e),
            "error": f"Worker spawn error: {str(e)}",
            "exit_reason": "spawn-error",
            "transcript_tail": "",
            "commit_count_ahead": 0,
            "driver": "unknown",
            "model": "",
            "event_chain_file": None,
            "event_chain_hash": None,
        }), file=sys.stderr)
        return 1

if __name__ == "__main__":
    sys.exit(main())
