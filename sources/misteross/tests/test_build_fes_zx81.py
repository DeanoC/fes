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

    def test_project_pins_vhdl_t80_dual_pll_and_hdmi_i2c_site(self) -> None:
        qsf = project_qsf(ROOT, ROOT / "build/fes-zx81-quartus/project", "00112233445566778899aabbccddeeff")
        self.assertIn("T80pa.vhd", qsf)
        self.assertIn("zx81_machine.sv", qsf)
        self.assertIn("sys_pll.v", qsf)
        self.assertIn("pixel_pll.v", qsf)
        self.assertIn("fes_computer_gp.v", qsf)
        self.assertIn('VERILOG_MACRO "QUARTUS=1"', qsf)
        self.assertIn(build_fes_zx81.ROM_MIF, build_fes_zx81.PINNED_INPUTS)
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
        self.assertIn('"sys_clock_hz":52000000', text)
        self.assertIn('"pixel_clock_hz":74250000', text)
        self.assertIn("scripts/build_fes_zx81.py", text)
        self.assertIn("fes_simple_computer.vh", text)

    def test_missing_quartus_is_a_supported_failure(self) -> None:
        env = os.environ.copy()
        env.pop("QUARTUS_ROOTDIR", None)
        with patch.dict(os.environ, env, clear=True):
            with self.assertRaises(BuildError) as raised:
                build_fes_zx81.authenticate_quartus(ROOT)
        self.assertIn("QUARTUS_ROOTDIR", str(raised.exception))

    def test_dirty_tree_is_rejected(self) -> None:
        # This legacy producer requires a standalone checkout; isolate the
        # dirty-tree check from the monorepo's module-root rejection.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for arguments in (("init", "-q"), ("config", "user.name", "Test"),
                              ("config", "user.email", "test@example.invalid"),
                              ("commit", "--allow-empty", "-qm", "fixture")):
                subprocess.run(["git", "-C", str(root), *arguments], check=True,
                               capture_output=True)
            (root / "untracked.v").write_text("// dirty input\n")
            with self.assertRaisesRegex(BuildError, "clean"):
                require_clean_source(root)

    def test_clocks_must_appear_in_timing_text(self) -> None:
        require_clocks("Fmax 52.00 MHz and 74.25 MHz")
        require_clocks("52.0 MHz and 74.27 MHz")
        with self.assertRaises(BuildError):
            require_clocks("Fmax 74.25 MHz only")
        with self.assertRaises(BuildError):
            require_clocks("Fmax 52.00 MHz only")

    def test_multicorner_slack_must_be_nonnegative(self) -> None:
        report = (
            "Worst-case Slack                                            ; 3.093 ; 0.161 ; 12.713   ; 1.115   ; 0.961               ;\n"
            "Design-wide TNS                                             ; 0.0   ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;\n"
        )
        timing = validate_timing_report(report)
        self.assertEqual(len(timing), 5)
        self.assertEqual(timing[0]["type"], "Setup")
        self.assertEqual(timing[0]["slack_ns"], 3.093)
        with self.assertRaises(BuildError):
            validate_timing_report(report.replace("3.093", "-0.1", 1))
        with self.assertRaises(BuildError):
            validate_timing_report("no summary")


if __name__ == "__main__":
    unittest.main()
