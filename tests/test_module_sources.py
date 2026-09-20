import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "scripts/module_sources.py"
spec = importlib.util.spec_from_file_location("module_sources", SCRIPT)
module_sources = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module_sources)


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True,
                                   stderr=subprocess.PIPE).strip()


class ModuleSourcesTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / "fes"
        self.root.mkdir()
        self.initialize(self.root, "https://example.invalid/fes.git")
        (self.root / ".gitignore").write_text("/out/\n")
        (self.root / "README.md").write_text("parent\n")
        self.module = self.root / "sources/FogCast"
        self.module.mkdir(parents=True)
        (self.module / "README.md").write_text("committed module\n")
        (self.module / "executable").write_text("#!/bin/sh\nexit 0\n")
        (self.module / "executable").chmod(0o755)
        (self.module / "link").symlink_to("README.md")
        self.commit(self.root, "initial")
        self.initial = git(self.root, "rev-parse", "HEAD")
        self.destination = self.root / "out/work/fixture"

    def initialize(self, root, origin=None):
        git(root, "init", "-q")
        git(root, "config", "user.name", "Test")
        git(root, "config", "user.email", "test@example.invalid")
        if origin:
            git(root, "remote", "add", "origin", origin)

    def commit(self, root, message):
        git(root, "add", ".")
        git(root, "commit", "-qm", message)

    def describe(self, **kwargs):
        return module_sources.describe(self.root, "FogCast", **kwargs)

    def materialize(self, revision=None, destination=None):
        return module_sources.materialize(self.root, "FogCast", revision or self.initial,
                                          destination or self.destination)

    def test_tracked_module_has_real_root_provenance(self):
        self.assertEqual(self.describe(), {
            "kind": "module", "repository": "https://example.invalid/fes.git",
            "commit": self.initial, "root_commit": self.initial,
            "path": "sources/FogCast", "tree": git(self.root, "rev-parse", "HEAD:sources/FogCast"),
            "dirty": False,
        })

    def test_unrelated_parent_changes_do_not_dirty_module_or_enter_snapshot(self):
        (self.root / "README.md").write_text("uncommitted parent\n")
        (self.root / "scratch").write_text("untracked parent\n")
        git(self.root, "add", "README.md")
        before = git(self.root, "status", "--porcelain=v1", "-z")
        self.assertFalse(self.describe()["dirty"])
        result = self.materialize()
        self.assertEqual(result, self.destination / "sources/FogCast")
        self.assertEqual((self.destination / "README.md").read_text(), "parent\n")
        self.assertFalse((self.destination / "scratch").exists())
        self.assertEqual(git(self.root, "status", "--porcelain=v1", "-z"), before)

    def test_module_staged_unstaged_and_untracked_changes_are_rejected(self):
        for kind in ("unstaged", "staged", "untracked"):
            with self.subTest(kind=kind):
                path = self.module / ("extra" if kind == "untracked" else "README.md")
                path.write_text("changed\n")
                if kind == "staged":
                    git(self.root, "add", "sources/FogCast/README.md")
                identity = self.describe(require_clean=False)
                self.assertTrue(identity["dirty"])
                self.assertEqual(identity["commit"], self.initial)
                self.assertEqual(identity["tree"], git(self.root, "rev-parse", "HEAD:sources/FogCast"))
                with self.assertRaisesRegex(ValueError, "dirty"):
                    self.describe()
                with self.assertRaisesRegex(ValueError, "dirty"):
                    self.materialize()
                git(self.root, "reset", "--hard", self.initial)
                if kind == "untracked":
                    path.unlink()

    def test_committed_module_change_selects_new_root_and_historical_bytes(self):
        (self.module / "README.md").write_text("new committed module\n")
        self.commit(self.root, "module update")
        current = git(self.root, "rev-parse", "HEAD")
        self.assertEqual(self.describe()["commit"], current)
        historical = self.describe(revision=self.initial)
        self.assertEqual(historical["commit"], self.initial)
        self.assertEqual(historical["root_commit"], current)
        result = self.materialize()
        self.assertEqual((result / "README.md").read_text(), "committed module\n")
        self.assertEqual(git(result, "rev-parse", "HEAD"), self.initial)
        self.assertEqual(git(result, "rev-parse", "--show-prefix"), "sources/FogCast/")
        self.assertEqual(git(result, "show", "HEAD:sources/FogCast/README.md"), "committed module")

    def test_snapshot_preserves_metadata_modes_links_and_reuses_clean_clone(self):
        result = self.materialize()
        self.assertTrue((self.destination / ".git").is_dir())
        self.assertFalse((result / ".git").exists())
        self.assertFalse((self.destination / ".git/objects/info/alternates").exists())
        self.assertEqual(git(result, "remote", "get-url", "origin"), "https://example.invalid/fes.git")
        self.assertTrue((result / "executable").stat().st_mode & 0o111)
        self.assertEqual(os.readlink(result / "link"), "README.md")
        self.assertEqual(self.materialize(), result)
        moved = self.destination.parent / "moved"
        self.destination.rename(moved)
        self.assertEqual(git(moved / "sources/FogCast", "rev-parse", "HEAD"), self.initial)

    def test_snapshot_changes_are_rejected_without_cleanup(self):
        result = self.materialize()
        overlay = result / "generated-lock.toml"
        overlay.write_text("intentional overlay\n")
        with self.assertRaisesRegex(ValueError, "snapshot is changed"):
            self.materialize()
        self.assertEqual(overlay.read_text(), "intentional overlay\n")
        overlay.unlink()
        (self.destination / "README.md").write_text("unexpected parent edit\n")
        with self.assertRaisesRegex(ValueError, "snapshot is changed"):
            self.materialize()

    def test_snapshot_wrong_head_or_origin_is_rejected(self):
        self.materialize()
        git(self.destination, "remote", "set-url", "origin", "https://example.invalid/other.git")
        with self.assertRaisesRegex(ValueError, "snapshot is changed"):
            self.materialize()
        git(self.destination, "remote", "set-url", "origin", "https://example.invalid/fes.git")
        git(self.destination, "config", "user.name", "Test")
        git(self.destination, "config", "user.email", "test@example.invalid")
        git(self.destination, "commit", "--allow-empty", "-qm", "different")
        with self.assertRaisesRegex(ValueError, "snapshot is changed"):
            self.materialize()

    def test_destination_must_be_ignored_bounded_and_not_symlinked(self):
        for destination in (self.root / "sources/new", self.root / "out/work", self.root / "out/work/../../../escape"):
            with self.subTest(destination=destination), self.assertRaisesRegex(ValueError, "beneath"):
                self.materialize(destination=destination)
        (self.root / "out").mkdir()
        (self.root / "out/work").symlink_to(self.root / "sources", target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "symlink"):
            self.materialize()
        (self.root / "out/work").unlink()
        (self.root / ".gitignore").write_text("")
        with self.assertRaisesRegex(ValueError, "ignored"):
            self.materialize()

    def test_invalid_name_and_missing_revision(self):
        for name in ("../FogCast", "FogCast/../misteross", "unknown"):
            with self.assertRaisesRegex(ValueError, "unknown"):
                module_sources.describe(self.root, name)
        for revision in ("HEAD", "../x", "0" * 40):
            with self.subTest(revision=revision), self.assertRaisesRegex(ValueError, "revision"):
                self.describe(revision=revision)

    def test_no_origin_is_not_replaced_with_fake_child_origin(self):
        git(self.root, "remote", "remove", "origin")
        self.assertIsNone(self.describe()["repository"])
        self.materialize()
        self.assertEqual(git(self.destination, "remote"), "")

    def test_staged_gitlink_to_module_conversion_needs_a_real_commit(self):
        child, revision = self.add_gitlink()
        # Convert only the index/worktree. HEAD still has a gitlink, so there
        # is no root commit containing the new module yet.
        shutil.rmtree(child / ".git")
        git(self.root, "rm", "--cached", "sources/libmister-runtime")
        git(self.root, "add", "sources/libmister-runtime")
        with self.assertRaisesRegex(ValueError, "tracked module"):
            module_sources.describe(self.root, "libmister-runtime")

    def add_gitlink(self):
        child = self.root / "sources/libmister-runtime"
        child.mkdir()
        self.initialize(child, "https://example.invalid/runtime.git")
        (child / "runtime.cpp").write_text("first\n")
        self.commit(child, "runtime initial")
        revision = git(child, "rev-parse", "HEAD")
        git(self.root, "update-index", "--add", "--cacheinfo", "160000", revision, "sources/libmister-runtime")
        git(self.root, "commit", "-qm", "runtime gitlink")
        return child, revision

    def test_gitlink_staged_pin_and_historical_revision_keep_child_identity(self):
        child, old = self.add_gitlink()
        (child / "runtime.cpp").write_text("second\n")
        self.commit(child, "runtime update")
        new = git(child, "rev-parse", "HEAD")
        with self.assertRaisesRegex(ValueError, "pin"):
            module_sources.describe(self.root, "libmister-runtime")
        git(self.root, "update-index", "--cacheinfo", "160000", new, "sources/libmister-runtime")
        selected = module_sources.describe(self.root, "libmister-runtime")
        self.assertEqual(selected["kind"], "gitlink")
        self.assertEqual(selected["commit"], new)
        self.assertEqual(selected["repository"], "https://example.invalid/runtime.git")
        self.assertEqual(selected["path"], ".")
        result = module_sources.materialize(self.root, "libmister-runtime", old, self.destination)
        self.assertEqual(result, self.destination)
        self.assertEqual(git(result, "rev-parse", "HEAD"), old)
        self.assertEqual((result / "runtime.cpp").read_text(), "first\n")
        self.assertEqual(git(result, "remote", "get-url", "origin"), selected["repository"])
        (child / "extra").write_text("dirty\n")
        with self.assertRaisesRegex(ValueError, "dirty"):
            module_sources.describe(self.root, "libmister-runtime")


if __name__ == "__main__":
    unittest.main()
