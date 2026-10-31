"""Actual upstream composition, retained-source and fail-closed mutation guards."""
import importlib.util
import json
from pathlib import Path
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

    def prepare(self, revision=None, *, hps_first=False):
        with mock.patch.object(B, "capture", return_value=self.sources):
            return B.prepare(self.fixture.archive, self.root / "prepared", revision, hps_first=hps_first)

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
        self.assertIn("if {$scanout_clear_count != 8}", sdc)
        self.assertNotIn("fes_scanout_pixel_reset|sync_regs*", sdc)
        self.assertNotIn("fes_scanout_system_reset|sync_regs*", sdc)

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
            + f"assign man_{c}user = 8'hE0;\n" for c in ("ar", "aw")) + "# vendor suffix\n"

    def test_exact_packing_preserves_vendor_attributes_and_nonsecure_context(self):
        data, profile = self.helper.retarget_adapter(self.source.encode(), self.definition)
        text = data.decode()
        self.assertEqual(profile["stream_id"], 13)
        self.assertFalse(profile["secure"])
        self.assertEqual(text.count("25'h0000de0"), 2)
        self.assertEqual(text.count("Output 25"), 2)
        self.assertEqual(text.count(r"\[24:0\]"), 2)
        self.assertTrue(text.startswith("# vendor prefix\n") and text.endswith("# vendor suffix\n"))
        self.assertEqual(profile["kernel_binding"], "not-admitted")

    def test_changed_vendor_ports_attributes_or_duplicate_assignment_are_refused(self):
        for source in (self.source.replace("8'hE0", "8'h00", 1),
                       self.source.replace("Output 8", "Output 16", 1),
                       self.source + "assign man_aruser = 8'hE0;\n"):
            with self.assertRaises(ValueError):
                self.helper.retarget_adapter(source.encode(), self.definition)

    def test_reserved_sid_and_changed_layout_are_refused(self):
        for definition in (self.definition.replace("SMMU_STREAM_ID 32'h0000000d", "SMMU_STREAM_ID 32'h0000000a"),
                           self.definition.replace("SMMU_SID_SHIFT 32'h00000008", "SMMU_SID_SHIFT 32'h00000009")):
            with self.assertRaises(ValueError):
                self.helper.retarget_adapter(self.source.encode(), definition)

    def test_linux_dma_requires_hps_first_before_output_creation(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "no-output"
            with self.assertRaisesRegex(ValueError, "requires explicit HPS-first"):
                B.prepare(Path(temp) / "missing.zip", output, linux_dma=True)
            self.assertFalse(output.exists())
