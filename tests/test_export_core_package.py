import hashlib
import json
import stat
import subprocess
import sys
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.core_package import encode_manifest, package_identity, read_package
from scripts import export_core_package
from scripts.export_core_package import (
    PackageExportError,
    build_identity,
    encode_build_record,
    export_package,
)
from tests.test_core_package import REPOSITORY_URI_CASES


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "tests" / "fixtures" / "core-bundle-v2"


def _run(*arguments: str, cwd: Path) -> str:
    return subprocess.check_output(arguments, cwd=cwd, text=True).strip()


class ExportCorePackageTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.work = Path(self.tempdir.name)
        self.repo = self.work / "source"
        self.repo.mkdir()
        _run("git", "init", "-q", cwd=self.repo)
        _run("git", "config", "user.email", "fixture@example.invalid", cwd=self.repo)
        _run("git", "config", "user.name", "Fixture", cwd=self.repo)
        _run("git", "remote", "add", "origin", "https://example.invalid/fes-pong", cwd=self.repo)
        (self.repo / ".gitignore").write_text("/build/\n/dependencies/\n", encoding="utf-8")
        (self.repo / "scripts").mkdir()
        (self.repo / "scripts" / "build.py").write_text("# deterministic recipe\n", encoding="utf-8")
        (self.repo / "abi").mkdir()
        (self.repo / "abi" / "fes-gp.json").write_text('{"version":1}\n', encoding="utf-8")
        _run("git", "add", ".", cwd=self.repo)
        _run("git", "commit", "-qm", "fixture source", cwd=self.repo)
        self.revision = _run("git", "rev-parse", "HEAD", cwd=self.repo)

        self.dependency = self.repo / "dependencies" / "framework"
        self.dependency.mkdir(parents=True)
        _run("git", "init", "-q", cwd=self.dependency)
        _run("git", "config", "user.email", "fixture@example.invalid", cwd=self.dependency)
        _run("git", "config", "user.name", "Fixture", cwd=self.dependency)
        (self.dependency / "pin.txt").write_text("pinned\n", encoding="utf-8")
        _run("git", "add", ".", cwd=self.dependency)
        _run("git", "commit", "-qm", "dependency", cwd=self.dependency)
        self.dependency_revision = _run("git", "rev-parse", "HEAD", cwd=self.dependency)

        self.build = self.repo / "build"
        self.build.mkdir()
        self.rbf = self.build / "core.rbf"
        self.payload = (FIXTURES / "payloads" / "fes-fixture.rbf").read_bytes()
        self.rbf.write_bytes(self.payload)
        self.store = self.work / "packages"
        self.record_fields = {
            "format": 1,
            "repository": "https://example.invalid/fes-pong",
            "revision": self.revision,
            "recipe": "scripts/build.py",
            "recipe_sha256": hashlib.sha256((self.repo / "scripts" / "build.py").read_bytes()).hexdigest(),
            "abi_definition": "abi/fes-gp.json",
            "abi_definition_sha256": hashlib.sha256((self.repo / "abi" / "fes-gp.json").read_bytes()).hexdigest(),
            "dependencies": {"dependencies/framework": self.dependency_revision},
            "tools": {"nextpnr-mistral": "0ab322bdc414c195bf1907875c2b6a8818d9f81a", "yosys": "Yosys 0.52"},
            "parameters": {"device": "5CSEBA6U23I7", "seed": 1, "timing_closed": True},
        }
        self.record = encode_build_record(self.record_fields)
        (self.build / "build-inputs.json").write_bytes(self.record)
        fields = tomllib.loads((FIXTURES / "manifests" / "valid-basic.toml").read_text(encoding="utf-8"))
        fields["build"]["revision"] = self.revision
        fields["build"]["recipe_sha256"] = self.record_fields["recipe_sha256"]
        fields["build"]["id"] = build_identity(self.record)
        fields["build"]["toolchain"] = "Yosys 0.52; nextpnr-mistral 0ab322bd"
        self.fields = fields
        self.manifest = encode_manifest(fields)

    def tearDown(self):
        self.tempdir.cleanup()

    def test_build_record_encoding_is_canonical_and_defines_build_id(self):
        shuffled = dict(reversed(list(self.record_fields.items())))
        self.assertEqual(encode_build_record(shuffled), self.record)
        self.assertEqual(self.record, json.dumps(self.record_fields, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode("utf-8") + b"\n")
        self.assertEqual(build_identity(self.record), hashlib.sha256(self.record).hexdigest()[:32])

    def test_build_record_matches_shared_rfc3986_repository_cases(self):
        for repository, valid in REPOSITORY_URI_CASES:
            with self.subTest(repository=repository):
                fields = dict(self.record_fields)
                fields["repository"] = repository
                if valid:
                    record = encode_build_record(fields)
                    self.assertEqual(json.loads(record)["repository"], repository)
                else:
                    with self.assertRaises(PackageExportError):
                        encode_build_record(fields)

    def test_exports_matching_sealed_directory_archive_and_external_evidence(self):
        directory = export_package(self.manifest, self.rbf, self.store)
        archive = directory.with_suffix(".fcore")
        evidence = directory.with_suffix(".build-inputs.json")

        self.assertEqual(directory, self.store / read_package(directory).package_id)
        self.assertEqual({path.name for path in directory.iterdir()}, {"manifest.toml", "core.rbf"})
        from_directory = read_package(directory)
        from_archive = read_package(archive)
        self.assertEqual(from_directory.package_id, from_archive.package_id)
        self.assertEqual(from_directory.manifest_bytes, from_archive.manifest_bytes)
        self.assertEqual(from_directory.payload_bytes, from_archive.payload_bytes)
        self.assertEqual(evidence.read_bytes(), self.record)
        for path in (directory, *directory.iterdir(), archive, evidence):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode) & 0o222, 0)

        expected_archive_size = (
            512 + ((len(self.manifest) + 511) // 512) * 512
            + 512 + ((len(self.payload) + 511) // 512) * 512
            + 1024
        )
        self.assertEqual(archive.stat().st_size, expected_archive_size)

    def test_manifest_metadata_changes_do_not_alias_the_same_payload(self):
        first = export_package(self.manifest, self.rbf, self.store)
        changed_fields = json.loads(json.dumps(self.fields))
        changed_fields["core"]["description"] = "Different release metadata"
        second = export_package(encode_manifest(changed_fields), self.rbf, self.store)

        self.assertNotEqual(first, second)
        self.assertEqual(read_package(first).payload_bytes, read_package(second).payload_bytes)
        self.assertNotEqual(read_package(first).package_id, read_package(second).package_id)

    def test_reuses_only_a_complete_byte_identical_export(self):
        directory = export_package(self.manifest, self.rbf, self.store)
        self.assertEqual(export_package(self.manifest, self.rbf, self.store), directory)

        archive = directory.with_suffix(".fcore")
        archive.chmod(0o644)
        archive.write_bytes(archive.read_bytes() + b"x")
        archive.chmod(0o444)
        with self.assertRaises(PackageExportError):
            export_package(self.manifest, self.rbf, self.store)

    def test_refuses_to_reuse_writable_package_members(self):
        directory = export_package(self.manifest, self.rbf, self.store)
        manifest_path = directory / "manifest.toml"
        manifest_path.chmod(0o644)

        with self.assertRaises(PackageExportError):
            export_package(self.manifest, self.rbf, self.store)

    def test_rejects_unpinned_or_changed_build_inputs(self):
        main_dirty = self.repo / "dirty.txt"
        dependency_dirty = self.dependency / "dirty.txt"
        recipe = self.repo / "scripts" / "build.py"
        abi = self.repo / "abi" / "fes-gp.json"
        mutations = (
            ("dirty main tree", lambda: main_dirty.write_text("dirty\n", encoding="utf-8"), lambda: main_dirty.unlink()),
            ("dirty dependency", lambda: dependency_dirty.write_text("dirty\n", encoding="utf-8"), lambda: dependency_dirty.unlink()),
            ("changed recipe", lambda: recipe.write_text("changed\n", encoding="utf-8"), lambda: recipe.write_text("# deterministic recipe\n", encoding="utf-8")),
            ("changed abi", lambda: abi.write_text("changed\n", encoding="utf-8"), lambda: abi.write_text('{"version":1}\n', encoding="utf-8")),
            ("wrong build id", lambda: self.fields["build"].__setitem__("id", "0" * 32), lambda: self.fields["build"].__setitem__("id", build_identity(self.record))),
        )
        for label, mutate, restore in mutations:
            with self.subTest(case=label):
                try:
                    mutate()
                    manifest = encode_manifest(self.fields)
                    with self.assertRaises(PackageExportError):
                        export_package(manifest, self.rbf, self.store)
                finally:
                    restore()

    def test_rejects_noncanonical_record_and_unsafe_input_paths(self):
        noncanonical = json.dumps(self.record_fields, indent=2, sort_keys=True).encode("utf-8") + b"\n"
        (self.build / "build-inputs.json").write_bytes(noncanonical)
        self.fields["build"]["id"] = build_identity(noncanonical)
        with self.assertRaises(PackageExportError):
            export_package(encode_manifest(self.fields), self.rbf, self.store)

    def test_rejects_recipe_or_abi_inputs_that_are_not_tracked_by_the_source_commit(self):
        generated = self.build / "generated-recipe.py"
        generated.write_text("# ignored generated input\n", encoding="utf-8")
        fields = dict(self.record_fields)
        fields["recipe"] = "build/generated-recipe.py"
        fields["recipe_sha256"] = hashlib.sha256(generated.read_bytes()).hexdigest()
        record = encode_build_record(fields)
        (self.build / "build-inputs.json").write_bytes(record)
        self.fields["build"]["recipe_sha256"] = fields["recipe_sha256"]
        self.fields["build"]["id"] = build_identity(record)

        with self.assertRaises(PackageExportError):
            export_package(encode_manifest(self.fields), self.rbf, self.store)

    def test_removes_its_publications_if_sources_become_dirty_before_completion(self):
        original = export_core_package._require_clean_repository
        main_checks = 0

        def dirty_before_final_check(root, revision, *, repository=None):
            nonlocal main_checks
            if Path(root).resolve() == self.repo.resolve():
                main_checks += 1
                if main_checks == 2:
                    (self.repo / "late-dirty.txt").write_text("changed\n", encoding="utf-8")
            return original(root, revision, repository=repository)

        package_id = package_identity(self.manifest, self.payload)
        with patch("scripts.export_core_package._require_clean_repository", side_effect=dirty_before_final_check):
            with self.assertRaises(PackageExportError):
                export_package(self.manifest, self.rbf, self.store)

        self.assertFalse((self.store / package_id).exists())
        self.assertFalse((self.store / f"{package_id}.fcore").exists())
        self.assertFalse((self.store / f"{package_id}.build-inputs.json").exists())

        self.fields["build"]["id"] = build_identity(self.record)
        unsafe = dict(self.record_fields)
        unsafe["recipe"] = "../outside.py"
        with self.assertRaises(PackageExportError):
            encode_build_record(unsafe)

        (self.repo / "linked.py").symlink_to(self.repo / "scripts" / "build.py")
        linked = dict(self.record_fields)
        linked["recipe"] = "linked.py"
        linked_record = encode_build_record(linked)
        (self.build / "build-inputs.json").write_bytes(linked_record)
        self.fields["build"]["id"] = build_identity(linked_record)
        with self.assertRaises(PackageExportError):
            export_package(encode_manifest(self.fields), self.rbf, self.store)

    def test_public_cli_and_make_entrypoint_export(self):
        manifest_path = self.build / "manifest.toml"
        manifest_path.write_bytes(self.manifest)
        result = subprocess.run(
            [
                sys.executable,
                str(ROOT / "scripts" / "export_core_package.py"),
                "--manifest", str(manifest_path),
                "--rbf", str(self.rbf),
                "--output", str(self.store),
            ],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(Path(result.stdout.strip()), self.store / read_package(Path(result.stdout.strip())).package_id)

        help_result = subprocess.run(["make", "help"], cwd=ROOT, text=True, capture_output=True)
        self.assertEqual(help_result.returncode, 0, help_result.stderr)
        self.assertIn("export-core-package", help_result.stdout)


if __name__ == "__main__":
    unittest.main()
