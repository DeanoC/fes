from __future__ import annotations

import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BUILD_OSS = ROOT / "scripts" / "build_oss.sh"


class OssPipelinePurityTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tempdir = tempfile.TemporaryDirectory(prefix="oss-purity-")
        self.fixture = Path(self.tempdir.name)
        self.install = self.fixture / "toolchain" / "install"
        self.bin_dir = self.install / "bin"
        self.bin_dir.mkdir(parents=True)
        self.build_root = self.fixture / "toolchain" / "build"
        self.marker = self.fixture / "invoked"
        for name in (
            "yosys",
            "nextpnr-mistral",
            "mistral-cv",
            "verilator",
            "openFPGALoader",
        ):
            self._shim(name)

    def tearDown(self) -> None:
        self.tempdir.cleanup()

    def _shim(self, name: str) -> None:
        path = self.bin_dir / name
        path.write_text(
            "#!/bin/sh\n"
            f"printf '%s\\n' '{name}' >> '{self.marker}'\n"
            "exit 0\n",
            encoding="utf-8",
        )
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    def _run(self, *args: str, extra_env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.update(
            {
                "TOOLCHAIN_INSTALL": str(self.install),
                "TOOLCHAIN_BUILD": str(self.build_root),
                "PATH": os.pathsep.join((str(self.bin_dir), os.environ.get("PATH", ""))),
            }
        )
        if extra_env:
            env.update(extra_env)
        return subprocess.run(
            [str(BUILD_OSS), *args],
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
        )

    def test_print_commands_uses_exact_open_pipeline_inputs(self) -> None:
        result = self._run("--print-commands", "--experiment", "010_blinky")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertIn("5CSEBA6U23I7", commands)
        self.assertIn("boards/de10nano/pins.qsf", commands)
        self.assertIn("boards/de10nano/clocks.sdc", commands)
        self.assertIn("top.rbf", commands)
        self.assertIn("run_logged.sh", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_script_and_commands_have_no_proprietary_lane_references(self) -> None:
        forbidden = ("quartus", "qsys", "sopc", "/opt/intel", "QUARTUS_ROOTDIR")
        script = BUILD_OSS.read_text(encoding="utf-8")
        result = self._run("--print-commands", "--experiment", "010_blinky")
        self.assertEqual(result.returncode, 0, result.stderr)
        for word in forbidden:
            self.assertNotIn(word, script)
            self.assertNotIn(word, result.stdout)

    def test_invalid_experiment_is_rejected_before_any_command_runs(self) -> None:
        result = self._run("--print-commands", "--experiment", "../../tmp")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid EXP", result.stderr + result.stdout)
        self.assertFalse(self.marker.exists(), "invalid experiment must stop before tool execution")


if __name__ == "__main__":
    unittest.main()
