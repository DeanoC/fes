# SPDX-License-Identifier: MIT
"""scripts/fit_i386_device.py: the virtual-pin envelope and the fitter-report
reader. These cover the parts that decide what a figure means -- which ports
become virtual pins, which report row each resource came from, and the clock
constraints -- with no Quartus, Yosys or device needed."""
import json
from pathlib import Path
import tempfile
import unittest

from scripts import fit_i386_device as fit

NETLIST = """\
module other(a, b);
  input a;
  output b;
  assign b = a;
endmodule
module fes_i386_cpu(clk, cold_reset, init_gpr, ddr_address, arch_cpl);
  input clk;
  input cold_reset;
  input [255:0] init_gpr;
  output [27:0] ddr_address;
  output [1:0] arch_cpl;
  output [639:0] arch_x87_regs;
  output wire signed [15:0] sample;
  wire [31:0] internal;
  reg [3:0] state;
endmodule
"""

AGILEX_REPORT = """\
+------------------------------------------+
; Fitter Resource Usage Summary            ;
+------------------------------------------+
; Logic utilization (ALMs needed / total ALMs on device) ; 21,345 / 46,800 ( 46 % ) ;
; Combinational ALUT usage for logic       ; 38,204           ;
; Dedicated logic registers                ; 19,004           ;
; DSP Blocks Needed [=A+B+C-D]             ; 9 / 188 ( 5 % )  ;
; M20K blocks                              ; 37 / 358 ( 10 % );
; Total block memory bits                  ; 215,040          ;
; Virtual pins                             ; 1,306            ;
"""
CYCLONE_REPORT = """\
; Logic utilization (in ALMs)              ; 19,870 / 41,910 ( 47 % ) ;
; Combinational ALUT usage for logic       ; 35,112           ;
; Dedicated logic registers                ; 18,990           ;
; Total DSP Blocks                         ; 4 / 112 ( 4 % )  ;
; Total RAM Blocks                         ; 110 / 553 ( 20 % ) ;
; Total block memory bits                  ; 198,912          ;
"""


MACHINE_PORTS = [("clk", "input"), ("pixel_clk", "input"), ("pixel_locked", "input"),
                 ("video", "output"), ("gpo", "input"), ("gpi", "output")]


class Ports(unittest.TestCase):
    def test_ports_come_from_the_netlist_with_vectors_wildcarded(self):
        self.assertEqual(fit.netlist_ports(NETLIST, "fes_i386_cpu"),
                         ["clk", "cold_reset", "init_gpr[*]", "ddr_address[*]", "arch_cpl[*]",
                          # "reg" is a substring of this port's name, and "wire"
                          # and "signed" are keywords in the next declaration.
                          "arch_x87_regs[*]", "sample[*]"])

    def test_ports_of_the_named_top_only(self):
        # The netlist holds every module; only the top's ports become pins.
        self.assertEqual(fit.netlist_ports(NETLIST, "other"), ["a", "b"])

    def test_a_missing_top_is_refused(self):
        with self.assertRaisesRegex(fit.FitError, "no module pc386_system"):
            fit.netlist_ports(NETLIST, "pc386_system")

    def test_a_top_without_body_declarations_is_refused(self):
        # Yosys's write_verilog always declares ports in the body. An ANSI-style
        # header would otherwise be read as a portless module, and every port
        # would silently become a real pin.
        with self.assertRaisesRegex(fit.FitError, "declares no ports in the netlist body"):
            fit.netlist_ports("module ansi (input wire a, output wire b);\nendmodule\n", "ansi")


