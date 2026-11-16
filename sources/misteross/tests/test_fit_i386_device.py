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
  function automatic [7:0] helper;
    input [7:0] not_a_port;
    input not_a_port_either;
    helper = not_a_port;
  endfunction
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
; Total virtual pins                       ; 4,700            ;
;   * ALMs containing virtual pins         ; 2,351 / 46,800 ( 5 % ) ;
; Total LABs:  partially or completely used                   ; 4,590 / 4,680         ; 98 %  ;
"""
CYCLONE_REPORT = """\
; Logic utilization (in ALMs)              ; 19,870 / 41,910 ( 47 % ) ;
; Combinational ALUT usage for logic       ; 35,112           ;
; Dedicated logic registers                ; 18,990           ;
; Total DSP Blocks                         ; 4 / 112 ( 4 % )  ;
; Total RAM Blocks                         ; 110 / 553 ( 20 % ) ;
; Total block memory bits                  ; 198,912          ;
; Total LABs:  partially or completely used                   ; 3,494 / 4,191         ; 83 %  ;
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

    def test_a_function_s_inputs_are_not_ports(self):
        # Yosys writes the flattened module's $pmux helpers as functions, and a
        # function declares its own inputs. Counting them put about 1,950 names
        # that are not nodes into the machine's virtual-pin list and made the
        # port counts meaningless -- and made M and M+ look like different
        # envelopes when their real port sets are identical.
        names = fit.netlist_ports(NETLIST, "fes_i386_cpu")
        self.assertNotIn("not_a_port[*]", names)
        self.assertNotIn("not_a_port_either", names)
        self.assertIn("clk", names)
        self.assertIn("arch_cpl[*]", names)

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
            # A commit alone is not the source that ran: the M+ fit is made by
            # editing a tracked file, so a fit records whether its checkout was
            # clean and carries the diff when it was not.
            self.assertIn("clean", inputs["fes_revision"])
            # One of these fits never implies a programming artifact or acceptance.
            self.assertIn("no hardware acceptance", inputs["classification"])

    def test_the_cpu_envelope_has_one_clock_and_cuts_every_virtual_port(self):
        sdc = fit.clock_constraints(fit.DESIGNS["cpu"], 1000.0 / 61.44)
        self.assertIn("create_clock -name clk_clock -period 16.2760 [get_ports clk]", sdc)
        self.assertNotIn("set_clock_groups", sdc)
        self.assertIn("derive_clock_uncertainty", sdc)
        # A virtual pin has no package, trace or external device, so a zero
        # delay on it invents a hold requirement on a path to a pin that does
        # not exist, and the fitter pads routing delay to meet it.
        self.assertNotIn("set_input_delay", sdc)
        self.assertNotIn("set_output_delay", sdc)
        self.assertIn("set virtual_inputs [remove_from_collection [all_inputs] [get_ports clk]]", sdc)
        self.assertIn("set_false_path -from $virtual_inputs", sdc)
        self.assertIn("set_false_path -to [all_outputs]", sdc)

    def test_the_machine_envelope_keeps_both_clocks_out_of_the_cut(self):
        sdc = fit.clock_constraints(fit.DESIGNS["machine"], 1000.0 / 61.44)
        self.assertIn("create_clock -name clk_clock -period 16.2760 [get_ports clk]", sdc)
        # The pixel clock is the shell's fixed 74.25 MHz and does not move with
        # the system clock; cores/fes-pc386/clocks.sdc times no path between them.
        self.assertIn("create_clock -name pixel_clk_clock -period 13.4680 [get_ports pixel_clk]", sdc)
        self.assertIn("set_clock_groups -asynchronous", sdc)
        self.assertIn("-group [get_clocks {clk_clock}]", sdc)
        self.assertIn("-group [get_clocks {pixel_clk_clock}]", sdc)
        # Both clocks are real inputs and must not be cut with the virtual ports.
        self.assertIn("[add_to_collection [get_ports clk] [get_ports pixel_clk]]", sdc)

    def test_port_directions_come_from_the_netlist(self):
        self.assertEqual(fit.parse_ports(NETLIST, "other"), [("a", "input"), ("b", "output")])


