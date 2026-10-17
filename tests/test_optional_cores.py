"""Optional cores stay out of an image without a stale package."""
import json
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import build
import bundle
import optional_cores
from recipes import recipe_for


FIGURE = {
    "seed": 4,
    "weight": 10,
    "passing": False,
    "worst_ratio": 0.7692,
    "clocks": {"clk_sys": {"achieved_mhz": 40.0, "constraint_mhz": 52.0}},
}
REVISIONS = {"misteross": "a" * 40, "mister-packages": "b" * 40}


def _package(core_id):
    return {
        "directory": Path("/packages") / core_id,
        "selection_path": Path("/records") / (core_id + ".toml"),
        "inputs": {
            "selection": {
                "format": 2, "kind": "core-package", "core_id": core_id,
                "package_id": "ab" * 32, "payload_sha256": "cd" * 32,
                "misteross_revision": "e" * 40, "mister_packages_revision": "f" * 40,
                "install_path": "/usr/share/mister-runtime/core-packages/" + "ab" * 32,
            },
            "selection_sha256": "11" * 32,
            "manifest_sha256": "22" * 32,
            "core_rbf_sha256": "cd" * 32,
        },
    }


class OptionalCoreTests(unittest.TestCase):
    def test_flags_are_exact(self):
        self.assertFalse(optional_cores.require_all({}))
        self.assertFalse(optional_cores.require_all({"FES_REQUIRE_ALL_CORES": "true"}))
        self.assertTrue(optional_cores.require_all({"FES_REQUIRE_ALL_CORES": "1"}))
        self.assertTrue(optional_cores.skip_all_optional({"FES_SKIP_OPTIONAL_CORES": "1"}))
        self.assertFalse(optional_cores.skip_all_optional({"FES_SKIP_OPTIONAL_CORES": "yes"}))

    def test_skip_list_rejects_required_unknown_and_strict_release(self):
        selected = (recipe_for("fes.pong"), recipe_for("fes.atari-st"))
        with self.assertRaisesRegex(ValueError, "required"):
            optional_cores.parse_skip_list({"FES_SKIP_CORES": "fes.pong"}, selected)
        with self.assertRaisesRegex(ValueError, "unknown"):
            optional_cores.parse_skip_list({"FES_SKIP_CORES": "fes.missing"}, selected)
        with self.assertRaisesRegex(ValueError, "unselected"):
            optional_cores.parse_skip_list(
                {"FES_SKIP_CORES": "fes.atari-st"}, (recipe_for("fes.pong"),))
        for env in (
            {"FES_REQUIRE_ALL_CORES": "1", "FES_SKIP_OPTIONAL_CORES": "1"},
            {"FES_REQUIRE_ALL_CORES": "1", "FES_SKIP_CORES": "fes.atari-st"},
        ):
            with self.assertRaisesRegex(ValueError, "conflicts"):
                optional_cores.parse_skip_list(env, selected)

    def test_skip_omits_optional_core_before_resolve(self):
        resolved = []

        def fake_resolve(revisions, output, env, action, recipe):
            resolved.append(recipe.core_id)
            return _package(recipe.core_id)

        env = {"FES_SKIP_OPTIONAL_CORES": "1"}
        with patch.object(build, "resolve_package_for_action", side_effect=fake_resolve):
            packages, unavailable = build.resolve_packages_for_action(
                REVISIONS, Path("/out"), env, "build", ("fes.pong", "fes.atari-st"))
        self.assertEqual(resolved, ["fes.pong"])
        self.assertEqual(packages[0]["inputs"]["selection"]["core_id"], "fes.pong")
        self.assertEqual(unavailable[0]["core_id"], "fes.atari-st")
        self.assertEqual(unavailable[0]["reason"], "skipped")
        self.assertEqual(unavailable[0]["seeds"], [])

    def test_named_skip_and_required_failure_stay_strict(self):
        def fake_resolve(revisions, output, env, action, recipe):
            if recipe.core_id == "fes.atari-st":
                raise AssertionError("skipped core was resolved")
            return _package(recipe.core_id)

        with patch.object(build, "resolve_package_for_action", side_effect=fake_resolve):
            _, unavailable = build.resolve_packages_for_action(
                REVISIONS, Path("/out"), {"FES_SKIP_CORES": "fes.atari-st"}, "image",
                ("fes.pong", "fes.atari-st"))
        self.assertEqual(unavailable[0]["reason"], "skipped")

        def fail_required(revisions, output, env, action, recipe):
            raise ValueError("selected fes.pong recipe failed")

        with patch.object(build, "resolve_package_for_action", side_effect=fail_required):
            with self.assertRaisesRegex(ValueError, "fes.pong"):
                build.resolve_packages_for_action(
                    REVISIONS, Path("/out"), {}, "build", ("fes.pong",))

    def test_timing_miss_is_recorded_once_without_another_package(self):
        calls = []

        def fake_resolve(revisions, output, env, action, recipe):
            calls.append(recipe.core_id)
            if recipe.core_id == "fes.atari-st":
                failure = ValueError("selected fes.atari-st recipe failed")
                failure.unavailable = {
                    "core_id": "fes.atari-st",
                    "reason": "timing-not-met",
                    "seeds": [4, 5],
                    "figures": [dict(FIGURE)],
                    "log": "/tmp/ignored",
                }
                raise failure
            return _package(recipe.core_id)

        with patch.object(build, "resolve_package_for_action", side_effect=fake_resolve), \
             patch.object(bundle.artifact_cache, "publish") as publish:
            packages, unavailable = build.resolve_packages_for_action(
                REVISIONS, Path("/out"), {}, "build", ("fes.pong", "fes.atari-st"))
        self.assertEqual(calls, ["fes.pong", "fes.atari-st"])
        publish.assert_not_called()
        self.assertEqual([package["inputs"]["selection"]["core_id"] for package in packages],
                         ["fes.pong"])
        row = unavailable[0]
        self.assertEqual(row["reason"], "timing-not-met")
        self.assertEqual(row["name"], "Atari ST")
        self.assertEqual(row["seeds"], [4, 5])
        self.assertEqual(row["figures"][0]["clocks"]["clk_sys"]["achieved_mhz"], 40.0)
        self.assertNotIn("log", row)

    def test_missing_report_and_strict_release_are_build_failures(self):
        def fail(revisions, output, env, action, recipe):
            if recipe.core_id == "fes.atari-st":
                raise ValueError("selected fes.atari-st recipe failed")
            return _package(recipe.core_id)

        with patch.object(build, "resolve_package_for_action", side_effect=fail):
            _, unavailable = build.resolve_packages_for_action(
                REVISIONS, Path("/out"), {}, "build", ("fes.pong", "fes.atari-st"))
            with self.assertRaisesRegex(ValueError, "fes.atari-st"):
                build.resolve_packages_for_action(
                    REVISIONS, Path("/out"), {"FES_REQUIRE_ALL_CORES": "1"}, "build",
                    ("fes.pong", "fes.atari-st"))
        self.assertEqual(unavailable[0]["reason"], "build-failed")
        self.assertEqual(unavailable[0]["figures"], [])

    def test_failed_producer_does_not_search_another_cache_key(self):
        calls = {"build": 0, "candidates": 0}

        def candidates(*args, **kwargs):
            calls["candidates"] += 1
            return []

        def fail(*args, **kwargs):
            calls["build"] += 1
            error = ValueError("selected fes.atari-st recipe failed")
            error.unavailable = {"reason": "timing-not-met", "core_id": "fes.atari-st",
                                 "seeds": [4], "figures": [dict(FIGURE)]}
            raise error

        with tempfile.TemporaryDirectory() as temporary, \
             patch.object(bundle, "require_private_audit_cache"), \
             patch.object(bundle, "canonical_package_record", return_value=b"{}"), \
             patch.object(bundle, "_plain_directory"), \
             patch.object(bundle, "_matching_package_candidates", side_effect=candidates), \
             patch.object(bundle, "_build_package", side_effect=fail), \
             patch.object(bundle.artifact_cache, "store_for", return_value=Path(temporary)), \
             patch.object(bundle.artifact_cache, "publish") as publish:
            with self.assertRaises(ValueError) as caught:
                bundle.resolve_core_package(
                    temporary, "b" * 40, Path(temporary) / "selection.toml",
                    recipe=recipe_for("fes.atari-st"))
            self.assertEqual(calls["build"], 1)
            self.assertEqual(calls["candidates"], 2)
            publish.assert_not_called()
            self.assertEqual(caught.exception.unavailable["reason"], "timing-not-met")

    def test_producer_report_is_read_once_and_removed(self):
        source = None

        def fake_run(args, **kwargs):
            root = Path(args[args.index("--root") + 1])
            report = root / "build" / optional_cores.PRODUCER_REPORT_NAME
            report.parent.mkdir(parents=True)
            report.write_text(json.dumps({
                "version": 1, "core_id": "fes.atari-st", "reason": "timing-not-met",
                "seeds": [4], "figures": [FIGURE], "log": "/tmp/placer.log",
            }))
            raise subprocess.CalledProcessError(1, args)

        with tempfile.TemporaryDirectory() as temporary, \
             patch.object(subprocess, "run", side_effect=fake_run):
            source = Path(temporary)
            with self.assertRaises(ValueError) as caught:
                bundle._build_package(source, recipe=recipe_for("fes.atari-st"))
            self.assertFalse((source / "build" / optional_cores.PRODUCER_REPORT_NAME).exists())
        self.assertEqual(caught.exception.unavailable["seeds"], [4])
        self.assertNotIn("log", caught.exception.unavailable)

    def test_manifest_fingerprint_and_verify_reuse(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            row = optional_cores.unavailable_record(
                "fes.atari-st", "timing-not-met", seeds=(4,), figures=(FIGURE,))
            path = optional_cores.publish_manifest(output, (row,))
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o444)
            optional_cores.verify_manifest(output, (row,))
            encoded = json.loads(path.read_text())
            self.assertEqual(encoded["version"], 1)
            self.assertEqual(encoded["cores"][0]["reason"], "timing-not-met")
            skipped = optional_cores.unavailable_record("fes.atari-st", "skipped")
            package = _package("fes.pong")
            first, info = build.image_fingerprint("base", {"sources": {}}, (package,), (row,))
            second, _ = build.image_fingerprint("base", {"sources": {}}, (package,), (skipped,))
            self.assertNotEqual(first, second)
            self.assertEqual(build.recorded_image_fingerprint(info), first)
            bare, bare_info = build.image_fingerprint("base", {"sources": {}}, (package,))
            self.assertNotIn("unavailable_cores", bare_info)
            self.assertEqual(build.recorded_image_fingerprint(bare_info), bare)
            self.assertIn("FES_UNAVAILABLE_CORES=" + str(path),
                          build.package_arguments((package,), path))
            (output / "inputs.json").write_text(json.dumps(info) + "\n")
            resolved = []

            def fake_resolve(revisions, output_path, env, action, recipe):
                resolved.append((action, recipe.core_id))
                return package

            with patch.object(build, "resolve_package_for_action", side_effect=fake_resolve):
                _, unavailable = build.resolve_packages_for_action(
                    REVISIONS, output, {}, "verify", ("fes.pong", "fes.atari-st"))
                with self.assertRaisesRegex(ValueError, "FES_REQUIRE_ALL_CORES=1"):
                    build.resolve_packages_for_action(
                        REVISIONS, output, {"FES_REQUIRE_ALL_CORES": "1"}, "verify",
                        ("fes.pong", "fes.atari-st"))
            self.assertEqual(resolved, [("verify", "fes.pong"), ("verify", "fes.pong")])
            self.assertNotIn(("verify", "fes.atari-st"), resolved)
            self.assertEqual(unavailable[0]["reason"], "timing-not-met")
            lines = optional_cores.warning_lines(unavailable)
            self.assertTrue(lines[0].startswith("WARNING:"))
            self.assertIn("fes.atari-st", "\n".join(lines))
            self.assertIn("timing-not-met", "\n".join(lines))
            self.assertIn("FES_REQUIRE_ALL_CORES=1", "\n".join(lines))
            self.assertIn("FES_SKIP_OPTIONAL_CORES=1", "\n".join(lines))

    def test_symlink_report_is_ignored(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            build_dir = root / "build"
            build_dir.mkdir()
            target = build_dir / "secret"
            target.write_text("secret")
            report = build_dir / optional_cores.PRODUCER_REPORT_NAME
            report.symlink_to(target)
            self.assertIsNone(optional_cores.read_producer_report(report))


if __name__ == "__main__":
    unittest.main()
