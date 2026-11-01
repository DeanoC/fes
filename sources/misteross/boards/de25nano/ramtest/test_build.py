"""Exercise retained preparation and generated controller checks without Quartus."""
import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock
import xml.etree.ElementTree as ET
import zipfile

SPEC = importlib.util.spec_from_file_location("de25_ramtest_builder", Path(__file__).with_name("build.py"))
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)


class RamtestBuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="fes-de25-ramtest-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.fixture = B.load(B.HERE.parent / "hps/test_build.py", "ramtest_hps_fixture")
        self.sources = B.capture()
        self.files = self.fixture.vendor_files()
        self.files["golden_top.v"] = self.files["golden_top.v"].replace(
            b"module golden_top", b"//`define ENABLE_LPDDR4B\nmodule golden_top").replace(
            b"qsys_top soc_inst (", b"qsys_top soc_inst (\n.ninit_done_ninit_done                     (ninit_done),")
        ip = "ip/EMIF_Qsys/EMIF_Qsys_emif_ddr4a_0.ip"
        self.vendor = {name: self.fixture.component_xml((("device", "A5EB013BB23BE4SR1"),
            ("MEM_DENSITY", "8192"), ("PHY_REFCLK_FREQ_MHZ", "100.0"))) for name in B.VENDOR_FILES}
        self.vendor["EMIF_Qsys.qsys"] = (
            f'<system xmlns:a="{self.fixture.ALTERA}" xmlns:i="{self.fixture.IP}">'
            '<a:module><a:entity_info><i:library>emif_lpddr4b</i:library></a:entity_info>'
            f'<i:parameter parameterId="logicalView"><i:value>{ip}</i:value></i:parameter>'
            '</a:module><device>A5EB013BB23BE4SR1</device></system>').encode()
        self.vendor["golden_top.v"] = '\n'.join(
            f'.emif_lpddr4b_mem_pin{i}(LPDDR4B_PIN{i})' for i in range(12)).encode()
        self.vendor["axil_driver_calibration.sv"] = b'module axil_driver_calibration; endmodule\n'
        self.archive = self.root / "vendor.zip"
        self.write_archive()

    def write_archive(self):
        with zipfile.ZipFile(self.archive, "w") as output:
            for name, data in self.files.items():
                output.writestr(self.fixture.PREFIX + name, data)
            for name, data in self.vendor.items():
                output.writestr(B.VENDOR_PREFIX + name, data)
        lock = (f'format = 1\nboard = "de25-nano"\ndevice = "{self.fixture.BUILD.DEVICE}"\n'
            '[archive]\nurl = "https://example.invalid/vendor.zip"\nfile = "vendor.zip"\n'
            f'prefix = "{self.fixture.PREFIX}"\nsha256 = "{B.sha(self.archive.read_bytes())}"\n')
        self.sources[B.BOARD + "hps/vendor.lock.toml"] = lock.encode()

    def prepare(self, revision=None):
        with mock.patch.object(B, "capture", return_value=self.sources):
            output, hps = B.prepare(self.archive, self.root / "prepared", revision)
        return output / "hps/project", hps

    def test_preparation_binds_three_memories_and_preserves_hps_boot_bytes(self):
        project, hps = self.prepare()
        inputs = json.loads((project.parent / "inputs.json").read_text())
        self.assertEqual(inputs["source_classification"], "working-copy-diagnostic")
        self.assertEqual(inputs["ramtest"]["physical_mmio"], "not-established")
        self.assertEqual(inputs["ramtest"]["runtime_admission"], "closed")
        self.assertTrue(inputs["ramtest"]["hps_memory_requires_kernel_grant"])
        self.assertEqual((project / self.fixture.HPS).read_bytes(), self.files[self.fixture.HPS])
        self.assertEqual((project / self.fixture.EMIF).read_bytes(), self.files[self.fixture.EMIF])
        self.assertEqual((project / self.fixture.BOOT).read_bytes(), self.files[self.fixture.BOOT])
        script = (project / "integrate-mailbox.tcl").read_text()
        self.assertIn("subsys_periph.fes_ramtest_hps_mem subsys_hps.f2sdram_adapter_axi4_sub", script)
        self.assertIn("subsys_periph.fes_ramtest_fpga_mem ramtest_b.emif_lpddr4b_s0_axi4", script)
        self.assertIn("subsys_debug.fpga_m_master/subsys_periph.pb_cpu_0_s0 baseAddress 0x100000", script)
        self.assertTrue((project / "prepare-emif-b.tcl").is_file())
        qsf=(project/'golden_top.qsf').read_text()
        for name in ('EMIF_Qsys_emif_ddr4a_0','EMIF_Qsys_reset_in'):
            self.assertIn('set_global_assignment -name IP_FILE ramtest_b/ip/EMIF_Qsys/'+name+'.ip',qsf)
        self.assertIn("`define ENABLE_LPDDR4B", (project / "golden_top.v").read_text())
        self.assertIn(".h2f_warm_reset_handshake_reset_ack (1'b1)", (project / "golden_top.v").read_text())
        self.assertIn(".fes_ramtest_hps_calibration_calibrated(1'b0)", (project / "golden_top.v").read_text())
        self.assertIn(".fes_ramtest_hps_calibration_ready(1'b0)", (project / "golden_top.v").read_text())
        self.assertEqual(len(inputs["ramtest"]["build_id"]), 32)
        for name, expected in inputs["ramtest"]["source_files_sha256"].items():
            self.assertEqual(hps.digest(project / "fes_mailbox" / name), expected)
        for name, expected in inputs["ramtest"]["vendor_reference_sha256"].items():
            self.assertEqual(expected, B.sha(self.vendor[name]))
        original = self.vendor["ip/EMIF_Qsys/EMIF_Qsys_emif_ddr4a_0.ip"]
        self.assertEqual((project / inputs["ramtest"]["emif_b_source"]).read_bytes(),
                         original.replace(b"A5EB013BB23BE4SR1", b"A5EB013BB23BE4SCS"))

    def test_selected_git_blobs_ignore_later_rtl_lock_and_definition_edits(self):
        repository = self.root / "repository"
        for name, data in self.sources.items():
            path = repository / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        def git(*arguments):
            return subprocess.check_output(["git", "-C", str(repository), *arguments], stderr=subprocess.PIPE)
        git("init", "-q")
        git("config", "user.name", "FES test")
        git("config", "user.email", "test@example.invalid")
        git("add", ".")
        git("commit", "-qm", "selected input")
        revision = git("rev-parse", "HEAD").decode().strip()
        keys = [B.BOARD + "ramtest/" + name for name in
                ("scan.v", "integrate.tcl", "generated/fes_de25_ramtest.vh")]
        keys.append(B.BOARD + "hps/vendor.lock.toml")
        for name in keys:
            (repository / name).write_bytes(b"later unselected source\n")
        with mock.patch.object(B, "ROOT", repository):
            output, _ = B.prepare(self.archive, self.root / "committed", revision)
        inputs = json.loads((output / "hps/inputs.json").read_text())
        self.assertEqual(inputs["fes_revision"], revision)
        self.assertNotIn("base_revision", inputs)
        for name in keys:
            self.assertEqual((output / "functional-source" / name).read_bytes(), self.sources[name])
            self.assertEqual(inputs["functional_snapshot_sha256"][name], B.sha(self.sources[name]))

    def test_offsets_derive_from_selected_generated_header(self):
        key = B.BOARD + "ramtest/generated/fes_de25_ramtest.vh"
        self.sources[key] = self.sources[key].replace(b"HPS_OFFSET 32'h00010400", b"HPS_OFFSET 32'h00010700")
        project, _ = self.prepare()
        self.assertIn("fes_ramtest_hps 0 0 0x10700", (project / "integrate-mailbox.tcl").read_text())

    def test_vendor_hash_and_reference_shape_changes_fail_closed(self):
        self.archive.write_bytes(self.archive.read_bytes() + b"changed")
        with self.assertRaisesRegex(ValueError, "differs from lock"):
            self.prepare()

    def test_vendor_fpga_pin_map_change_is_rejected(self):
        self.vendor["golden_top.v"] = self.vendor["golden_top.v"].replace(b"LPDDR4B_PIN11", b"OTHER_PIN11")
        self.write_archive()
        with self.assertRaisesRegex(ValueError, "pin/export map changed"):
            self.prepare()

    def test_controller_selection_rejects_missing_duplicate_and_escaped_sources(self):
        path = self.root / "controller.qsys"
        original = self.vendor["EMIF_Qsys.qsys"]
        for value in (original.replace(b"emif_lpddr4b", b"other"),
                      original.replace(b"</system>", original[original.index(b"<a:module>"):original.index(b"</a:module>")+11] + b"</system>"),
                      original.replace(b"ip/EMIF_Qsys/EMIF_Qsys_emif_ddr4a_0.ip", b"../outside.ip")):
            path.write_bytes(value)
            with self.subTest(value=value), self.assertRaises(ValueError):
                B.emif_b_ip(path)

    def handoff(self, project):
        tree = self.fixture.handoff_tree()
        root = tree.getroot()
        mapping = root.find("connection/parameter[@name='addressMapSysInfo']/value")
        def interface(module, name, ports):
            value = module.find("parameter[@name='componentDefinition']/value")
            if value is None:
                value = ET.SubElement(ET.SubElement(module, "parameter", {"name": "componentDefinition"}), "value")
                definition = ET.fromstring('<componentDefinition><boundary><interfaces/></boundary></componentDefinition>')
            else:
                definition = ET.fromstring(value.text)
            listing = definition.find('boundary/interfaces')
            for old in listing.findall('interface'):
                if old.findtext('name') == name:
                    listing.remove(old)
            point = ET.SubElement(listing, 'interface')
            ET.SubElement(point, 'name').text = name
            entries = ET.SubElement(point, 'ports')
            for role, direction, width in ports:
                port = ET.SubElement(entries, 'port')
                for key, data in (('role', role), ('direction', direction), ('width', str(width))):
                    ET.SubElement(port, key).text = data
            value.text = ET.tostring(definition, encoding='unicode')
        def route(kind, start, end, **parameters):
            connection = ET.SubElement(root, 'connection', {'kind': kind, 'start': start, 'end': end})
            for name, value in parameters.items():
                ET.SubElement(ET.SubElement(connection, 'parameter', {'name': name}), 'value').text = str(value)
            return connection
        additions = []
        for channel, kind, rate, offset in (("hps", "fes_de25_ramtest", 100000000, 0x10400),
                ("fpga", "fes_de25_ramtest", 100000000, 0x10500),
                ("sdram", "fes_de25_sdram_ramtest", 50000000, 0x10600)):
            path = "subsys_periph.fes_ramtest_" + channel
            module = ET.SubElement(root, "module", {"path": path, "kind": kind})
            interface(module, 'avs', (("address", "Input", 8), ("writedata", "Input", 32), ("response", "Output", 2)))
            if channel != 'sdram':
                interface(module, 'avm', (('address', 'Output', 32), ('burstcount', 'Output', 5),
                    ('read', 'Output', 1), ('write', 'Output', 1), ('writedata', 'Output', 128),
                    ('byteenable', 'Output', 16), ('waitrequest', 'Input', 1), ('readdata', 'Input', 128),
                    ('readdatavalid', 'Input', 1), ('writeresponsevalid', 'Input', 1), ('response', 'Input', 2)))
            else:
                interface(module, 'dram', tuple((name, 'Output', width) for name, width in
                    (('clk', 1), ('cke', 1), ('addr', 13), ('ba', 2), ('cs_n', 2), ('ras_n', 1),
                     ('cas_n', 1), ('we_n', 1), ('dqm', 2))) + (('dq', 'Bidir', 16),))
            normalized = path.replace('.', '_')
            route('avalon', 'subsys_periph_pb_cpu_0.m0', normalized + '.avs', baseAddress=hex(offset))
            route('reset', 'subsys_periph_periph_rst_in.out_reset', normalized + '.reset')
            route('clock', 'subsys_periph_periph_clk.out_clk' if channel != 'sdram' else 'subsys_periph_fes_ramtest_clk50.out_clk',
                  normalized + '.clk', clockRateSysInfo=rate)
            additions.append(f'<slave name="{path}.avs" start="{offset}" end="{offset + 256}"/>')
        mapping.text = mapping.text.replace("</address-map>", ''.join(additions) + "</address-map>")
        for path, name, width in (('subsys_hps.f2sdram_adapter', 'axi4_sub', 32), ('ramtest_b.emif_lpddr4b', 's0_axi4', 30)):
            module = root.find(f"module[@path='{path}']")
            if module is None:
                module = ET.SubElement(root, 'module', {'path': path, 'kind': 'emif_io96b_lpddr4', 'version': '0.0'})
            interface(module, name, (('araddr', 'Input', width), ('awaddr', 'Input', width),
                ('wdata', 'Input', 256), ('rdata', 'Output', 256), ('rresp', 'Output', 2), ('bresp', 'Output', 2)))
        route('avalon', 'subsys_periph_fes_ramtest_hps.avm', 'subsys_hps_f2sdram_adapter.axi4_sub', baseAddress=0)
        route('avalon', 'subsys_periph_fes_ramtest_fpga.avm', 'ramtest_b_emif_lpddr4b.s0_axi4', baseAddress=0,
              addressMapSysInfo='<address-map><slave name="ramtest_b.emif_lpddr4b.s0_axi4" start="0" end="0x40000000"/></address-map>')
        route('clock', 'ramtest_b_fabric100.out_clk', 'ramtest_b_emif_lpddr4b.s0_axi4_clock_in', clockRateSysInfo=100000000)
        route('clock', 'ramtest_b_control50.out_clk', 'ramtest_b_emif_lpddr4b.s0_axi4lite_clock', clockRateSysInfo=50000000)
        route('reset', 'rst_in.out_reset', 'ramtest_b_emif_lpddr4b.s0_axi4lite_reset_n')
        jtag_map = ET.fromstring(mapping.text)
        for slave in jtag_map.findall('slave'):
            for name in ('start', 'end'):
                slave.set(name, hex(int(slave.get(name), 0) + 0x100000))
        route('avalon', 'subsys_debug_fpga_m.master', 'subsys_periph_pb_cpu_0.s0', baseAddress='0x100000',
              addressMapSysInfo=ET.tostring(jtag_map, encoding='unicode'))
        path = project / "qsys_top/qsys_top.sopcinfo"
        path.parent.mkdir(exist_ok=True)
        tree.write(path)
        return tree, path

    def test_generated_endpoint_clock_response_map_and_emif_geometry_guards(self):
        project, hps = self.prepare()
        tree, path = self.handoff(project)
        self.assertEqual(len(B.verify_generated(project, hps)["endpoints"]), 3)
        mutations = []
        changed = copy.deepcopy(tree)
        changed.find("module[@path='subsys_periph.fes_ramtest_fpga']").set("kind", "other")
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        changed.getroot().remove(changed.find("connection[@start='subsys_periph_fes_ramtest_fpga.avm']"))
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        value = changed.find("module[@path='ramtest_b.emif_lpddr4b']/parameter/value")
        value.text = value.text.replace('<width>30</width>', '<width>32</width>')
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        value = changed.find("module[@path='subsys_periph.fes_ramtest_fpga']/parameter/value")
        value.text = value.text.replace('<role>writeresponsevalid</role>', '<role>unused</role>')
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        value = changed.find("connection[@start='subsys_periph_fes_ramtest_fpga.avm']/parameter[@name='addressMapSysInfo']/value")
        value.text = value.text.replace('0x40000000', '0x80000000')
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        changed.find("connection[@start='subsys_debug_fpga_m.master']/parameter[@name='baseAddress']/value").text = '0'
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        changed.find("connection[@end='subsys_periph_fes_ramtest_sdram.clk']/parameter/value").text = "100000000"
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        value = changed.find("module[@path='subsys_periph.fes_ramtest_hps']/parameter/value")
        value.text = value.text.replace("<width>2</width>", "<width>1</width>")
        mutations.append(changed)
        changed = copy.deepcopy(tree)
        value = changed.find("connection/parameter[@name='addressMapSysInfo']/value")
        value.text = value.text.replace('start="66560"', 'start="66564"')
        mutations.append(changed)
        for changed in mutations:
            changed.write(path)
            with self.subTest(xml=ET.tostring(changed.getroot())), self.assertRaises(ValueError):
                B.verify_generated(project, hps)
        tree.write(path)
        inputs = json.loads((project.parent / "inputs.json").read_text())
        source = project / inputs["ramtest"]["emif_b_source"]
        source.write_bytes(source.read_bytes().replace(b">8192<", b">4096<"))
        with self.assertRaisesRegex(ValueError, "memory/PHY parameters"):
            B.verify_generated(project, hps)

    def test_generation_rejects_mutated_selected_rtl_and_physical_top(self):
        project, hps = self.prepare()
        self.handoff(project)
        for target, message in ((project / 'fes_mailbox/scan.v', 'source changed'),
                                (project / 'golden_top.v', 'physical top changed')):
            original = target.read_bytes()
            target.write_bytes(original + b'\n// unselected edit\n')
            with self.subTest(target=target), self.assertRaisesRegex(ValueError, message):
                B.verify_generated(project, hps)
            target.write_bytes(original)


if __name__ == "__main__":
    unittest.main()
