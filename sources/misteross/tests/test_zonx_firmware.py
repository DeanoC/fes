# SPDX-License-Identifier: GPL-2.0-or-later
"""Execute the actual open Zon X diagnostic through the ZX81 CPU/socket."""
import importlib.util
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-zx81"


class ZonXFirmwareSimulationTest(unittest.TestCase):
    def test_cpu_audio_diagnostic(self):
        verilator = shutil.which(os.environ.get("VERILATOR", "verilator"))
        if not verilator:
            self.skipTest("Verilator is required for CPU audio simulation")
        spec = importlib.util.spec_from_file_location("zonx_rom", ROOT / "scripts" / "make_zx81_zonx_tone_rom.py")
        rom = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(rom)
        with tempfile.TemporaryDirectory(prefix="fes-zonx-firmware-") as directory:
            directory = Path(directory)
            firmware = directory / "diagnostic.hex"
            firmware.write_text("".join(f"{byte:02x}\n" for byte in rom.make_rom(fast=True)))
            sources = [CORE / "sim" / "expansion_machine.v",
                       CORE / "expansions" / "zonx.v", CORE / "expansions" / "zonx_ay.v",
                       ROOT / "experiments" / "901_plugged_base" / "sim" / "mistral_ff_model.v"]
            sources += [CORE / "rtl" / name for name in
                        ("zx81_machine_clock.v", "zx81_machine.sv", "t80pa.v", "zx81_dpram.v",
                         "zx81_expansion_socket.v", "zx81_ram_pack.v", "tv80/tv80_core.v",
                         "tv80/tv80_alu.v", "tv80/tv80_mcode.v", "tv80/tv80_reg.v")]
            warnings = ("UNUSEDSIGNAL", "UNOPTFLAT", "CASEINCOMPLETE", "WIDTHTRUNC", "WIDTHEXPAND",
                        "SYNCASYNCNET", "PINCONNECTEMPTY", "DECLFILENAME", "IMPLICITSTATIC",
                        "VARHIDDEN", "UNUSEDPARAM", "CASEX", "BLKSEQ")
            command = [verilator, "--cc", "--exe", "--build", "--top-module", "expansion_machine",
                       "-GCART_PRESENT=1", f'-GFIRMWARE_INIT="{firmware}"', "-DTV80_REFRESH=1", "-Wall",
                       *[f"-Wno-{warning}" for warning in warnings],
                       f"-I{CORE / 'rtl'}", f"-I{CORE / 'rtl' / 'tv80'}",
                       "--Mdir", str(directory / "build"), *map(str,sources),
                       str(CORE / "sim" / "zonx_firmware_tb.cpp")]
            built = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(built.returncode,0,built.stdout+built.stderr)
            result = subprocess.run([str(directory / "build" / "Vexpansion_machine")],
                                    capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            self.assertIn("Zon X CPU firmware:",result.stdout)


if __name__ == "__main__":
    unittest.main()
