import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MAKEFILE = ROOT / "Makefile"
QSF = ROOT / "boards" / "de10nano" / "pins.qsf"
SDC = ROOT / "boards" / "de10nano" / "clocks.sdc"
BOARD_README = ROOT / "boards" / "de10nano" / "README.md"
RTL = ROOT / "experiments" / "010_blinky" / "rtl" / "top.v"


class BlinkySourcePolicyTests(unittest.TestCase):
    def test_qsf_has_only_authoritative_clock_and_led_locations(self):
        self.assertTrue(QSF.is_file(), QSF)
        locations = re.findall(
            r"(?m)^\s*set_location_assignment\s+(\S+)\s+-to\s+(\S+)\s*$",
            QSF.read_text(encoding="utf-8"),
        )
        self.assertEqual(
            locations,
            [("PIN_V11", "FPGA_CLK1_50"), ("PIN_W15", "LED[0]")],
        )

    def test_qsf_declares_lvttl_for_both_ports(self):
        self.assertTrue(QSF.is_file(), QSF)
        standards = re.findall(
            r'(?m)^\s*set_instance_assignment\s+-name\s+IO_STANDARD\s+"([^"]+)"\s+-to\s+(\S+)\s*$',
            QSF.read_text(encoding="utf-8"),
        )
        self.assertEqual(
            standards,
            [("3.3-V LVTTL", "FPGA_CLK1_50"), ("3.3-V LVTTL", "LED[0]")],
        )

    def test_sdc_declares_a_twenty_nanosecond_input_clock(self):
        self.assertTrue(SDC.is_file(), SDC)
        sdc = SDC.read_text(encoding="utf-8")
        self.assertRegex(
            sdc,
            r"(?m)^\s*create_clock\s+-name\s+FPGA_CLK1_50\s+-period\s+20\.000\s+\[get_ports\s+\{FPGA_CLK1_50\}\]\s*$",
        )

    def test_rtl_exposes_only_a_counter_clock_and_led(self):
        self.assertTrue(RTL.is_file(), RTL)
        rtl = RTL.read_text(encoding="utf-8")
        self.assertRegex(rtl, r"module\s+top\s*#\s*\(")
        self.assertRegex(rtl, r"parameter\s+integer\s+COUNTER_BITS\s*=\s*25")
        self.assertRegex(rtl, r"input\s+wire\s+FPGA_CLK1_50")
        self.assertRegex(rtl, r"output\s+wire\s+\[0:0\]\s+LED")
        self.assertRegex(rtl, r"always\s*@\s*\(posedge\s+FPGA_CLK1_50\)")
        self.assertRegex(rtl, r"LED\s*\[0\]")
        for forbidden in (
            "PLL",
            "RAM",
            "BRAM",
            "LUTRAM",
            "HPS",
            "DSP",
            "ALTPLL",
            "altsyncram",
            "M10K",
            "cyclonev",
        ):
            self.assertNotRegex(
                rtl,
                re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                forbidden,
            )

    def test_sim_authenticates_verilator_before_lint(self):
        makefile = MAKEFILE.read_text(encoding="utf-8")
        auth = re.search(
            r"doctor\.py[\"']?\s+--check-tool\s+verilator",
            makefile,
        )
        self.assertIsNotNone(auth)
        self.assertLess(
            auth.start(),
            makefile.index("--lint-only"),
        )

    def test_board_readme_cites_both_pin_sources(self):
        self.assertTrue(BOARD_README.is_file(), BOARD_README)
        readme = BOARD_README.read_text(encoding="utf-8")
        self.assertIn(
            "https://www.terasic.com.tw/cgi-bin/page/archive.pl?Language=English&No=1046",
            readme,
        )
        self.assertIn(
            "https://github.com/MiSTer-devel/MemTest_MiSTer/blob/master/sys/sys.tcl",
            readme,
        )


if __name__ == "__main__":
    unittest.main()
