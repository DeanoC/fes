# SPDX-License-Identifier: MIT
"""The pc386 Quartus seal's sources, machine netlist, project, timing gate and
manifest. No Quartus or Yosys needed."""
import json
from pathlib import Path
import tempfile
import tomllib
import unittest

from scripts import build_fes_pc386_oss as oss
from scripts import build_fes_pc386_quartus as quartus
from scripts import fes_build_common as board
from scripts.export_core_package import build_identity

ROOT = Path(__file__).resolve().parents[1]
BUILD_ID = "0123456789abcdef0123456789abcdef"
IDENTITIES = {"quartus_sh": "version=17.0.2; sha256=" + "1" * 64, "yosys": "commit=1f7d0273; sha256=" + "2" * 64}


# TimeQuest's clock table and multicorner summary, as Quartus 17.0.2 writes
# them for the pc386 shell (seed 1 of the M4 shell, which missed setup).
CLOCK_TABLE = """\
; Clock Name                                                 ; Type      ; Period ; Frequency  ; Rise  ; Fall   ;
; FPGA_CLK1_50                                               ; Base      ; 20.000 ; 50.0 MHz   ; 0.000 ; 10.000 ;
; system_clock|pll|general[0].gpll~FRACTIONAL_PLL|vcoph[0]   ; Generated ; 2.325  ; 430.11 MHz ; 0.000 ; 1.162  ;
; system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; Generated ; 16.276 ; 61.44 MHz  ; 0.000 ; 8.138  ;
; system_clock|pll|general[1].gpll~PLL_OUTPUT_COUNTER|divclk ; Generated ; 81.380 ; 12.29 MHz  ; 0.000 ; 40.690 ;
; video_clock|pll|general[0].gpll~FRACTIONAL_PLL|vcoph[0]    ; Generated ; 2.244  ; 445.63 MHz ; 0.000 ; 1.122  ;
; video_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk  ; Generated ; 13.464 ; 74.27 MHz  ; 0.000 ; 6.732  ;
"""
MISSED = """\
; Worst-case Slack                                            ; -2.454  ; 0.107 ; 7.297    ; 0.394   ; 1.122               ;
; Design-wide TNS                                             ; -49.849 ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;
"""
MET = """\
; Worst-case Slack                                            ; 0.212   ; 0.107 ; 7.297    ; 0.394   ; 1.122               ;
; Design-wide TNS                                             ; 0.0     ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;
"""


class SourcesTest(unittest.TestCase):
    def test_shell_and_machine_split_the_producers_sources(self):
        self.assertTrue(set(quartus.SHELL_SOURCES) <= set(oss.RTL_SOURCES))
        self.assertFalse(set(quartus.SHELL_SOURCES) & set(quartus.MACHINE_SOURCES))
        self.assertEqual(set(quartus.SHELL_SOURCES) | set(quartus.MACHINE_SOURCES), set(oss.RTL_SOURCES))
        self.assertIn("cores/fes-pc386/rtl/pc386_diag.sv", quartus.MACHINE_SOURCES)

    def test_pinned_inputs_exist(self):
        for path in quartus.PINNED_INPUTS:
            self.assertTrue((ROOT / path).is_file(), path)

    def test_machine_program_elaborates_at_the_sealed_rates(self):
        program = quartus.machine_program(Path("out/pc386_diag.v"))
        for name, value in oss.guest_rates().items():
            self.assertRegex(program, rf"-chparam {name} {value}[ ;]")
        self.assertIn(f"hierarchy -top {quartus.MACHINE_TOP} ", program)
        self.assertIn("memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d;", program)
        self.assertTrue(program.endswith("write_verilog out/pc386_diag.v"))
        for source in quartus.SHELL_SOURCES:
            self.assertNotIn(source, program)


class IdentifiersTest(unittest.TestCase):
    def test_hierarchy_and_index_names_become_plain_and_unique(self):
        netlist = (
            "  wire a_b_0_ ;\n"
            "  reg [7:0] \\a.b[0] ;\n"
            "  reg [7:0] \\machine.vga.palette [15:0];\n"
            '  (* enum_value_0001 = "\\\\ATTACK" *)\n'
            "  (* \\always_ff  = 32'd1 *)\n"
            "  assign x = \\a.b[0] [3] ^ \\machine.vga.palette [1][0];\n"
        )
        plain = quartus.plain_identifiers(netlist)
        self.assertIn("  reg [7:0] a_b_0__1 ;\n", plain)
        self.assertIn("  reg [7:0] machine_vga_palette [15:0];\n", plain)
        self.assertIn("assign x = a_b_0__1 [3] ^ machine_vga_palette [1][0];", plain)
        self.assertIn('"\\\\ATTACK"', plain)
        self.assertIn("(* \\always_ff  = 32'd1 *)", plain)

    def test_a_rewritten_name_never_spells_a_keyword(self):
        self.assertEqual(quartus.plain_identifiers("wire \\always.ff ;"), "wire always_ff_1 ;")


