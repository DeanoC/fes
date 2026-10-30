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

    def test_current_visibility_and_readiness_cdc_cuts_are_narrow_and_fail_closed(self):
        sdc = B.capture()[B.BOARD + "zx81-hps/clocks.sdc"].decode()
        self.assertIn(
            "set computer_visible_head [get_pins -compatibility_mode -nocase -nowarn "
            "{fes_video_inst|visible_sync[0]|d}]", sdc)
        self.assertIn("if {[get_collection_size $computer_visible_head] != 1}", sdc)
        self.assertIn("set_false_path -to $computer_visible_head", sdc)
        self.assertIn("set computer_video_ready_head [get_pins", sdc)
        self.assertIn("{soc_inst|subsys_periph|fes_computer|fes_computer|transport_ready_meta|d}]", sdc)
        self.assertIn("if {[get_collection_size $computer_video_ready_head] != 1}", sdc)
        self.assertIn("set_false_path -to $computer_video_ready_head", sdc)
        self.assertIn("Keep synchronizer stage-to-stage, frame-gate and output paths timed.", sdc)
        self.assertNotIn("set_clock_groups", sdc)

    def test_historical_quartus_report_is_not_presented_as_current_head_evidence(self):
        record = B.ROOT / "sources/misteross/docs/validation/2026-10-29-de25-zx81-hps"
        producer_inputs = json.loads((record / "compiled-5a7382c4b5/reports/inputs.json").read_text())
        selected = producer_inputs["functional_snapshot_sha256"]
        drifted = {
            name for name, recorded in selected.items()
            if hashlib.sha256((B.ROOT / name).read_bytes()).hexdigest() != recorded
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

    def test_simulation_receipts_are_scoped_to_historical_sources(self):
        record = B.ROOT / "sources/misteross/docs/validation/2026-10-29-de25-zx81-hps/simulation"
        source_head = "d715651b4171da7a69fa0d896dc1bd67168dd064"
        reference = "786e6383b32617af8baeb32e9de6cf99231a19f9"
        leaf_path = B.BOARD + "zx81-hps/fes_de25_computer.v"
        current_leaf = B.ROOT / leaf_path
        current_leaf_bytes = current_leaf.read_bytes()
        self.assertEqual(len(current_leaf_bytes), 9176)
        self.assertEqual(hashlib.sha256(current_leaf_bytes).hexdigest(),
                         "9f0f57bf95e9bf8f1d669b2e090a700d7cfbc3ee46a09ad17568f9bba79c04bd")

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
        self.assertEqual(drifted, {
            leaf_path,
            B.BOARD + "zx81-hps/computer_tb.cpp",
            B.BOARD + "zx81-hps/sim.py",
            B.BOARD + "zx81-hps/build.py",
        })

        report = " ".join((record / "README.md").read_text().split())
        self.assertIn(f"PR head `{source_head}`", report)
        self.assertIn("18 of those 22 files still match", report)
        self.assertIn("does not exercise the current PR leaf", report)
        self.assertNotIn("22 matching current input files", report)
        for changed_name in ("fes_de25_computer.v", "computer_tb.cpp", "sim.py", "build.py"):
            self.assertIn(f"`{changed_name}`", report)

        leaf_binding = json.loads((record / "leaf-build-id-binding.json").read_text())
        self.assertEqual(leaf_binding["guarded_reference_leaf_sha256"],
                         "26ce1e51454000f2762045cc442fbbae881404a98b94334eea8ba11e43a9ad0a")
        self.assertNotIn("current_leaf_sha256", leaf_binding)
        self.assertIn("not the current PR leaf", leaf_binding["evidence_scope"])
        elaboration = json.loads((record / "default-elaboration-binding.json").read_text())
        self.assertEqual(elaboration["elaborated_reference_leaf_sha256"],
                         leaf_binding["guarded_reference_leaf_sha256"])
        self.assertNotIn("current_leaf_sha256", elaboration)
        self.assertIn("does not describe the current PR leaf", elaboration["evidence_scope"])

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
        module = ET.SubElement(root, "module", {"path": "subsys_periph.fes_computer", "kind": "fes_de25_computer", "version": "1.0"})
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
        data = root.findall("connection")[-1].find("parameter/value")
        mapping = ET.fromstring(data.text)
        ET.SubElement(mapping, "slave", {"name": "unexpected.alias", "start": "0x10310", "end": "0x10330"})
        data.text = ET.tostring(mapping, encoding="unicode")
        ET.ElementTree(root).write(handoff)
        with self.assertRaisesRegex(ValueError, "overlapping peripheral"):
            B.verify_computer(project, HPS)

    def test_generated_computer_version_and_native_response_ports_are_required(self):
        project, handoff, root = self.sopc()
        root.find("module").set("version", "2.0")
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


if __name__ == "__main__":
    unittest.main()
