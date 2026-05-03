"""chitin_governance — opt-in side-effect gate for plugin authors.

When a chitin router plugin needs to perform an action with side
effects (file write, shell exec, network call), it should route
that action through this library FIRST. The library shells to
`chitin-kernel gate evaluate --hook-stdin` and returns the
deterministic verdict.

Usage:

    from chitin_governance import gate_action, GateBlocked

    # Check before writing a file
    try:
        gate_action(
            tool_name='Write',
            tool_input={'file_path': '/tmp/output.json'},
            agent='my-plugin',
            session_id='router-plugin-foo',
        )
    except GateBlocked as e:
        # Kernel denied — fall back gracefully
        print(f'Action blocked by chitin: {e}')
        return

    # If we get here, the kernel allowed the action
    with open('/tmp/output.json', 'w') as f:
        f.write(...)

Design notes:
  - This is OPT-IN. Plugins that don't import this lib aren't
    gated by chitin's governance. Operator's responsibility to
    only install trusted plugins (see plugins_trust in chitin.yaml).
    Hard sandbox enforcement (bubblewrap, seccomp) is a future
    work item.
  - Subprocess overhead (~10ms per gate call). Plugin authors
    should batch related actions when possible.
  - Returns the kernel's exact decision; allows the plugin to
    react (retry differently, escalate to operator, etc.).
"""
from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass


class GateBlocked(Exception):
    """Raised when chitin-kernel denies the proposed action."""

    def __init__(self, reason: str, rule_id: str | None = None):
        super().__init__(reason)
        self.reason = reason
        self.rule_id = rule_id


@dataclass(frozen=True)
class GateDecision:
    """Structured decision returned by chitin-kernel."""

    allowed: bool
    reason: str | None = None
    rule_id: str | None = None
    raw: dict | None = None


def gate_action(
    tool_name: str,
    tool_input: dict,
    agent: str = 'plugin',
    session_id: str | None = None,
    cwd: str | None = None,
    raise_on_deny: bool = True,
    kernel_binary: str = 'chitin-kernel',
    timeout_s: float = 5.0,
) -> GateDecision:
    """Run a hypothetical action through the chitin kernel gate.

    Returns a GateDecision; raises GateBlocked when raise_on_deny=True
    and the kernel denies. Falls open (allow) if the kernel binary is
    missing — protects plugins from chitin install errors silently
    blocking everything.
    """
    payload = {
        'hook_event_name': 'PreToolUse',
        'tool_name': tool_name,
        'tool_input': tool_input,
    }
    if session_id:
        payload['session_id'] = session_id
    if cwd:
        payload['cwd'] = cwd
    try:
        result = subprocess.run(
            [kernel_binary, 'gate', 'evaluate', '--hook-stdin', f'--agent={agent}'],
            input=json.dumps(payload),
            capture_output=True,
            text=True,
            timeout=timeout_s,
        )
    except FileNotFoundError:
        # Kernel binary not on PATH → fail-open (with stderr warning)
        import sys
        print(
            json.dumps({
                'level': 'warn',
                'component': 'chitin_governance',
                'msg': 'kernel-binary-missing-failing-open',
                'binary': kernel_binary,
            }),
            file=sys.stderr,
        )
        return GateDecision(allowed=True, reason='kernel-missing-fail-open')
    except subprocess.TimeoutExpired:
        return GateDecision(allowed=True, reason='kernel-timeout-fail-open')

    # Exit code 0 + empty stdout = silent allow
    if result.returncode == 0 and not result.stdout.strip():
        return GateDecision(allowed=True)

    # Kernel emits JSON on stdout
    try:
        parsed = json.loads(result.stdout)
    except json.JSONDecodeError:
        return GateDecision(allowed=True, reason='kernel-non-json-fail-open')

    decision_obj = parsed.get('decision')
    if isinstance(decision_obj, str):
        # Top-level shape: { decision: "allow"|"deny" }
        allowed = decision_obj == 'allow' or decision_obj == 'continue'
        msg = parsed.get('reason') or parsed.get('message')
    elif isinstance(decision_obj, dict):
        # Nested shape: { decision: { Allowed: bool, Reason: str } }
        allowed = bool(decision_obj.get('Allowed'))
        msg = decision_obj.get('Reason')
    else:
        # Block-shape: { decision: "block", reason: "..." }
        allowed = parsed.get('decision') != 'block'
        msg = parsed.get('reason') or parsed.get('message')

    rule_id = None
    if isinstance(decision_obj, dict):
        rule_id = decision_obj.get('RuleID')

    decision = GateDecision(allowed=allowed, reason=msg, rule_id=rule_id, raw=parsed)
    if not allowed and raise_on_deny:
        raise GateBlocked(msg or 'kernel-denied', rule_id=rule_id)
    return decision


# ─── Convenience wrappers for common side effects ─────────────────


def gate_file_write(file_path: str, **kw):
    """Gate a file write before performing it."""
    return gate_action('Write', {'file_path': file_path}, **kw)


def gate_shell_exec(command: str, **kw):
    """Gate a shell exec before running it."""
    return gate_action('Bash', {'command': command}, **kw)


def gate_http_request(url: str, **kw):
    """Gate an HTTP request before sending it."""
    return gate_action('WebFetch', {'url': url}, **kw)
