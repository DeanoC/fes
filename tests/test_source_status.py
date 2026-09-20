import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import source_status as status


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


class SourceStatusTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "repo"
        self.root.mkdir()
        git(self.root, "init", "-q", "-b", "main")
        git(self.root, "config", "user.name", "Test")
        git(self.root, "config", "user.email", "test@example.invalid")
        (self.root / "file").write_text("base")
        git(self.root, "add", ".")
        git(self.root, "commit", "-qm", "base")
        self.base = git(self.root, "rev-parse", "HEAD")
        self.remote = Path(self.temp.name) / "remote.git"
        git(self.root, "clone", "-q", "--bare", str(self.root), str(self.remote))
        git(self.root, "remote", "add", "origin", str(self.remote))

    def commit(self, message):
        git(self.root, "commit", "--allow-empty", "-qm", message)
        return git(self.root, "rev-parse", "HEAD")

    def test_remote_and_dirty_worktree_are_observed_without_mutation(self):
        (self.root / "file").write_text("dirty")
        (self.root / "untracked").write_text("preserve")
        index = (self.root / ".git/index").read_bytes()
        refs = git(self.root, "show-ref")
        row = status.observe(self.root, "FES", False, 2)
        self.assertEqual(row["relation"], "equal")
        self.assertEqual(row["remote_head"], self.base)
        self.assertIsNotNone(row["observed_at"])
        self.assertEqual(row["checkout_state"], "dirty")
        self.assertIn(" M file", row["changes"])
        self.assertIn("?? untracked", row["changes"])
        self.assertEqual(index, (self.root / ".git/index").read_bytes())
        self.assertEqual(refs, git(self.root, "show-ref"))
        self.assertFalse((self.root / ".git/FETCH_HEAD").exists())
        self.assertEqual((self.root / "file").read_text(), "dirty")

    def test_ancestry_and_divergence(self):
        tip = self.commit("advance")
        self.assertEqual(status.relation(self.root, self.base, tip), "behind")
        self.assertEqual(status.relation(self.root, tip, self.base), "ahead")
        git(self.root, "checkout", "-q", "--detach", self.base)
        other = self.commit("other")
        self.assertEqual(status.relation(self.root, other, tip), "diverged")

    def test_remote_object_unavailable_is_not_claimed_behind(self):
        git(self.remote, "update-ref", "refs/heads/main", self.base)
        self.assertEqual(status.relation(self.root, self.base, "a" * 40), "different-history-unavailable")

    def test_offline_never_queries_remote(self):
        original = status.git
        def guarded(root, *args, **kwargs):
            self.assertNotIn("ls-remote", args)
            return original(root, *args, **kwargs)
        with patch.object(status, "git", side_effect=guarded):
            row = status.observe(self.root, "FES", True, 2)
        self.assertEqual(row["remote_state"], "offline")
        self.assertEqual(row["relation"], "unknown")
        self.assertIsNone(row["observed_at"])

    def test_unavailable_remote_and_missing_main(self):
        git(self.root, "remote", "set-url", "origin", str(self.remote / "missing"))
        row = status.observe(self.root, "FES", False, 2)
        self.assertEqual(row["remote_state"], "unavailable")
        git(self.root, "remote", "set-url", "origin", str(self.remote))
        git(self.remote, "update-ref", "-d", "refs/heads/main")
        self.assertEqual(status.observe(self.root, "FES", False, 2)["remote_state"], "missing-ref")

    def test_timeout(self):
        original = status.git
        def timeout(root, *args, **kwargs):
            if "ls-remote" in args:
                raise subprocess.TimeoutExpired("git", 1)
            return original(root, *args, **kwargs)
        with patch.object(status, "git", side_effect=timeout):
            row = status.observe(self.root, "FES", False, 1)
        self.assertEqual(row["remote_state"], "timeout")
        self.assertIsNone(row["remote_head"])

    def test_uninitialized_component_uses_selected_index_and_remote_url(self):
        path = "sources/FogCast"
        git(self.root, "update-index", "--add", "--cacheinfo", "160000", self.base, path)
        (self.root / ".gitmodules").write_text(f'[submodule "{path}"]\n path = {path}\n url = {self.remote}\n')
        row = status.observe(self.root, "FogCast", False, 2)
        self.assertEqual(row["selected"], self.base)
        self.assertEqual(row["checkout_state"], "uninitialized")
        self.assertIsNone(row["checkout_head"])
        self.assertIsNone(row["committed_selection"])
        self.assertEqual(row["relation"], "equal")

    def test_staged_pin_and_mismatched_component_checkout(self):
        child = self.root / "sources/FogCast"
        child.parent.mkdir()
        git(self.root, "clone", "-q", str(self.remote), str(child))
        git(self.root, "update-index", "--add", "--cacheinfo", "160000", self.base, "sources/FogCast")
        git(self.root, "commit", "-qm", "select")
        next_pin = self.commit("new pin")
        git(self.root, "update-index", "--cacheinfo", "160000", next_pin, "sources/FogCast")
        row = status.observe(self.root, "FogCast", True, 2)
        self.assertEqual(row["committed_selection"], self.base)
        self.assertEqual(row["selected"], next_pin)
        self.assertFalse(row["checkout_matches_selection"])
        self.assertEqual(row["checkout_state"], "clean")

    def test_git_disables_index_refresh_and_lazy_fetch(self):
        with patch.object(status.subprocess, "run") as run:
            status.git(self.root, "status")
        env = run.call_args.kwargs["env"]
        self.assertEqual(env["GIT_NO_LAZY_FETCH"], "1")
        self.assertEqual(env["GIT_OPTIONAL_LOCKS"], "0")
        self.assertEqual(env["GIT_TERMINAL_PROMPT"], "0")

    def test_ancestry_error_does_not_claim_divergence(self):
        tip = self.commit("advance")
        original = status.git
        def failed(root, *args, **kwargs):
            if "merge-base" in args:
                return subprocess.CompletedProcess(args, 128, "", "unavailable")
            return original(root, *args, **kwargs)
        with patch.object(status, "git", side_effect=failed):
            self.assertEqual(status.relation(self.root, self.base, tip), "different-history-unavailable")

    def add_modules(self):
        for name in status.COMPONENTS:
            path = self.root / "sources" / name
            path.mkdir(parents=True)
            (path / "module.txt").write_text(name)
        git(self.root, "add", "sources")
        return self.commit("tracked modules")

    def test_module_status_uses_root_identity_and_scoped_dirty_paths(self):
        selected = self.add_modules()
        (self.root / "file").write_text("unrelated dirty parent")
        clean = status.observe(self.root, "FogCast", True, 2)
        self.assertEqual(clean["source_kind"], "module")
        self.assertEqual(clean["remote_owner"], "FES")
        self.assertEqual(clean["module_path"], "sources/FogCast")
        self.assertEqual(clean["module_tree"], git(self.root, "rev-parse", "HEAD:sources/FogCast"))
        self.assertEqual(clean["selected"], selected)
        self.assertEqual(clean["committed_selection"], selected)
        self.assertEqual(clean["checkout_head"], selected)
        self.assertEqual(clean["checkout_state"], "clean")
        self.assertTrue(clean["checkout_matches_selection"])
        (self.root / "sources/FogCast/module.txt").write_text("staged module change")
        git(self.root, "add", "sources/FogCast/module.txt")
        (self.root / "sources/FogCast/extra").write_text("untracked")
        index = (self.root / ".git/index").read_bytes()
        dirty = status.observe(self.root, "FogCast", True, 2)
        self.assertEqual(dirty["checkout_state"], "dirty")
        self.assertEqual(dirty["selected"], selected)
        self.assertEqual(dirty["module_tree"], clean["module_tree"])
        self.assertEqual(dirty["changes"], ["M  sources/FogCast/module.txt", "?? sources/FogCast/extra"])
        self.assertEqual((self.root / ".git/index").read_bytes(), index)

    def test_module_remote_observation_reuses_parent_once(self):
        selected = self.add_modules()
        original = status.git
        calls = []
        def observed(root, *args, **kwargs):
            if "ls-remote" in args:
                calls.append((root, args))
            return original(root, *args, **kwargs)
        with patch.object(status, "git", side_effect=observed):
            result = status.report(self.root, timeout=2)
        self.assertEqual(len(calls), 1)
        parent = result["sources"][0]
        for child in result["sources"][1:]:
            self.assertEqual(child["selected"], selected)
            self.assertEqual(child["remote_owner"], "FES")
            self.assertEqual(child["remote_head"], self.base)
            self.assertEqual(child["relation"], "ahead")
            self.assertEqual(child["observed_at"], parent["observed_at"])
        self.assertFalse((self.root / ".git/FETCH_HEAD").exists())

    def test_deleted_module_is_dirty_not_uninitialized(self):
        selected = self.add_modules()
        git(self.root, "rm", "-r", "sources/FogCast")
        row = status.observe(self.root, "FogCast", True, 2)
        self.assertEqual(row["checkout_state"], "dirty")
        self.assertEqual(row["selected"], selected)
        self.assertEqual(row["changes"], ["D  sources/FogCast/module.txt"])

    def test_module_direct_observation_uses_parent_origin(self):
        selected = self.add_modules()
        row = status.observe(self.root, "FogCast", False, 2)
        self.assertEqual(row["selected"], selected)
        self.assertEqual(row["remote_head"], self.base)
        self.assertEqual(row["remote_state"], "observed")
        self.assertEqual(row["relation"], "ahead")

    def test_module_text_cli_names_shared_fes_main(self):
        self.add_modules()
        result = subprocess.run([sys.executable, str(Path(status.__file__)), "--root", str(self.root), "--offline"], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("module=sources/FogCast", result.stdout)
        self.assertIn("remote=FES main (shared repository)", result.stdout)
        self.assertNotIn("checkout=uninitialized", result.stdout)

    def test_json_cli(self):
        result = subprocess.run([sys.executable, str(Path(status.__file__)), "--root", str(self.root), "--offline", "--json"], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(len(report["sources"]), 5)
        self.assertEqual(report["sources"][0]["selected"], self.base)


if __name__ == "__main__":
    unittest.main()
