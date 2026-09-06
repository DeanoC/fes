from __future__ import annotations

import os
import shutil
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

    def _canonical_fixture(self, *, symlink_bin: bool = False, symlink_build_tools: bool = False) -> tuple[Path, Path]:
        repository = self.fixture / "canonical-repository"
        for relative in (
            "scripts/build_oss.sh",
            "scripts/run_logged.sh",
            "scripts/collect_manifest.py",
            "scripts/lockfile.py",
            "scripts/experiment_policy.py",
            "scripts/oss_summary.py",
            "toolchain.lock",
            "boards/de10nano/pins.qsf",
            "boards/de10nano/clocks.sdc",
            "experiments/010_blinky/rtl/top.v",
            "experiments/020_linux_mailbox/rtl/top.v",
            "experiments/030_m10k_rom/rtl/top.v",
            "experiments/040_mlab_ram/rtl/top.v",
            "experiments/050_lut_mul/rtl/top.v",
        ):
            destination = repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / relative, destination)

        install = repository / "build/toolchain/install"
        build = repository / "build/toolchain/build"
        install.mkdir(parents=True)
        build.mkdir(parents=True)
        external = self.fixture / "nested-external"
        external_bin = external / "bin"
        external_bin.mkdir(parents=True)
        external_build = external / "build"
        external_build.mkdir(parents=True)
        marker = external / "invoked"

        def shim(path: Path, name: str) -> None:
            path.write_text(
                "#!/bin/sh\n"
                f"printf '%s\\n' '{name}' >> '{marker}'\n"
                "exit 0\n",
                encoding="utf-8",
            )
            path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

        if symlink_bin:
            for name in ("yosys", "nextpnr-mistral"):
                shim(external_bin / name, name)
            (install / "bin").symlink_to(external_bin, target_is_directory=True)
        else:
            bin_dir = install / "bin"
            bin_dir.mkdir()
            for name in ("yosys", "nextpnr-mistral"):
                shim(bin_dir / name, name)

        pins = {
            "yosys": "13b43f8c85ec430a33ee55d058fb4c32b42b6910",
            "nextpnr": "7d4f72c0aabc15da932748a54e82a6ff7b41921e",
        }
        for lock_name, commit in pins.items():
            evidence = external_build / lock_name if symlink_build_tools else build / lock_name
            if symlink_build_tools:
                evidence.mkdir(parents=True)
                (build / lock_name).symlink_to(evidence, target_is_directory=True)
            else:
                evidence.mkdir(parents=True)
            binary_name = "nextpnr-mistral" if lock_name == "nextpnr" else lock_name
            binary = install / "bin" / binary_name
            digest = __import__("hashlib").sha256(binary.read_bytes()).hexdigest()
            (evidence / f".built-{commit}").write_text(f"commit={commit}", encoding="utf-8")
            (evidence / f".digest-{commit}.sha256").write_text(digest, encoding="utf-8")
        return repository, marker

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
        self.assertIn("--compress-rbf", commands)
        self.assertIn("run_logged.sh", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_selects_mailbox_policy_and_hps_source(self) -> None:
        result = self._run("--print-commands", "--experiment", "020_linux_mailbox")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/020_linux_mailbox/rtl/top.v", commands)
        self.assertIn("-top top", commands)
        self.assertIn("-nobram -nolutram -nodsp", commands)
        self.assertIn("--freq 50", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_only_for_m10k_rom(self) -> None:
        result = self._run("--print-commands", "--experiment", "030_m10k_rom")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/030_m10k_rom/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertIn("--freq 50", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_lut_memory_only_for_mlab_ram(self) -> None:
        result = self._run("--print-commands", "--experiment", "040_mlab_ram")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/040_mlab_ram/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nodsp -top top", commands)
        self.assertNotIn("-nolutram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_dsp_disabled_for_lut_mul(self) -> None:
        result = self._run("--print-commands", "--experiment", "050_lut_mul")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/050_lut_mul/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
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

    def test_hostile_experiment_value_is_rejected_without_shell_execution(self) -> None:
        hostile = "020_linux_mailbox' ; touch '" + str(self.fixture / "selector-marker") + "' ; echo '"
        result = self._run("--print-commands", "--experiment", hostile)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid EXP", result.stderr + result.stdout)
        self.assertFalse((self.fixture / "selector-marker").exists())
        self.assertFalse(self.marker.exists(), "hostile experiment must stop before tool execution")

    def test_output_symlink_is_rejected_before_external_target_changes(self) -> None:
        experiment = "999_symlink_guard"
        experiment_root = ROOT / "experiments" / experiment
        output_link = ROOT / "build" / "oss" / experiment
        outside = self.fixture / "outside-output"
        outside.mkdir()
        sentinel = outside / "sentinel.txt"
        sentinel.write_text("untouched\n", encoding="utf-8")
        (experiment_root / "rtl").mkdir(parents=True)
        shutil.copy2(ROOT / "experiments" / "010_blinky" / "rtl" / "top.v", experiment_root / "rtl" / "top.v")
        output_link.parent.mkdir(parents=True, exist_ok=True)
        output_link.symlink_to(outside, target_is_directory=True)
        try:
            result = self._run("--experiment", experiment)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("symlink", (result.stderr + result.stdout).lower())
            self.assertEqual(sentinel.read_text(encoding="utf-8"), "untouched\n")
            self.assertFalse((outside / "yosys.log").exists())
            self.assertFalse(self.marker.exists(), "unsafe output must stop before tool execution")
        finally:
            output_link.unlink(missing_ok=True)
            shutil.rmtree(experiment_root, ignore_errors=True)

    def test_real_mode_rejects_external_toolchain_roots_before_invocation(self) -> None:
        result = self._run("--experiment", "010_blinky")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("canonical", (result.stderr + result.stdout).lower())
        self.assertFalse(self.marker.exists(), "external roots must stop before tool execution")

    def test_real_mode_rejects_symlinked_toolchain_roots_before_invocation(self) -> None:
        install_link = self.fixture / "install-link"
        build_link = self.fixture / "build-link"
        install_link.symlink_to(self.install, target_is_directory=True)
        build_link.symlink_to(self.build_root, target_is_directory=True)
        result = self._run(
            "--experiment",
            "010_blinky",
            extra_env={
                "TOOLCHAIN_INSTALL": str(install_link),
                "TOOLCHAIN_BUILD": str(build_link),
            },
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("canonical", (result.stderr + result.stdout).lower())
        self.assertFalse(self.marker.exists(), "symlinked roots must stop before tool execution")

    def test_real_mode_rejects_canonical_install_bin_symlink_before_invocation(self) -> None:
        repository, marker = self._canonical_fixture(symlink_bin=True)
        env = os.environ.copy()
        env.update(
            {
                "TOOLCHAIN_INSTALL": str(repository / "build/toolchain/install"),
                "TOOLCHAIN_BUILD": str(repository / "build/toolchain/build"),
            }
        )
        result = subprocess.run(
            [str(repository / "scripts/build_oss.sh"), "--experiment", "010_blinky"],
            cwd=repository,
            env=env,
            text=True,
            capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlink", (result.stderr + result.stdout).lower())
        self.assertFalse(marker.exists(), "nested install symlink must stop before tool execution")

    def test_real_mode_rejects_canonical_build_tool_symlinks_before_invocation(self) -> None:
        repository, marker = self._canonical_fixture(symlink_build_tools=True)
        env = os.environ.copy()
        env.update(
            {
                "TOOLCHAIN_INSTALL": str(repository / "build/toolchain/install"),
                "TOOLCHAIN_BUILD": str(repository / "build/toolchain/build"),
            }
        )
        result = subprocess.run(
            [str(repository / "scripts/build_oss.sh"), "--experiment", "010_blinky"],
            cwd=repository,
            env=env,
            text=True,
            capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlink", (result.stderr + result.stdout).lower())
        self.assertFalse(marker.exists(), "nested build symlink must stop before tool execution")


if __name__ == "__main__":
    unittest.main()
