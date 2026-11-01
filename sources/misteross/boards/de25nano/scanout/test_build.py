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

    def prepare(self, revision=None, *, hps_first=False, linux_dma=False, package_build_id=None):
        with mock.patch.object(B, "capture", return_value=self.sources):
            return B.prepare(self.fixture.archive, self.root / "prepared", revision, hps_first=hps_first, linux_dma=linux_dma,
                             package_build_id=package_build_id)

    def test_package_identity_is_inserted_before_generation_with_an_explicit_seed(self):
        selected = "0123456789abcdeffedcba9876543210"
        original_load = B.load
        def load(path, name):
            module = original_load(path, name)
            if name == "scanout_retained_linux_dma":
                # The small upstream fixture has placeholder adapter RTL;
                # its real conversion is exercised by LinuxDMAProfileTests.
                module.configure = mock.Mock(return_value=("# fixture port configuration\n",
                    {"master_ownership": "scanout-only"}, {}))
            return module
        with mock.patch.object(B, "load", side_effect=load):
            output, _, _, _ = self.prepare("a" * 40, hps_first=True, linux_dma=True, package_build_id=selected)
        directory = output / "combined/hps"
        inputs = json.loads((directory / "inputs.json").read_text())
        self.assertEqual(inputs["scanout"]["build_id"], selected)
        self.assertEqual(inputs["scanout"]["build_identity_scheme"], "canonical-functional-record-v2")
        self.assertIn("set_instance_parameter_value fes_scanout BUILD_ID 0x" + selected,
                      (directory / "project/integrate-mailbox.tcl").read_text())
        self.assertIn("set_global_assignment -name SEED 1\n", (directory / "project/golden_top.qsf").read_text())

    def test_package_identity_rejects_wrong_profile_dirty_source_zero_and_invalid_ids(self):
        for revision, linux_dma, value in ((None, True, "a" * 32), ("b" * 40, False, "a" * 32),
                ("b" * 40, True, "0" * 32), ("b" * 40, True, "A" * 32),
                ("b" * 40, True, "a" * 31), ("b" * 40, True, 1)):
            with self.subTest(revision=revision, linux_dma=linux_dma, value=value), self.assertRaises(ValueError):
                self.prepare(revision, hps_first=True, linux_dma=linux_dma, package_build_id=value)

    def test_build_identity_binds_actual_sources_and_profile(self):
        output, _, _, _ = self.prepare("a" * 40, hps_first=True)
        directory = output / "combined/hps"
        inputs = json.loads((directory / "inputs.json").read_text())
        selected = inputs["scanout"]["build_id"]
        self.assertRegex(selected, r"^[0-9a-f]{32}$")
        self.assertIn("set_instance_parameter_value fes_scanout BUILD_ID 0x" + selected,
                      (directory / "project/integrate-mailbox.tcl").read_text())
        self.assertEqual(selected, B.build_identity(dict(reversed(list(self.sources.items()))), True, False))
        self.assertNotEqual(selected, B.build_identity(self.sources, True, True))
        changed = dict(self.sources)
        changed[B.BOARD + "scanout/fes_de25_scanout.v"] += b"\n"
        self.assertNotEqual(selected, B.build_identity(changed, True, False))

    def test_generated_build_identity_refuses_zero_mismatch_and_overflow(self):
        endpoint = ET.Element("module")
        value = ET.SubElement(ET.SubElement(endpoint, "parameter", {"name": "BUILD_ID"}), "value")
        expected = "0123456789abcdeffedcba9876543210"
        for actual in ("0x" + expected, "128'h" + expected, str(int(expected, 16))):
            value.text = actual
            self.assertEqual(B.endpoint_build_identity(self.root, endpoint, None, expected)["build_id"], expected)
        for actual in ("0", "0x1", str(1 << 128), "unknown"):
            value.text = actual
            with self.subTest(actual=actual), self.assertRaises(ValueError):
                B.endpoint_build_identity(self.root, endpoint, None, expected)

    def test_visibility_cdc_cut_targets_only_the_registered_request_to_the_first_d_pin(self):
        sdc = self.sources[B.BOARD + "scanout/clocks.sdc"].decode()
        self.assertIn("set scanout_visible_source [get_registers {fes_video_inst|visible_request}]", sdc)
        self.assertIn(
            "set scanout_visible_head [get_pins -compatibility_mode -nocase -nowarn "
            "{fes_video_inst|visible_sync[0]|d}]", sdc)
        self.assertIn("if {[get_collection_size $scanout_visible_source] != 1 || "
                      "[get_collection_size $scanout_visible_head] != 1}", sdc)
        self.assertIn("set_false_path -from $scanout_visible_source -to $scanout_visible_head", sdc)
        self.assertNotIn("set_false_path -to $scanout_visible_head", sdc)
        self.assertNotIn("set_clock_groups", sdc)

    def test_hdmi_data_and_de_use_registered_visibility_with_lock_assertion(self):
        board = self.sources[B.BOARD + "scanout/fes_de25_scanout_board.v"].decode()
        # One registered source crosses, and only pixel-domain registers clear
        # the synchronizer; lock loss still clears the pixel release at once.
        self.assertIn("always @(posedge system_clk or negedge top_reset_n)\n"
                      "        if (!top_reset_n) visible_request <= 1'b0;\n"
                      "        else visible_request <= first_lock_seen && qualified_lock && !fault && hdmi_ready_sync[1];", board)
        self.assertIn("fes_de25_top_reset visible_release (.clk(pixel_clk), .ninit_done(1'b0),\n"
                      "        .pll_locked(qualified_lock), .reset_n(visible_reset_n));", board)
        self.assertIn("always @(posedge pixel_clk or negedge visible_reset_n)", board)
        self.assertNotIn("negedge qualified_lock", board)
        self.assertNotIn("wire visible_request", board)
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

    def test_guard_sits_before_pixel_crossing_on_independent_system_clock_and_reset(self):
        output, _, _, _ = self.prepare()
        project = output / "combined/hps/project"
        script = (project / "integrate-mailbox.tcl").read_text()
        self.assertIn("add_connection pb_cpu_0.m0 fes_scanout_guard.avs", script)
        self.assertIn("add_connection fes_scanout_guard.avm fes_ctrl_cdc.s0", script)
        self.assertIn("add_connection periph_clk.out_clk fes_scanout_guard.clk", script)
        self.assertIn("add_connection periph_rst_in.out_reset fes_scanout_guard.reset", script)
        self.assertNotIn("add_connection fes_scanout_system_reset.out_reset fes_scanout_guard.reset", script)
        self.assertNotIn("add_connection pb_cpu_0.m0 fes_ctrl_cdc.s0", script)
        self.assertEqual((project / "fes_mailbox/fes_de25_scanout_guard.v").read_bytes(),
                         self.sources[B.BOARD + "scanout/fes_de25_scanout_guard.v"])

    def test_linux_dma_preparation_captures_exclusive_profile_and_renders_real_removal(self):
        original_load = B.load
        def load(path, name):
            module = original_load(path, name)
            if name == "scanout_retained_linux_dma":
                # The upstream fixture has a placeholder custom-IP description;
                # conversion has its own parser tests. Exercise the real retained
                # isolation derivation and caller profile/receipt wiring here.
                module.configure = mock.Mock(return_value=("# fixture port configuration\n",
                    {"master_ownership": "scanout-only"}, {}))
            return module
        with mock.patch.object(B, "load", side_effect=load):
            output, _, _, _ = self.prepare(hps_first=True, linux_dma=True)
        project = output / "combined/hps/project"
        script = (project / "integrate-mailbox.tcl").read_text()
        self.assertNotIn("fes_ddr_probe", script)
        self.assertNotIn("fes_probe_mem", script)
        self.assertIn("remove_instance ext_hps_f2sdram_master", script)
        self.assertIn("subsys_periph.fes_scanout_mem subsys_hps.f2sdram_adapter_axi4_sub", script)
        inputs = json.loads((project.parent / "inputs.json").read_text())
        self.assertEqual(inputs["f2sdram_master_profile"], "scanout-only")
        self.assertEqual(inputs["scanout"]["linux_dma"]["master_ownership"], "scanout-only")
        self.assertIn("removed", inputs["combined_display"]["debug_paths"])

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
        self.assertIn("pb_cpu_0.m0/fes_scanout_guard.avs baseAddress 0x10300", script)
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
        sdc_path.write_text(original_sdc.replace("fes_video_inst|visible_release|sync_regs*|aclr", "fes_video_inst|missing_release|sync_regs*|aclr", 1))
        with self.assertRaisesRegex(ValueError, "does not constrain the visibility release clear pin"):
            B.verify_reset_synchronizers(project)
        sdc_path.write_text(original_sdc.replace("set_false_path -from $scanout_visible_source -to $scanout_visible_head", "set_false_path -to $scanout_visible_head", 1))
        with self.assertRaisesRegex(ValueError, "cut only the registered visibility request"):
            B.verify_reset_synchronizers(project)
        for before, after in (
            ("fes_video_inst|hdmi_ready_sync[0]|d", "fes_video_inst|hdmi_ready_sync[1]|d"),
            ("set_false_path -from $scanout_ready_source -to $scanout_ready_head", "set_false_path -to $scanout_ready_head"),
        ):
            sdc_path.write_text(original_sdc.replace(before, after, 1))
            with self.subTest(after=after), self.assertRaisesRegex(ValueError, "HDMI-ready source to stage0 D"):
                B.verify_reset_synchronizers(project)
        sdc_path.write_text(original_sdc.replace("$scanout_clear_count != 10", "$scanout_clear_count != 8", 1))
        with self.assertRaisesRegex(ValueError, "ten reset/visibility clear pins"):
            B.verify_reset_synchronizers(project)
        for cut in ("fes_video_inst|video_active|aclr", "fes_video_inst|visible_sync[1]|aclr"):
            sdc_path.write_text(original_sdc.replace("fes_video_inst|visible_release|sync_regs*|aclr}]", "fes_video_inst|visible_release|sync_regs*|aclr " + cut + "}]", 1))
            with self.subTest(cut=cut), self.assertRaisesRegex(ValueError, "data/DE gate clears timed"):
                B.verify_reset_synchronizers(project)
        board_path.write_text(original_board.replace(
            "always @(posedge pixel_clk or negedge visible_reset_n)",
            "always @(posedge pixel_clk or negedge qualified_lock)", 1))
        with self.assertRaisesRegex(ValueError, "registered source and a pixel-domain reset release"):
            B.verify_reset_synchronizers(project)
        board_path.write_text(original_board)
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
        self.assertIn("if {$scanout_clear_count != 10}", sdc)
        cuts = re.findall(r"set scanout_(?:aclr|clrn) \[get_pins [^{]*\{[^}]*\}\]", sdc)
        self.assertEqual(len(cuts), 2)
        for pin, cut in zip(("aclr", "clrn"), cuts):
            self.assertIn(f"fes_video_inst|visible_release|sync_regs*|{pin}", cut)
        self.assertNotIn("visible_sync", "".join(cuts))
        self.assertNotIn("video_active", "".join(cuts))
        self.assertIn("if {[get_collection_size $scanout_visible_clear] != 2}", sdc)
        self.assertIn("if {[get_collection_size $scanout_gate_clear] != 1}", sdc)
        self.assertNotIn("set_false_path -to $scanout_visible_clear", sdc)
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

    def test_hps_first_is_captured_without_changing_loader_or_dma_admission(self):
        output, _, hps, _ = self.prepare("a" * 40, hps_first=True)
        directory = output / "combined/hps"
        inputs = json.loads((directory / "inputs.json").read_text())
        qsf = (directory / "project/golden_top.qsf").read_text()
        self.assertEqual(qsf.count('set_global_assignment -name HPS_INITIALIZATION "HPS FIRST"'), 1)
        self.assertEqual(inputs["scanout"]["hps_initialization"], "HPS FIRST")
        self.assertFalse(inputs["scanout"]["default_enabled"])
        self.assertIn("not-qualified", inputs["scanout"]["dma_admission"])
        self.assertFalse((directory / "linux-programming.json").exists())
        with self.assertRaisesRegex(ValueError, "must be boolean"):
            B.prepare(self.fixture.archive, self.root / "invalid", hps_first=1)
        self.assertFalse((self.root / "invalid").exists())

    def test_hps_first_compile_binds_linux_phases_without_programming(self):
        output, _, hps, display = self.prepare("a" * 40, hps_first=True)
        directory = output / "combined/hps"
        project = directory / "project"
        (directory / "generation.json").write_text("{}\n")
        artifact = project / "output_files/golden_top.sof"
        artifact.parent.mkdir()
        artifact.write_bytes(b"synthetic-not-a-bitstream")
        def compile_stub(*args):
            (directory / "build.json").write_text("{}\n")
            return artifact
        calls = []
        def convert(argv, cwd, log):
            self.assertEqual(Path(argv[0]).name, "quartus_pfg")
            self.assertEqual(cwd, project)
            calls.append(list(map(str, argv)))
            if len(calls) == 1:
                Path(argv[3]).write_bytes(b"synthetic-complete")
            else:
                (directory / "linux-boot.hps.jic").write_bytes(b"synthetic-boot")
                (directory / "linux-boot.core.rbf").write_bytes(b"synthetic-core")
        with mock.patch.object(hps, "compile_project", side_effect=compile_stub), \
             mock.patch.object(hps, "run_logged", side_effect=convert):
            self.assertEqual(B.compile_project(output, hps, display, Path("/tools/quartus_sh")), artifact)
        receipt = json.loads((directory / "linux-programming.json").read_text())
        self.assertEqual(len(calls), 2)
        self.assertEqual(receipt["fes_revision"], "a" * 40)
        self.assertEqual(receipt["hps_initialization"], "HPS FIRST")
        self.assertEqual(len(receipt["scanout_source_sha256"]), 64)
        self.assertEqual(receipt["inputs_sha256"], hps.digest(directory / "inputs.json"))
        self.assertEqual(receipt["generation_sha256"], hps.digest(directory / "generation.json"))
        self.assertEqual(receipt["f2sdram_master_profile"], "diagnostic")
        self.assertIsNone(receipt["linux_dma"])
        self.assertEqual(receipt["hardware_programming"], "not-performed")
        self.assertFalse(receipt["production_admission"])
        for name, item in receipt["artifacts"].items():
            self.assertEqual(item["sha256"], hps.digest(directory / name))

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


