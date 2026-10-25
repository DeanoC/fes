"""Native HPS I2C identity and complete fitted-boundary regression checks."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]


def module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / (name + '.py'))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


auditor = module('audit_fes_pcxt_hps_i2c')
reference = module('characterize_fes_pcxt_hps_i2c')


def mapping(names):
    return json.dumps({'count':len(names), 'pins':{n:[0, n] for n in names}}).encode().hex()


def design():
    cells = {}
    ports = {}
    hps = dict(type=auditor.HPS, parameters={},
               attributes={'NEXTPNR_BEL':auditor.SITE,
                           'FES_PINMAP_V1':mapping(('out_clk','out_data','scl','sda'))},
               port_directions={'out_clk':'output','out_data':'output','scl':'input','sda':'input'},
               connections={'out_clk':[4], 'out_data':[5], 'scl':[6], 'sda':[7]})
    cells['controller'] = hps
    cells['zero'] = dict(type='MISTRAL_CONST', parameters={'LUT':'0'*32},
                         attributes={'FES_PINMAP_V1':json.dumps({'count':1, 'pins':{'Q':[0,'COMBOUT']}}).encode().hex()},
                         port_directions={'Q':'output'}, connections={'Q':[8]})
    for i, (external, (pin, bel, output, feedback)) in enumerate(auditor.PADS.items()):
        ports[external] = {'direction':'inout', 'bits':[i+2]}
        cells['pad' + str(i)] = dict(type='MISTRAL_IO', parameters={},
            attributes={'LOC':pin, 'NEXTPNR_BEL':bel, 'IO_STANDARD':'3.3-V LVTTL',
                        'FES_PINMAP_V1':mapping(('I','O','OE','PAD'))},
            port_directions={'I':'input','O':'output','OE':'input','PAD':'inout'},
            connections={'PAD':[i+2], 'I':[8], 'OE':hps['connections'][output], 'O':hps['connections'][feedback]})
    return {'modules':{'top':{'ports':ports, 'cells':cells}}}


class HpsI2cAuditTests(unittest.TestCase):
    def test_actual_port_roles_do_not_depend_on_private_cell_names(self):
        result = auditor.audit(design())
        self.assertTrue(result['structure_valid'])
        self.assertEqual(len(result['endpoints']), 4)
        self.assertEqual(len(result['unsupported_native_boundaries']), 4)
        self.assertFalse(result['physical_qualified'])
        self.assertIsNone(result['declared_phase_relation_to_fpga_clocks'])

    def test_high_data_wrong_oe_source_and_crossed_feedback_rejected(self):
        for mutation in ('high','oe','feedback'):
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                d = design(); cells = d['modules']['top']['cells']
                if mutation == 'high':
                    cells['zero']['parameters']['LUT'] = '1'*32
                elif mutation == 'oe':
                    cells['pad0']['connections']['OE'] = [5]
                else:
                    cells['controller']['connections']['scl'] = [7]
                auditor.audit(d)

    def test_wrong_site_pad_and_pin_polarity_rejected(self):
        for mutation in ('site','pad','polarity'):
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                d = design(); cells = d['modules']['top']['cells']
                if mutation == 'site':
                    cells['controller']['attributes']['NEXTPNR_BEL'] = auditor.HPS + '.52.59.0'
                elif mutation == 'pad':
                    cells['pad0']['attributes']['LOC'] = 'PIN_AA4'
                else:
                    p = json.loads(bytes.fromhex(cells['pad0']['attributes']['FES_PINMAP_V1']))
                    p['pins']['OE'][0] = 3
                    cells['pad0']['attributes']['FES_PINMAP_V1'] = json.dumps(p).encode().hex()
                auditor.audit(d)

    def test_unsupported_zero_delays_are_unknown_and_acceptance_cannot_be_injected(self):
        d = design(); result = auditor.audit(d)
        arcs = [dict(e, analogue_supported=False, native_slow100_rise_ps=[0,0],
                     native_slow100_fall_ps=[0,0]) for e in result['endpoints']]
        result = auditor.audit(d, {'arcs':arcs, 'physical_qualified':True})
        self.assertEqual(len(result['unsupported_native_boundaries']), 4)
        self.assertTrue(all(r['rise_ps'] is None and r['fall_ps'] is None for r in result['native_observations']))
        self.assertFalse(result['physical_qualified'])
        self.assertIsNone(result['hard_ip_launch_bounds_ns'])

    def test_missing_duplicate_wrong_boundary_and_nonfinite_delays_rejected(self):
        d = design(); endpoints = auditor.audit(d)['endpoints']
        original = [dict(e, analogue_supported=True, native_slow100_rise_ps=[2,4],
                         native_slow100_fall_ps=[1,3]) for e in endpoints]
        for mutation in ('missing','duplicate','source','delay'):
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                rows = copy.deepcopy(original)
                if mutation == 'missing': rows.pop()
                elif mutation == 'duplicate': rows[-1] = rows[0]
                elif mutation == 'source': rows[0]['source_port'] = 'out_data'
                else: rows[0]['native_slow100_rise_ps'] = [0,float('nan')]
                auditor.audit(d, {'arcs':rows})

    def test_cli_rejects_rbf_changed_since_immutable_route(self):
        with tempfile.TemporaryDirectory() as directory:
            p = Path(directory)
            (p/'routed.json').write_text(json.dumps(design()))
            (p/'diagnostic.rbf').write_bytes(b'original')
            (p/'diagnostic.json').write_text('{}')
            receipt = dict(completed=True, postflight_identity_pass=True, returncode=0, output=str(p),
                           artifacts_sha256={str(x):hashlib.sha256(x.read_bytes()).hexdigest() for x in p.iterdir()})
            (p/'receipt.json').write_text(json.dumps(receipt))
            (p/'diagnostic.rbf').write_bytes(b'changed')
            with mock.patch.object(sys, 'argv', ['audit', '--route-receipt', str(p/'receipt.json'),
                                                '--output', str(p/'result.json')]), self.assertRaises(ValueError):
                auditor.main()
            self.assertFalse((p/'result.json').exists())


class HpsI2cReferenceTests(unittest.TestCase):
    def setUp(self):
        fixture = ROOT / 'tests/fixtures/pcxt_hps_i2c_timing'
        self.output = (fixture/'output-path.rpt').read_text()
        self.input = (fixture/'input-path.rpt').read_text()

    def parse(self, text):
        return reference.parse_path(text, 'SDA', 'out', 'fall', 'late', reference.CORNERS[0])

    def test_real_fitted_oe_and_feedback_paths_have_complete_boundaries(self):
        output = self.parse(self.output)
        self.assertEqual(output['source'], 'hdmi_i2c|out_data')
        self.assertEqual(output['delay_ns'], '9.413')
        incoming = reference.parse_path(self.input, 'SDA', 'in', 'rise', 'late', reference.CORNERS[0])
        self.assertEqual(incoming['destination'], 'hdmi_i2c|sda')
        self.assertEqual(incoming['delay_ns'], '6.316')

    def test_data_in_place_of_oe_wrong_site_and_missing_buffer_rejected(self):
        for text in (self.output.replace('~output|oe','~output|i'),
                     self.output.replace('X52_Y60_N111','X52_Y59_N111'),
                     '\n'.join(line for line in self.output.splitlines() if '~output|o ' not in line)):
            with self.subTest(text=text[:20]), self.assertRaises(ValueError):
                self.parse(text)

    def test_duplicate_source_transition_and_delay_corruption_rejected(self):
        for text in (self.output*2, self.output.replace('out_data','out_clk'),
                     self.output.replace('; Delay     ; 9.413', '; Delay     ; NaN'),
                     self.output.replace(';   4.816 ;', '; 123.456 ;')):
            with self.subTest(text=text[:20]), self.assertRaises((ValueError, ArithmeticError)):
                self.parse(text)

    def test_source_identity_ignores_only_known_quartus_bookkeeping(self):
        text = reference.sources(0)['top.qsf']
        self.assertEqual(reference.normalized_source('top.qsf', text+'\nset_global_assignment -name LAST_QUARTUS_VERSION "17.0.2 Lite Edition"\n'), text)
        self.assertNotEqual(reference.normalized_source('top.qsf', text.replace('AUTO_OPEN_DRAIN_PINS OFF', 'AUTO_OPEN_DRAIN_PINS ON')), text)


if __name__ == '__main__':
    unittest.main()
