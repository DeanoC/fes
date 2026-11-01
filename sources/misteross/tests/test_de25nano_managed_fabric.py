"""Canonical MENU rejects unowned fabric initiators and stale generated RTL."""
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


D = load('managed_fabric_dma', 'boards/de25nano/scanout/linux_dma.py')
H = load('managed_fabric_hps', 'boards/de25nano/hps/build.py')


class ManagedFabricTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.project = self.directory / 'project'
        self.fixture = json.loads(gzip.decompress((ROOT / 'tests/fixtures/de25_managed_fabric.json.gz').read_bytes()))
        self.restore_fixture()

    def restore_fixture(self):
        for name, text in self.fixture['files'].items():
            path = self.project / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        self.set_hps_width('256')

    def set_hps_width(self, width):
        # A minimal synthetic IP-XACT parameter view for projected tests.
        # Actual generation separately checks the complete vendor parameter set.
        path = self.project / D.HPS_IP
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('<ip:component xmlns:ip="http://www.accellera.org/XMLSchema/IPXACT/1685-2014" '
            'xmlns:a="http://www.altera.com/XMLSchema/IPXact2014/extensions"><ip:vendorExtensions>'
            '<a:altera_module_parameters><ip:parameters><ip:parameter parameterId="f2s_data_width">'
            '<ip:value>' + width + '</ip:value></ip:parameter></ip:parameters>'
            '</a:altera_module_parameters></ip:vendorExtensions></ip:component>')

    def project_removals(self):
        # This is an explicitly synthetic projection of the old real graph.
        # The producer must separately pass actual 26.3 generation and fitting.
        path = self.project / 'qsys_top/qsys_top.sopcinfo'
        tree = ET.parse(path)
        for node in list(tree.getroot()):
            if node.tag == 'module' and any((node.get('path') or '').startswith(prefix)
                    for prefix in ('subsys_debug', 'ace5lite_cache_coherency_translator_0')):
                tree.getroot().remove(node)
            elif node.tag == 'connection' and any(prefix in ' '.join(node.attrib.values())
                    for prefix in ('subsys_debug', 'ace5lite_cache_coherency_translator_0')):
                tree.getroot().remove(node)
        native = tree.find("module[@path='subsys_hps.agilex_hps']/parameter[@name='componentDefinition']/value")
        definition = ET.fromstring(native.text)
        interfaces = definition.find('boundary/interfaces')
        for point in list(interfaces):
            if point.findtext('name') in ('fpga2hps', 'fpga2hps_clock', 'fpga2hps_reset'):
                interfaces.remove(point)
        native.text = ET.tostring(definition, encoding='unicode')
        tree.write(path)
        path = self.project / 'qsys_top/synth/qsys_top.v'
        source = path.read_text()
        for name in ('subsys_debug', 'ace5lite_cache_coherency_translator_0'):
            source, count = re.subn(r'(?ms)^\t\w+ ' + name + r' \(\n.*?^\t\);\n', '', source)
            self.assertEqual(count, 1)
        source, count = re.subn(r'(?m)^\s*\.fpga2hps_\w+\s*\([^()]*\),[^\n]*\n', '', source)
        self.assertGreater(count, 3)
        path.write_text(source)
        self.set_hps_width('0')

    def mutate_routes(self, change):
        path = self.project / 'qsys_top/qsys_top.sopcinfo'
        tree = ET.parse(path)
        change(tree.getroot())
        tree.write(path)

    def test_predecessor_really_contains_the_unowned_f2h_debug_route(self):
        with self.assertRaisesRegex(ValueError, 'maintenance initiator/translator'):
            D.verify_managed_fabric(self.project, H)

    def test_projected_managed_graph_preserves_hps_control_ocm_and_sid13_memory(self):
        self.project_removals()
        result = D.verify_managed_fabric(self.project, H)
        self.assertEqual(result['initiator_count'], 8)
        self.assertEqual(result['fabric_to_hps'], [['subsys_hps_f2sdram_adapter.axi4_man', 'subsys_hps_agilex_hps.f2sdram']])
        self.assertEqual(result['unused_f2h_interface'], 'disabled and absent')
        self.assertEqual(result['memory_handoff']['master_count'], 1)
        self.assertEqual(result['runtime_admission'], 'not-qualified')

    def test_extra_initiator_is_refused_even_when_it_only_reaches_ocm(self):
        self.project_removals()
        self.mutate_routes(lambda root: ET.SubElement(root, 'connection',
            {'kind': 'avalon', 'start': 'unexpected.master', 'end': 'ocm.axi_s1'}))
        with self.assertRaisesRegex(ValueError, 'initiator inventory'):
            D.verify_managed_fabric(self.project, H)

    def test_known_initiator_cannot_add_an_f2h_or_control_bypass(self):
        for target, error in (('subsys_hps_agilex_hps.fpga2hps', 'path into HPS'),
                              ('subsys_periph_fes_scanout_guard.avs', 'control/OCM')):
            with self.subTest(target=target):
                self.restore_fixture()
                self.project_removals()
                self.mutate_routes(lambda root: ET.SubElement(root, 'connection',
                    {'kind': 'avalon', 'start': 'subsys_hps_agilex_hps.hps2fpga', 'end': target}))
                with self.assertRaisesRegex(ValueError, error):
                    D.verify_managed_fabric(self.project, H)

    def test_missing_hps_control_path_is_refused(self):
        self.project_removals()
        def remove(root):
            for node in root.findall('connection'):
                if node.get('start') == 'subsys_hps_agilex_hps.lwhps2fpga' and node.get('kind') == 'avalon':
                    root.remove(node)
        self.mutate_routes(remove)
        with self.assertRaises(ValueError):
            D.verify_managed_fabric(self.project, H)

    def test_stale_generated_debug_instance_is_refused(self):
        self.project_removals()
        path = self.project / 'qsys_top/synth/qsys_top.v'
        path.write_text(path.read_text() + '\nold_debug subsys_debug (.clk(1\'b0));\n')
        with self.assertRaisesRegex(ValueError, 'parent retains'):
            D.verify_managed_fabric(self.project, H)

    def test_native_bridge_must_be_disabled_and_generated_ports_absent(self):
        self.project_removals()
        self.set_hps_width('256')
        with self.assertRaisesRegex(ValueError, 'not disabled'):
            D.verify_managed_fabric(self.project, H)
        self.set_hps_width('0')
        path = self.project / 'qsys_top/synth/qsys_top.v'
        original = path.read_text()
        for value in ('', "1'b0", 'unowned_request'):
            with self.subTest(value=value):
                source, count = re.subn(r'(?m)^(\t\w+ subsys_hps \(\n)',
                    lambda m: m[1] + '\t\t.fpga2hps_arvalid (' + value + '),\n', original)
                self.assertEqual(count, 1)
                path.write_text(source)
                with self.assertRaisesRegex(ValueError, 'parent retains unused'):
                    D.verify_managed_fabric(self.project, H)
        path.write_text(original)

    def test_actual_failed_sdk_graph_with_enabled_floating_bridge_is_refused(self):
        fixture = json.loads(gzip.decompress((ROOT / 'tests/fixtures/de25_managed_fabric_floating.json.gz').read_bytes()))
        for name, text in fixture['files'].items():
            self.assertEqual(hashlib.sha256(text.encode()).hexdigest(), fixture['sha256'][name])
            path = self.project / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text(text)
        with self.assertRaisesRegex(ValueError, 'not disabled'):
            D.verify_managed_fabric(self.project, H)

    def test_generated_native_interface_cannot_remain_exposed(self):
        self.project_removals()
        path = self.project / 'qsys_top/qsys_top.sopcinfo'
        tree = ET.parse(path)
        native = tree.find("module[@path='subsys_hps.agilex_hps']/parameter[@name='componentDefinition']/value")
        definition = ET.fromstring(native.text)
        point = ET.SubElement(definition.find('boundary/interfaces'), 'interface')
        ET.SubElement(point, 'name').text = 'fpga2hps'
        native.text = ET.tostring(definition, encoding='unicode')
        tree.write(path)
        with self.assertRaisesRegex(ValueError, 'remains exposed'):
            D.verify_managed_fabric(self.project, H)

    @unittest.skipUnless(shutil.which('tclsh'), 'requires Tcl')
    def test_actual_removal_script_checks_targets_and_keeps_hps_instances(self):
        script = D.isolate_managed_fabric_script('remove_instance ext_hps_f2sdram_master\nsave_system top.qsys\n',
                                                'save_system top.qsys\n')
        prefix = '''set instances {ext_hps_f2sdram_master subsys_debug ace5lite_cache_coherency_translator_0 subsys_hps subsys_periph ocm}
proc get_instances {} {return $::instances}
proc remove_instance {name} {lappend ::removed $name}
proc get_connections {} {return {clk_100.out_clk/subsys_hps.fpga2hps_clk rst_in.out_reset/subsys_hps.fpga2hps_rst}}
proc remove_connection {name} {lappend ::connections $name}
proc save_system {name} {}
set removed {}
set connections {}
'''
        result = subprocess.run(['tclsh'], input=prefix + script + '\nputs $removed\n',
                                text=True, capture_output=True, check=True, timeout=10)
        self.assertEqual(result.stdout.strip(), 'ext_hps_f2sdram_master subsys_debug ace5lite_cache_coherency_translator_0')
        missing = prefix.replace('subsys_debug ', '')
        result = subprocess.run(['tclsh'], input=missing + '\nif {[catch {\n' + script +
                                '\n} message]} {puts $message; exit 4}\n',
                                text=True, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 4)
        self.assertIn('managed fabric removal target changed', result.stdout)
        refreshed = prefix.replace('return {clk_100.out_clk/subsys_hps.fpga2hps_clk rst_in.out_reset/subsys_hps.fpga2hps_rst}', 'return {}')
        result = subprocess.run(['tclsh'], input=refreshed + '\nif {[catch {\n' + script +
                                '\n} message]} {puts $message; exit 4}\nputs $removed\n',
                                text=True, capture_output=True, check=True, timeout=10)
        self.assertEqual(result.stdout.strip(), 'ext_hps_f2sdram_master subsys_debug ace5lite_cache_coherency_translator_0')
        unexpected = prefix.replace('return {clk_100.out_clk/subsys_hps.fpga2hps_clk rst_in.out_reset/subsys_hps.fpga2hps_rst}',
                                    'return {unknown.master/subsys_hps.fpga2hps}')
        result = subprocess.run(['tclsh'], input=unexpected + '\nif {[catch {\n' + script +
                                '\n} message]} {puts $message; exit 4}\n',
                                text=True, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 4)
        self.assertIn('vendor F2H connection changed', result.stdout)
        for bad in ('', 'save_system top.qsys\n', 'remove_instance ext_hps_f2sdram_master\nsave_system top.qsys\nsave_system top.qsys\n'):
            with self.assertRaises(ValueError):
                D.isolate_managed_fabric_script(bad, 'save_system top.qsys\n')


if __name__ == '__main__':
    unittest.main()
