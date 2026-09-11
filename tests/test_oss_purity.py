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
            "experiments/060_dsp_mul/rtl/top.v",
            "experiments/070_mixed_mem/rtl/top.v",
            "experiments/080_dsp_mem/rtl/top.v",
            "experiments/090_pll_clock/rtl/top.v",
            "experiments/100_dsp_rom/rtl/top.v",
            "experiments/110_pll_reset/rtl/top.v",
            "experiments/120_pll_dsp/rtl/top.v",
            "experiments/130_pll_dsp_40/rtl/top.v",
            "experiments/140_pll_dsp_20/rtl/top.v",
            "experiments/150_pll_dsp_80/rtl/top.v",
            "experiments/160_pll_dsp_100/rtl/top.v",
            "experiments/170_pll_dual/rtl/top.v",
            "experiments/180_pll_frac/rtl/top.v",
            "experiments/190_pll_frac_441/rtl/top.v",
            "experiments/200_pll_frac_dual/rtl/top.v",
            "experiments/610_pll_frac_7425/rtl/top.v",
            "experiments/620_ddr_clock/rtl/top.v",
            "experiments/620_ddr_clock/pins.qsf",
            "experiments/630_sdr_output/rtl/top.v",
            "experiments/630_sdr_output/pins.qsf",
            "experiments/640_sdr_input/rtl/top.v",
            "experiments/640_sdr_input/pins.qsf",
            "experiments/650_ddr_input/rtl/top.v",
            "experiments/650_ddr_input/pins.qsf",
            "experiments/660_ddr_data/rtl/top.v",
            "experiments/660_ddr_data/pins.qsf",
            "experiments/670_altiobuf/rtl/top.v",
            "experiments/670_altiobuf/pins.qsf",
            "experiments/690_ddr_bidir/rtl/top.v",
            "experiments/690_ddr_bidir/pins.qsf",
            "experiments/210_pll_duty/rtl/top.v",
            "experiments/220_pll_phase/rtl/top.v",
            "experiments/230_pll_phase_180/rtl/top.v",
            "experiments/240_pll_phase_270/rtl/top.v",
            "experiments/250_pll_triple/rtl/top.v",
            "experiments/260_pll_quad/rtl/top.v",
            "experiments/270_pll_multi_duty/rtl/top.v",
            "experiments/280_pll_quadrature/rtl/top.v",
            "experiments/290_pll_phase_select/rtl/top.v",
            "experiments/300_pll_ref25/rtl/top.v",
            "experiments/300_pll_ref25/clocks.sdc",
            "experiments/310_pll_ref100/rtl/top.v",
            "experiments/310_pll_ref100/clocks.sdc",
            "experiments/320_pll_phase50/rtl/top.v",
            "experiments/330_pll_phase100/rtl/top.v",
            "experiments/340_pll_phase45/rtl/top.v",
            "experiments/350_pll_two/rtl/top.v",
            "experiments/360_pll_clkena/rtl/top.v",
            "experiments/370_pll_clkena_low/rtl/top.v",
            "experiments/380_pll_clkena_branch/rtl/top.v",
            "experiments/390_pll_clkena_status/rtl/top.v",
            "experiments/400_pll_clkena_reg2/rtl/top.v",
            "experiments/410_dsp_triple/rtl/top.v",
            "experiments/420_dsp_mul18/rtl/top.v",
            "experiments/430_dsp_mul27/rtl/top.v",
            "experiments/440_dsp_preadder/rtl/top.v",
            "experiments/450_dsp_mac/rtl/top.v",
            "experiments/460_dsp_reg/rtl/top.v",
            "experiments/470_mlab_init/rtl/top.v",
            "experiments/500_m10k_be20/rtl/top.v",
            "experiments/680_m10k_mix20be10/rtl/top.v",
            "experiments/700_m10k_aclr/rtl/top.v",
            "experiments/710_m10k_aclr_prim/rtl/top.v",
            "experiments/720_m10k_aclr_infer/rtl/top.v",
            "experiments/730_m10k_tdp_tclk/rtl/top.v",
            "experiments/740_m10k_dual_pll/rtl/top.v",
            "experiments/750_dsp18x19/rtl/top.v",
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
            "yosys": "fca8ca0a5354e52ce0e158bc6e1eed481e590ed8",
            "nextpnr": "9144784db9b4b85c1257be4d1943b9ac11ceb3e8",
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

    def test_print_commands_enables_block_memory_and_pll_for_m10k_tdp_mixed(self) -> None:
        for experiment in (
            "570_m10k_tdp_mix20_10",
            "580_m10k_tdp_mix10_20",
            "590_m10k_tdp_mix16_8",
            "600_m10k_tdp_mix8_16",
        ):
            result = self._run("--print-commands", "--experiment", experiment)
            self.assertEqual(result.returncode, 0, result.stderr)
            commands = result.stdout
            self.assertIn(f"experiments/{experiment}/rtl/top.v", commands)
            self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
            self.assertNotIn("-nobram", commands)
            self.assertNotIn("--router", commands)
            self.assertNotIn("hps_gp_model.v", commands)
            self.assertIn("--freq 50", commands)
            self.assertIn("--compress-rbf", commands)
            self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_and_pll_for_m10k_tdp_byte(self) -> None:
        for experiment in ("550_m10k_tdp_be20", "560_m10k_tdp_be16"):
            result = self._run("--print-commands", "--experiment", experiment)
            self.assertEqual(result.returncode, 0, result.stderr)
            commands = result.stdout
            self.assertIn(f"experiments/{experiment}/rtl/top.v", commands)
            self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
            self.assertNotIn("-nobram", commands)
            self.assertNotIn("--router", commands)
            self.assertNotIn("hps_gp_model.v", commands)
            self.assertIn("--freq 50", commands)
            self.assertIn("--compress-rbf", commands)
            self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_and_pll_for_m10k_tdp(self) -> None:
        for experiment in ("530_m10k_tdp10", "540_m10k_tdp20"):
            result = self._run("--print-commands", "--experiment", experiment)
            self.assertEqual(result.returncode, 0, result.stderr)
            commands = result.stdout
            self.assertIn(f"experiments/{experiment}/rtl/top.v", commands)
            self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
            self.assertNotIn("-nobram", commands)
            self.assertNotIn("--router", commands)
            self.assertNotIn("hps_gp_model.v", commands)
            self.assertIn("--freq 50", commands)
            self.assertIn("--compress-rbf", commands)
            self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_and_pll_for_m10k_byte_enable(self) -> None:
        result = self._run("--print-commands", "--experiment", "500_m10k_be20")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/500_m10k_be20/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_and_router1_for_mixed_byte_enable(self) -> None:
        result = self._run("--print-commands", "--experiment", "680_m10k_mix20be10")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/680_m10k_mix20be10/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("m10k_mixbe_model.v", commands)
        self.assertIn("--router router1", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_for_m10k_aclr(self) -> None:
        result = self._run("--print-commands", "--experiment", "700_m10k_aclr")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/700_m10k_aclr/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("--router", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_for_m10k_aclr_prim(self) -> None:
        result = self._run("--print-commands", "--experiment", "710_m10k_aclr_prim")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/710_m10k_aclr_prim/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("m10k_aclr_model.v", commands)
        self.assertNotIn("--router", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_for_m10k_aclr_infer(self) -> None:
        result = self._run("--print-commands", "--experiment", "720_m10k_aclr_infer")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/720_m10k_aclr_infer/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("--router", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_memory_for_m10k_tdp_tclk(self) -> None:
        result = self._run("--print-commands", "--experiment", "730_m10k_tdp_tclk")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/730_m10k_tdp_tclk/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("m10k_tdp_tclk_model.v", commands)
        self.assertNotIn("--router", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_uses_default_router_for_dual_pll_m10k(self) -> None:
        result = self._run("--print-commands", "--experiment", "740_m10k_dual_pll")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/740_m10k_dual_pll/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("--router", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
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
        for experiment in ("040_mlab_ram", "470_mlab_init"):
            result = self._run("--print-commands", "--experiment", experiment)
            self.assertEqual(result.returncode, 0, result.stderr)
            commands = result.stdout
            self.assertIn(f"experiments/{experiment}/rtl/top.v", commands)
            self.assertIn("synth_intel_alm -nobram -nodsp -top top", commands)
            self.assertNotIn("-nolutram", commands)
            self.assertNotIn("hps_gp_model.v", commands)
            self.assertIn("--freq 50", commands)
            self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_and_lab_memory_for_mixed_mem(self) -> None:
        result = self._run("--print-commands", "--experiment", "070_mixed_mem")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/070_mixed_mem/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nodsp -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("-nolutram", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_lab_and_dsp_for_dsp_mem(self) -> None:
        result = self._run("--print-commands", "--experiment", "080_dsp_mem")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/080_dsp_mem/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("-nolutram", commands)
        self.assertNotIn("-nodsp", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_block_and_dsp_for_dsp_rom(self) -> None:
        result = self._run("--print-commands", "--experiment", "100_dsp_rom")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/100_dsp_rom/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nolutram -top top", commands)
        self.assertNotIn("-nobram", commands)
        self.assertNotIn("-nodsp", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_dual(self) -> None:
        result = self._run("--print-commands", "--experiment", "170_pll_dual")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/170_pll_dual/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_frac(self) -> None:
        result = self._run("--print-commands", "--experiment", "180_pll_frac")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/180_pll_frac/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_frac_441(self) -> None:
        result = self._run("--print-commands", "--experiment", "190_pll_frac_441")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/190_pll_frac_441/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_frac_dual(self) -> None:
        result = self._run("--print-commands", "--experiment", "200_pll_frac_dual")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/200_pll_frac_dual/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_frac_7425(self) -> None:
        result = self._run("--print-commands", "--experiment", "610_pll_frac_7425")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/610_pll_frac_7425/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_ddr_clock(self) -> None:
        result = self._run("--print-commands", "--experiment", "620_ddr_clock")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/620_ddr_clock/rtl/top.v", commands)
        self.assertIn("experiments/620_ddr_clock/pins.qsf", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("ddr_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_sdr_output(self) -> None:
        result = self._run("--print-commands", "--experiment", "630_sdr_output")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/630_sdr_output/rtl/top.v", commands)
        self.assertIn("experiments/630_sdr_output/pins.qsf", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_sdr_input(self) -> None:
        result = self._run("--print-commands", "--experiment", "640_sdr_input")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/640_sdr_input/rtl/top.v", commands)
        self.assertIn("experiments/640_sdr_input/pins.qsf", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_ddr_input(self) -> None:
        result = self._run("--print-commands", "--experiment", "650_ddr_input")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/650_ddr_input/rtl/top.v", commands)
        self.assertIn("experiments/650_ddr_input/pins.qsf", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("altddio_in_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_ddr_data(self) -> None:
        result = self._run("--print-commands", "--experiment", "660_ddr_data")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/660_ddr_data/rtl/top.v", commands)
        self.assertIn("experiments/660_ddr_data/pins.qsf", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("altddio_out_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_ddr_bidir(self) -> None:
        result = self._run("--print-commands", "--experiment", "690_ddr_bidir")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/690_ddr_bidir/rtl/top.v", commands)
        self.assertIn("experiments/690_ddr_bidir/pins.qsf", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("altddio_bidir_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_altiobuf(self) -> None:
        result = self._run("--print-commands", "--experiment", "670_altiobuf")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/670_altiobuf/rtl/top.v", commands)
        self.assertIn("experiments/670_altiobuf/pins.qsf", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("altiobuf_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_duty(self) -> None:
        result = self._run("--print-commands", "--experiment", "210_pll_duty")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/210_pll_duty/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_phase(self) -> None:
        result = self._run("--print-commands", "--experiment", "220_pll_phase")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/220_pll_phase/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_phase_180(self) -> None:
        result = self._run("--print-commands", "--experiment", "230_pll_phase_180")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/230_pll_phase_180/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_phase_270(self) -> None:
        result = self._run("--print-commands", "--experiment", "240_pll_phase_270")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/240_pll_phase_270/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_triple(self) -> None:
        result = self._run("--print-commands", "--experiment", "250_pll_triple")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/250_pll_triple/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_quad(self) -> None:
        result = self._run("--print-commands", "--experiment", "260_pll_quad")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/260_pll_quad/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_multi_duty(self) -> None:
        result = self._run("--print-commands", "--experiment", "270_pll_multi_duty")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/270_pll_multi_duty/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_quadrature(self) -> None:
        result = self._run("--print-commands", "--experiment", "280_pll_quadrature")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/280_pll_quadrature/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_phase_select(self) -> None:
        result = self._run("--print-commands", "--experiment", "290_pll_phase_select")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/290_pll_phase_select/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_ref25(self) -> None:
        result = self._run("--print-commands", "--experiment", "300_pll_ref25")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/300_pll_ref25/rtl/top.v", commands)
        self.assertIn("experiments/300_pll_ref25/clocks.sdc", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 25", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_ref100(self) -> None:
        result = self._run("--print-commands", "--experiment", "310_pll_ref100")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/310_pll_ref100/rtl/top.v", commands)
        self.assertIn("experiments/310_pll_ref100/clocks.sdc", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 100", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_phase50(self) -> None:
        result = self._run("--print-commands", "--experiment", "320_pll_phase50")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/320_pll_phase50/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_new_pll_ladder(self) -> None:
        for experiment in (
            "330_pll_phase100",
            "340_pll_phase45",
            "350_pll_two",
            "360_pll_clkena",
            "370_pll_clkena_low",
            "380_pll_clkena_branch",
            "390_pll_clkena_status",
            "400_pll_clkena_reg2",
        ):
            with self.subTest(experiment=experiment):
                result = self._run("--print-commands", "--experiment", experiment)
                self.assertEqual(result.returncode, 0, result.stderr)
                commands = result.stdout
                self.assertIn(f"experiments/{experiment}/rtl/top.v", commands)
                self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
                self.assertNotIn("hps_gp_model.v", commands)
                self.assertNotIn("pll_model.v", commands)
                self.assertNotIn("clkena_model.v", commands)
                self.assertIn("--freq 50", commands)
                self.assertIn("--compress-rbf", commands)
                self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_keeps_memory_and_dsp_disabled_for_pll_clock(self) -> None:
        result = self._run("--print-commands", "--experiment", "090_pll_clock")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/090_pll_clock/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -nodsp -top top", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_pll_and_dsp_for_frequency_products(self) -> None:
        for experiment in (
            "130_pll_dsp_40",
            "140_pll_dsp_20",
            "150_pll_dsp_80",
            "160_pll_dsp_100",
        ):
            with self.subTest(experiment=experiment):
                result = self._run("--print-commands", "--experiment", experiment)
                self.assertEqual(result.returncode, 0, result.stderr)
                commands = result.stdout
                self.assertIn(f"experiments/{experiment}/rtl/top.v", commands)
                self.assertIn("synth_intel_alm -nobram -nolutram -top top", commands)
                self.assertNotIn("-nodsp", commands)
                self.assertNotIn("hps_gp_model.v", commands)
                self.assertNotIn("pll_model.v", commands)
                self.assertIn("--freq 50", commands)
                self.assertIn("--compress-rbf", commands)
                self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_pll_and_dsp_for_pll_dsp(self) -> None:
        result = self._run("--print-commands", "--experiment", "120_pll_dsp")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/120_pll_dsp/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -top top", commands)
        self.assertNotIn("-nodsp", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertNotIn("pll_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_dsp_only_for_dsp_mul(self) -> None:
        result = self._run("--print-commands", "--experiment", "060_dsp_mul")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/060_dsp_mul/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -top top", commands)
        self.assertNotIn("-nodsp", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_dsp_only_for_dsp_triple(self) -> None:
        result = self._run("--print-commands", "--experiment", "410_dsp_triple")
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = result.stdout
        self.assertIn("experiments/410_dsp_triple/rtl/top.v", commands)
        self.assertIn("synth_intel_alm -nobram -nolutram -top top", commands)
        self.assertIn("setattr -mod -unset keep_hierarchy packed_product; flatten;", commands)
        self.assertNotIn("-nodsp", commands)
        self.assertNotIn("hps_gp_model.v", commands)
        self.assertIn("--freq 50", commands)
        self.assertIn("--compress-rbf", commands)
        self.assertFalse(self.marker.exists(), "print mode must not invoke a tool")

    def test_print_commands_enables_dsp_modes(self) -> None:
        for experiment, extra in (
            ("420_dsp_mul18", None),
            ("430_dsp_mul27", None),
            ("440_dsp_preadder", "setparam -set PREADDER_EN 1"),
            ("450_dsp_mac", "chtype -set MISTRAL_MUL18X18 t:dsp18_mac"),
            ("460_dsp_reg", "setparam -set INREG_CTRL_AX 1"),
            ("750_dsp18x19", "chtype -set MISTRAL_MUL18X19 t:dsp18x19"),
        ):
            result = self._run("--print-commands", "--experiment", experiment)
            self.assertEqual(result.returncode, 0, result.stderr)
            commands = result.stdout
            self.assertIn(f"experiments/{experiment}/rtl/top.v", commands)
            self.assertIn("synth_intel_alm -nobram -nolutram -top top", commands)
            self.assertNotIn("-nodsp", commands)
            self.assertNotIn("hps_gp_model.v", commands)
            if extra is not None:
                self.assertIn(extra, commands)
            self.assertIn("--freq 50", commands)
            self.assertIn("--compress-rbf", commands)
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