class Envelope(unittest.TestCase):
    def test_every_port_but_the_clock_is_virtual(self):
        with tempfile.TemporaryDirectory() as directory:
            netlist = Path(directory) / "fes_i386_cpu.netlist.v"
            netlist.write_text(NETLIST)
            output = Path(directory) / "out"
            output.mkdir()
            metadata = {"series": "26.3", "qsf_version": "26.3.0 Pro Edition", "edition": "pro"}
            project = fit.write_project(output, fit.DEVICES["de25"], fit.DESIGNS["cpu"], netlist,
                                        1000.0 / 61.44, 1, metadata)
            qsf = (project / "fes_i386_cpu.qsf").read_text()
            # Unbraced: Quartus 17.0.2 rejects a braced node name outright and
            # leaves the port as a real pin, which is a different experiment.
            self.assertIn("set_instance_assignment -name VIRTUAL_PIN ON -to init_gpr[*]\n", qsf)
            self.assertIn("set_instance_assignment -name VIRTUAL_PIN ON -to arch_cpl[*]\n", qsf)
            self.assertNotIn("{", qsf.split("VIRTUAL_PIN")[1].split("\n")[0])
            self.assertNotIn("VIRTUAL_PIN ON -to clk\n", qsf)
            self.assertIn("set_global_assignment -name DEVICE A5EB013BB23BE4SCS", qsf)
            self.assertIn('set_global_assignment -name FAMILY "Agilex 5"', qsf)
            inputs = json.loads((output / "inputs.json").read_text())
            self.assertEqual(inputs["real_clocks"], ["clk"])
            self.assertNotIn("clk", inputs["virtual_ports"])
            self.assertEqual(inputs["clock_hz"], 61_440_000)
            self.assertEqual(inputs["virtual_port_count"], len(inputs["virtual_ports"]))
            self.assertTrue(inputs["host"])
            # One of these fits never implies a programming artifact or acceptance.
            self.assertIn("no hardware acceptance", inputs["classification"])

    def test_the_cpu_envelope_has_one_clock_and_zero_virtual_io_delays(self):
        sdc = fit.clock_constraints(fit.DESIGNS["cpu"], 1000.0 / 61.44,
                                    [("clk", "input"), ("cold_reset", "input")])
        self.assertIn("create_clock -name clk_clock -period 16.2760 [get_ports clk]", sdc)
        self.assertNotIn("set_clock_groups", sdc)
        self.assertIn("derive_clock_uncertainty", sdc)
        for bound in ("max", "min"):
            self.assertIn(f"set_input_delay -clock clk_clock -{bound} 0.000 $data_inputs", sdc)
            self.assertIn(f"set_output_delay -clock clk_clock -{bound} 0.000 $data_outputs", sdc)

    def test_the_machine_envelope_has_two_asynchronous_clocks(self):
        ports = [(name + "[*]" if name in {"video", "gpo", "gpi"} else name, direction)
                 for name, direction in MACHINE_PORTS]
        sdc = fit.clock_constraints(fit.DESIGNS["machine"], 1000.0 / 61.44, ports)
        self.assertIn("create_clock -name clk_clock -period 16.2760 [get_ports clk]", sdc)
        # The pixel clock is the shell's fixed 74.25 MHz and does not move with
        # the system clock; cores/fes-pc386/clocks.sdc times no path between them.
        self.assertIn("create_clock -name pixel_clk_clock -period 13.4680 [get_ports pixel_clk]", sdc)
        self.assertIn("set_clock_groups -asynchronous", sdc)
        self.assertIn("-group [get_clocks {clk_clock}]", sdc)
        self.assertIn("-group [get_clocks {pixel_clk_clock}]", sdc)
        # The video output lives in pixel_clk (pc386_system.sv:66-67). With the
        # two clocks asynchronous, constraining it against the system clock
        # would have TimeQuest cut its paths rather than analyse them, and the
        # fit could close without the video output being looked at at all.
        self.assertIn("set_output_delay -clock pixel_clk_clock -max 0.000 [get_ports {video[*]}]", sdc)
        self.assertIn("set_input_delay -clock pixel_clk_clock -max 0.000 [get_ports {pixel_locked}]", sdc)
        # ... and a pixel-domain port is not also constrained against the system clock.
        self.assertIn("[get_ports {video[*]}]]]\n", sdc)
        self.assertNotIn("set_output_delay -clock clk_clock -max 0.000 [get_ports {video[*]}]", sdc)

    def test_an_input_gets_no_output_delay_and_the_reverse(self):
        ports = [(name + "[*]" if name in {"video", "gpo", "gpi"} else name, direction)
                 for name, direction in MACHINE_PORTS]
        sdc = fit.clock_constraints(fit.DESIGNS["machine"], 1000.0 / 61.44, ports)
        self.assertNotIn("set_output_delay -clock pixel_clk_clock -max 0.000 [get_ports {pixel_locked}]", sdc)
        self.assertNotIn("set_input_delay -clock pixel_clk_clock -max 0.000 [get_ports {video[*]}]", sdc)

    def test_a_port_clock_table_naming_a_port_the_netlist_lacks_is_refused(self):
        design = dict(fit.DESIGNS["machine"], port_clock={"nonexistent": "pixel_clk"})
        with self.assertRaisesRegex(fit.FitError, "does not have: nonexistent"):
            fit.clock_constraints(design, 16.276, [("clk", "input"), ("pixel_clk", "input")])

    def test_port_directions_come_from_the_netlist(self):
        self.assertEqual(fit.parse_ports(NETLIST, "other"), [("a", "input"), ("b", "output")])


