import hashlib
import json
import stat
import subprocess
import sys
import tempfile
import tomllib
import unittest
from dataclasses import replace
from pathlib import Path
from unittest.mock import patch

from scripts import export_core_bundle
from scripts.core_lock import CorePin
from scripts.export_core_bundle import (
    BundleExportError,
    BundleManifest,
    encode_manifest,
    export_bundle,
)


TOOLCHAIN = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
ROOT = Path(__file__).resolve().parents[1]


class ExportCoreBundleTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.root = Path(self.tempdir.name) / "repo"
        self.root.mkdir()
        self.payload = b"sealed megadrive rbf\n"
        self.pin = CorePin(
            name="megadrive",
            repo="https://github.com/MiSTer-devel/MegaDrive_MiSTer",
            commit="7365a137cfd8fa6f041e964d8b953159c0ec42d9",
            rbf_path="releases/MegaDrive_20260603.rbf",
            rbf_sha256="0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839",
            rbf_size=4296864,
            project="MegaDrive.qpf",
            rationale="fixture",
        )
        self.recipe = self.root / "scripts" / "rebuild_core.py"
        self.recipe.parent.mkdir()
        self.recipe.write_text("# deterministic rebuild recipe\n", encoding="utf-8")
        self.rbf = self.root / "build" / "rebuild" / "megadrive" / "megadrive.rbf"
        self.rbf.parent.mkdir(parents=True)
        self.rbf.write_bytes(self.payload)
        self.compare_path = self.rbf.parent / "compare.json"
        self._write_compare()

    def tearDown(self):
        self.tempdir.cleanup()

    def _compare(self):
        digest = hashlib.sha256(self.payload).hexdigest()
        return {
            "core": "megadrive",
            "commit": "7365a137cfd8fa6f041e964d8b953159c0ec42d9",
            "project": "MegaDrive.qpf",
            "built_sha256": digest,
            "built_size": len(self.payload),
            "locked_sha256": "0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839",
            "locked_size": 4296864,
            "match": False,
            "quartus": "/opt/quartus/bin/quartus_sh",
            "quartus_version": TOOLCHAIN,
            "built_rbf": str(self.rbf),
            "source": str(self.root / "build" / "cores" / "megadrive"),
            "build_date": "260603",
            "identical": False,
            "recipe_sha256": hashlib.sha256(self.recipe.read_bytes()).hexdigest(),
        }

    def _write_compare(self, value=None):
        self.compare_path.write_text(
            json.dumps(self._compare() if value is None else value), encoding="utf-8"
        )

    def _expected_manifest(self):
        return {
            "format": 1,
            "abi": "mister",
            "system": "megadrive",
            "artifact": "megadrive.rbf",
            "sha256": hashlib.sha256(self.payload).hexdigest(),
            "size": len(self.payload),
            "repository": self.pin.repo,
            "revision": self.pin.commit,
            "recipe": "scripts/rebuild_core.py",
            "recipe_sha256": hashlib.sha256(self.recipe.read_bytes()).hexdigest(),
            "toolchain": TOOLCHAIN,
        }

    def test_exports_closed_manifest_and_read_only_content_addressed_files(self):
        bundle = export_bundle(self.pin, self.root)

        self.assertEqual(
            bundle,
            self.root
            / "build"
            / "bundles"
            / "megadrive"
            / hashlib.sha256(self.payload).hexdigest(),
        )
        self.assertEqual(
            sorted(path.name for path in bundle.iterdir()),
            ["megadrive-rbf.toml", "megadrive.rbf"],
        )
        self.assertEqual((bundle / "megadrive.rbf").read_bytes(), self.payload)
        self.assertEqual(
            tomllib.loads((bundle / "megadrive-rbf.toml").read_text(encoding="utf-8")),
            self._expected_manifest(),
        )
        self.assertEqual((bundle / "megadrive-rbf.toml").read_bytes().count(b"\n"), 11)
        for path in bundle.iterdir():
            self.assertTrue(path.is_file())
            self.assertFalse(path.is_symlink())
            self.assertEqual(stat.S_IMODE(path.stat().st_mode) & 0o222, 0)
        self.assertEqual(stat.S_IMODE(bundle.stat().st_mode) & 0o222, 0)

    def test_public_script_and_make_entrypoint_start(self):
        help_result = subprocess.run(
            [sys.executable, str(ROOT / "scripts" / "export_core_bundle.py"), "--help"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(help_result.returncode, 0, help_result.stderr)
        self.assertIn("--core", help_result.stdout)

        make_result = subprocess.run(
            ["make", "export-core-bundle", "CORE=not-a-core"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertNotEqual(make_result.returncode, 0)
        self.assertIn("unknown core not-a-core", make_result.stderr)

    def test_publishes_the_snapshot_that_comparison_evidence_validated(self):
        validated_payload = self.payload
        replacement = b"x" * len(validated_payload)
        original_validate = export_core_bundle._validate_compare

        def mutate_after_validation(*args):
            original_validate(*args)
            self.rbf.write_bytes(replacement)

        with patch(
            "scripts.export_core_bundle._validate_compare",
            side_effect=mutate_after_validation,
        ):
            bundle = export_bundle(self.pin, self.root)

        digest = hashlib.sha256(validated_payload).hexdigest()
        self.assertEqual(bundle.name, digest)
        self.assertEqual((bundle / "megadrive.rbf").read_bytes(), validated_payload)
        manifest = tomllib.loads((bundle / "megadrive-rbf.toml").read_text(encoding="utf-8"))
        self.assertEqual(manifest["sha256"], digest)
        self.assertEqual(manifest["size"], len(validated_payload))

    def test_rejects_forged_quartus_version_or_executable_identity(self):
        forged = {
            "version": ("quartus_version", "Version 17.0.2 Build forged"),
            "executable": ("quartus", "/usr/local/bin/not-quartus"),
        }
        for label, (field, value) in forged.items():
            with self.subTest(label=label):
                compare = self._compare()
                compare[field] = value
                self._write_compare(compare)
                with self.assertRaises(BundleExportError):
                    export_bundle(self.pin, self.root)

    def test_seals_temporary_directory_before_publish_and_cleans_it_on_failure(self):
        observed_modes = []

        def reject_publish(source, destination):
            observed_modes.append(stat.S_IMODE(Path(source).stat().st_mode))
            raise OSError("simulated publish failure")

        with patch("scripts.export_core_bundle._publish_no_replace", side_effect=reject_publish):
            with self.assertRaises(OSError):
                export_bundle(self.pin, self.root)

        self.assertEqual(len(observed_modes), 1)
        self.assertEqual(observed_modes[0] & 0o222, 0)
        parent = self.root / "build" / "bundles" / "megadrive"
        self.assertEqual(list(parent.glob(".export-*")), [])

    def test_refuses_competing_final_digest_directory_without_replacing_it(self):
        digest = hashlib.sha256(self.payload).hexdigest()
        parent = self.root / "build" / "bundles" / "megadrive"
        final = parent / digest
        original_chmod = export_core_bundle.os.chmod

        for label, contents in (("empty", {}), ("partial", {"partial": b"keep me"})):
            with self.subTest(label=label):
                def inject_final(path, mode, *args, **kwargs):
                    original_chmod(path, mode, *args, **kwargs)
                    path = Path(path)
                    if (
                        path.parent == parent
                        and path.name.startswith(".export-")
                        and mode == 0o555
                    ):
                        final.mkdir()
                        for name, data in contents.items():
                            (final / name).write_bytes(data)

                try:
                    with patch("scripts.export_core_bundle.os.chmod", side_effect=inject_final):
                        try:
                            export_bundle(self.pin, self.root)
                        except Exception as exc:
                            failure = exc
                        else:
                            failure = None

                    self.assertIsInstance(failure, BundleExportError)
                    self.assertTrue(final.is_dir())
                    self.assertEqual(
                        {path.name: path.read_bytes() for path in final.iterdir()}, contents
                    )
                finally:
                    if final.exists():
                        final.chmod(0o755)
                        for path in final.iterdir():
                            path.unlink()
                        final.rmdir()

    def test_rejects_stale_comparison_evidence(self):
        bad_values = {
            "wrong core": ("core", "other"),
            "wrong commit": ("commit", "0" * 40),
            "wrong project": ("project", "Other.qpf"),
            "wrong built digest": ("built_sha256", "0" * 64),
            "wrong built size": ("built_size", len(self.payload) + 1),
        }
        for label, (field, value) in bad_values.items():
            with self.subTest(label=label):
                compare = self._compare()
                compare[field] = value
                self._write_compare(compare)
                with self.assertRaises(BundleExportError):
                    export_bundle(self.pin, self.root)

    def test_refuses_a_non_authoritative_upstream_revision(self):
        pin = replace(self.pin, commit="0" * 40)
        compare = self._compare()
        compare["commit"] = pin.commit
        self._write_compare(compare)

        with self.assertRaises(BundleExportError):
            export_bundle(pin, self.root)

    def test_rejects_missing_rbf_or_comparison_evidence(self):
        self.rbf.unlink()
        with self.assertRaises(BundleExportError):
            export_bundle(self.pin, self.root)

        self.rbf.write_bytes(self.payload)
        self.compare_path.unlink()
        with self.assertRaises(BundleExportError):
            export_bundle(self.pin, self.root)

    def test_rejects_unrecognized_comparison_fields(self):
        compare = self._compare()
        compare["untrusted"] = True
        self._write_compare(compare)

        with self.assertRaises(BundleExportError):
            export_bundle(self.pin, self.root)

    def test_rejects_comparison_path_outside_rebuild_core_directory(self):
        escaped = self.root / "build" / "rebuild" / "escaped.rbf"
        escaped.write_bytes(self.payload)
        compare = self._compare()
        compare["built_rbf"] = str(escaped)
        self._write_compare(compare)

        with self.assertRaises(BundleExportError):
            export_bundle(self.pin, self.root)

    def test_refuses_to_reuse_partial_digest_directory(self):
        digest = hashlib.sha256(self.payload).hexdigest()
        partial = self.root / "build" / "bundles" / "megadrive" / digest
        partial.mkdir(parents=True)
        (partial / "megadrive.rbf").write_bytes(self.payload)

        with self.assertRaises(BundleExportError):
            export_bundle(self.pin, self.root)

    def test_encode_manifest_rejects_invalid_closed_values(self):
        manifest = BundleManifest(**self._expected_manifest())
        invalid = {
            "control character": replace(manifest, repository="https://example.invalid/\n"),
            "non-https repository": replace(manifest, repository="http://example.invalid/core"),
            "invalid revision": replace(manifest, revision="A" * 40),
            "invalid digest": replace(manifest, sha256="A" * 64),
            "zero size": replace(manifest, size=0),
            "wrong ABI": replace(manifest, abi="other"),
            "wrong system": replace(manifest, system="other"),
            "wrong artifact": replace(manifest, artifact="other.rbf"),
        }
        for label, value in invalid.items():
            with self.subTest(label=label):
                with self.assertRaises(BundleExportError):
                    encode_manifest(value)


if __name__ == "__main__":
    unittest.main()
