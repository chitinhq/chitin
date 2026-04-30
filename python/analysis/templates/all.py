"""Single import to load every v1 template (triggers registration).

Alphabetical for predictability. Registration order does not matter for
correctness in v1 (no overlapping rule_ids).
"""
from analysis.templates import bounds_max_files_changed  # noqa: F401
from analysis.templates import no_curl_pipe_bash         # noqa: F401
from analysis.templates import no_destructive_rm         # noqa: F401
from analysis.templates import no_force_push             # noqa: F401
