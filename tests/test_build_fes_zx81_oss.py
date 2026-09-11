from __future__ import annotations

import subprocess
import unittest
from pathlib import Path
from scripts.build_fes_zx81_oss import (
    OUTPUT_RELATIVE,
    RTL_SOURCES,
    BuildError,
    _manifest,
    build_commands,
)


ROOT = Path(__file__).resolve().parents[1]


class BuildFesZx81OssTests(unittest.TestCase):
    def test_make_entrypoint_uses_the_oss_recipe(self) -> None:
        result = subprocess.run(
            ["make", "-n", "build-fes-zx81"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout.strip(),
            f'python3 scripts/build_fes_zx81_oss.py --root "{ROOT}"',
        )
        phony = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertRegex(phony, r"\.PHONY:.*\bbuild-fes-zx81\b")

    def test_oss_commands_use_tv80_m10k_and_not_vhdl_t80(self) -> None:
        yosys, nextpnr = build_commands(
            ROOT,
            ROOT / OUTPUT_RELATIVE,
            "00112233445566778899aabbccddeeff",
            {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
        )
        program = yosys[2]
        self.assertIn("tv80_core.v", program)
        self.assertIn("t80pa.v", program)
        self.assertNotIn("-DFES_ZX81_OSS=1", program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", program)
        self.assertNotIn("-nobram", program)
        self.assertNotIn("T80pa.vhd", program)
        self.assertIn("cores/fes-zx81/rtl/top.v", program)
        self.assertIn("--freq", nextpnr)
        self.assertIn("74.25", nextpnr)
        self.assertIn("--seed", nextpnr)
        self.assertIn("2", nextpnr)
        self.assertIn("--placer-heap-timingweight", nextpnr)
        self.assertIn("300", nextpnr)
        self.assertIn("--placer-heap-critexp", nextpnr)
        self.assertIn("5", nextpnr)
        self.assertIn("router1", nextpnr)
        self.assertIn("--timing-allow-fail", nextpnr)
        self.assertNotIn("--tmg-ripup", nextpnr)
        joined = " ".join(nextpnr)
        self.assertIn("cores/fes-zx81/constraints-oss.qsf", joined)
        self.assertIn("cores/fes-zx81/clocks-oss.sdc", joined)
        self.assertNotIn("cores/fes-zx81/clocks.sdc", joined)
        self.assertNotIn("cores/fes-zx81/constraints.qsf ", joined)

    def test_oss_top_uses_mistral_io_without_quartus(self) -> None:
        top = (ROOT / "cores/fes-zx81/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("MISTRAL_IO hdmi_scl_pad", top)
        self.assertIn('BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0"', top)
        self.assertIn("`ifdef QUARTUS", top)
        self.assertIn("hdmi_scl_low ? 1'b0 : 1'bz", top)
        dpram = (ROOT / "cores/fes-zx81/rtl/zx81_dpram.v").read_text(encoding="utf-8")
        self.assertNotIn("FES_ZX81_OSS", dpram)
        self.assertIn('ramstyle = "M10K"', dpram)
        self.assertIn("assign q_a = ram[address_a]", dpram)
        sys_pll = (ROOT / "cores/fes-zx81/rtl/sys_pll.v").read_text(encoding="utf-8")
        self.assertIn('.output_clock_frequency0("52.0 MHz")', sys_pll)
        self.assertNotIn('.output_clock_frequency0("50.0 MHz")', sys_pll)

    def test_oss_rejects_wrong_output_directory(self) -> None:
        with self.assertRaises(BuildError):
            build_commands(
                ROOT,
                ROOT / "build/other",
                "00112233445566778899aabbccddeeff",
                {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
            )

    def test_oss_manifest_carries_source_identity(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_zx81_oss.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{}}'
        )
        evidence = {
            "build_id": "d" * 32,
            "rbf": {"size": 16, "sha256": "e" * 64},
        }
        tools = {
            "mistral": "commit=" + "f" * 40 + "; sha256=" + "1" * 64,
            "nextpnr-mistral": "commit=" + "2" * 40 + "; sha256=" + "3" * 64,
            "yosys": "commit=" + "4" * 40 + "; sha256=" + "5" * 64,
        }
        manifest = _manifest(
            record,
            evidence,
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            tools,
        )
        self.assertIn(b"repository = ", manifest)
        self.assertIn(b"revision = \"" + (b"a" * 40) + b"\"", manifest)
        self.assertNotIn(b"recipe = ", manifest)
