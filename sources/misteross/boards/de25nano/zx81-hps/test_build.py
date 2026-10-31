"""Focused source, PLL and generated-topology guards for the HPS ZX81 lane."""
import hashlib
import importlib.util
import io
import json
from contextlib import redirect_stderr
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25_hps_zx81_builder", HERE / "build.py")
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)
HPS = B.load(HERE.parent / "hps/build.py", "de25_hps_zx81_test_helpers")


class HpsZX81BuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="fes-de25-zx81-hps-build-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def test_tape_ram_adaptation_preserves_the_entire_native_branch(self):
        source = B.ROOT / "sources/misteross/cores/fes-zx81/rtl/zx81_tape_store.v"
        original = source.read_bytes()
        replacement = (B.HERE / "tape_store.v").read_bytes()
        adapted, record = B.retarget_tape(original)
        self.assertEqual(adapted, replacement)
        self.assertEqual(adapted.partition(b"\n`else\n")[2], original + b"`endif\n")
        self.assertEqual(record["source_sha256"], hashlib.sha256(original).hexdigest())
        self.assertEqual(record["derived_sha256"], hashlib.sha256(adapted).hexdigest())
        for changed in (original + b"\n", original.replace(b"read_data", b"different_data")):
            with self.assertRaisesRegex(ValueError, "adaptation contract changed"):
                B.retarget_tape(changed)
        with self.assertRaisesRegex(ValueError, "adaptation contract changed"):
            B.retarget_tape(original, replacement + b"\n`else\n")

    def test_capture_selects_only_the_owned_board_renderer(self):
        sources = B.capture()
        self.assertIn(B.BOARD + "zx81-hps/video.v", sources)
        self.assertNotIn("sources/misteross/cores/fes-zx81/rtl/zx81_video_720p.v", sources)
        definition = sources[B.BOARD + "zx81-hps/fes_de25_computer_hw.tcl"].decode()
        self.assertIn("add_fileset_file video.v VERILOG PATH video.v", definition)
        self.assertNotIn("zx81_video_720p_de25", definition)

    def test_patch_anchor_must_exist_exactly_once(self):
        self.assertEqual(B.replace_once("before ANCHOR after", "ANCHOR", "replacement"),
                         "before replacement after")
        for source in ("missing", "ANCHOR duplicate ANCHOR"):
            with self.subTest(source=source), self.assertRaisesRegex(ValueError, "patch target changed"):
                B.replace_once(source, "ANCHOR", "replacement")

    def test_capture_contains_current_mailbox_machine_hid_tape_and_generated_consumers(self):
        sources = B.capture()
        required = {
            "sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v",
            *["sources/misteross/cores/fes-zx81/rtl/" + name for name in
              ("zx81_keyboard.v", "zx81_tape_store.v", "zx81_machine.sv", "zx81_machine_clock.v")],
            *[B.BOARD + "zx81-hps/generated/" + name for name in
              ("fes_de25_computer.vh", "fes_de25_computer.hpp", "fes_computer.vh")],
        }
        self.assertTrue(required <= sources.keys())
        self.assertNotIn("sources/misteross/cores/fes-zx81/rtl/fes_computer_gp.v", sources)
        for name in required:
            self.assertEqual(sources[name], (B.ROOT / name).read_bytes())

    def test_hdmi_readiness_has_registered_sources_and_only_first_stage_cuts(self):
        files = B.capture()
        sdc = files[B.BOARD + "zx81-hps/clocks.sdc"].decode()
        board = files[B.BOARD + "zx81-hps/board.v"].decode()
        self.assertIn("reg video_ready_source", board)
        self.assertIn("assign video_ready = video_ready_source;", board)
        self.assertIn("{fes_video_inst|hdmi_config|READY} {fes_video_inst|hdmi_ready_sync[0]} {fes_video_inst|hdmi_ready_sync[1]}", sdc)
        self.assertIn("{fes_video_inst|video_ready_source} {soc_inst|subsys_periph|fes_computer|fes_computer|video_ready_sync[0]}", sdc)
        self.assertIn('"${ready_stage0}|D"', sdc)
        self.assertIn("set_false_path -from $ready_source_reg -to $ready_stage0_d", sdc)
        self.assertIn('if {[get_collection_size $ready_nodes] != 1}', sdc)
        self.assertNotIn("set_false_path -to $ready_stage1_reg", sdc)
        self.assertNotIn("set_clock_groups", sdc)

    def test_readiness_cdc_cut_is_narrow_and_same_domain_visibility_stays_timed(self):
        sdc = B.capture()[B.BOARD + "zx81-hps/clocks.sdc"].decode()
        self.assertIn("{soc_inst|subsys_periph|fes_computer|fes_computer|pixel_transport_ready_sync[0]|D}", sdc)
        self.assertIn("set_false_path -to $computer_pixel_ready_stage0_d", sdc)
        self.assertIn("if {$computer_pixel_ready_clear_count != 2}", sdc)
        self.assertIn("pixel_transport_reset_release[0]|aclr", sdc)
        self.assertIn("pixel_transport_reset_release[1]|aclr", sdc)
        self.assertIn("Stage-to-stage paths and both generated clocks stay timed", sdc)
        # The board's pixel_reset_n is released by a pixel-clock chain whose
        # clear pins are already cut, so its visibility synchronizer D path is
        # same-domain and must remain timed.
        self.assertNotIn("visible_sync", sdc)
        self.assertNotIn("transport_ready_meta", sdc)
        self.assertNotIn("set_clock_groups", sdc)

    def test_frame_ownership_constraints_and_reset_pin_counts_match_rtl(self):
        sdc = B.capture()[B.BOARD + "zx81-hps/clocks.sdc"].decode()
        self.assertIn("if {$computer_clear_count != 4}", sdc)
        self.assertIn("request {*|video|request} {*|video|request_sync[0]}", sdc)
        self.assertIn("acknowledge {*|video|acknowledge} {*|video|acknowledge_sync[0]}", sdc)
        self.assertIn("if {[get_collection_size $owner_rows_source] != 9 || [get_collection_size $owner_rows_sink] != 9}", sdc)
        self.assertIn("if {[get_collection_size $owner_reset_pins] != 4}", sdc)
        self.assertIn("sys_release*|aclr", sdc)
        self.assertIn("pix_release*|aclr", sdc)
        self.assertNotIn("set_clock_groups", sdc)

    def test_historical_quartus_report_is_not_presented_as_current_head_evidence(self):
        record = B.ROOT / "sources/misteross/docs/validation/2026-10-29-de25-zx81-hps"
        producer_inputs = json.loads((record / "compiled-5a7382c4b5/reports/inputs.json").read_text())
        selected = producer_inputs["functional_snapshot_sha256"]
        drifted = {
            name for name, recorded in selected.items()
            if not (B.ROOT / name).is_file() or
            hashlib.sha256((B.ROOT / name).read_bytes()).hexdigest() != recorded
        }
        self.assertTrue({
            B.BOARD + "zx81-hps/board.v",
            B.BOARD + "zx81-hps/clocks.sdc",
            B.BOARD + "zx81-hps/fes_de25_computer.v",
        } <= drifted)
        report = " ".join((record / "README.md").read_text().split())
        self.assertIn("**Evidence status:** historical producer", report)
        self.assertIn("not compiled from the current branch", report)
        self.assertIn("not valid for SRAM testing the current source", report)
        self.assertIn("does not validate the current HPS or scanout false-path selectors", report)
        self.assertNotIn("Later evidence commits do not change the producer's 41 selected input files", report)
        manifest = record / "SHA256SUMS"
        for line in manifest.read_text().splitlines():
            digest, name = line.split("  ", 1)
            self.assertEqual(hashlib.sha256((record / name).read_bytes()).hexdigest(), digest, name)

    def test_simulation_receipts_are_scoped_to_historical_sources(self):
        record = B.ROOT / "sources/misteross/docs/validation/2026-10-29-de25-zx81-hps/simulation"
        source_head = "d715651b4171da7a69fa0d896dc1bd67168dd064"
        reference = "786e6383b32617af8baeb32e9de6cf99231a19f9"
        reference_leaf = "26ce1e51454000f2762045cc442fbbae881404a98b94334eea8ba11e43a9ad0a"
        leaf_path = B.BOARD + "zx81-hps/fes_de25_computer.v"
        self.assertNotEqual(hashlib.sha256((B.ROOT / leaf_path).read_bytes()).hexdigest(), reference_leaf)

        inputs = json.loads((record / "input-hashes.json").read_text())
        self.assertEqual(inputs["reference_commit"], reference)
        self.assertIn(source_head, inputs["source_scope"])
        self.assertNotIn("functional_current_sources", inputs)
        recorded = inputs["functional_reference_sources"]
        self.assertEqual(len(recorded), 22)
        drifted = {
            item["path"] for item in recorded
            if hashlib.sha256((B.ROOT / item["path"]).read_bytes()).hexdigest() != item["sha256"]
        }
        self.assertTrue({
            leaf_path,
            B.BOARD + "zx81-hps/computer_tb.cpp",
            B.BOARD + "zx81-hps/sim.py",
            B.BOARD + "zx81-hps/build.py",
        } <= drifted)

        report = " ".join((record / "README.md").read_text().split())
        self.assertIn(f"PR #1232 source head `{source_head}`", report)
        self.assertIn("18 of those 22 files still matched", report)
        self.assertIn("does not exercise the current leaf", report)
        self.assertNotIn("22 matching current input files", report)
        for changed_name in ("fes_de25_computer.v", "computer_tb.cpp", "sim.py", "build.py"):
            self.assertIn(f"`{changed_name}`", report)

        leaf_binding = json.loads((record / "leaf-build-id-binding.json").read_text())
        self.assertEqual(leaf_binding["guarded_reference_leaf_sha256"], reference_leaf)
        self.assertNotIn("current_leaf_sha256", leaf_binding)
        self.assertIn("not the current PR leaf", leaf_binding["evidence_scope"])
        elaboration = json.loads((record / "default-elaboration-binding.json").read_text())
        self.assertEqual(elaboration["elaborated_reference_leaf_sha256"],
                         leaf_binding["guarded_reference_leaf_sha256"])
        self.assertNotIn("current_leaf_sha256", elaboration)
        self.assertIn("does not describe the current PR leaf", elaboration["evidence_scope"])
        for directory in (record, record / "repair-binding"):
            for line in (directory / "SHA256SUMS").read_text().splitlines():
                digest, name = line.split("  ", 1)
                self.assertEqual(hashlib.sha256((directory / name).read_bytes()).hexdigest(), digest, name)

    def test_selected_revision_capture_reads_git_blobs_instead_of_working_files(self):
        sources = B.capture()
        selected = "b" * 40
        target = "sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v"
        frozen = dict(sources)
        frozen[target] += b"\n// selected Git blob differs from working file\n"
        calls = []
        def git_blob(command, *, cwd):
            self.assertEqual(command[:2], ["git", "show"])
            self.assertEqual(cwd, B.ROOT)
            self.assertTrue(command[2].startswith(selected + ":"))
            path = command[2][len(selected) + 1:]
            calls.append(path)
            return frozen[path]
        with mock.patch.object(B.subprocess, "check_output", side_effect=git_blob):
            captured = B.capture(selected)
        self.assertEqual(captured, frozen)
        self.assertEqual(set(calls), sources.keys())
        self.assertEqual(len(calls), len(sources))
        self.assertNotEqual(captured[target], (B.ROOT / target).read_bytes())

    def test_dirty_generation_and_build_refuse_before_preparation_or_tools(self):
        dirty = {"base_revision": "b" * 40, "source_classification": "working-copy-diagnostic"}
        for action in ("generate", "build"):
            with self.subTest(action=action), mock.patch.object(B, "identity", return_value=dirty), \
                    mock.patch.object(B, "prepare") as prepare, redirect_stderr(io.StringIO()) as error:
                with self.assertRaises(SystemExit) as caught:
                    B.main([action, "--vendor-archive", str(self.root / "missing.zip")])
                self.assertEqual(caught.exception.code, 1)
                self.assertIn("requires clean committed source", error.getvalue())
                prepare.assert_not_called()

    def linux_fixture(self, order="HPS FIRST"):
        directory = self.root / "hps"
        project = directory / "project"
        boot = project / "software/u-boot/spl/u-boot-spl-dtb.hex"
        boot.parent.mkdir(parents=True)
        boot.write_bytes(b"locked-vendor-loader-fixture")
        (project / "golden_top.qsf").write_text(
            f'set_global_assignment -name HPS_INITIALIZATION "{order}"\n')
        (directory / "inputs.json").write_text(json.dumps({
            "fes_revision": "a" * 40,
            "computer": {"hps_initialization": order, "build_id": "b" * 32}}))
        artifact = project / "fixture.sof"
        artifact.write_bytes(b"synthetic-not-a-bitstream")
        return directory, project, artifact

    def test_linux_packaging_refuses_fpga_first_before_tools(self):
        _, _, artifact = self.linux_fixture("AFTER INIT_DONE")
        with mock.patch.object(HPS, "run_logged") as run:
            with self.assertRaisesRegex(ValueError, "requires captured HPS-first"):
                B.package_hps_first(self.root, HPS, Path("/tool/quartus_sh"), artifact)
            run.assert_not_called()

    def test_linux_packaging_checks_project_mode_before_tools(self):
        _, project, artifact = self.linux_fixture()
        with (project / "golden_top.qsf").open("a") as stream:
            stream.write('set_global_assignment -name HPS_INITIALIZATION "AFTER INIT_DONE"\n')
        with mock.patch.object(HPS, "run_logged") as run:
            with self.assertRaisesRegex(ValueError, "assignment differs"):
                B.package_hps_first(self.root, HPS, Path("/tool/quartus_sh"), artifact)
            run.assert_not_called()

    def test_linux_packaging_refuses_stale_phase_artifacts_before_tools(self):
        directory, _, artifact = self.linux_fixture()
        (directory / "linux-boot.core.rbf").write_bytes(b"stale-not-a-bitstream")
        with mock.patch.object(HPS, "run_logged") as run:
            with self.assertRaisesRegex(ValueError, "output already exists"):
                B.package_hps_first(self.root, HPS, Path("/tool/quartus_sh"), artifact)
            run.assert_not_called()

    def test_linux_packaging_binds_phase_artifacts_without_programmer(self):
        directory, project, artifact = self.linux_fixture()
        calls = []
        def run(argv, cwd, log):
            self.assertEqual(Path(argv[0]).name, "quartus_pfg")
            self.assertEqual(cwd, project)
            calls.append(list(map(str, argv)))
            if len(calls) == 1:
                Path(argv[3]).write_bytes(b"synthetic-complete")
            else:
                self.assertIn("hps=on", argv)
                (directory / "linux-boot.hps.jic").write_bytes(b"synthetic-phase1")
                (directory / "linux-boot.core.rbf").write_bytes(b"synthetic-phase2")
        with mock.patch.object(HPS, "run_logged", side_effect=run):
            B.package_hps_first(self.root, HPS, Path("/tool/quartus_sh"), artifact)
        receipt = json.loads((directory / "linux-programming.json").read_text())
        self.assertEqual(len(calls), 2)
        self.assertEqual(receipt["hardware_programming"], "not-performed")
        self.assertFalse(receipt["production_admission"])
        for name, item in receipt["artifacts"].items():
            self.assertEqual(item["sha256"], HPS.digest(directory / name))
        self.assertEqual(len(receipt["artifacts"]), 3)

    def test_linux_packaging_does_not_publish_missing_split_outputs(self):
        directory, _, artifact = self.linux_fixture()
        with mock.patch.object(HPS, "run_logged"):
            with self.assertRaisesRegex(ValueError, "missing regular nonempty"):
                B.package_hps_first(self.root, HPS, Path("/tool/quartus_sh"), artifact)
        self.assertFalse((directory / "linux-programming.json").exists())

    def pll(self, name="pixel", rate=74250000, m=297, n=5, c=40):
        folder = self.root / name
        folder.mkdir(exist_ok=True)
        xml = folder / (name + ".xml")
        xml.write_text(f'<system><perimeter><interface name="outclk0">'
                       f'<property name="clockRate" value="{rate}"/></interface></perimeter>'
                       '<entity><parameter name="gui_actual_phase_shift0" value="0"/>'
                       '<parameter name="gui_actual_duty_cycle0" value="50"/></entity></system>')
        hdl = folder / "altera_iopll_2110/synth/atom.v"
        hdl.parent.mkdir(parents=True, exist_ok=True)
        parameters = {"fb_clk_m_div": m, "ref_clk_n_div": n, "out_clk_0_c_div": c,
                      "fb_clk_fractional_div_num": 1, "fb_clk_fractional_div_den": 1,
                      "fb_clk_fractional_div_value": 1}
        hdl.write_text("\n".join(f".{key}({value})" for key, value in parameters.items()) +
                       f"\n.out_clk_0_freq(36'd{rate})\n")
        return folder, xml, hdl

    def test_generated_100_and_74_25_mhz_plls_bind_actual_dividers_and_bytes(self):
        for name, rate, m, n, c in (("system", 100000000, 20, 1, 10), ("pixel", 74250000, 297, 5, 40)):
            with self.subTest(name=name):
                folder, xml, hdl = self.pll(name, rate, m, n, c)
                result = B.pll_review(folder, rate)
                self.assertEqual(result["clock_hz"], rate)
                self.assertEqual(result["counters"]["fb_clk_m_div"], m)
                self.assertEqual(result["xml_sha256"], hashlib.sha256(xml.read_bytes()).hexdigest())
                self.assertEqual(result["hdl_sha256"], hashlib.sha256(hdl.read_bytes()).hexdigest())
                hdl.write_text(hdl.read_text() + "// changed producer bytes\n")
                self.assertNotEqual(B.pll_review(folder, rate)["hdl_sha256"], result["hdl_sha256"])

    def test_generated_pll_rejects_rounded_missing_duplicate_or_fractional_contracts(self):
        folder, xml, hdl = self.pll()
        original_xml, original_hdl = xml.read_text(), hdl.read_text()
        corruptions = (
            ("rate", lambda: xml.write_text(original_xml.replace("74250000", "74249999")), "PLL rate differs"),
            ("missing rate", lambda: xml.write_text("<system/>"), "PLL rate differs"),
            ("phase", lambda: xml.write_text(original_xml.replace('phase_shift0" value="0"', 'phase_shift0" value="1"')), "phase/duty differs"),
            ("duty", lambda: xml.write_text(original_xml.replace('duty_cycle0" value="50"', 'duty_cycle0" value="49.5"')), "phase/duty differs"),
            ("missing phase", lambda: xml.write_text(original_xml.replace('<parameter name="gui_actual_phase_shift0" value="0"/>', '')), "phase/duty differs"),
            ("rounded atom", lambda: hdl.write_text(original_hdl.replace("m_div(297)", "m_div(296)")), "silently rounded"),
            ("missing counter", lambda: hdl.write_text(original_hdl.replace(".ref_clk_n_div(5)", "")), "counter contract changed"),
            ("zero counter", lambda: hdl.write_text(original_hdl.replace("c_div(40)", "c_div(0)")), "counter contract changed"),
            ("duplicate counter", lambda: hdl.write_text(original_hdl + ".fb_clk_m_div(297)\n"), "counter contract changed"),
            ("fraction", lambda: hdl.write_text(original_hdl.replace("fractional_div_num(1)", "fractional_div_num(2)")), "fractional PLL"),
            ("wrong frequency metadata", lambda: hdl.write_text(original_hdl.replace("36'd74250000", "36'd74249999")), "frequency metadata differs"),
            ("missing frequency metadata", lambda: hdl.write_text(original_hdl.replace(".out_clk_0_freq(36'd74250000)", "")), "frequency metadata differs"),
        )
        for label, change, message in corruptions:
            xml.write_text(original_xml)
            hdl.write_text(original_hdl)
            with self.subTest(label=label):
                change()
                with self.assertRaisesRegex(ValueError, message):
                    B.pll_review(folder, 74250000)
        xml.write_text(original_xml)
        hdl.write_text(original_hdl)
        hdl.with_name("another.v").write_text(original_hdl)
        with self.assertRaisesRegex(ValueError, "atom changed"):
            B.pll_review(folder, 74250000)

    def sopc(self):
        project = self.root / "project"
        handoff = project / "qsys_top/qsys_top.sopcinfo"
        handoff.parent.mkdir(parents=True, exist_ok=True)
        definition_path = project / "fes_mailbox/generated/fes_de25_computer.vh"
        definition_path.parent.mkdir(parents=True, exist_ok=True)
        definition_path.write_bytes((HERE / "generated/fes_de25_computer.vh").read_bytes())
        root = ET.Element("Ensemble")
        module = ET.SubElement(root, "module", {"path": "subsys_periph.fes_computer", "kind": "fes_de25_computer", "version": "1.1"})
        definition = ET.Element("component")
        boundary = ET.SubElement(ET.SubElement(definition, "boundary"), "interfaces")
        point = ET.SubElement(boundary, "interface")
        ET.SubElement(point, "name").text = "avs"
        ports = ET.SubElement(point, "ports")
        for role, direction, width in (("address", "Input", 5), ("read", "Input", 1), ("write", "Input", 1),
                ("byteenable", "Input", 4), ("writedata", "Input", 32), ("readdata", "Output", 32),
                ("readdatavalid", "Output", 1), ("waitrequest", "Output", 1),
                ("writeresponsevalid", "Output", 1), ("response", "Output", 2)):
            port = ET.SubElement(ports, "port")
            for key, value in (("role", role), ("direction", direction), ("width", str(width))):
                ET.SubElement(port, key).text = value
        item = ET.SubElement(module, "parameter", {"name": "componentDefinition"})
        ET.SubElement(item, "value").text = ET.tostring(definition, encoding="unicode")
        mapping = ET.Element("address-map")
        for name, start, span in (("sysid.control_slave", 0x10000, 8), ("fes_mailbox.avs", 0x100a0, 32),
                                  ("fes_ddr_probe.avs", 0x10100, 128), ("fes_computer.avs", 0x10300, 32)):
            ET.SubElement(mapping, "slave", {"name": "subsys_periph." + name, "start": hex(start), "end": hex(start + span)})
        connections = (
            ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_computer.avs", {"baseAddress": "0x10300"}),
            ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_computer.clk", {"clockRateSysInfo": "100000000"}),
            ("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_computer.reset", {}),
            ("clock", "subsys_periph_fes_computer_pixel_clk.out_clk", "subsys_periph_fes_computer.pixel_clk", {"clockRateSysInfo": "74250000"}),
            ("avalon", "subsys_hps_agilex_hps.lwhps2fpga", "subsys_periph_pb_cpu_0.s0",
             {"addressMapSysInfo": ET.tostring(mapping, encoding="unicode")}),
        )
        for kind, start, end, parameters in connections:
            connection = ET.SubElement(root, "connection", {"kind": kind, "start": start, "end": end})
            for key, value in parameters.items():
                item = ET.SubElement(connection, "parameter", {"name": key})
                ET.SubElement(item, "value").text = value
        jtag_mapping = ET.fromstring(ET.tostring(mapping, encoding="unicode"))
        for slave in jtag_mapping:
            for address in ("start", "end"):
                slave.set(address, hex(int(slave.get(address), 0) + 0x100000))
        jtag = ET.SubElement(root, "connection", {"kind": "avalon", "start": "subsys_debug_fpga_m.master",
                                                "end": "subsys_periph_pb_cpu_0.s0"})
        for name, value in (("baseAddress", "0x100000"), ("addressMapSysInfo", ET.tostring(jtag_mapping, encoding="unicode"))):
            item = ET.SubElement(jtag, "parameter", {"name": name})
            ET.SubElement(item, "value").text = value
        ET.ElementTree(root).write(handoff)
        return project, handoff, root

    def test_generated_computer_route_and_inherited_slave_map_are_checked(self):
        project, _, _ = self.sopc()
        result = B.verify_computer(project, HPS)
        self.assertEqual(result["clock_hz"], 100000000)
        self.assertEqual(result["pixel_hz"], 74250000)
        self.assertEqual(result["physical_mmio"], "not-established")
        computer = next(row for row in result["peripheral_slave_map"] if row["name"].endswith("fes_computer.avs"))
        self.assertEqual((computer["base"], computer["span"]), (0x10300, 32))
        self.assertEqual(result["jtag_computer_base"], 0x110300)

    def test_generated_computer_missing_routes_wrong_clock_base_or_overlapping_slave_fail(self):
        for index in range(4):
            project, handoff, root = self.sopc()
            root.remove(root.findall("connection")[index])
            ET.ElementTree(root).write(handoff)
            with self.subTest(missing_route=index), self.assertRaisesRegex(ValueError, "route differs"):
                B.verify_computer(project, HPS)
        for route, field, value, message in ((0, "baseAddress", "0x10304", "base differs"),
                (1, "clockRateSysInfo", "99999999", "system clock differs"),
                (3, "clockRateSysInfo", "74249999", "pixel clock differs")):
            project, handoff, root = self.sopc()
            root.findall("connection")[route].find("parameter[@name='" + field + "']/value").text = value
            ET.ElementTree(root).write(handoff)
            with self.subTest(field=field, route=route), self.assertRaisesRegex(ValueError, message):
                B.verify_computer(project, HPS)
        project, handoff, root = self.sopc()
        data = root.findall("connection")[4].find("parameter/value")
        mapping = ET.fromstring(data.text)
        ET.SubElement(mapping, "slave", {"name": "unexpected.alias", "start": "0x10310", "end": "0x10330"})
        data.text = ET.tostring(mapping, encoding="unicode")
        ET.ElementTree(root).write(handoff)
        with self.assertRaisesRegex(ValueError, "overlapping peripheral"):
            B.verify_computer(project, HPS)

    def test_generated_computer_version_and_native_response_ports_are_required(self):
        project, handoff, root = self.sopc()
        root.find("module").set("version", "1.0")
        ET.ElementTree(root).write(handoff)
        with self.assertRaisesRegex(ValueError, "endpoint/version"):
            B.verify_computer(project, HPS)
        for role, mutation in (("address", "narrow"), ("writeresponsevalid", "missing"), ("response", "narrow")):
            project, handoff, root = self.sopc()
            data = root.find("module/parameter/value")
            definition = ET.fromstring(data.text)
            ports = definition.find("boundary/interfaces/interface/ports")
            port = next(p for p in ports if p.findtext("role") == role)
            if mutation == "missing":
                ports.remove(port)
            else:
                port.find("width").text = "1"
            data.text = ET.tostring(definition, encoding="unicode")
            ET.ElementTree(root).write(handoff)
            with self.subTest(role=role), self.assertRaisesRegex(ValueError, "width/response interface differs"):
                B.verify_computer(project, HPS)

    def test_jtag_control_route_is_required_and_its_actual_window_is_checked(self):
        for mutation, message in (("missing", "JTAG control bridge"), ("base", "JTAG control bridge"),
                                  ("span", "JTAG computer slave")):
            project, handoff, root = self.sopc()
            route = root.findall("connection")[-1]
            if mutation == "missing":
                root.remove(route)
            elif mutation == "base":
                route.find("parameter[@name='baseAddress']/value").text = "0x0"
            else:
                data = route.find("parameter[@name='addressMapSysInfo']/value")
                mapping = ET.fromstring(data.text)
                next(s for s in mapping if s.get("name").endswith("fes_computer.avs")).set("end", "0x110310")
                data.text = ET.tostring(mapping, encoding="unicode")
            ET.ElementTree(root).write(handoff)
            with self.subTest(mutation=mutation), self.assertRaisesRegex(ValueError, message):
                B.verify_computer(project, HPS)


if __name__ == "__main__":
    unittest.main()
