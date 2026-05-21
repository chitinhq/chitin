"""Single import to load every v1 template (triggers registration).

Alphabetical for predictability. Registration order does not matter for
correctness in v1 (no overlapping rule_ids).
"""
from chitin_telemetry.templates import bounds_max_files_changed  # noqa: F401
from chitin_telemetry.templates import bounds_max_lines_changed   # noqa: F401
from chitin_telemetry.templates import no_curl_pipe_bash         # noqa: F401
from chitin_telemetry.templates import no_destructive_rm         # noqa: F401
from chitin_telemetry.templates import no_force_push             # noqa: F401
from chitin_telemetry.templates import lockdown                  # noqa: F401
from chitin_telemetry.templates import router_heuristic_deny     # noqa: F401
from chitin_telemetry.templates import envelope_closed           # noqa: F401
from chitin_telemetry.templates import default_deny_unknown      # noqa: F401
from chitin_telemetry.templates import no_git_merge_main         # noqa: F401
from chitin_telemetry.templates import no_shell_chmod_777        # noqa: F401
from chitin_telemetry.templates import no_shell_sudo             # noqa: F401
