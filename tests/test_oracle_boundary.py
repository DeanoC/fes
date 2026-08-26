import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ORACLE = ROOT / "scripts" / "build_oracle.sh"


class OracleBoundaryTests(unittest.TestCase):
    def _run(self, *args, env=None):
        merged = os.environ.copy()
        merged.pop("QUARTUS_ROOTDIR", None)
        if env:
            merged.update(env)
        return subprocess.run(
            [str(ORACLE), *args],
            cwd=ROOT,
            env=merged,
            text=True,
            capture_output=True,
        )

    def _quartus(self, version):
        temp = tempfile.TemporaryDirectory()
        root = Path(temp.name) / "quartus-root"
        binary = root / "quartus" / "bin" / "quartus_sh"
        binary.parent.mkdir(parents=True)
        marker = Path(temp.name) / "compile-marker"
        binary.write_text(
            "#!/usr/bin/env bash\n"
            "if [[ ${1:-} == --version ]]; then\n"
            f"  printf '%s\\n' 'Quartus Prime Version {version}'\n"
            "else\n"
            f"  : > '{marker}'\n"
            "fi\n",
            encoding="utf-8",
        )
        binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
        return temp, root, marker

    def test_absent_root_is_friendly_and_does_not_create_oracle_output(self):
        result = self._run("--experiment", "010_blinky")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn(
            "Quartus oracle unavailable; OSS and simulation remain usable",
            result.stdout + result.stderr,
        )
        self.assertFalse((ROOT / "build" / "oracle" / "010_blinky").exists())

    def test_wrong_quartus_version_is_rejected(self):
        temp, root, marker = self._quartus("17.0.0")
        with temp:
            result = self._run(
                "--experiment",
                "010_blinky",
                "--print-commands",
                env={"QUARTUS_ROOTDIR": str(root)},
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("17.0.2", result.stdout + result.stderr)
        self.assertFalse(marker.exists())

    def test_exact_version_reaches_print_commands_without_compile(self):
        temp, root, marker = self._quartus("17.0.2")
        with temp:
            result = self._run(
                "--experiment",
                "010_blinky",
                "--print-commands",
                env={"QUARTUS_ROOTDIR": str(root)},
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("quartus_sh", result.stdout)
        self.assertIn("--flow compile", result.stdout)
        self.assertIn("experiments/010_blinky/rtl/top.v", result.stdout)
        self.assertIn("boards/de10nano/clocks.sdc", result.stdout)
        self.assertFalse(marker.exists())

    def test_oss_and_sim_make_recipes_have_no_quartus_lane_reference(self):
        for target in ("oss", "sim"):
            result = subprocess.run(
                ["make", "-n", target, "EXP=010_blinky"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn("quartus", result.stdout.lower())
            self.assertNotIn("quartus_rootdir", result.stdout.lower())


if __name__ == "__main__":
    unittest.main()
