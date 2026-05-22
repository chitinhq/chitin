"""chitin-governance — hermes plugin that calls chitin-kernel gate on every
pre_tool_call and blocks denied actions.

Spec: docs/superpowers/specs/2026-04-22-chitin-governance-v1-design.md
"""
from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
from typing import Any, Dict, Optional

_CHITIN_KERNEL = os.environ.get("CHITIN_KERNEL_PATH") or shutil.which("chitin-kernel") or os.path.expanduser("~/.local/bin/chitin-kernel")
_GATE_TIMEOUT_SEC = 5

# CHITIN_POLICY_FILE — explicit path to the operator's chitin.yaml. When set,
# the plugin passes --policy-file to chitin-kernel so cwd doesn't matter for
# policy lookup. Required when hermes runs from worktrees of non-chitin repos
# or scratch dirs (the common case for swarm spawns); without this, the
# kernel's cwd-walk-upward returns no_policy_found and enforcement disappears.
#
# CHITIN_REQUIRE_POLICY=1 — fail-closed when no policy is found. Pairs with
# CHITIN_POLICY_FILE: with both set, the explicit file always loads (no
# fall-through); with only REQUIRE_POLICY set, the cwd-walk still happens
# but no_policy_found becomes a hard deny instead of an allow.
def _truthy(v: str) -> bool:
    return v.strip().lower() in ("1", "true", "yes", "on")

# Hermes's pre_tool_call runs in the gateway/agent process cwd, which is
# typically the hermes install dir — not where the shell command will
# actually execute. Hermes tool calls almost always start with
#   cd <path> && ...
# so we walk every `cd` in the chain and use the LAST one (shells update
# cwd left-to-right before running the final command). Any earlier cd is
# a stopover. Using the first cd (previous impl) was a bypass surface:
# `cd ~/scratch && cd ~/governed && rm -rf go/` would pick up ~/scratch.
#
# Patterns matched (all of these produce a capture):
#   cd /abs/path         → /abs/path
#   cd relative/path     → abs(getcwd()/relative/path)
#   cd ~/path            → expanded
#   cd "path with space" → path with space (I1 fix)
#   cd 'path with space' → path with space
_CD_RE = re.compile(
    r'\bcd\s+(?:"([^"]+)"|\'([^\']+)\'|([^\s;&|]+))'
)


def _resolve_cwd(tool_name: str, args: Dict[str, Any]) -> str:
    """Return the cwd the tool call would actually execute against.

    For shell tools, parse every `cd` in the command and return the final
    effective cwd. For non-shell tools, return the process cwd.
    """
    if tool_name in ("terminal", "bash", "shell"):
        cmd = (args or {}).get("command") or (args or {}).get("cmd") or ""
        # Track effective cwd as we walk the chain. Start from process cwd
        # and update on each cd (matching normal shell semantics).
        cwd = os.getcwd()
        for m in _CD_RE.finditer(cmd):
            # Match groups: (double-quoted, single-quoted, unquoted).
            # Exactly one is non-empty per match.
            target = next((g for g in m.groups() if g), "")
            if not target:
                continue
            target = os.path.expanduser(target)
            if os.path.isabs(target):
                cwd = target
            else:
                cwd = os.path.abspath(os.path.join(cwd, target))
        return cwd
    return os.getcwd()