class RamAttribute(unittest.TestCase):
    """Agilex 5 has no M10K, and its altera_syncram refuses the name outright."""

    def test_only_the_block_name_changes(self):
        text = 'wire a;\n(* ramstyle = "M10K" *) reg [7:0] m [0:255];\n' \
               '(* ramstyle = "MLAB, no_rw_check" *) reg [31:0] s [0:15];\n'
        rewritten, count = fit.retarget_ram_attribute(text)
        self.assertEqual(count, 1)
        self.assertIn('ramstyle = "M20K"', rewritten)
        self.assertNotIn('"M10K"', rewritten)
        # MLAB is legal on both devices and is left alone.
        self.assertIn('ramstyle = "MLAB, no_rw_check"', rewritten)
        self.assertEqual(rewritten.replace('"M20K"', '"M10K"'), text)

    def test_a_netlist_that_already_asks_for_m20k_is_refused(self):
        # The round-trip check could not then tell a rewritten request from an
        # original one, so the rename would no longer prove what it claims.
        with self.assertRaisesRegex(fit.FitError, "already asks for M20K"):
            fit.retarget_ram_attribute('(* ramstyle = "M20K" *) reg x;\n')

    def test_the_cyclone_side_reads_the_netlist_unchanged(self):
        with tempfile.TemporaryDirectory() as directory:
            netlist = Path(directory) / "n.v"
            netlist.write_text('(* ramstyle = "M10K" *) reg [7:0] m [0:255];\n')
            fitted, record = fit.prepare_netlist(netlist, fit.DEVICES["de10"],
                                                 Path(directory) / "work")
            self.assertEqual(fitted, netlist)
            self.assertEqual(record["fitted_sha256"], record["source_sha256"])
            self.assertIn("unchanged", record["ram_attribute"])

    def test_the_agilex_side_records_what_it_renamed(self):
        with tempfile.TemporaryDirectory() as directory:
            netlist = Path(directory) / "n.v"
            netlist.write_text('(* ramstyle = "M10K" *) reg [7:0] m [0:255];\n')
            fitted, record = fit.prepare_netlist(netlist, fit.DEVICES["de25"],
                                                 Path(directory) / "work")
            self.assertNotEqual(fitted, netlist)
            self.assertNotEqual(record["fitted_sha256"], record["source_sha256"])
            self.assertEqual(record["ram_attribute_count"], 1)
            self.assertIn('ramstyle = "M20K"', fitted.read_text())


class DuplicateAttributes(unittest.TestCase):
    """Yosys writes a bare and a valued parallel_case on the same casez.
    Quartus 17.0.2 warns; Quartus Pro 26.3 stops with Error (22074)."""

    DUPLICATE = ("module m;\n"
                 "    (* full_case = 32'd1 *)\n"
                 "    (* parallel_case = 32'd1 *)\n"
                 "    (* parallel_case *)\n"
                 "    casez (s)\n"
                 "    endcase\n"
                 "endmodule\n")

    def test_only_the_repeated_bare_attribute_goes(self):
        kept, removed = fit.drop_duplicate_attributes(self.DUPLICATE)
        self.assertEqual(removed, 1)
        self.assertIn("(* parallel_case = 32'd1 *)", kept)
        self.assertIn("(* full_case = 32'd1 *)", kept)
        self.assertNotIn("(* parallel_case *)", kept)
        self.assertIn("casez (s)", kept)

    def test_a_lone_bare_attribute_stays(self):
        # 620 of the machine netlist's 650 bare parallel_case attributes repeat
        # nothing, and Quartus accepts them; only the duplicates are the problem.
        text = "module m;\n    (* parallel_case *)\n    casez (s)\n    endcase\nendmodule\n"
        kept, removed = fit.drop_duplicate_attributes(text)
        self.assertEqual(removed, 0)
        self.assertEqual(kept, text)

    def test_a_bare_attribute_of_another_name_stays(self):
        text = ("module m;\n    (* parallel_case = 32'd1 *)\n    (* full_case *)\n"
                "    casez (s)\n    endcase\nendmodule\n")
        kept, removed = fit.drop_duplicate_attributes(text)
        self.assertEqual(removed, 0)
        self.assertEqual(kept, text)

    def test_the_valued_attribute_may_come_second(self):
        text = ("module m;\n    (* parallel_case *)\n    (* parallel_case = 32'd1 *)\n"
                "    casez (s)\n    endcase\nendmodule\n")
        kept, removed = fit.drop_duplicate_attributes(text)
        self.assertEqual(removed, 1)
        self.assertNotIn("(* parallel_case *)\n", kept)

    def test_attributes_of_separate_statements_are_separate_runs(self):
        text = ("module m;\n    (* parallel_case = 32'd1 *)\n    casez (s)\n    endcase\n"
                "    (* parallel_case *)\n    casez (t)\n    endcase\nendmodule\n")
        kept, removed = fit.drop_duplicate_attributes(text)
        self.assertEqual(removed, 0, "a valued attribute on one statement must not strip "
                                     "a bare one on another")
        self.assertEqual(kept, text)