class LinuxDmaTests(unittest.TestCase):
    def setUp(self):
        self.helper = B.load(B.HERE / "linux_dma.py", "scanout_linux_dma_tests")
        self.definition = (B.HERE / "generated/fes_de25_scanout.vh").read_text()
        self.source = "# vendor prefix\n" + "".join(
            f"add_interface_port axi4_man man_{c}user {c}user Output 8\n"
            + r"output wire \[7:0\]   man_" + c + "user,\n"
            + f"assign man_{c}user = 8'hE0;\n" for c in ("ar", "aw")) + "".join(
            f"set_interface_property {interface} {field} 1\n"
            for interface, fields in (("axi4_man", ("readIssuingCapability", "combinedIssuingCapability")),
                                      ("axi4_sub", ("readAcceptanceCapability", "combinedAcceptanceCapability")))
            for field in fields) + "# vendor suffix\n"

    def test_exact_packing_preserves_vendor_attributes_and_nonsecure_context(self):
        data, profile = self.helper.retarget_adapter(self.source.encode(), self.definition)
        text = data.decode()
        self.assertEqual(profile["stream_id"], 13)
        self.assertFalse(profile["secure"])
        self.assertEqual(profile["read_transactions"], 16)
        self.assertEqual(text.count("Capability 16\n"), 4)
        self.assertEqual(text.count("25'h0001ae0"), 2)
        # Independently decode the raw HPS port, rather than using generated
        # shift constants to check the same packing expression against itself.
        words = re.findall(r"assign man_(?:ar|aw)user = 25'h([0-9a-f]+);", text)
        self.assertEqual(len(words), 2)
        for encoded in words:
            word = int(encoded, 16)
            self.assertEqual((word >> 9) & 0xffff, 13)
            self.assertEqual((word >> 8) & 1, 0)
            self.assertEqual(word & 0xff, 0xe0)
        # The previous CSR-derived encoding actually selects secure stream 6.
        self.assertEqual((0x0000de0 >> 9) & 0xffff, 6)
        self.assertEqual((0x0000de0 >> 8) & 1, 1)
        self.assertEqual(text.count("Output 25"), 2)
        self.assertEqual(text.count(r"\[24:0\]"), 2)
        self.assertTrue(text.startswith("# vendor prefix\n") and text.endswith("# vendor suffix\n"))
        self.assertEqual(profile["kernel_binding"], "not-admitted")

    def test_changed_vendor_ports_attributes_or_duplicate_assignment_are_refused(self):
        for source in (self.source.replace("8'hE0", "8'h00", 1),
                       self.source.replace("Output 8", "Output 16", 1),
                       self.source + "assign man_aruser = 8'hE0;\n",
                       self.source.replace("readIssuingCapability 1", "readIssuingCapability 2"),
                       self.source + "set_interface_property axi4_sub readAcceptanceCapability 1\n"):
            with self.assertRaises(ValueError):
                self.helper.retarget_adapter(source.encode(), self.definition)

    def test_reserved_sid_and_changed_layout_are_refused(self):
        for definition in (self.definition.replace("SMMU_STREAM_ID 32'h0000000d", "SMMU_STREAM_ID 32'h0000000a"),
                           self.definition.replace("SMMU_SID_SHIFT 32'h00000009", "SMMU_SID_SHIFT 32'h00000008"),
                           self.definition.replace("SMMU_SECURE_BIT 32'h00000008", "SMMU_SECURE_BIT 32'h00000018")):
            with self.assertRaises(ValueError):
                self.helper.retarget_adapter(self.source.encode(), definition)

    def test_isolation_removes_only_probe_and_memory_debug_path_and_refuses_source_drift(self):
        hps_script = (B.HERE.parent / "hps/integrate.tcl").read_text()
        script = hps_script.replace("@PROBE_BASE@", "0x10100").replace("@TOP_SYSTEM@", "top.qsys")
        derived = self.helper.isolate_script(script, "save_system top.qsys\n")
        self.assertNotIn("fes_ddr_probe", derived)
        self.assertNotIn("fes_probe_mem", derived)
        self.assertIn("remove_instance ext_hps_f2sdram_master", derived)
        self.assertIn("add_instance fes_mailbox fes_de25_mailbox 1.0", derived)
        for changed in (script.replace("fes_de25_ddr_probe 1.0", "fes_de25_ddr_probe 1X0"),
                        script.replace("baseAddress 0x10100", "baseAddress arbitrary"),
                        script + "save_system top.qsys\n"):
            with self.assertRaises(ValueError): self.helper.isolate_script(changed, "save_system top.qsys\n")

    def test_linux_dma_requires_hps_first_before_output_creation(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "no-output"
            with self.assertRaisesRegex(ValueError, "requires explicit HPS-first"):
                B.prepare(Path(temp) / "missing.zip", output, linux_dma=True)
            self.assertFalse(output.exists())