class Envelope2(unittest.TestCase):
    """The pin gate, which is what makes the envelope a claim rather than an
    assertion. It must also run for a fit whose timing missed, because a missed
    rung's area is quoted beside the rungs that passed."""

    REPORT = "; Total pins                       ; 1 / 351 ( < 1 % ) ;\n"

    def test_one_real_pin_against_one_real_clock_passes(self):
        record = {}
        fit.check_envelope(record, self.REPORT, {"real_clocks": ["clk"]})
        self.assertEqual(record["real_pins"], 1)

    def test_more_real_pins_than_real_clocks_is_a_failure(self):
        with self.assertRaisesRegex(fit.FitError, "not the intended envelope"):
            fit.check_envelope({}, "; Total pins ; 314 / 314 ;\n", {"real_clocks": ["clk"]})

    def test_a_report_without_a_pin_count_is_refused(self):
        with self.assertRaisesRegex(fit.FitError, "cannot be confirmed"):
            fit.check_envelope({}, "; Logic utilization (in ALMs) ; 1 / 2 ;\n",
                               {"real_clocks": ["clk"]})

    def test_it_does_not_run_twice(self):
        record = {"real_pins": 2}
        fit.check_envelope(record, "; Total pins ; 314 / 314 ;\n", {"real_clocks": ["clk"]})
        self.assertEqual(record["real_pins"], 2)


class ClockCoverage(unittest.TestCase):
    """TimeQuest's own coverage rows, checked on the failure path too: a missed
    rung bounds the closure from above, so the bound must not come from a clock
    the analysis never looked at."""

    CLEAN = ("; Illegal Clocks               ; 0 ; 0 ;\n"
             "; Unconstrained Clocks         ; 0 ; 0 ;\n")

    def _report(self, directory, text):
        out = Path(directory) / "output_files"
        out.mkdir(parents=True)
        (out / "fes_i386_cpu.sta.rpt").write_text(text)
        return Path(directory)

    def test_full_coverage_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            record = {}
            fit.check_clock_coverage(record, self._report(directory, self.CLEAN), "fes_i386_cpu")
            self.assertEqual(len(record["unconstrained_clock_rows"]), 2)

    def test_an_unconstrained_clock_is_a_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            text = ("; Illegal Clocks               ; 0 ; 0 ;\n"
                    "; Unconstrained Clocks         ; 1 ; 0 ;\n")
            with self.assertRaisesRegex(fit.FitError, "unconstrained or illegal clocks"):
                fit.check_clock_coverage({}, self._report(directory, text), "fes_i386_cpu")

    def test_a_report_without_the_coverage_rows_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(fit.FitError, "lacks unconstrained/illegal clock coverage"):
                fit.check_clock_coverage({}, self._report(directory, "; Something ; 0 ; 0 ;\n"),
                                         "fes_i386_cpu")

    def test_a_missing_report_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(fit.FitError, "cannot be confirmed"):
                fit.check_clock_coverage({}, Path(directory), "fes_i386_cpu")


class Caps(unittest.TestCase):
    def test_each_design_gets_the_cap_the_method_promised(self):
        # A fit reported as "not measured within the cap" must have been given
        # the cap the method precommitted, or the label is false.
        self.assertEqual(fit.DEFAULT_TIMEOUT_MINUTES["cpu"], 90)
        self.assertEqual(fit.DEFAULT_TIMEOUT_MINUTES["machine"], 240)