class Resources(unittest.TestCase):
    def test_each_figure_records_the_report_row_it_came_from(self):
        # A figure without the row it was read from is the kind of unlabelled
        # number this lane has been bitten by; a renamed row must fail loudly.
        for report, alm_label, ram, used in (
                (AGILEX_REPORT, "Logic utilization (ALMs needed / total ALMs on device)",
                 "M20K blocks", 21_345),
                (CYCLONE_REPORT, "Logic utilization (in ALMs)", "Total RAM Blocks", 19_870)):
            with self.subTest(label=alm_label):
                found = fit.resources(report)
                self.assertEqual(found["alms"]["report_label"], alm_label)
                self.assertEqual(found["alms"]["used"], used)
                self.assertEqual(found["ram_blocks"]["report_label"], ram)
                self.assertGreater(found["aluts"]["used"], 0)
                self.assertGreater(found["registers"]["used"], 0)

    def test_virtual_pins_are_reported_when_the_fitter_gives_them(self):
        # Each virtual pin is built in the fabric, so part of the ALMs is
        # envelope. The count is reported beside them and never subtracted.
        self.assertEqual(fit.resources(AGILEX_REPORT)["virtual_pins"]["used"], 1_306)

    def test_a_missing_optional_row_is_not_a_failure(self):
        self.assertNotIn("virtual_pins", fit.resources(CYCLONE_REPORT))

    def test_device_percentages_come_from_the_report_not_from_this_file(self):
        found = fit.resources(AGILEX_REPORT)
        self.assertEqual(found["alms"]["available"], 46_800)
        self.assertAlmostEqual(found["alms"]["percent_of_device"], 45.61, places=2)
        self.assertEqual(found["ram_blocks"]["available"], 358)

    def test_a_report_missing_a_figure_is_refused(self):
        with self.assertRaisesRegex(fit.FitError, "has no alms"):
            fit.resources("; Dedicated logic registers ; 10 ;\n"
                          "; Combinational ALUT usage for logic ; 10 ;\n"
                          "; Total DSP Blocks ; 0 ;\n; M20K blocks ; 0 ;\n"
                          "; Total block memory bits ; 0 ;\n")


class Flow(unittest.TestCase):
    def test_pro_synthesises_with_quartus_syn_and_the_standard_edition_with_map(self):
        self.assertEqual(fit.tool_names({"edition": "pro"}), ("quartus_syn",))
        self.assertEqual(fit.tool_names({"edition": "lite"}), ("quartus_map",))

    def test_the_two_devices_keep_their_own_totals_and_ram_block_names(self):
        # Cyclone V ALMs and Agilex 5 ALMs are not the same unit, and the two
        # devices share no RAM block either; nothing here compares them.
        self.assertEqual(fit.DEVICES["de10"].alms, 41_910)
        self.assertEqual(fit.DEVICES["de25"].alms, 46_800)
        self.assertEqual(fit.DEVICES["de10"].ram_block, "M10K")
        self.assertEqual(fit.DEVICES["de25"].ram_block, "M20K")
        self.assertEqual(fit.DEVICES["de10"].family, "Cyclone V")
        self.assertEqual(fit.DEVICES["de25"].family, "Agilex 5")


if __name__ == "__main__":
    unittest.main()
