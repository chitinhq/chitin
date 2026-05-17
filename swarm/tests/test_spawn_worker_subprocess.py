"""Unit tests for spawn_worker_subprocess prompt transport."""

from __future__ import annotations

import json
import importlib.machinery
import importlib.util
import tempfile
import unittest
import hashlib
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "workflows" / "spawn_worker_subprocess.py"


def load_module():
    loader = importlib.machinery.SourceFileLoader("spawn_worker_subprocess", str(SCRIPT))
    spec = importlib.util.spec_from_loader("spawn_worker_subprocess", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class SpawnWorkerSubprocessTests(unittest.TestCase):
    def test_prepare_worker_command_codex_uses_stdin_not_prompt_argv(self):
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "codex",
                "cmd": "codex",
                "model": "gpt-5.5",
                "prompt": "ticket body here",
                "args": ["exec", "--model", "{model}", "{prompt}"],
            }
        )

        self.assertEqual(argv, ["codex", "exec", "--model", "gpt-5.5"])
        self.assertEqual(stdin_text, "ticket body here")

    def test_prepare_worker_command_gemini_uses_empty_prompt_flag_plus_stdin(self):
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "gemini",
                "cmd": "gemini",
                "model": "gemini-2.5-flash",
                "prompt": "ticket body here",
                "args": ["-y", "-p", "{prompt}", "--model", "{model}"],
            }
        )

        self.assertEqual(argv, ["gemini", "-y", "-p", "", "--model", "gemini-2.5-flash"])
        self.assertEqual(stdin_text, "ticket body here")

    def test_prepare_worker_command_copilot_uses_stdin_not_prompt_argv(self):
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "copilot",
                "cmd": "chitin-kernel",
                "model": "gpt-4.1",
                "prompt": "ticket body here",
                "system_prompt": "soul prompt",
                "args": ["drive", "copilot", "--model", "{model}", "--cwd", ".", "{prompt}"],
            }
        )

        self.assertEqual(
            argv,
            [
                "chitin-kernel", "drive", "copilot", "--model", "gpt-4.1", "--cwd", ".",
                "--append-system-prompt", "soul prompt",
            ],
        )
        self.assertEqual(stdin_text, "ticket body here")

    def test_prepare_worker_command_codex_prefixes_system_prompt_into_ticket_body(self):
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "codex",
                "cmd": "codex",
                "model": "gpt-5.5",
                "prompt": "ticket body here",
                "system_prompt": "soul prompt",
                "args": ["exec", "--model", "{model}", "{prompt}"],
            }
        )

        self.assertEqual(argv, ["codex", "exec", "--model", "gpt-5.5"])
        self.assertEqual(stdin_text, "soul prompt\n\nticket body here")

    def test_prepare_worker_command_max_size_prompt_stays_off_argv(self):
        # Boundary: max. A very large ticket body must travel via stdin_text,
        # never argv — that is the whole point of the change (ps visibility +
        # ARG_MAX). Check every stdin-routed driver.
        module = load_module()
        huge_prompt = "x" * 500_000
        for driver, cmd in (("codex", "codex"), ("copilot", "chitin-kernel"), ("gemini", "gemini")):
            argv, stdin_text = module.prepare_worker_command(
                {
                    "driver": driver,
                    "cmd": cmd,
                    "model": "m",
                    "prompt": huge_prompt,
                    "args": ["--model", "{model}", "{prompt}"],
                }
            )
            self.assertEqual(stdin_text, huge_prompt, msg=driver)
            self.assertNotIn(huge_prompt, argv, msg=driver)

    def test_prepare_worker_command_missing_prompt_does_not_crash(self):
        # Boundary: empty. A config with no "prompt" key must not raise — the
        # helper falls back to an empty prompt, still routed via stdin so the
        # {prompt} placeholder never becomes a literal argv entry.
        module = load_module()
        argv, stdin_text = module.prepare_worker_command(
            {
                "driver": "codex",
                "cmd": "codex",
                "model": "gpt-5.5",
                "args": ["exec", "--model", "{model}", "{prompt}"],
            }
        )
        self.assertEqual(argv, ["codex", "exec", "--model", "gpt-5.5"])
        self.assertEqual(stdin_text, "")

    def test_build_transcript_tail_empty_boundary(self):
        # Boundary: empty. A worker with no emitted output should not invent
        # transcript metadata.
        module = load_module()

        self.assertEqual(module.build_transcript_tail("", ""), "")

    def test_build_transcript_tail_max_boundary(self):
        # Boundary: max. Keep exactly TRANSCRIPT_TAIL_LINES final lines so
        # operator-visible comments stay bounded even for verbose sessions.
        module = load_module()
        stdout = "\n".join(f"out-{i}" for i in range(45))
        stderr = "\n".join(f"err-{i}" for i in range(45))

        tail = module.build_transcript_tail(stdout, stderr)

        lines = tail.splitlines()
        self.assertEqual(len(lines), module.TRANSCRIPT_TAIL_LINES)
        self.assertEqual(lines[0], "[stderr] err-5")
        self.assertEqual(lines[-1], "[stderr] err-44")

    def test_summarize_completed_run_detects_zero_commit_session(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.object(module, "commits_ahead_of_base", return_value=0) as ahead:
            summary = module.summarize_completed_run(
                {"driver": "copilot", "model": "gpt-4.1", "default_branch": "swarm"},
                0,
                "nothing to do\n",
                "worker exited cleanly\n",
                tmp,
            )

        ahead.assert_called_once_with(tmp, {"driver": "copilot", "model": "gpt-4.1", "default_branch": "swarm"})
        self.assertEqual(summary["status"], "completed_no_commit")
        self.assertEqual(summary["exit_reason"], "model-concluded-nothing")
        self.assertEqual(summary["commit_count_ahead"], 0)
        self.assertIn("[stdout] nothing to do", summary["transcript_tail"])

    def test_summarize_completed_run_failed_session_is_classified(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.object(module, "commits_ahead_of_base", return_value=0):
            summary = module.summarize_completed_run(
                {"driver": "copilot", "model": "gpt-4.1"},
                7,
                "",
                "traceback",
                tmp,
            )

        self.assertEqual(summary["status"], "failed")
        self.assertEqual(summary["exit_reason"], "session-error")

    def test_resolve_base_ref_prefers_config_default_branch(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(
                module.resolve_base_ref({"default_branch": "swarm"}, tmp),
                "origin/swarm",
            )

    def test_resolve_base_ref_accepts_full_origin_ref(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(
                module.resolve_base_ref({"default_branch": "origin/develop"}, tmp),
                "origin/develop",
            )

    def test_commits_ahead_uses_resolved_base_ref_in_error(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(RuntimeError, "origin/swarm"):
                module.commits_ahead_of_base(tmp, {"default_branch": "swarm"})

    def test_extract_file_scope_globs_from_spec_section(self):
        module = load_module()
        spec = """
## Overview
ignore `frontend/**`

## File-system scope
- `apps/portal/**`
- `packages/ui/**`
- !`frontend/**`

## Invariants and Boundaries
- invariant: done
"""
        self.assertEqual(
            module.extract_file_scope_globs(spec),
            ["apps/portal/**", "packages/ui/**", "!frontend/**"],
        )

    def test_summarize_completed_run_rejects_path_scope_violation(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.object(module, "commits_ahead_of_base", return_value=1), \
             mock.patch.object(module, "validate_file_scope", return_value={
                 "ok": False,
                 "enforced": True,
                 "source": ".specify/specs/005/spec.md",
                 "globs": ["apps/portal/**"],
                 "changed_files": ["frontend/src/App.tsx"],
                 "violations": ["frontend/src/App.tsx"],
             }):
            summary = module.summarize_completed_run(
                {"driver": "codex", "model": "gpt-5.5"},
                0,
                "",
                "",
                tmp,
            )

        self.assertEqual(summary["status"], "failed")
        self.assertEqual(summary["exit_reason"], "path-scope-violation")
        self.assertIn("frontend/src/App.tsx", summary["error"])

    def test_validate_file_scope_allows_matching_changed_paths(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.object(module, "load_file_scope", return_value={"source": "config", "globs": ["apps/portal/**"]}), \
             mock.patch.object(module, "changed_files_since_base", return_value=["apps/portal/src/App.tsx"]):
            result = module.validate_file_scope(tmp, {})

        self.assertTrue(result["ok"])
        self.assertTrue(result["enforced"])

    def test_load_file_scope_resolves_workspace_spec_root_by_slug(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            spec_root = Path(tmp) / ".specify" / "specs"
            spec_dir = spec_root / "005-portal-auth-wiring"
            spec_dir.mkdir(parents=True)
            (spec_dir / "spec.md").write_text(
                "## File-system scope\n- `apps/portal/**`\n",
                encoding="utf-8",
            )
            result = module.load_file_scope({
                "spec_root": str(spec_root),
                "ticket_body": "Spec-kit entry: `.specify/specs/005-portal-auth-wiring/spec.md`",
            })

        self.assertEqual(result["globs"], ["apps/portal/**"])
        self.assertTrue(str(result["source"]).endswith("005-portal-auth-wiring/spec.md"))

    def test_detect_event_chain_returns_latest_hash(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            old_file = home / "events-old.jsonl"
            old_file.write_text(json.dumps({"this_hash": "old-hash"}) + "\n")
            before = module.snapshot_event_files(str(home))

            new_file = home / "events-new.jsonl"
            new_file.write_text(
                json.dumps({"this_hash": "first"}) + "\n" +
                json.dumps({"this_hash": "final-hash"}) + "\n"
            )

            chain_file, chain_hash = module.detect_event_chain(str(home), before)

        self.assertEqual(chain_file, str(new_file))
        self.assertEqual(chain_hash, "final-hash")

    def test_detect_event_chain_empty_file_boundary(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            before = module.snapshot_event_files(str(home))
            empty = home / "events-empty.jsonl"
            empty.write_text("")

            chain_file, chain_hash = module.detect_event_chain(str(home), before)

        self.assertEqual(chain_file, str(empty))
        self.assertIsNone(chain_hash)

    def test_materialize_driver_prompt_artifacts_writes_claude_md(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            module.materialize_driver_prompt_artifacts(
                {"driver": "claude-code", "system_prompt": "soul prompt"},
                tmp,
            )
            self.assertEqual((Path(tmp) / "CLAUDE.md").read_text(encoding="utf-8"), "soul prompt\n")

    def test_resolve_soul_file_and_hash_mismatch_audit_flag(self):
        module = load_module()
        with tempfile.TemporaryDirectory() as tmp:
            souls = Path(tmp) / "souls"
            souls.mkdir()
            soul_file = souls / "knuth.md"
            soul_file.write_text("before\n", encoding="utf-8")
            resolved = module.resolve_soul_file({"soul_id": "knuth", "souls_dir": str(souls)})
            self.assertEqual(resolved, soul_file)

            observed_hash = module.compute_file_sha256(soul_file)
            assert observed_hash is not None
            with mock.patch.object(module, "commits_ahead_of_base", return_value=1):
                summary = module.summarize_completed_run(
                    {"driver": "codex", "model": "gpt-5.5"},
                    0,
                    "",
                    "",
                    tmp,
                    soul_hash_mismatch=True,
                    observed_soul_hash=observed_hash,
                )

        self.assertEqual(summary["audit_flags"], ["soul_hash_mismatch"])
        self.assertEqual(summary["observed_soul_hash"], hashlib.sha256(b"before\n").hexdigest())


if __name__ == "__main__":
    unittest.main()
