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
        shutil.rmtree(self.directory / 'managed-constraints-originals', ignore_errors=True)
        (self.directory / 'managed-debug-constraints.json').unlink(missing_ok=True)
        for name, text in self.fixture['files'].items():
            path = self.project / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        self.set_hps_width('256', '128')
        constraints = json.loads(gzip.decompress((ROOT / 'tests/fixtures/de25_managed_jtag_sdc.json.gz').read_bytes()))
        for name, text in constraints['files'].items():
            self.assertEqual(hashlib.sha256(text.encode()).hexdigest(), constraints['sha256'][name])
            (self.project / name).write_text(text)

    def set_hps_width(self, width, h2f='0'):
        # A minimal synthetic IP-XACT parameter view for projected tests.
        # Actual generation separately checks the complete vendor parameter set.
        path = self.project / D.HPS_IP
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('<ip:component xmlns:ip="http://www.accellera.org/XMLSchema/IPXACT/1685-2014" '
            'xmlns:a="http://www.altera.com/XMLSchema/IPXact2014/extensions"><ip:vendorExtensions>'
            '<a:altera_module_parameters><ip:parameters><ip:parameter parameterId="f2s_data_width">'
            '<ip:value>' + width + '</ip:value></ip:parameter><ip:parameter parameterId="H2F_Width">'
            '<ip:value>' + h2f + '</ip:value></ip:parameter></ip:parameters>'
            '</a:altera_module_parameters></ip:vendorExtensions></ip:component>')

    def project_removals(self):
        # This is an explicitly synthetic projection of the old real graph.
        # The producer must separately pass actual 26.3 generation and fitting.
        path = self.project / 'qsys_top/qsys_top.sopcinfo'
        tree = ET.parse(path)
        for node in list(tree.getroot()):
            if node.tag == 'module' and any((node.get('path') or '').startswith(prefix)
                    for prefix in ('subsys_debug', 'ace5lite_cache_coherency_translator_0')) or node.get('path') == 'ocm':
                tree.getroot().remove(node)
            elif node.tag == 'connection' and (any(prefix in ' '.join(node.attrib.values())
                    for prefix in ('subsys_debug', 'ace5lite_cache_coherency_translator_0', '.hps2fpga')) or
                    (node.get('end') or '').startswith('ocm.')):
                tree.getroot().remove(node)
        native = tree.find("module[@path='subsys_hps.agilex_hps']/parameter[@name='componentDefinition']/value")
        definition = ET.fromstring(native.text)
        interfaces = definition.find('boundary/interfaces')
        for point in list(interfaces):
            if point.findtext('name') in ('fpga2hps', 'fpga2hps_clock', 'fpga2hps_reset',
                                          'hps2fpga', 'hps2fpga_axi_clock', 'hps2fpga_axi_reset'):
                interfaces.remove(point)
        native.text = ET.tostring(definition, encoding='unicode')
        tree.write(path)
        path = self.project / 'qsys_top/synth/qsys_top.v'
        source = path.read_text()
        for name in ('subsys_debug', 'ace5lite_cache_coherency_translator_0', 'ocm'):
            source, count = re.subn(r'(?ms)^\t\w+ ' + name + r' \(\n.*?^\t\);\n', '', source)
            self.assertEqual(count, 1)
        for bridge in ('fpga2hps', 'hps2fpga'):
            source, count = re.subn(r'(?m)^\s*\.' + bridge + r'_\w+\s*\([^()]*\),[^\n]*\n', '', source)
            self.assertGreater(count, 3)
        path.write_text(source)
        self.set_hps_width('0')
        D.retire_managed_jtag_constraints(self.project)

    def mutate_routes(self, change):
        path = self.project / 'qsys_top/qsys_top.sopcinfo'
        tree = ET.parse(path)
        change(tree.getroot())
        tree.write(path)

    def test_predecessor_really_contains_the_unowned_f2h_debug_route(self):
        with self.assertRaisesRegex(ValueError, 'maintenance initiator/translator'):
            D.verify_managed_fabric(self.project, H)

    def test_projected_managed_graph_preserves_hps_control_and_sid13_memory(self):
        self.project_removals()
        result = D.verify_managed_fabric(self.project, H)
        self.assertEqual(result['initiator_count'], 7)
        self.assertNotIn('subsys_hps_agilex_hps.hps2fpga', result['initiator_endpoints'])
        self.assertEqual(result['removed_instances'], ['subsys_debug', 'ace5lite_cache_coherency_translator_0', 'ocm'])
        self.assertEqual(result['disabled_hps_parameters'], {'f2s_data_width': '0', 'H2F_Width': '0'})
        self.assertEqual(result['unused_h2f_interface'], 'disabled and absent')
        self.assertEqual(result['fabric_to_hps'], [['subsys_hps_f2sdram_adapter.axi4_man', 'subsys_hps_agilex_hps.f2sdram']])
        self.assertEqual(result['unused_f2h_interface'], 'disabled and absent')
        self.assertEqual(result['memory_handoff']['master_count'], 1)
        self.assertEqual(result['runtime_admission'], 'not-qualified')

    def test_extra_initiator_is_refused_even_when_it_only_reaches_control(self):
        self.project_removals()
        self.mutate_routes(lambda root: ET.SubElement(root, 'connection',
            {'kind': 'avalon', 'start': 'unexpected.master', 'end': 'subsys_periph_fes_scanout.avs'}))
        with self.assertRaisesRegex(ValueError, 'initiator inventory'):
            D.verify_managed_fabric(self.project, H)

    def test_window_masters_belong_only_to_the_window_profile(self):
        self.project_removals()
        # The window profile names its masters: a graph without them is refused.
        with self.assertRaisesRegex(ValueError, 'initiator inventory'):
            D.verify_managed_fabric(self.project, H, window=True)
        # And a build without the window refuses the window's masters.
        self.mutate_routes(lambda root: [ET.SubElement(root, 'connection',
            {'kind': 'avalon', 'start': master, 'end': 'subsys_periph_unreviewed.s0'}) for master in D.WINDOW_MASTERS])
        with self.assertRaisesRegex(ValueError, 'initiator inventory'):
            D.verify_managed_fabric(self.project, H)

    def test_retained_on_chip_memory_or_full_bridge_route_is_refused(self):
        # The fixture is the real predecessor graph: restoring only the memory
        # or only the bridge route must fail, whichever check sees it first.
        original = ET.parse(self.project / 'qsys_top/qsys_top.sopcinfo').getroot()
        memory = original.find("module[@path='ocm']")
        route = next(c for c in original.findall('connection')
                     if c.get('start') == 'subsys_hps_agilex_hps.hps2fpga' and c.get('end') == 'ocm.axi_s1')
        self.assertEqual(memory.get('kind'), 'intel_onchip_memory')
        for node, error in ((memory, 'initiator/translator/memory: ocm'), (route, 'initiator inventory')):
            with self.subTest(node=node.tag):
                self.restore_fixture()
                self.project_removals()
                self.mutate_routes(lambda root: root.append(node))
                with self.assertRaisesRegex(ValueError, error):
                    D.verify_managed_fabric(self.project, H)
        self.restore_fixture()
        self.project_removals()
        path = self.project / 'qsys_top/synth/qsys_top.v'
        path.write_text(path.read_text() + "\nocm ocm (.clk1_clk(1'b0));\n")
        with self.assertRaisesRegex(ValueError, 'parent retains'):
            D.verify_managed_fabric(self.project, H)

    def test_known_initiator_cannot_add_an_f2h_or_control_bypass(self):
        for target, error in (('subsys_hps_agilex_hps.fpga2hps', 'path into HPS'),
                              ('subsys_periph_fes_scanout_guard.avs', 'control route differs')):
            with self.subTest(target=target):
                self.restore_fixture()
                self.project_removals()
                self.mutate_routes(lambda root: ET.SubElement(root, 'connection',
                    {'kind': 'avalon', 'start': 'subsys_hps_agilex_hps.lwhps2fpga', 'end': target}))
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
        for widths in (('256', '0'), ('0', '128'), ('0', '32')):
            self.set_hps_width(*widths)
            with self.subTest(widths=widths), self.assertRaisesRegex(ValueError, 'not disabled'):
                D.verify_managed_fabric(self.project, H)
        self.set_hps_width('0')
        path = self.project / 'qsys_top/synth/qsys_top.v'
        original = path.read_text()
        for port, value in (('fpga2hps_arvalid', ''), ('fpga2hps_arvalid', "1'b0"), ('fpga2hps_arvalid', 'unowned_request'),
                            ('hps2fpga_arready', "1'b0"), ('hps2fpga_clk_clk', 'clk_100_out_clk_clk')):
            with self.subTest(port=port, value=value):
                source, count = re.subn(r'(?m)^(\t\w+ subsys_hps \(\n)',
                    lambda m: m[1] + '\t\t.' + port + ' (' + value + '),\n', original)
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
        # That recorded graph predates the on-chip memory removal. Project only
        # the memory away so the enabled floating bridge is what gets refused.
        with self.assertRaisesRegex(ValueError, 'initiator/translator/memory: ocm'):
            D.verify_managed_fabric(self.project, H)
        def remove_memory(root):
            for node in list(root):
                if node.get('path') == 'ocm' or (node.get('end') or '').startswith('ocm.'):
                    root.remove(node)
        self.mutate_routes(remove_memory)
        with self.assertRaisesRegex(ValueError, 'not disabled'):
            D.verify_managed_fabric(self.project, H)

    def test_generated_native_interface_cannot_remain_exposed(self):
        self.project_removals()
        path = self.project / 'qsys_top/qsys_top.sopcinfo'
        tree = ET.parse(path)
        native = tree.find("module[@path='subsys_hps.agilex_hps']/parameter[@name='componentDefinition']/value")
        definition = ET.fromstring(native.text)
        point = ET.SubElement(definition.find('boundary/interfaces'), 'interface')
        name = ET.SubElement(point, 'name')
        for exposed in ('fpga2hps', 'hps2fpga', 'hps2fpga_axi_clock'):
            name.text = exposed
            native.text = ET.tostring(definition, encoding='unicode')
            tree.write(path)
            with self.subTest(exposed=exposed), self.assertRaisesRegex(ValueError, 'remains exposed'):
                D.verify_managed_fabric(self.project, H)

    def test_retired_jtag_source_preserves_all_other_constraints_and_original_bytes(self):
        original = (self.project / 'ghrd_timing.sdc').read_bytes()
        self.project_removals()
        receipt = D.verify_managed_debug_constraints(self.project)
        self.assertEqual((self.directory / receipt['original_file']).read_bytes(), original)
        derived = (self.project / 'ghrd_timing.sdc').read_bytes()
        self.assertEqual(derived, original.replace(b'# sourcing JTAG related SDC\nsource ./jtag.sdc\n',
                          b'# Canonical MENU has no fabric JTAG interfaces.\n'))
        self.assertIn(b'create_clock -name EMIF_REF_CLOCK', derived)
        with self.assertRaisesRegex(ValueError, 'constraint input differs'):
            D.retired_jtag_sdc(original + b'\n')
        with self.assertRaisesRegex(ValueError, 'constraint input differs'):
            D.retired_jtag_sdc(derived)

    def test_retired_debug_constraints_and_their_retained_receipt_are_rechecked(self):
        self.project_removals()
        paths = [self.project / 'ghrd_timing.sdc', self.project / 'jtag.sdc',
                 self.directory / 'managed-constraints-originals/ghrd_timing.sdc',
                 self.directory / 'managed-debug-constraints.json']
        for path in paths:
            original = path.read_bytes()
            try:
                if path.suffix == '.json':
                    data = json.loads(original); data['derived_sha256'] = '0' * 64
                    path.write_text(json.dumps(data))
                else:
                    path.write_bytes(original + b'\n')
                with self.subTest(path=str(path)), self.assertRaises(ValueError):
                    D.verify_managed_fabric(self.project, H)
            finally:
                path.write_bytes(original)
        self.assertEqual(D.verify_managed_fabric(self.project, H)['retired_debug_constraints'],
                         D.verify_managed_debug_constraints(self.project))

    def test_pr_emif_derivation_removes_only_the_ignored_clock_and_stays_audited(self):
        self.project_removals()
        before = (self.project / 'ghrd_timing.sdc').read_bytes()
        receipt = D.retire_managed_emif_reference(self.project)
        after = (self.project / 'ghrd_timing.sdc').read_bytes()
        self.assertEqual(after, before.replace(D.EMIF_REFERENCE['removed_duplicate'].encode(),
            b'# Generated EMIF SDC owns the 6.000 ns LPDDR4A reference clock.'))
        self.assertEqual(receipt['emif_reference']['period_ns'], '6.000')
        self.assertEqual(D.verify_managed_fabric(self.project, H)['retired_debug_constraints'], receipt)
        with self.assertRaisesRegex(ValueError, 'already retired'):
            D.retire_managed_emif_reference(self.project)
        record_path = self.directory / 'managed-debug-constraints.json'
        for case in ('period', 'extra', 'missing_policy', 'constraint'):
            with self.subTest(case=case):
                record = json.loads(json.dumps(receipt))
                if case == 'period': record['emif_reference']['period_ns'] = '20.000'
                elif case == 'extra': record['emif_reference']['waiver'] = True
                elif case == 'missing_policy': del record['emif_reference']
                else: (self.project / 'ghrd_timing.sdc').write_bytes(after+b'\nset_false_path -from *\n')
                record_path.write_text(json.dumps(record))
                with self.assertRaises(ValueError): D.verify_managed_fabric(self.project, H)
                (self.project / 'ghrd_timing.sdc').write_bytes(after)
                record_path.write_text(json.dumps(receipt))

    @unittest.skipUnless(shutil.which('tclsh'), 'requires Tcl')
    def test_actual_removal_script_checks_targets_and_keeps_hps_instances(self):
        script = D.isolate_managed_fabric_script('remove_instance ext_hps_f2sdram_master\nsave_system top.qsys\n',
                                                'save_system top.qsys\n')
        prefix = '''set instances {ext_hps_f2sdram_master subsys_debug ace5lite_cache_coherency_translator_0 subsys_hps subsys_periph ocm}
proc get_instances {} {return $::instances}
proc remove_instance {name} {lappend ::removed $name}
proc get_connections {} {return {clk_100.out_clk/subsys_hps.fpga2hps_clk rst_in.out_reset/subsys_hps.fpga2hps_rst clk_100.out_clk/subsys_hps.hps2fpga_clk rst_in.out_reset/subsys_hps.hps2fpga_rst subsys_hps.hps2fpga/ocm.axi_s1 clk_100.out_clk/ocm.clk1 rst_in.out_reset/ocm.reset1 subsys_hps.lwhps2fpga/subsys_periph.pb_cpu_0_s0 clk_100.out_clk/subsys_hps.lwhps2fpga_clk}}
proc remove_connection {name} {lappend ::connections $name}
proc save_system {name} {}
set removed {}
set connections {}
'''
        result = subprocess.run(['tclsh'], input=prefix + script + '\nputs $removed\nputs $connections\n',
                                text=True, capture_output=True, check=True, timeout=10)
        removed, connections = result.stdout.strip().split('\n')
        self.assertEqual(removed, 'ext_hps_f2sdram_master subsys_debug ace5lite_cache_coherency_translator_0 ocm')
        # The lightweight control bridge is not a substring match for the full bridge.
        self.assertEqual(connections.split(), prefix.split('return {')[1].split('}')[0].split()[:7])
        for absent in ('subsys_debug ', ' ocm}'):
            missing = prefix.replace(absent, '}' if absent.endswith('}') else '')
            result = subprocess.run(['tclsh'], input=missing + '\nif {[catch {\n' + script +
                                    '\n} message]} {puts $message; exit 4}\n',
                                    text=True, capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 4)
            self.assertIn('managed fabric removal target changed', result.stdout)
        missing = prefix.replace('subsys_debug ', '')
        result = subprocess.run(['tclsh'], input=missing + '\nif {[catch {\n' + script +
                                '\n} message]} {puts $message; exit 4}\n',
                                text=True, capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 4)
        self.assertIn('managed fabric removal target changed', result.stdout)
        refreshed = re.sub(r'(?m)^proc get_connections .*$', 'proc get_connections {} {return {}}', prefix)
        result = subprocess.run(['tclsh'], input=refreshed + '\nif {[catch {\n' + script +
                                '\n} message]} {puts $message; exit 4}\nputs $removed\n',
                                text=True, capture_output=True, check=True, timeout=10)
        self.assertEqual(result.stdout.strip(), 'ext_hps_f2sdram_master subsys_debug ace5lite_cache_coherency_translator_0 ocm')
        for extra in ('unknown.master/subsys_hps.fpga2hps', 'unknown.master/ocm.axi_s1',
                      'subsys_hps.hps2fpga/unknown.slave'):
            unexpected = re.sub(r'(?m)^proc get_connections .*$',
                                'proc get_connections {} {return {' + extra + '}}', prefix)
            result = subprocess.run(['tclsh'], input=unexpected + '\nif {[catch {\n' + script +
                                    '\n} message]} {puts $message; exit 4}\n',
                                    text=True, capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 4)
            self.assertIn('vendor bridge/memory connection changed', result.stdout)
        for bad in ('', 'save_system top.qsys\n', 'remove_instance ext_hps_f2sdram_master\nsave_system top.qsys\nsave_system top.qsys\n'):
            with self.assertRaises(ValueError):
                D.isolate_managed_fabric_script(bad, 'save_system top.qsys\n')


if __name__ == '__main__':
    unittest.main()
