"""Core-independent board contract and per-program assertions; no hardware."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('selected_hps_context', ROOT/'platform/embedded/nano/hps_context.py')
hps = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hps)

HPS_IP = 'hps_subsys/ip/hps_subsys/agilex_hps.ip'
EMIF_IP = 'hps_subsys/ip/qsys_top/emif_io96b_hps.ip'
ADAPTER_IP = 'hps_subsys/ip/hps_subsys/f2sdram_adapter.ip'
IP_NS = 'http://www.accellera.org/XMLSchema/IPXACT/1685-2014'
ALTERA_NS = 'http://www.altera.com/XMLSchema/IPXact2014/extensions'


class SelectedProgramTests(unittest.TestCase):
    def project(self, folder):
        project = Path(folder)
        hps_dir = project/'board/hps'
        hps_dir.mkdir(parents=True)
        before = {HPS_IP: {'f2s_data_width': '256', 'MPU_clock': '800'},
                  EMIF_IP: {'EMIF_PROTOCOL': 'LPDDR4', 'memory_timing': 'fixed'},
                  ADAPTER_IP: {'DATA_WIDTH': '256'}}
        (hps_dir/'inputs.json').write_text(json.dumps({'vendor_hps_emif_parameters': before}))
        after = copy.deepcopy(before)
        after[HPS_IP]['f2s_data_width'] = '0'
        after[HPS_IP].update({key: 'true' for key in ('ED_en_sim_gen', 'FPGA_EMAC0_tx_clk_en',
            'FPGA_EMAC1_tx_clk_en', 'FPGA_EMAC2_tx_clk_en', 'FPGA_EMAC_PTP_clk_en')})
        for name, values in after.items():
            self.write_ip(hps_dir/'project'/name, values)
        return project, before, after

    def write_ip(self, path, values, duplicate=None):
        path.parent.mkdir(parents=True, exist_ok=True)
        root = ET.Element('{'+IP_NS+'}component')
        extension = ET.SubElement(root, '{'+ALTERA_NS+'}altera_module_parameters')
        parameters = ET.SubElement(extension, '{'+IP_NS+'}parameters')
        for key, value in [*values.items(), *([(duplicate, values[duplicate])] if duplicate else [])]:
            row = ET.SubElement(parameters, '{'+IP_NS+'}parameter', parameterId=key)
            ET.SubElement(row, '{'+IP_NS+'}value').text = value
        path.write_bytes(ET.tostring(root))

    def test_full_parameter_inventory_is_fixed_and_read_only(self):
        with tempfile.TemporaryDirectory() as folder:
            project, before, after = self.project(folder)
            snapshot = {p: p.read_bytes() for p in project.rglob('*') if p.is_file()}
            self.assertEqual(hps._program_parameters(project), after)
            self.assertEqual({p: p.read_bytes() for p in project.rglob('*') if p.is_file()}, snapshot)
            for fault in ('memory', 'missing', 'extra', 'duplicate', 'coherent-port', 'symlink'):
                selected_ip = HPS_IP if fault == 'coherent-port' else EMIF_IP
                path = project/'board/hps/project'/selected_ip
                original = path.read_bytes()
                changed = dict(after[selected_ip])
                try:
                    if fault == 'memory':
                        changed['memory_timing'] = 'other'
                    elif fault == 'missing':
                        changed.pop('memory_timing')
                    elif fault == 'extra':
                        changed['new_parameter'] = '1'
                    elif fault == 'coherent-port':
                        changed['f2s_data_width'] = '256'
                    self.write_ip(path, changed, duplicate='memory_timing' if fault == 'duplicate' else None)
                    if fault == 'symlink':
                        target = project/'outside.ip'
                        target.write_bytes(path.read_bytes())
                        path.unlink()
                        path.symlink_to(target)
                    with self.subTest(fault=fault), self.assertRaises(ValueError):
                        hps._program_parameters(project)
                finally:
                    path.unlink()
                    path.write_bytes(original)

    def test_platform_digest_ignores_program_identity_but_binds_board_facts(self):
        source = json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-context.json').read_bytes())
        source['programming_profile'] = 'de25-embedded-hps-first-ddr-diagnostic-v1'
        source['control_abi'] = 0x10203041
        other = dict(source, package_id='a'*64, build_id='b'*32, core_sha256='c'*64,
                     core_size=128, core_id='fes.other-core', overlay_revision='d'*40,
                     raw_sof_sha256='e'*64, original_pair_sha256='f'*64)
        with patch.object(hps, '_program_parameters', return_value={'full-hps-and-ddr': 'same'}), \
             patch.object(hps, '_pinmux_module', return_value=SimpleNamespace(configuration_sha256=lambda project, source: '1'*64)), \
             patch.object(hps, '_fabric_stop_inventory', return_value={'complete-master-inventory': 'same'}):
            first = hps._program_platform(Path('fixture'), source)
            second = hps._program_platform(Path('fixture'), other)
            self.assertEqual(first['platform'], second['platform'])
            self.assertEqual(first['platform']['format'],2)
            self.assertEqual(first['platform']['scope'],'nano-contained-program-platform-v2')
            self.assertEqual(first['platform']['hps_configuration_sha256'],'1'*64)
            self.assertNotEqual(first['boot_context_sha256'], second['boot_context_sha256'])
            for field in ('device', 'hps_io_hash', 'pfg_identity', 'control_abi', 'abi'):
                changed = dict(source, **{field: source[field]+1 if isinstance(source[field], int) else source[field]+'changed'})
                self.assertNotEqual(first['platform'], hps._program_platform(Path('fixture'), changed)['platform'])
            with patch.object(hps, '_program_parameters', return_value={'full-hps-and-ddr': 'different'}):
                self.assertNotEqual(first['platform'], hps._program_platform(Path('fixture'), source)['platform'])

    def test_selected_record_binds_maps_and_requires_explicit_build_mode(self):
        source = {'package_id': 'a'*64, 'build_id': 'b'*32}
        contract = {'platform': {'complete': 'fixture'}}
        with patch.object(hps, '_derive', return_value=source), \
             patch.object(hps, '_program_platform', return_value=contract), \
             patch.object(hps, '_control', return_value={'owned': 'control'}), \
             patch.object(hps, '_computer', return_value={'owned': 'computer'}):
            record = hps._selected_program('fixture')
            self.assertEqual(record['package_id'], source['package_id'])
            self.assertEqual(record['build_id'], source['build_id'])
            self.assertEqual(record['platform'], contract['platform'])
            self.assertEqual(record['control'], {'owned': 'control'})
        build = {'program': 'fpgaload', 'hps_project': 'fixture', 'hps_computer': {},
                 'selected_programs': True, 'computer_reload': True, 'computer_storage_return': True,
                 'hps_program_platform': contract, 'hps_program_platform_sha256': hps.digest(contract)}
        with patch.object(hps, 'checked_computer', return_value={'owned': 'computer'}), \
             patch.object(hps, 'program_platform', return_value=contract):
            self.assertEqual(hps.checked_program_platform(build), contract)
            for change in ({'selected_programs': False}, {'selected_programs': 1},
                           {'computer_storage_return': False}, {'computer_reload': False, 'computer_storage_return': False}, {'program': 'probe'},
                           {'hps_program_platform': {}}, {'hps_program_platform_sha256': '0'*64}):
                with self.subTest(change=change), self.assertRaises(ValueError):
                    hps.checked_program_platform(dict(build, **change))
        self.assertIsNone(hps.checked_program_platform({'program': 'probe'}))


if __name__ == '__main__':
    unittest.main()
