"""Tests for the git/PR poller. Uses real git for commits; stubs gh for PRs."""
from __future__ import annotations

import json
import subprocess
import tempfile
from pathlib import Path
from unittest.mock import patch

from argus.cross_source_db import init_cross_source_db
from argus.git_ingest import ingest_repo, discover_repos


def _make_git_repo(path: Path) -> None:
    """Initialize a real local git repo with one commit, so `git log` has output."""
    path.mkdir(parents=True, exist_ok=True)
    env = {
        "GIT_AUTHOR_NAME": "test",
        "GIT_AUTHOR_EMAIL": "test@example.invalid",
        "GIT_COMMITTER_NAME": "test",
        "GIT_COMMITTER_EMAIL": "test@example.invalid",
    }
    subprocess.run(["git", "init", "-q", "-b", "main", str(path)], check=True)
    subprocess.run(["git", "-C", str(path), "config", "user.email", "test@example.invalid"], check=True)
    subprocess.run(["git", "-C", str(path), "config", "user.name", "test"], check=True)
    (path / "README.md").write_text("hello")
    subprocess.run(["git", "-C", str(path), "add", "README.md"], check=True, env={**env})
    subprocess.run(["git", "-C", str(path), "commit", "-q", "-m", "initial"], check=True, env={**env})


def test_empty_repo_succeeds_with_zero_inserts():
    """Repo with no commits since the watermark inserts zero rows."""
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        repo = tmpdir / "repo"
        _make_git_repo(repo)
        xs_db = tmpdir / "xs.db"

        # First poll picks up the initial commit.
        r1 = ingest_repo(repo, xs_db, repo_name="repo")
        assert r1["commits_inserted"] >= 1

        # Second poll on the same repo with no new commits inserts nothing.
        r2 = ingest_repo(repo, xs_db, repo_name="repo")
        assert r2["commits_inserted"] == 0


def test_missing_repo_returns_zero():
    """A nonexistent repo path produces a zero result, no crash."""
    with tempfile.TemporaryDirectory() as tmpdir:
        xs_db = Path(tmpdir) / "xs.db"
        result = ingest_repo(Path(tmpdir) / "no-such-repo", xs_db, repo_name="ghost")
        assert result["commits_inserted"] == 0
        assert result["prs_inserted"] == 0


def test_pr_metadata_ingested_via_stubbed_gh():
    """PR list with one open + one merged lands as two cross_source_events rows.

    We don't patch subprocess.run globally (that would break the git
    invocations the indexer uses); instead we patch _gh_pr_list, which
    is the seam intended for stubbing.
    """
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        repo = tmpdir / "repo"
        _make_git_repo(repo)
        xs_db = tmpdir / "xs.db"

        # First poll advances the watermark past the initial commit.
        ingest_repo(repo, xs_db, repo_name="repo")

        def fake_pr_list(repo_path, state, since_ts):
            if state == "open":
                return [{
                    "number": 101,
                    "title": "open one",
                    "state": "OPEN",
                    "author": {"login": "alice"},
                    "createdAt": "2999-01-01T00:00:00Z",
                    "mergedAt": None,
                    "headRefName": "feat/x",
                    "baseRefName": "main",
                    "isDraft": False,
                }]
            return [{
                "number": 102,
                "title": "merged one",
                "state": "MERGED",
                "author": {"login": "bob"},
                "createdAt": "2999-01-01T00:00:00Z",
                "mergedAt": "2999-01-02T00:00:00Z",
                "headRefName": "feat/y",
                "baseRefName": "main",
                "isDraft": False,
            }]

        with patch("argus.git_ingest._gh_pr_list", side_effect=fake_pr_list):
            result = ingest_repo(repo, xs_db, repo_name="repo")

        # opened(101), opened(102), merged(102) — three rows
        assert result["prs_inserted"] == 3


def test_discover_repos_skips_non_repo_dirs():
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        (tmpdir / "not_a_repo").mkdir()
        (tmpdir / "repo").mkdir()
        _make_git_repo(tmpdir / "repo")

        repos = discover_repos([tmpdir])
        names = {r.name for r in repos}
        assert "repo" in names
        assert "not_a_repo" not in names
