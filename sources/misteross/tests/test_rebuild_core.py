import hashlib
import json
import os
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts import core_lock
from scripts import fetch_core
from scripts import rebuild_core


ROOT = Path(__file__).resolve().parents[1]
REBUILD = ROOT / "scripts" / "rebuild_core.py"


class RebuildCoreTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.root = Path(self.tempdir.name) / "repo"
        self.root.mkdir()
        self.quartus_marker = Path(self.tempdir.name) / "quartus-called"
        self.path_marker = Path(self.tempdir.name) / "path-quartus-called"

    def tearDown(self):
        self.tempdir.cleanup()

    def _git(self, args, cwd):
        subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True)

    def _pin_and_fetch(self, payload=b"locked-rbf", rbf_name="core_20260603.rbf"):
        digest = hashlib.sha256(payload).hexdigest()
        upstream = Path(self.tempdir.name) / "upstream"
        if upstream.exists():
            shutil.rmtree(upstream)
        upstream.mkdir()
        (upstream / "releases").mkdir()
        (upstream / "sys").mkdir()
        (upstream / "releases" / rbf_name).write_bytes(payload)
        (upstream / "core.qpf").write_text(
            'QUARTUS_VERSION = "17.0"\nPROJECT_REVISION = "core"\n',
            encoding="utf-8",
        )
        (upstream / "rtl.v").write_text("module core; endmodule\n", encoding="utf-8")
        (upstream / "sys" / "build_id.tcl").write_text(
            "\t"
            + rebuild_core.CLOCK_BUILD_DATE_LINE
            + "\n",
            encoding="utf-8",
        )
        self._git(["init"], upstream)
        self._git(["config", "user.email", "test@example.com"], upstream)
        self._git(["config", "user.name", "test"], upstream)
        self._git(["add", "."], upstream)
        self._git(["commit", "-m", "pin"], upstream)
        commit = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=upstream,
            text=True,
            capture_output=True,
            check=True,
        ).stdout.strip()
        pin = core_lock.CorePin(
            name="fixture",
            repo=str(upstream),
            commit=commit,
            rbf_path=f"releases/{rbf_name}",
            rbf_sha256=digest,
            rbf_size=len(payload),
            project="core.qpf",
            rationale="unit fixture",
        )
        lock = self.root / "cores.lock"
        lock.write_text(
            "\n".join(
                [
                    "[core.fixture]",
                    f'repo = "https://example.invalid/fixture.git"',
                    f'commit = "{commit}"',
                    f'rbf_path = "releases/{rbf_name}"',
                    f'rbf_sha256 = "{digest}"',
                    f"rbf_size = {len(payload)}",
                    'project = "core.qpf"',
                    'rationale = "unit fixture"',
                    "",
                ]
            ),
            encoding="utf-8",
        )
        source = fetch_core.fetch_core(pin, self.root)
        return pin, lock, source

    def _quartus(self, version="17.0.2", payload=b"built-rbf", layout="bin"):
        quartus_root = Path(self.tempdir.name) / "quartus-root"
        if layout == "nested":
            binary = quartus_root / "quartus" / "bin" / "quartus_sh"
        else:
            binary = quartus_root / "bin" / "quartus_sh"
        binary.parent.mkdir(parents=True)
        marker = self.quartus_marker
        binary.write_text(
            "#!/usr/bin/env bash\n"
            "set -eu\n"
            "if [[ ${1:-} == --version ]]; then\n"
            f"  printf '%s\\n' 'Quartus Prime Version {version}'\n"
            "  exit 0\n"
            "fi\n"
            f"printf '%s\\n' compiled > '{marker}'\n"
            "mkdir -p output_files\n"
            "revision=${3:-core}\n"
            f"printf '%s' '{payload.decode('ascii')}' > \"output_files/${{revision}}.rbf\"\n"
            f"printf '%s\\n' \"${{MISTER_BUILD_DATE-}}\" > '{marker}.date'\n",
            encoding="utf-8",
        )
        binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
        return quartus_root, binary

    def _run(self, *args, env=None, path_prefix=None):
        merged = os.environ.copy()
        merged.pop("QUARTUS_ROOTDIR", None)
        merged.pop("QSYS_ROOTDIR", None)
        merged.pop("BUILD_DATE", None)
        merged.pop("MISTER_BUILD_DATE", None)
        if path_prefix is not None:
            merged["PATH"] = os.pathsep.join((str(path_prefix), merged.get("PATH", "")))
        if env:
            merged.update(env)
        return subprocess.run(
            [sys.executable, str(REBUILD), *args],
            cwd=ROOT,
            env=merged,
            text=True,
            capture_output=True,
        )

    def test_missing_quartus_rootdir_fails_without_compiling(self):
        pin, lock, source = self._pin_and_fetch()
        result = self._run("--core", "fixture", "--lock", str(lock), "--root", str(self.root))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("QUARTUS_ROOTDIR", result.stderr)
        self.assertFalse(self.quartus_marker.exists())
        self.assertEqual(
            subprocess.run(
                ["git", "status", "--porcelain=v1"],
                cwd=source,
                text=True,
                capture_output=True,
                check=True,
            ).stdout.strip(),
            "",
        )
        self.assertFalse((self.root / "build" / "rebuild").exists())

    def test_wrong_version_is_rejected(self):
        pin, lock, source = self._pin_and_fetch()
        quartus_root, _ = self._quartus(version="17.1.0")
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            env={"QUARTUS_ROOTDIR": str(quartus_root)},
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("17.0.2", result.stderr)
        self.assertFalse(self.quartus_marker.exists())

    def test_missing_fetch_is_rejected(self):
        pin, lock, source = self._pin_and_fetch()
        shutil.rmtree(source)
        quartus_root, _ = self._quartus()
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            env={"QUARTUS_ROOTDIR": str(quartus_root)},
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fetch-core", result.stderr)
        self.assertFalse(self.quartus_marker.exists())

    def test_print_commands_does_not_compile_or_stage(self):
        pin, lock, source = self._pin_and_fetch()
        quartus_root, _ = self._quartus()
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            "--print-commands",
            env={"QUARTUS_ROOTDIR": str(quartus_root)},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("core: fixture", result.stdout)
        self.assertIn("--flow compile core", result.stdout)
        self.assertIn("build_date: 260603", result.stdout)
        self.assertFalse(self.quartus_marker.exists())
        self.assertFalse((self.root / "build" / "rebuild" / "fixture" / "project").exists())

    def test_path_quartus_is_ignored(self):
        pin, lock, source = self._pin_and_fetch()
        quartus_root, _ = self._quartus()
        path_dir = Path(self.tempdir.name) / "on-path"
        path_dir.mkdir()
        path_binary = path_dir / "quartus_sh"
        path_binary.write_text(
            "#!/usr/bin/env bash\n"
            f"printf '%s\\n' path > '{self.path_marker}'\n"
            "exit 0\n",
            encoding="utf-8",
        )
        path_binary.chmod(path_binary.stat().st_mode | stat.S_IXUSR)
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            path_prefix=path_dir,
            env={"QUARTUS_ROOTDIR": str(quartus_root)},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(self.quartus_marker.exists())
        self.assertFalse(self.path_marker.exists())

    def test_rebuild_reports_mismatch_without_dirtying_fetch(self):
        pin, lock, source = self._pin_and_fetch(payload=b"locked-rbf")
        quartus_root, _ = self._quartus(payload=b"built-rbf")
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            env={"QUARTUS_ROOTDIR": str(quartus_root)},
        )
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertIn("identical: no", result.stdout)
        self.assertIn("upstream sha256", result.stdout)
        self.assertIn("rebuild sha256", result.stdout)
        artifact = self.root / "build" / "rebuild" / "fixture" / "fixture.rbf"
        self.assertEqual(artifact.read_bytes(), b"built-rbf")
        report = json.loads(
            (self.root / "build" / "rebuild" / "fixture" / "compare.json").read_text(
                encoding="utf-8"
            )
        )
        self.assertFalse(report["match"])
        self.assertEqual(report["built_sha256"], hashlib.sha256(b"built-rbf").hexdigest())
        self.assertEqual(report["locked_sha256"], pin.rbf_sha256)
        self.assertFalse((source / "output_files").exists())
        self.assertFalse((source / "db").exists())
        self.assertEqual(
            subprocess.run(
                ["git", "status", "--porcelain=v1", "--untracked-files=all"],
                cwd=source,
                text=True,
                capture_output=True,
                check=True,
            ).stdout.strip(),
            "",
        )
        staged_release = (
            self.root / "build" / "rebuild" / "fixture" / "project" / "releases" / "core.rbf"
        )
        self.assertFalse(staged_release.exists())

    def test_rebuild_reports_match_when_bytes_equal(self):
        payload = b"same-bytes"
        pin, lock, source = self._pin_and_fetch(payload=payload)
        quartus_root, _ = self._quartus(payload=payload, layout="nested")
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            env={"QUARTUS_ROOTDIR": str(quartus_root)},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("identical: yes", result.stdout)
        report = json.loads(
            (self.root / "build" / "rebuild" / "fixture" / "compare.json").read_text(
                encoding="utf-8"
            )
        )
        self.assertTrue(report["match"])
        self.assertEqual(report["built_size"], len(payload))
        self.assertEqual(report["build_date"], "260603")
        self.assertEqual((self.quartus_marker.with_suffix(".date")).read_text(encoding="utf-8").strip(), "260603")
        staged_tcl = (
            self.root / "build" / "rebuild" / "fixture" / "project" / "sys" / "build_id.tcl"
        ).read_text(encoding="utf-8")
        self.assertIn("MISTER_BUILD_DATE", staged_tcl)
        self.assertNotIn(rebuild_core.CLOCK_BUILD_DATE_LINE, staged_tcl)
        self.assertEqual(
            (self.root / "build" / "rebuild" / "fixture" / "project" / "build_id.v")
            .read_text(encoding="utf-8"),
            '`define BUILD_DATE "260603"',
        )
        self.assertIn(
            rebuild_core.CLOCK_BUILD_DATE_LINE,
            (source / "sys" / "build_id.tcl").read_text(encoding="utf-8"),
        )

    def test_megadrive_build_id_line_is_the_patch_needle(self):
        path = ROOT / "build" / "cores" / "megadrive" / "sys" / "build_id.tcl"
        if not path.is_file():
            self.skipTest("megadrive fetch not present")
        self.assertIn(rebuild_core.CLOCK_BUILD_DATE_LINE, path.read_text(encoding="utf-8"))

    def test_derive_build_date_from_release_name(self):
        self.assertEqual(
            rebuild_core.derive_build_date("releases/MegaDrive_20260603.rbf"),
            "260603",
        )
        self.assertIsNone(rebuild_core.derive_build_date("releases/core.rbf"))
        pin = core_lock.load_lock(ROOT / "cores.lock")["megadrive"]
        self.assertEqual(rebuild_core.derive_build_date(pin.rbf_path), "260603")

    def test_invalid_build_date_is_rejected(self):
        pin, lock, source = self._pin_and_fetch()
        quartus_root, _ = self._quartus()
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            "--build-date",
            "2026-06-03",
            env={"QUARTUS_ROOTDIR": str(quartus_root)},
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("YYMMDD", result.stderr)
        self.assertFalse(self.quartus_marker.exists())


if __name__ == "__main__":
    unittest.main()
