# SPDX-License-Identifier: MIT
"""The pc386 Quartus seal's sources, guest rates, machine netlist, project,
timing gate, resources and manifest. No Quartus or Yosys needed."""
import json
from pathlib import Path
import re
import tempfile
import tomllib
import unittest

from scripts import build_fes_pc386_quartus as quartus
from scripts import fes_build_common as board
from scripts.export_core_package import build_identity

ROOT = Path(__file__).resolve().parents[1]
BUILD_ID = "0123456789abcdef0123456789abcdef"
IDENTITIES = {"quartus_sh": "version=17.0.2; sha256=" + "1" * 64, "yosys": "commit=1f7d0273; sha256=" + "2" * 64}


# TimeQuest's clock table and multicorner summary, as Quartus 17.0.2 writes
# them for the pc386 shell (seed 1 of the M4 shell, which missed setup), and
# the Fitter Summary's resource rows.
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
FIT_SUMMARY = """\
; Logic utilization (in ALMs)     ; 9,754 / 41,910 ( 23 % )                     ;
; Total registers                 ; 12637                                       ;
; Total pins                      ; 33 / 314 ( 11 % )                           ;
; Total block memory bits         ; 151,955 / 5,662,720 ( 3 % )                 ;
; Total RAM Blocks                ; 53 / 553 ( 10 % )                           ;
; Total DSP Blocks                ; 21 / 112 ( 19 % )                           ;
"""


class SourcesTest(unittest.TestCase):
    def test_shell_and_machine_split_the_sources(self):
        self.assertFalse(set(quartus.SHELL_SOURCES) & set(quartus.MACHINE_SOURCES))
        self.assertEqual(len(set(quartus.MACHINE_SOURCES)), len(quartus.MACHINE_SOURCES))
        self.assertEqual(quartus.MACHINE_SOURCES[-1], "cores/fes-pc386/rtl/pc386_system.sv")
        self.assertIn("cores/fes-common/rtl/i386/fes_i386_cpu.sv", quartus.MACHINE_SOURCES)
        self.assertIn("cores/fes-pc386/rtl/pc386_rom.sv", quartus.MACHINE_SOURCES)
        self.assertNotIn("cores/fes-pc386/rtl/top.sv", quartus.MACHINE_SOURCES)

    def test_pinned_inputs_exist(self):
        for path in quartus.PINNED_INPUTS:
            self.assertTrue((ROOT / path).is_file(), path)
        self.assertIn(quartus.FIRMWARE_LOCK, quartus.PINNED_INPUTS)

    def test_the_rom_contents_are_where_pc386_rom_reads_them(self):
        default = re.search(r'parameter ROM_HEX = "([^"]+)"', (ROOT / "cores/fes-pc386/rtl/pc386_rom.sv").read_text())
        self.assertEqual(default.group(1), quartus.ROM_HEX)

    def test_machine_program_elaborates_at_the_sealed_rates(self):
        program = quartus.machine_program(Path("out/pc386_system.v"))
        for name, value in quartus.guest_rates().items():
            self.assertRegex(program, rf"-chparam {name} {value}[ ;]")
        self.assertIn(f"hierarchy -top {quartus.MACHINE_TOP} ", program)
        self.assertIn("memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d;", program)
        self.assertTrue(program.endswith("write_verilog out/pc386_system.v"))
        for source in quartus.SHELL_SOURCES:
            self.assertNotIn(source, program)


class RatesTest(unittest.TestCase):
    def test_rates_at_100_mhz_are_the_machine_defaults(self):
        rates = quartus.guest_rates(100_000_000)
        expected = {"ISA": (1, 12), "PIT": (21, 880), "US": (1, 100), "DMA": (1, 24), "DOT0": (1007, 4000),
                    "DOT1": (14161, 50000), "OPL2": (7, 14080), "AUDIO": (3, 6250)}
        for name, pair in expected.items():
            self.assertEqual((rates[f"{name}_N"], rates[f"{name}_D"]), pair, name)
        self.assertEqual(rates["RTC_N"] / rates["RTC_D"], 256 / 781250)
        self.assertEqual(rates["ATA_RESET_CLOCKS"], 1000)
        with self.assertRaises(board.BuildError):
            quartus.guest_rates(50_000_000)

    def test_the_shell_defaults_are_the_sealed_rates(self):
        top = (ROOT / "cores/fes-pc386/rtl/top.sv").read_text()
        for name, value in quartus.guest_rates().items():
            self.assertRegex(top, rf"\b{name} = {value}\b")


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


class ResourcesTest(unittest.TestCase):
    def test_the_fit_summary_gives_each_resource(self):
        self.assertEqual(quartus.fit_resources(FIT_SUMMARY), {
            "alms": {"used": 9754, "available": 41910}, "registers": {"used": 12637},
            "ram_blocks": {"used": 53, "available": 553}, "block_memory_bits": {"used": 151955, "available": 5662720},
            "dsp_blocks": {"used": 21, "available": 112},
        })

    def test_a_missing_row_fails(self):
        with self.assertRaisesRegex(board.BuildError, "Total DSP Blocks"):
            quartus.fit_resources(FIT_SUMMARY.replace("Total DSP Blocks", "Total DSPs"))


class ManifestTest(unittest.TestCase):
    def test_manifest_is_the_system_labelled_quartus(self):
        record = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 1)
        evidence = {"rbf": {"sha256": "3" * 64, "size": 4096}}
        fields = tomllib.loads(quartus.manifest(record, evidence, "https://github.com/DeanoC/fes", "a" * 40,
                                                IDENTITIES).decode())
        self.assertEqual(fields["core"]["id"], "fes.pc386")
        self.assertEqual(fields["core"]["name"], "FES pc386 (Quartus)")
        self.assertEqual(fields["abi"], {"id": "fes.computer", "major": 1, "minor": 0})
        self.assertEqual([item["id"] for item in fields["interfaces"]], list(quartus.INTERFACES))
        self.assertEqual(fields["build"]["id"], build_identity(record))
        self.assertIn("Quartus Prime Lite 17.0.2", fields["build"]["toolchain"])
        self.assertIn("Yosys", fields["build"]["toolchain"])
        parameters = json.loads(record)["parameters"]
        self.assertEqual(parameters["compiler"], "quartus-17.0.2")
        self.assertEqual(json.loads(parameters["guest_rates"]), quartus.guest_rates())
        self.assertEqual(json.loads(parameters["firmware"]), quartus.firmware_digests(ROOT))
        self.assertIn("read_verilog -sv -I cores/fes-common/generated", parameters["machine_netlist"])
        self.assertIn(" $MACHINE_SOURCES; hierarchy -top pc386_system ", parameters["machine_netlist"])
        self.assertEqual(len(parameters["machine_sources_sha256"]), 64)

    def test_the_record_names_its_files_from_the_repository_root(self):
        fields = json.loads(quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40,
                                                        IDENTITIES, 1))
        self.assertEqual(fields["recipe"], "sources/misteross/scripts/build_fes_pc386_quartus.py")
        self.assertEqual(fields["abi_definition"], "sources/misteross/cores/fes-common/generated/fes_computer.vh")

    def test_the_seed_is_part_of_the_build_identity(self):
        first = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 1)
        second = quartus.create_build_record(ROOT, "https://github.com/DeanoC/fes", "a" * 40, IDENTITIES, 2)
        self.assertNotEqual(build_identity(first), build_identity(second))


if __name__ == "__main__":
    unittest.main()
