import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import tomllib
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).resolve().parents[1] / "scripts/import_modules.py"
spec = importlib.util.spec_from_file_location("import_modules", SCRIPT)
imports = importlib.util.module_from_spec(spec)
spec.loader.exec_module(imports)


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True,
                                   stderr=subprocess.PIPE).strip()


class ImportModulesTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.area = Path(self.temporary.name)
        self.root = self.area / "disposable"
        self.root.mkdir()
        self.init(self.root)
        (self.root / ".gitignore").write_text("/out/\n")
        (self.root / "README.md").write_text("original FES\n")
        self.sources = {}
        self.old = {}
        sections = []
        for name in imports.COMPONENTS:
            source = self.area / name
            source.mkdir()
            self.init(source)
            git(source, "remote", "add", "origin", "https://example.invalid/" + name + ".git")
            (source / "module.txt").write_text("old " + name)
            self.commit(source, "original")
            self.old[name] = git(source, "rev-parse", "HEAD")
            (source / "module.txt").write_text("selected " + name)
            (source / ".gitignore").write_text("/build/\n")
            self.commit(source, "selected")
            revision = git(source, "rev-parse", "HEAD")
            self.sources[name] = {"repository": str(source), "commit": revision}
            (self.root / "sources" / name).mkdir(parents=True)
            git(self.root, "update-index", "--add", "--cacheinfo", "160000", revision, "sources/" + name)
            sections.append(f'[submodule "sources/{name}"]\n path = sources/{name}\n url = https://example.invalid/{name}.git\n')
        sections.append('[submodule "external/tool"]\n path = external/tool\n url = https://example.invalid/tool.git\n')
        (self.root / "external/tool").mkdir(parents=True)
        git(self.root, "update-index", "--add", "--cacheinfo", "160000", self.old["FogCast"], "external/tool")
        (self.root / ".gitmodules").write_text("\n".join(sections))
        self.commit(self.root, "selected FES checkpoint")
        self.base = git(self.root, "rev-parse", "HEAD")

    def init(self, root):
        git(root, "init", "-q")
        git(root, "config", "user.name", "Test")
        git(root, "config", "user.email", "test@example.invalid")

    def commit(self, root, message):
        git(root, "add", ".")
        git(root, "commit", "-qm", message)

    def test_dry_run_is_read_only_and_reports_real_parents(self):
        index = (self.root / ".git/index").read_bytes()
        refs = git(self.root, "show-ref")
        plan = imports.prepare(self.root, self.sources)
        self.assertFalse(plan["executed"])
        self.assertEqual(plan["required_parents"], [self.base] + [self.sources[n]["commit"] for n in imports.COMPONENTS])
        self.assertEqual((self.root / ".git/index").read_bytes(), index)
        self.assertEqual(git(self.root, "show-ref"), refs)
        self.assertFalse((self.root / "config/source-imports.toml").exists())
        self.assertFalse((self.root / ".git/FETCH_HEAD").exists())

    def test_execute_stages_exact_files_and_retains_original_history(self):
        plan = imports.prepare(self.root, self.sources, execute=True)
        self.assertTrue(plan["executed"])
        self.assertEqual(git(self.root, "rev-parse", "HEAD"), self.base)
        provenance = tomllib.loads((self.root / "config/source-imports.toml").read_text())
        self.assertEqual(provenance["required_parents"], plan["required_parents"])
        for name, selected in self.sources.items():
            path = self.root / "sources" / name
            self.assertEqual((path / "module.txt").read_text(), "selected " + name)
            self.assertFalse((path / ".git").exists())
            self.assertEqual(git(self.root, "rev-parse", "refs/imports/" + name), selected["commit"])
            self.assertEqual(git(self.root, "show", self.old[name] + ":module.txt"), "old " + name)
            expected_tree = git(selected["repository"], "rev-parse", selected["commit"] + "^{tree}")
            self.assertEqual(provenance["imports"][name]["tree"], expected_tree)
            self.assertEqual(provenance["imports"][name]["repository"], "https://example.invalid/" + name + ".git")
            self.assertEqual(git(self.root, "rev-parse", plan["staged_tree"] + ":sources/" + name), expected_tree)
        self.assertEqual(git(self.root, "config", "-f", ".gitmodules", "--get", "submodule.external/tool.url"), "https://example.invalid/tool.git")
        self.assertNotIn("FogCast", (self.root / ".gitmodules").read_text())
        self.assertTrue(git(self.root, "ls-files", "--stage", "--", "external/tool").startswith("160000 "))
        self.assertEqual((self.root / "README.md").read_text(), "original FES\n")
        self.assertEqual(git(self.root, "diff", "--name-only"), "")
        self.assertFalse((self.root / ".git/FETCH_HEAD").exists())
        # Demonstrate the suggested integrator operation without changing refs:
        # a multi-parent import commit keeps every original SHA reachable.
        args = ["commit-tree", plan["staged_tree"], "-m", "reviewed import"]
        for parent in plan["required_parents"]:
            args += ["-p", parent]
        commit = git(self.root, *args)
        parents = git(self.root, "show", "-s", "--format=%P", commit).split()
        self.assertEqual(parents, plan["required_parents"])
        for old in self.old.values():
            self.assertEqual(subprocess.run(["git", "-C", str(self.root), "merge-base", "--is-ancestor", old, commit]).returncode, 0)
        self.assertEqual(git(self.root, "rev-parse", "HEAD"), self.base)

    def test_pin_mismatch_is_rejected_before_any_fetch(self):
        self.sources["FogCast"]["commit"] = self.old["FogCast"]
        with self.assertRaisesRegex(ValueError, "match selected pin"):
            imports.prepare(self.root, self.sources, execute=True)
        self.assertEqual(git(self.root, "for-each-ref", "refs/imports"), "")
        self.assertEqual(git(self.root, "status", "--porcelain"), "")

    def test_explicit_reselection_imports_new_local_commit_without_pin_checkpoint(self):
        child = self.initialize_child()
        selected = self.sources["FogCast"]["commit"]
        source = Path(self.sources["FogCast"]["repository"])
        (source / "module.txt").write_text("reviewed local update")
        self.commit(source, "reviewed local update")
        imported = git(source, "rev-parse", "HEAD")
        self.sources["FogCast"]["commit"] = imported
        self.assertEqual(git(child, "rev-parse", "HEAD"), selected)
        with self.assertRaisesRegex(ValueError, "allow-reselection"):
            imports.prepare(self.root, self.sources, execute=True)
        plan = imports.prepare(self.root, self.sources, execute=True, allow_reselection=True)
        self.assertEqual(git(self.root, "rev-parse", "HEAD"), self.base)
        self.assertEqual((child / "module.txt").read_text(), "reviewed local update")
        self.assertEqual(len(plan["required_parents"]), 5)
        self.assertIn(imported, plan["required_parents"])
        self.assertEqual(git(self.root, "rev-parse", "refs/imports/prior/FogCast"), selected)
        record = tomllib.loads((self.root / "config/source-imports.toml").read_text())["imports"]["FogCast"]
        self.assertEqual(record["prior_gitlink"], selected)
        self.assertEqual(record["imported_commit"], imported)
        self.assertEqual(record["prior_history_ref"], "refs/imports/prior/FogCast")
        args = ["commit-tree", plan["staged_tree"], "-m", "reviewed import"]
        for parent in plan["required_parents"]:
            args += ["-p", parent]
        commit = git(self.root, *args)
        self.assertEqual(subprocess.run(["git", "-C", str(self.root), "merge-base", "--is-ancestor", selected, commit]).returncode, 0)
        self.assertEqual(git(self.root, "rev-parse", "HEAD"), self.base)

    def test_divergent_reselection_requires_prior_history_as_an_additional_parent(self):
        selected = self.sources["FogCast"]["commit"]
        source = Path(self.sources["FogCast"]["repository"])
        git(source, "checkout", "--orphan", "independent")
        git(source, "rm", "-rf", ".")
        (source / "module.txt").write_text("independent reviewed implementation")
        self.commit(source, "independent history")
        imported = git(source, "rev-parse", "HEAD")
        self.sources["FogCast"]["commit"] = imported
        plan = imports.prepare(self.root, self.sources, execute=True, allow_reselection=True)
        self.assertIn(selected, plan["required_parents"])
        self.assertIn(imported, plan["required_parents"])
        self.assertEqual(len(plan["required_parents"]), 6)
        self.assertEqual(git(self.root, "show", selected + ":module.txt"), "selected FogCast")
        args = ["commit-tree", plan["staged_tree"], "-m", "reviewed divergent import"]
        for parent in plan["required_parents"]:
            args += ["-p", parent]
        commit = git(self.root, *args)
        for prior in (selected, imported):
            self.assertEqual(subprocess.run(["git", "-C", str(self.root), "merge-base", "--is-ancestor", prior, commit]).returncode, 0)

    def test_reselection_still_requires_an_available_full_commit(self):
        self.sources["FogCast"]["commit"] = "0" * 40
        with self.assertRaisesRegex(ValueError, "unavailable"):
            imports.prepare(self.root, self.sources, execute=True, allow_reselection=True)
        self.sources["FogCast"]["commit"] = "HEAD"
        with self.assertRaisesRegex(ValueError, "full lowercase"):
            imports.prepare(self.root, self.sources, execute=True, allow_reselection=True)
        self.assertEqual(git(self.root, "for-each-ref", "refs/imports"), "")
        self.assertEqual(git(self.root, "status", "--porcelain"), "")

    def test_dirty_destination_and_untracked_files_are_preserved(self):
        for relative in ("README.md", "untracked.txt"):
            with self.subTest(relative=relative):
                path = self.root / relative
                path.write_text("preserve this")
                with self.assertRaisesRegex(ValueError, "dirty"):
                    imports.prepare(self.root, self.sources, execute=True)
                self.assertEqual(path.read_text(), "preserve this")
                if relative == "README.md":
                    git(self.root, "checkout", "--", relative)
                else:
                    path.unlink()

    def initialize_child(self):
        child = self.root / "sources/FogCast"
        child.parent.mkdir(parents=True, exist_ok=True)
        git(self.root, "clone", "-q", self.sources["FogCast"]["repository"], str(child))
        return child

    def test_dirty_initialized_child_is_rejected_without_moving_it(self):
        child = self.initialize_child()
        (child / "extra").write_text("user work")
        with self.assertRaisesRegex(ValueError, "dirty"):
            imports.prepare(self.root, self.sources, execute=True)
        self.assertEqual((child / "extra").read_text(), "user work")
        self.assertFalse((self.root / "out/module-import-backups/FogCast").exists())

    def test_ignored_child_files_and_uninitialized_content_are_preserved(self):
        child = self.initialize_child()
        (child / "build").mkdir()
        (child / "build/private.txt").write_text("preserve ignored work")
        with self.assertRaisesRegex(ValueError, "ignored files"):
            imports.prepare(self.root, self.sources, execute=True)
        self.assertEqual((child / "build/private.txt").read_text(), "preserve ignored work")
        # Another uninitialized gitlink may hide arbitrary files from parent status.
        other = self.root / "sources/misteross"
        other.mkdir(exist_ok=True)
        (other / "unrelated").write_text("preserve")
        (child / "build/private.txt").unlink()
        (child / "build").rmdir()
        with self.assertRaisesRegex(ValueError, "unrelated files"):
            imports.prepare(self.root, self.sources, execute=True)
        self.assertEqual((other / "unrelated").read_text(), "preserve")

    def test_clean_child_is_backed_up_without_recursive_deletion(self):
        child = self.initialize_child()
        original_git_config = (child / ".git/config").read_bytes()
        imports.prepare(self.root, self.sources, execute=True)
        backup = self.root / "out/module-import-backups/FogCast"
        self.assertEqual((backup / ".git/config").read_bytes(), original_git_config)
        self.assertEqual((backup / "module.txt").read_text(), "selected FogCast")
        self.assertFalse((child / ".git").exists())

    def test_failure_rolls_back_index_children_and_created_refs(self):
        child = self.initialize_child()
        original = imports.git
        def fail(root, *args, **kwargs):
            if args[:2] == ("read-tree", "--prefix=sources/misteross/"):
                raise ValueError("injected read-tree failure")
            return original(root, *args, **kwargs)
        with patch.object(imports, "git", side_effect=fail), self.assertRaisesRegex(ValueError, "injected"):
            imports.prepare(self.root, self.sources, execute=True)
        self.assertEqual(git(self.root, "status", "--porcelain"), "")
        self.assertEqual(git(self.root, "for-each-ref", "refs/imports"), "")
        self.assertTrue((child / ".git").exists())
        self.assertFalse((self.root / "config/source-imports.toml").exists())

    def test_extra_submodule_section_is_preserved_but_surprising_import_keys_rejected(self):
        with (self.root / ".gitmodules").open("a") as output:
            output.write('\n[submodule "sources/FogCast"]\n unexpected = keep\n')
        self.commit(self.root, "unexpected section key")
        with self.assertRaisesRegex(ValueError, "unexpected"):
            imports.prepare(self.root, self.sources, execute=True)
        self.assertIn("unexpected = keep", (self.root / ".gitmodules").read_text())

    def test_tool_checkout_is_never_a_destination(self):
        with patch.object(imports, "TOOL_CHECKOUT", self.root), self.assertRaisesRegex(ValueError, "disposable"):
            imports.prepare(self.root, self.sources, execute=True)

    def test_source_worktree_edits_are_not_imported_or_changed(self):
        source = Path(self.sources["FogCast"]["repository"])
        (source / "module.txt").write_text("uncommitted source work")
        imports.prepare(self.root, self.sources, execute=True)
        self.assertEqual((self.root / "sources/FogCast/module.txt").read_text(), "selected FogCast")
        self.assertEqual((source / "module.txt").read_text(), "uncommitted source work")

    def test_failed_import_does_not_recursively_delete_unexpected_files(self):
        original = imports.git
        unexpected = self.root / "sources/misteross/unrelated.txt"
        def fail(root, *args, **kwargs):
            if args[:2] == ("read-tree", "--prefix=sources/misteross/"):
                unexpected.parent.mkdir(parents=True)
                unexpected.write_text("concurrent user work")
                raise ValueError("injected failure")
            return original(root, *args, **kwargs)
        with patch.object(imports, "git", side_effect=fail), self.assertRaisesRegex(ValueError, "preserved unexpected files"):
            imports.prepare(self.root, self.sources, execute=True)
        self.assertEqual(unexpected.read_text(), "concurrent user work")

    def test_primary_checkout_of_tool_worktree_is_protected(self):
        worker = self.area / "tool-worker"
        git(self.root, "worktree", "add", "-q", "--detach", str(worker))
        with patch.object(imports, "TOOL_CHECKOUT", worker), self.assertRaisesRegex(ValueError, "canonical"):
            imports.prepare(self.root, self.sources, execute=True)

    def test_cli_defaults_to_dry_run_and_requires_explicit_destination(self):
        args = [sys.executable, str(SCRIPT), "--destination", str(self.root)]
        for name, source in self.sources.items():
            args += ["--source", name + "=" + source["repository"] + "@" + source["commit"]]
        result = subprocess.run(args, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(json.loads(result.stdout)["executed"])
        self.assertEqual(git(self.root, "status", "--porcelain"), "")
        missing = subprocess.run([sys.executable, str(SCRIPT)], text=True, capture_output=True)
        self.assertEqual(missing.returncode, 2)


if __name__ == "__main__":
    unittest.main()
