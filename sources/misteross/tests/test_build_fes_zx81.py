from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import build_fes_zx81
from scripts.build_fes_zx81 import (
    BuildError,
    compile_command,
    create_build_record,
    project_qsf,
    require_clean_source,
    require_clocks,
    validate_timing_report,
)
from scripts.export_core_package import build_identity
from tests.quartus_timing_fixture import assert_timing_gate


ROOT = Path(__file__).resolve().parents[1]


class BuildFesZx81Tests(unittest.TestCase):
    def test_make_entrypoint_uses_the_quartus_recipe(self) -> None:
        result = subprocess.run(
            ["make", "-n", "build-fes-zx81-quartus"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            f'python3 scripts/build_fes_zx81.py --root "{ROOT}"',
        )

    def test_make_entrypoint_passes_a_supplied_rom(self) -> None:
        result = subprocess.run(
            ["make", "-n", "build-fes-zx81-quartus", "FES_ZX81_ROM=zx81.rom"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            f'python3 scripts/build_fes_zx81.py --root "{ROOT}" --rom "zx81.rom"',
        )

    def test_missing_rom_make_entrypoint_reports_the_required_variable(self) -> None:
        env = os.environ.copy()
        env.pop("FES_ZX81_ROM", None)
        result = subprocess.run(
            ["make", "build-fes-zx81-quartus"],
            cwd=ROOT,
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        output = result.stdout + result.stderr
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("set FES_ZX81_ROM", output)
        self.assertNotIn("Is a directory", output)

    def test_zx81_smoke_runs_machine_variants_without_a_user_rom(self) -> None:
        env = os.environ.copy()
        env.pop("FES_ZX81_ROM", None)
        result = subprocess.run(
            ["make", "-n", "sim-fes-zx81-smoke"],
            cwd=ROOT,
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("SKIP sim-fes-zx81-rom", result.stdout)
        self.assertIn("SKIP sim-fes-zx81-machine", result.stdout)
        self.assertIn("variant_probe.hex", result.stdout)
        self.assertIn("zx80_load_probe.hex", result.stdout)
        self.assertIn('zx80:-DZX81_MODEL_ZX80', result.stdout)

    def test_rom_benches_generate_and_use_hex_or_skip_without_rom(self) -> None:
        targets = (
            "sim-fes-zx81-rom",
            "sim-fes-zx81-machine",
            "sim-fes-zx81-save",
        )
        env = os.environ.copy()
        env.pop("FES_ZX81_ROM", None)
        skipped = subprocess.run(
            ["make", "-n", *targets],
            cwd=ROOT,
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(skipped.returncode, 0, skipped.stderr)
        for target in targets:
            with self.subTest(target=target):
                self.assertIn(f"SKIP {target}", skipped.stdout)

        rom = subprocess.run(
            ["make", "-n", *targets, "FES_ZX81_ROM=zx81.rom"],
            cwd=ROOT,
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(rom.returncode, 0, rom.stderr)
        hex_path = str(ROOT / "build/sim/zx81-rom/zx81.hex")
        self.assertIn(
            f'python3 scripts/zx81_rom.py --rom "zx81.rom" --hex "{hex_path}"',
            rom.stdout,
        )
        self.assertIn(f"-GROM_INIT='\"{hex_path}\"'", rom.stdout)
        self.assertEqual(rom.stdout.count(f"-GFIRMWARE_INIT='\"{hex_path}\"'"), 2)
        self.assertIn("cores/fes-zx81/sim/rom_init_tb.cpp", rom.stdout)
        self.assertIn("cores/fes-zx81/sim/machine_tb.cpp", rom.stdout)
        self.assertIn("cores/fes-zx81/sim/save_tb.cpp", rom.stdout)

    def test_project_pins_nmos_z80_dual_pll_and_hdmi_i2c_site(self) -> None:
        qsf = project_qsf(ROOT, ROOT / "build/fes-zx81-quartus/project", "00112233445566778899aabbccddeeff")
        self.assertIn("fes_z80_nmos.sv", qsf)
        self.assertIn("fes_z80_alu.sv", qsf)
        self.assertIn("fes_z80_engine.sv", qsf)
        self.assertIn("fes_z80_bus.sv", qsf)
        self.assertNotIn("T80pa.vhd", qsf)
        self.assertNotIn("VHDL_FILE", qsf)
        self.assertIn("zx81_machine.sv", qsf)
        self.assertIn("sys_pll.v", qsf)
        self.assertIn("pixel_pll.v", qsf)
        self.assertNotIn("fes_audio_pll.v", qsf)
        self.assertIn("fes_audio_output.v", qsf)
        self.assertIn("fes_audio_i2s.v", qsf)
        self.assertIn("fes_computer_gp.v", qsf)
        self.assertIn('VERILOG_MACRO "QUARTUS=1"', qsf)
        self.assertIn('set_parameter -name FIRMWARE_INIT "zx81.mif"', qsf)
        self.assertIn("scripts/zx81_rom.py", build_fes_zx81.PINNED_INPUTS)
        self.assertFalse(any("zx8x" in path for path in build_fes_zx81.PINNED_INPUTS))
        self.assertNotIn("tv80", qsf)
        self.assertNotIn("t80pa.v", qsf)
        self.assertIn("128'h00112233445566778899aabbccddeeff", qsf)
        self.assertIn("HPSINTERFACEPERIPHERALI2C_X52_Y60_N111", qsf)
        self.assertIn("PIN_U10 -to HDMI_I2C_SCL", qsf)
        self.assertIn("PIN_AA4 -to HDMI_I2C_SDA", qsf)
        top = (ROOT / "cores/fes-zx81/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn(".out_clk(hdmi_scl_low)", top)
        self.assertIn(".out_data(hdmi_sda_low)", top)
        self.assertIn("hdmi_scl_low ? 1'b0 : 1'bz", top)
        self.assertIn("hdmi_sda_low ? 1'b0 : 1'bz", top)
        self.assertNotIn(".out_clk()", top)

    def test_compile_command_is_quartus_flow(self) -> None:
        command = compile_command(Path("/opt/quartus/bin/quartus_sh"))
        self.assertEqual(
            command,
            ("/opt/quartus/bin/quartus_sh", "--flow", "compile", "top"),
        )

    def test_create_build_record_names_quartus_and_both_clocks(self) -> None:
        record = create_build_record(
            ROOT,
            "https://github.com/DeanoC/misteross.git",
            "0123456789abcdef0123456789abcdef01234567",
            {"quartus_sh": "version=17.0.2; sha256=" + ("ab" * 32)},
        )
        self.assertTrue(record.endswith(b"\n"))
        identity = build_identity(record)
        self.assertEqual(len(identity), 32)
        text = record.decode("utf-8")
        self.assertIn('"compiler":"quartus-17.0.2"', text)
        self.assertIn('"sys_clock_hz":52224000', text)
        self.assertIn('"pixel_clock_hz":74250000', text)
        self.assertIn('"audio_clock_hz":12288000', text)
        self.assertIn("scripts/build_fes_zx81.py", text)
        self.assertIn("fes_simple_computer.vh", text)

    def test_rom_is_user_supplied(self) -> None:
        with self.assertRaisesRegex(BuildError, "FES_ZX81_ROM"):
            build_fes_zx81.build(ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            wrong = Path(temporary) / "zx81.rom"
            wrong.write_bytes(bytes(8192))
            with self.assertRaisesRegex(ValueError, "edition 3"):
                build_fes_zx81.build(ROOT, rom=wrong)

    def test_missing_quartus_is_a_supported_failure(self) -> None:
        env = os.environ.copy()
        env.pop("QUARTUS_ROOTDIR", None)
        with patch.dict(os.environ, env, clear=True):
            with self.assertRaises(BuildError) as raised:
                build_fes_zx81.authenticate_quartus(ROOT)
        self.assertIn("QUARTUS_ROOTDIR", str(raised.exception))

    def test_dirty_tree_is_rejected(self) -> None:
        # Dirty tracked module must fail before compiler discovery.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for arguments in (("init", "-q"), ("config", "user.name", "Test"),
                              ("config", "user.email", "test@example.invalid"),
                              ("commit", "--allow-empty", "-qm", "fixture")):
                subprocess.run(["git", "-C", str(root), *arguments], check=True,
                               capture_output=True)
            module = root / "sources/misteross"
            module.mkdir(parents=True)
            (module / "tracked.v").write_text("// fixture\n")
            subprocess.run(["git", "-C", str(root), "add", "."], check=True)
            subprocess.run(["git", "-C", str(root), "commit", "-qm", "module"], check=True, capture_output=True)
            (root / "untracked.v").write_text("// dirty input\n")
            with self.assertRaisesRegex(BuildError, "clean"):
                require_clean_source(module)

    def test_clocks_must_appear_in_timing_text(self) -> None:
        require_clocks("Fmax 52.224 MHz and 74.25 MHz and 12.288 MHz")
        require_clocks("52.224 MHz and 74.27 MHz and 12.288 MHz")
        require_clocks("TimeQuest derived 52.24 MHz, 74.27 MHz and 12.29 MHz")
        with self.assertRaises(BuildError):
            require_clocks("Fmax 52.224 MHz and 74.25 MHz only")
        with self.assertRaises(BuildError):
            require_clocks("Fmax 74.25 MHz only")
        with self.assertRaises(BuildError):
            require_clocks("Fmax 52.224 MHz only")

    def test_quartus_timing_gate_requires_every_clock_analysed(self) -> None:
        assert_timing_gate(self, validate_timing_report, BuildError)
        with self.assertRaises(BuildError):
            validate_timing_report("no summary")


if __name__ == "__main__":
    unittest.main()