class SecondaryFrontEnd(unittest.TestCase):
    """Quartus reading the SystemVerilog itself: a cross-check on the netlist
    flow, not a second area result."""

    def test_it_is_refused_for_the_machine(self):
        with self.assertRaisesRegex(fit.FitError, "fes_i386_cpu only"):
            fit.write_sv_project(Path("/nonexistent"), fit.DEVICES["de25"],
                                 fit.DESIGNS["machine"], 16.276, 1, {})

    def test_it_reads_the_cpu_rtl_in_the_seal_s_order(self):
        sources = [path.name for path in fit.cpu_sources()]
        self.assertEqual(sources[0], "fes_i386_pkg.sv")
        self.assertEqual(sources[-1], "fes_i386_cpu.sv")
        self.assertIn("fes_i386_x87.sv", sources)

    def test_the_project_is_labelled_a_secondary_cross_check(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "out"
            output.mkdir()
            metadata = {"series": "26.3", "qsf_version": "26.3.0 Pro Edition", "edition": "pro"}
            project = fit.write_sv_project(output, fit.DEVICES["de25"], fit.DESIGNS["cpu"],
                                           16.276, 1, metadata)
            qsf = (project / "fes_i386_cpu.qsf").read_text()
            self.assertIn("set_global_assignment -name SYSTEMVERILOG_FILE fes_i386_cpu.sv", qsf)
            self.assertIn("set_global_assignment -name SYSTEMVERILOG_FILE fes_i386_pkg.sv", qsf)
            # The RTL's ports are multi-dimensional packed types whose final
            # pin names are not known before elaboration, so every pin is made
            # virtual by wildcard and the clock assigned back. The fit's pin
            # gate is what proves it took effect.
            self.assertIn("set_instance_assignment -name VIRTUAL_PIN ON -to *\n", qsf)
            self.assertIn("set_instance_assignment -name VIRTUAL_PIN OFF -to clk\n", qsf)
            inputs = json.loads((output / "inputs.json").read_text())
            self.assertEqual(inputs["front_end"], "quartus-systemverilog")
            self.assertIn("SECONDARY", inputs["classification"])
            self.assertIn("not a second area result", inputs["classification"])
            self.assertIsNone(inputs["netlist_sha256"])
            self.assertEqual(inputs["virtual_ports"],
                             ["* (wildcard; the clock is assigned back)"])
            # The same rename the primary applies to the netlist, applied to
            # the RTL: without it Quartus Pro refuses, because fes_i386_l1i,
            # fes_i386_l1d and fes_i386_btb ask for Cyclone V block RAM.
            self.assertEqual(inputs["ram_attribute_count"], 30)
            self.assertNotIn('ramstyle = "M10K"',
                             (project / "fes_i386_l1i.sv").read_text())
            self.assertIn('ramstyle = "M20K"', (project / "fes_i386_l1i.sv").read_text())
            self.assertNotEqual(inputs["source_sha256"]["fes_i386_l1i.sv"],
                                inputs["fitted_source_sha256"]["fes_i386_l1i.sv"])
            # A module with no block-RAM request is copied unchanged.
            self.assertEqual(inputs["source_sha256"]["fes_i386_pkg.sv"],
                             inputs["fitted_source_sha256"]["fes_i386_pkg.sv"])


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

    def test_the_envelope_share_of_the_alms_is_reported(self):
        # Each virtual pin is built in the fabric, so part of the ALM figure is
        # envelope. The fitter's own count of it is reported beside the ALMs
        # and never subtracted from them.
        found = fit.resources(AGILEX_REPORT)
        self.assertEqual(found["virtual_pins"]["used"], 4_700)
        self.assertEqual(found["alms_with_virtual_pins"]["used"], 2_351)

    def test_both_fitters_lab_rows_are_read(self):
        # Both report this row under the same label. It is not a capacity
        # figure -- a LAB holds 10 ALMs on both devices and counts as used when
        # one ALM in it is used -- so it is recorded and never used as the
        # budget. An earlier version of this test asserted the Agilex fitter
        # reported no LABs at all, which was simply wrong.
        self.assertEqual(fit.resources(CYCLONE_REPORT)["labs"]["used"], 3_494)
        self.assertEqual(fit.resources(CYCLONE_REPORT)["labs"]["available"], 4_191)
        self.assertEqual(fit.resources(AGILEX_REPORT)["labs"]["used"], 4_590)
        self.assertEqual(fit.resources(AGILEX_REPORT)["labs"]["available"], 4_680)
        # 10 ALMs per LAB on both devices, which is why the two percentages are
        # not two views of one occupancy.
        self.assertEqual(fit.DEVICES["de25"].alms / 4_680, 10.0)
        self.assertEqual(fit.DEVICES["de10"].alms / 4_191, 10.0)

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