class ProjectTest(unittest.TestCase):
    def test_project_reads_the_netlist_and_the_shell(self):
        with tempfile.TemporaryDirectory() as temporary:
            project = Path(temporary) / "project"
            project.mkdir()
            text = quartus.project_qsf(ROOT, project, BUILD_ID, 3)
        self.assertIn('set_global_assignment -name VERILOG_MACRO "QUARTUS=1"', text)
        self.assertIn(f"FES_PC386_BUILD_ID=128'h{BUILD_ID}", text)
        self.assertIn("set_global_assignment -name SEED 3", text)
        self.assertIn(f"HPS_LOCATION {quartus.I2C_SITE} -entity top -to hdmi_i2c", text)
        self.assertIn(f"set_global_assignment -name VERILOG_FILE {quartus.MACHINE_NETLIST}", text)
        self.assertIn('SYSTEMVERILOG_FILE "', text)
        self.assertIn("cores/fes-pc386/rtl/top.sv", text)
        self.assertIn(quartus.SDC, text)
        self.assertIn("set_location_assignment PIN_V11 -to FPGA_CLK1_50", text)
        for source in quartus.MACHINE_SOURCES:
            self.assertNotIn(source, text)

    def test_project_rejects_a_bad_build_id_and_seed(self):
        with self.assertRaises(board.BuildError):
            quartus.project_qsf(ROOT, ROOT, "XYZ", 1)
        with self.assertRaises(board.BuildError):
            quartus.project_qsf(ROOT, ROOT, BUILD_ID, 0)


class TimingTest(unittest.TestCase):
    def test_a_met_report_passes_with_each_clock(self):
        timing = quartus.validate_timing_report(CLOCK_TABLE + MET)
        self.assertEqual(timing["analyses"]["setup"], {"worst_slack_ns": 0.212, "tns_ns": 0.0})
        self.assertEqual({label: clock["mhz"] for label, clock in timing["clocks"].items()},
                         {"system": 61.44, "audio": 12.29, "pixel": 74.27})

    def test_a_setup_miss_fails(self):
        with self.assertRaisesRegex(board.BuildError, "Setup timing fails"):
            quartus.validate_timing_report(CLOCK_TABLE + MISSED)

    def test_a_wrong_or_missing_clock_fails(self):
        with self.assertRaisesRegex(board.BuildError, "system clock is 54.0 MHz"):
            quartus.validate_timing_report(CLOCK_TABLE.replace("16.276 ; 61.44 MHz", "18.519 ; 54.0 MHz") + MET)
        with self.assertRaisesRegex(board.BuildError, "pixel clock"):
            quartus.validate_timing_report(CLOCK_TABLE.replace("video_clock", "other_clock") + MET)
        with self.assertRaisesRegex(board.BuildError, "worst-case slack"):
            quartus.validate_timing_report(CLOCK_TABLE)


class ManifestTest(unittest.TestCase):
    def test_manifest_is_the_oss_package_labelled_quartus(self):
        record = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 1)
        evidence = {"rbf": {"sha256": "3" * 64, "size": 4096}}
        fields = tomllib.loads(quartus.manifest(record, evidence, "https://github.com/DeanoC/fes", "a" * 40,
                                                IDENTITIES).decode())
        self.assertEqual(fields["core"]["id"], oss.CORE["id"])
        self.assertEqual(fields["core"]["name"], "FES pc386 M4 diagnostic (Quartus)")
        self.assertEqual(fields["abi"], {"id": "fes.computer", "major": 1, "minor": 0})
        self.assertEqual([item["id"] for item in fields["interfaces"]], list(oss.INTERFACES))
        self.assertEqual(fields["build"]["id"], build_identity(record))
        self.assertIn("Quartus Prime Lite 17.0.2", fields["build"]["toolchain"])
        self.assertIn("Yosys", fields["build"]["toolchain"])
        parameters = json.loads(record)["parameters"]
        self.assertEqual(parameters["compiler"], "quartus-17.0.2")
        self.assertEqual(json.loads(parameters["guest_rates"]), oss.guest_rates())

    def test_the_seed_is_part_of_the_build_identity(self):
        first = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 1)
        second = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 2)
        self.assertNotEqual(build_identity(first), build_identity(second))


if __name__ == "__main__":
    unittest.main()
