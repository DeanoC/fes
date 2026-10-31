"""Actual upstream composition, retained-source and fail-closed mutation guards."""
import importlib.util
import json
from pathlib import Path
import re
import tempfile
import unittest
from unittest import mock
import xml.etree.ElementTree as ET

SPEC = importlib.util.spec_from_file_location("de25_scanout_builder_test", Path(__file__).with_name("build.py"))
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)


class ScanoutBuildTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="de25-scanout-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        fixture_module = B.load(B.HERE.parent / "combined/test_build.py", "scanout_upstream_fixture")
        self.fixture = fixture_module.CombinedBuildTests()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.sources = B.capture()
        self.sources.update(self.fixture.sources)

    def prepare(self, revision=None):
        with mock.patch.object(B, "capture", return_value=self.sources):
            return B.prepare(self.fixture.archive, self.root / "prepared", revision)

    def test_visibility_cdc_cut_targets_only_the_first_synchronizer_d_pin(self):
        sdc = self.sources[B.BOARD + "scanout/clocks.sdc"].decode()
        self.assertIn(
            "set scanout_visible_head [get_pins -compatibility_mode -nocase -nowarn "
            "{fes_video_inst|visible_sync[0]|d}]", sdc)
        self.assertIn("if {[get_collection_size $scanout_visible_head] != 1}", sdc)
        self.assertIn("set_false_path -to $scanout_visible_head", sdc)
        self.assertIn("Cut only the first synchronizer D input", sdc)
        self.assertNotIn("set_clock_groups", sdc)

    def test_hdmi_data_and_de_use_registered_visibility_with_lock_assertion(self):
        board = self.sources[B.BOARD + "scanout/fes_de25_scanout_board.v"].decode()
        self.assertIn("always @(posedge pixel_clk or negedge qualified_lock)", board)
        self.assertIn("always @(posedge pixel_clk or negedge visible_sync[1])", board)
        self.assertIn("assign HDMI_TX_D = video_active && visible_line ? video_rgb : 24'd0;", board)
        self.assertIn("assign HDMI_TX_DE = video_active && visible_line && video_de;", board)
        self.assertNotIn("assign HDMI_TX_D = visible_request", board)
        self.assertNotIn("assign HDMI_TX_DE = visible_request", board)

    def test_actual_preparation_replaces_pattern_with_response_preserving_real_scanout(self):
        output, _, hps, _ = self.prepare()
        project = output / "combined/hps/project"
        inputs = json.loads((project.parent / "inputs.json").read_text())
        qsf = (project / "golden_top.qsf").read_text()
        top = (project / "golden_top.v").read_text()
        self.assertNotIn("fes_menu_pattern_memory", qsf)
        self.assertNotIn("fes_de25_combined_video", top)
        self.assertFalse((project / "fes_combined/video.v").exists())
        self.assertFalse((project / "fes_combined/fes_menu_pattern_memory.v").exists())
        self.assertIn("fes_de25_scanout_board fes_video_inst", top)
        self.assertIn(".h2f_warm_reset_handshake_reset_ack         (1'b1)", top)
        self.assertIn(".fes_scanout_pixel_clk_clk (scanout_pixel_clk)", top)
        script = (project / "integrate-mailbox.tcl").read_text()
        self.assertIn("add_instance fes_scanout_pixel_clock altera_clock_bridge 19.2.0", script)
        self.assertIn("add_connection fes_scanout_pixel_clock.out_clk subsys_periph.fes_scanout_pixel_clk", script)
        self.assertIn("fes_scanout_pixel_clk EXPORT_OF fes_scanout_pixel_clock.in_clk", script)
        self.assertTrue(inputs["combined_display"]["ddr_scanout"])
        self.assertFalse(inputs["scanout"]["default_enabled"])
        self.assertNotIn("reviewed_video_derivation", inputs["combined_display"])
        # Preserve the original HPS producer files and its receipt: the new
        # producer must not overwrite fes_mailbox/build.py or integrate.tcl.
        for name, value in inputs["source_sha256"].items():
            self.assertEqual(hps.digest(project / "fes_mailbox" / name), value)
        for name, value in inputs["scanout"]["source_sha256"].items():
            self.assertEqual(hps.digest(project / "fes_mailbox" / name), value)
        self.assertIn("scanout_build.py", inputs["scanout"]["source_sha256"])
        self.assertEqual((project / self.fixture.hps.HPS).read_bytes(), self.fixture.files[self.fixture.hps.HPS])
        self.assertEqual((project / self.fixture.hps.EMIF).read_bytes(), self.fixture.files[self.fixture.hps.EMIF])
        self.assertEqual((project / self.fixture.hps.BOOT).read_bytes(), self.fixture.files[self.fixture.hps.BOOT])

    def test_source_snapshot_survives_later_working_source_mutation(self):
        revision = "a" * 40
        output, _, _, _ = self.prepare(revision)
        key = B.BOARD + "scanout/fes_de25_scanout.v"
        original = self.sources[key]
        self.sources[key] = b"unselected edit"
        project = output / "combined/hps/project"
        self.assertEqual((project / "fes_mailbox/fes_de25_scanout.v").read_bytes(), original)
        self.assertEqual((output / "functional-source" / key).read_bytes(), original)
        inputs = json.loads((project.parent / "inputs.json").read_text())
        self.assertEqual(inputs["fes_revision"], revision)
        self.assertNotIn("base_revision", inputs)
        self.assertEqual(inputs["functional_snapshot_sha256"][key], B.sha(original))

    def test_rendered_register_offset_comes_from_selected_generated_definition(self):
        key = B.BOARD + "scanout/generated/fes_de25_scanout.vh"
        self.sources[key] = self.sources[key].replace(b"PERIPHERAL_OFFSET 32'h00010200", b"PERIPHERAL_OFFSET 32'h00010300")
        output, _, _, _ = self.prepare()
        project = output / "combined/hps/project"
        script = (project / "integrate-mailbox.tcl").read_text()
        self.assertIn("pb_cpu_0.m0/fes_ctrl_cdc.s0 baseAddress 0x10300", script)
        self.assertNotIn("@SCANOUT_BASE@", script)
        self.assertIn("subsys_periph.fes_scanout_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0", script)
        self.assertEqual(json.loads((project.parent / "inputs.json").read_text())["scanout"]["peripheral_offset"], 0x10300)

    def test_generation_marks_actual_scanout_and_requires_handoff_validation(self):
        output, combined, hps, display = self.prepare()
        path = output / "combined/hps/generation.json"
        def upstream(*args):
            path.write_text(json.dumps({"combined_display": {"ddr_scanout": False}}))
        with mock.patch.object(combined, "generate", side_effect=upstream), mock.patch.object(B, "verify_handoff", return_value={"ddr_scanout": True}) as verify:
            B.generate(output, combined, hps, display, None, "test")
            self.assertEqual(verify.call_count, 1)
        self.assertTrue(json.loads(path.read_text())["combined_display"]["ddr_scanout"])
        with mock.patch.object(combined, "generate", side_effect=upstream), mock.patch.object(B, "verify_handoff", side_effect=ValueError("bad actual bridge")):
            with self.assertRaisesRegex(ValueError, "bad actual bridge"):
                B.generate(output, combined, hps, display, None, "test")

    def test_top_reset_source_feeds_both_domain_synchronizers_without_changing_board_port(self):
        output, _, _, _ = self.prepare()
        project = output / "combined/hps/project"
        script = (project / "integrate-mailbox.tcl").read_text()
        self.assertIn("add_instance fes_scanout_top_reset altera_reset_bridge 19.2.0", script)
        self.assertIn("add_connection fes_scanout_top_reset.out_reset subsys_periph.fes_scanout_pixel_reset", script)
        self.assertIn("add_connection fes_scanout_top_reset.out_reset subsys_periph.fes_scanout_system_reset", script)
        self.assertIn("add_connection fes_scanout_pixel_clock.out_clk subsys_periph.fes_scanout_pixel_clk", script)
        self.assertIn("fes_scanout_reset EXPORT_OF fes_scanout_top_reset.in_reset", script)
        self.assertNotIn("fes_scanout_reset EXPORT_OF subsys_periph.fes_scanout_reset", script)
        self.assertNotIn("add_connection rst_in.out_reset subsys_periph.fes_scanout_pixel_reset", script)
        self.assertIn(".fes_scanout_reset_reset_n (scanout_reset_n)", (project / "golden_top.v").read_text())
        integrate = (project / "fes_mailbox/scanout_integrate.tcl").read_text()
        self.assertIn("fes_scanout_pixel_reset fes_pixel_clk.out_clk deassert", integrate)
        self.assertIn("fes_scanout_system_reset periph_clk.out_clk deassert", integrate)
        self.assertIn("add_connection fes_scanout_system_reset.out_reset $instance.s0_reset", integrate)
        self.assertIn("add_connection fes_scanout_pixel_reset.out_reset $instance.m0_reset", integrate)
        self.assertIn('if {[string compare $instance "fes_ctrl_cdc"] == 0}', integrate)
        self.assertEqual(B.verify_reset_synchronizers(project)["sdc_clear_pins"], 10)

    def test_generation_reset_guard_rejects_unregistered_gate_or_stale_sdc_targets(self):
        output, _, _, _ = self.prepare()
        project = output / "combined/hps/project"
        board_path = project / "fes_mailbox/fes_de25_scanout_board.v"
        original_board = board_path.read_text()
        board_path.write_text(original_board.replace(
            "assign HDMI_TX_DE = video_active && visible_line && video_de;",
            "assign HDMI_TX_DE = visible_request && video_active && visible_line && video_de;", 1))
        with self.assertRaisesRegex(ValueError, "not pixel-registered"):
            B.verify_reset_synchronizers(project)
        board_path.write_text(original_board)
        sdc_path = project / "fes_mailbox/clocks.sdc"
        original_sdc = sdc_path.read_text()
        sdc_path.write_text(original_sdc.replace("fes_video_inst|visible_sync[1]|aclr", "fes_video_inst|missing_visible_sync|aclr", 1))
        with self.assertRaisesRegex(ValueError, "does not constrain the registered visibility clear pin"):
            B.verify_reset_synchronizers(project)
        sdc_path.write_text(original_sdc.replace("$scanout_clear_count != 10", "$scanout_clear_count != 8", 1))
        with self.assertRaisesRegex(ValueError, "ten reset/visibility clear pins"):
            B.verify_reset_synchronizers(project)
        sdc_path.write_text(original_sdc.replace("fes_video_inst|visible_sync[1]|aclr}]", "fes_video_inst|visible_sync[1]|aclr fes_video_inst|video_active|aclr}]", 1))
        with self.assertRaisesRegex(ValueError, "data/DE gate clear timed"):
            B.verify_reset_synchronizers(project)
        sdc_path.write_text(original_sdc.replace("rst_controller_002|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[1]|clrn", "rst_controller_002|missing|clrn", 1))
        with self.assertRaisesRegex(ValueError, "does not constrain the generated rst_controller_002 stage 1"):
            B.verify_reset_synchronizers(project)

    def test_sdc_targets_the_generated_pixel_and_system_reset_synchronizers(self):
        sdc = (B.HERE / "clocks.sdc").read_text()
        for controller in ("rst_controller_001", "rst_controller_002"):
            for stage in (0, 1):
                self.assertIn(
                    f"soc_inst|{controller}|alt_rst_sync_uq1|"
                    f"altera_reset_synchronizer_int_chain[{stage}]|aclr",
                    sdc,
                )
                self.assertIn(
                    f"soc_inst|{controller}|alt_rst_sync_uq1|"
                    f"altera_reset_synchronizer_int_chain[{stage}]|clrn",
                    sdc,
                )
        self.assertNotIn("altera_reset_synchronizer_int_chain*|aclr", sdc)
        self.assertNotIn("altera_reset_synchronizer_int_chain*|clrn", sdc)
        for register in ("visible_sync[0]", "visible_sync[1]"):
            self.assertIn(f"fes_video_inst|{register}|aclr", sdc)
            self.assertIn(f"fes_video_inst|{register}|clrn", sdc)
        self.assertIn("if {$scanout_clear_count != 10}", sdc)
        cuts = re.findall(r"set scanout_(?:aclr|clrn) \[get_pins [^{]*\{[^}]*\}\]", sdc)
        self.assertEqual(len(cuts), 2)
        self.assertNotIn("video_active", "".join(cuts))
        self.assertIn("if {[get_collection_size $scanout_gate_clear] != 1}", sdc)
        self.assertNotIn("set_false_path -to $scanout_gate_clear", sdc)
        self.assertNotIn("fes_scanout_pixel_reset|sync_regs*", sdc)
        self.assertNotIn("fes_scanout_system_reset|sync_regs*", sdc)

    def test_clock_fault_ingress_has_quartus_native_identification(self):
        rtl = (B.HERE / "fes_de25_scanout.v").read_text()
        self.assertIn('altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED"', rtl)
        self.assertIn("reg clock_fault_meta;", rtl)
        self.assertIn("clock_fault_meta <= clock_fault;", rtl)
        self.assertIn("clock_fault_sync <= clock_fault_meta;", rtl)
        self.assertIn("clock_fault_meta|d", (B.HERE / "clocks.sdc").read_text())

    def test_postcompile_pll_mutation_is_rejected_with_failed_receipt_retained(self):
        output, _, hps, display = self.prepare()
        directory = output / "combined/hps"
        (directory / "generation.json").write_text("{}\n")
        pll = directory / "project/fes_combined/sys_pll.ip"
        pll.write_bytes(pll.read_bytes().replace(b">74.25<", b">73.0<"))
        def compile_stub(*args):
            (directory / "build.json").write_text("{}\n")
            return directory / "project/output_files/golden_top.sof"
        with mock.patch.object(hps, "compile_project", side_effect=compile_stub):
            with self.assertRaisesRegex(ValueError, "changed pixel PLL"):
                B.compile_project(output, hps, display, None)
        self.assertFalse(json.loads((directory / "build.json").read_text())["post_compile_pixel_pll_parameters_unchanged"])

    def reset_fixture(self, parameters=None, output_edges=None, output_role="reset_n"):
        expected = {"ACTIVE_LOW_RESET": "1", "SYNCHRONOUS_EDGES": "none", "NUM_RESET_OUTPUTS": "1", "USE_RESET_REQUEST": "0"}
        expected.update(parameters or {})
        if output_edges is None:
            output_edges = expected["SYNCHRONOUS_EDGES"].upper()
        source = self.root / "child/ip/reset.ip"
        source.parent.mkdir(parents=True, exist_ok=True)
        source.write_bytes(self.fixture.hps.component_xml(tuple(expected.items())))
        module = ET.Element("module", {"path": "reset", "kind": "altera_reset_bridge", "version": "19.2.0"})
        for name, value in (("logicalView", "ip/reset.ip"), ("componentDefinition", "<componentDefinition><boundary><interfaces>" +
            ''.join(f'<interface><name>{name}</name><ports><port><role>{role}</role><direction>{direction}</direction><width>1</width></port></ports>'
                    f'<parameters><parameterValueMap><entry><key>synchronousEdges</key><value>{edges}</value></entry></parameterValueMap></parameters></interface>'
                    for name, direction, role, edges in (("in_reset", "Input", "reset_n", expected["SYNCHRONOUS_EDGES"].upper()),
                                                        ("out_reset", "Output", output_role, output_edges))) +
            "</interfaces></boundary></componentDefinition>")):
            ET.SubElement(ET.SubElement(module, "parameter", {"name": name}), "value").text = value
        return module

    def test_actual_logical_view_parameters_and_derived_reset_polarity_edges_are_guarded(self):
        hps = self.fixture.hps.BUILD
        self.assertEqual(B.verify_reset_bridge(self.root, self.reset_fixture(), hps)["parameters"]["ACTIVE_LOW_RESET"], "1")
        deassert = self.reset_fixture({"SYNCHRONOUS_EDGES": "deassert"}, output_edges="DEASSERT")
        self.assertEqual(B.verify_reset_bridge(self.root, deassert, hps, "DEASSERT")["parameters"]["SYNCHRONOUS_EDGES"], "deassert")
        for name, value in (("ACTIVE_LOW_RESET", "0"), ("SYNCHRONOUS_EDGES", "deassert"),
                            ("NUM_RESET_OUTPUTS", "2"), ("USE_RESET_REQUEST", "1")):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "parameters differ"):
                B.verify_reset_bridge(self.root, self.reset_fixture({name: value}), hps)
        with self.assertRaisesRegex(ValueError, "synchronization differs"):
            B.verify_reset_bridge(self.root, self.reset_fixture(output_edges="DEASSERT"), hps)
        with self.assertRaisesRegex(ValueError, "synchronization differs"):
            B.verify_reset_bridge(self.root, self.reset_fixture({"SYNCHRONOUS_EDGES": "deassert"}, output_edges="NONE"), hps, "DEASSERT")
        with self.assertRaisesRegex(ValueError, "polarity differs"):
            B.verify_reset_bridge(self.root, self.reset_fixture(output_role="reset"), hps)
        module = self.reset_fixture()
        module.set("version", "19.1.0")
        with self.assertRaisesRegex(ValueError, "version/kind"):
            B.verify_reset_bridge(self.root, module, hps)

    def test_logical_view_resolution_rejects_duplicate_files_and_escaped_paths(self):
        module = self.reset_fixture()
        duplicate = self.root / "other/ip/reset.ip"
        duplicate.parent.mkdir(parents=True)
        duplicate.write_bytes((self.root / "child/ip/reset.ip").read_bytes())
        with self.assertRaisesRegex(ValueError, "missing or ambiguous"):
            B.referenced_parameters(self.root, module, self.fixture.hps.BUILD)
        module.find("parameter[@name='logicalView']/value").text = "../reset.ip"
        with self.assertRaisesRegex(ValueError, "invalid generated"):
            B.referenced_parameters(self.root, module, self.fixture.hps.BUILD)


if __name__ == "__main__": unittest.main()