def _on_pre_tool_call(tool_name: str, args: Dict[str, Any], session_id: str = "", **kwargs):
    """Called by hermes before every tool call.

    Returns None to allow, or a dict {"action":"block","message":...} to deny.
    Hermes uses the message as the agent's next-turn input.

    cwd is resolved from the tool args (e.g., `cd X && ...` prefix) rather
    than os.getcwd() because hermes runs in its install directory while
    tool calls execute shell commands that cd into governed trees.
    """
    cwd = _resolve_cwd(tool_name, args or {})
    cmd = [
        _CHITIN_KERNEL, "gate", "evaluate",
        "--tool", tool_name,
        "--args-json", json.dumps(args or {}),
        "--agent", "hermes",
        "--cwd", cwd,
    ]
    policy_file = os.environ.get("CHITIN_POLICY_FILE", "").strip()
    if policy_file:
        cmd.extend(["--policy-file", policy_file])
    # Forward hermes's session id so the kernel stamps chain_id +
    # session_id onto the decision row. Without it the console — which
    # groups decisions into sessions by chain_id||session_id||envelope_id
    # — cannot see hermes: this CLI path resolves an envelope only when
    # one is active, so envelope_id alone is not a reliable anchor.
    if session_id:
        cmd.extend(["--session-id", session_id])
    try:
        cp = subprocess.run(
            cmd,
            capture_output=True, text=True, timeout=_GATE_TIMEOUT_SEC,
        )
    except FileNotFoundError:
        return _block_message(
            reason="governance_disabled: chitin-kernel binary not found",
            suggestion=f"Install or set CHITIN_KERNEL_PATH. Looked at: {_CHITIN_KERNEL}",
            rule_id="gate_unreachable",
        )
    except subprocess.TimeoutExpired:
        return _block_message(
            reason="gate_unreachable: chitin-kernel gate timed out",
            suggestion="Check chitin-kernel health; try `chitin-kernel gate status`.",
            rule_id="gate_unreachable",
        )
    except Exception as exc:
        return _block_message(
            reason=f"gate_unreachable: {exc}",
            suggestion="Check chitin-kernel logs.",
            rule_id="gate_unreachable",
        )

    stdout = (cp.stdout or "").strip()
    if not stdout:
        return _block_message(
            reason="gate returned empty output",
            suggestion=f"Check stderr: {cp.stderr[:200] if cp.stderr else '(none)'}",
            rule_id="gate_unreachable",
        )
    try:
        decision = json.loads(stdout)
    except json.JSONDecodeError:
        return _block_message(
            reason=f"gate returned non-JSON: {stdout[:200]}",
            suggestion="",
            rule_id="gate_unreachable",
        )

    if decision.get("allowed"):
        return None  # allow

    # Policy-scope override: if the agent is running in a cwd with no
    # chitin.yaml up the tree, governance does not apply — fall through
    # to allow. This is a practical deviation from the spec's strict
    # fail-closed-on-no-policy: in practice hermes cwd is often outside
    # any governed repo (hermes-agent install dir, scratch dirs, etc.),
    # and blocking every tool call there breaks legitimate work. The
    # gate binary still fails closed when invoked directly; only the
    # plugin's auto-firing path honors this allowance.
    #
    # Operator opts out of the lenient default by setting
    # CHITIN_REQUIRE_POLICY=1 (typically in the swarm runner spawn env),
    # which makes no_policy_found a hard deny — the agent is told it
    # cannot proceed because the operator explicitly required policy
    # coverage. Combined with CHITIN_POLICY_FILE this is unreachable
    # (the explicit file always loads), so the strict-mode operator
    # effectively gets cwd-independent enforcement.
    if decision.get("rule_id") == "no_policy_found":
        if _truthy(os.environ.get("CHITIN_REQUIRE_POLICY", "")):
            return _block_message(
                reason="no chitin.yaml found in cwd and CHITIN_REQUIRE_POLICY=1; fail-closed",
                suggestion="Set CHITIN_POLICY_FILE to an explicit policy path, or run from a directory with chitin.yaml above it.",
                rule_id="no_policy_found",
                escalation="elevated",
            )
        return None  # no governance in this cwd — allow (legacy lenient default)

    mode = decision.get("mode", "enforce")
    if mode == "monitor":
        # Monitor mode = log-only, allow execution despite rule match.
        return None

    return _block_message(
        reason=decision.get("reason", "action blocked"),
        suggestion=decision.get("suggestion", ""),
        corrected=decision.get("corrected_command", ""),
        rule_id=decision.get("rule_id", "unknown"),
        escalation=decision.get("escalation", "normal"),
    )


def _block_message(*, reason: str, suggestion: str = "", corrected: str = "",
                   rule_id: str = "unknown", escalation: str = "normal"):
    """Format a block directive in the shape hermes expects (dict with
    action=block + message). Per hermes_cli/plugins.py:get_pre_tool_call_block_message,
    a plain string is silently ignored — the hook must return a dict.
    """
    parts = [f"Action blocked: {reason}"]
    if suggestion:
        parts.append(f"Suggestion: {suggestion}")
    if corrected:
        parts.append(f"Try: {corrected}")
    parts.append(f"(policy: {rule_id}, escalation: {escalation})")
    return {"action": "block", "message": "\n".join(parts)}


def register(ctx) -> None:
    """Hermes plugin entry point."""
    ctx.register_hook("pre_tool_call", _on_pre_tool_call)
