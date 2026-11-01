"""Source-context identity and omission failures, without programming hardware."""
import importlib.util
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
NANO = ROOT/'platform/embedded/nano'
spec = importlib.util.spec_from_file_location('test_hps_context', NANO/'hps_context.py')
context = importlib.util.module_from_spec(spec)
spec.loader.exec_module(context)
spec = importlib.util.spec_from_file_location('test_hps_cold', NANO/'cold_profile.py')
cold = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cold)
GOLDEN = ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-context.json'


class HPSContextTests(unittest.TestCase):
    def setUp(self):
        self.descriptor = json.loads(GOLDEN.read_bytes())
        self.record = {'program': 'bootcontext', 'hps_project': 'sealed-project',
                       'hps_context': self.descriptor,
                       'hps_context_sha256': context.digest(self.descriptor)}

    def test_canonical_descriptor_matches_both_consumers(self):
        self.assertEqual(context.encode(self.descriptor), GOLDEN.read_bytes())
        self.assertEqual(context.digest(self.descriptor),
                         '94da3557f175eae05144d70937c3afceda20c6c9f9ca9d25155535da3e0814fc')
        with patch.object(context, 'derive', return_value=self.descriptor) as derive:
            self.assertEqual(context.checked(self.record), self.descriptor)
            derive.assert_called_once_with('sealed-project')
        header = cold.header({'sd_pio': False, 'chain_sha256': 'a'*64,
                              'owner_seed': 1, 'entry_image_sha256': 'b'*64,
                              'entry_image_size': 128,
                              'hps_context_sha256': self.record['hps_context_sha256']})
        digest = self.record['hps_context_sha256']
        self.assertIn('#define FES_NANO_HPS_CONTEXT_SHA {'+
                      ','.join('0x'+digest[i:i+2] for i in range(0, 64, 2))+'}', header)

    def test_native_receipt_key_and_source_or_digest_changes_reject(self):
        entry = dict(self.record)
        entry['native_program'] = entry.pop('program')
        with patch.object(context, 'derive', return_value=self.descriptor):
            self.assertEqual(context.checked(entry, program_key='native_program'), self.descriptor)
            for key, value in (('hps_context_sha256', 'f'*64),
                               ('hps_context', dict(self.descriptor, package_id='f'*64)),
                               ('program', 'probe')):
                with self.subTest(key=key), self.assertRaises(ValueError):
                    context.checked(dict(self.record, **{key: value}))
            changed = dict(self.descriptor, hps_io_hash='f'*64)
            with patch.object(context, 'derive', return_value=changed):
                with self.assertRaises(ValueError):
                    context.checked(self.record)

    def test_partial_or_omitted_context_cannot_claim_bootcontext(self):
        for key in ('hps_context', 'hps_context_sha256', 'hps_project'):
            partial = dict(self.record)
            del partial[key]
            with self.subTest(key=key), self.assertRaises(ValueError):
                context.checked(partial)
        with self.assertRaises(ValueError):
            context.checked({'program': 'bootcontext'})
        with patch.object(context, 'derive', side_effect=AssertionError('unexpected project access')):
            self.assertIsNone(context.checked({'program': 'probe'}))

    def test_cli_rejects_wrong_board_program_and_missing_project_before_output(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)/'not-created'
            common = [sys.executable, str(ROOT/'platform/embedded/tamago/run.py'),
                      '--compiler', '/bin/false', '--output', str(output)]
            for args in (['bootcontext'], ['fpgacontext'],
                         ['bootcontext', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json')],
                         ['fpgacontext', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json')],
                         ['fpgacontext', '--board', 'de25-nano', '--profile', str(NANO/'sd-profile.json'),
                          '--hps-first-project', 'absent'],
                         ['probe', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json'),
                          '--hps-first-project', 'absent']):
                with self.subTest(args=args):
                    result = subprocess.run(common+args, capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, 2, result.stderr)
                    self.assertFalse(output.exists())

    def test_invalid_project_cannot_change_a_callers_firmware_imports(self):
        # The FPGA verifier's sys.path edits previously caused the following
        # firmware import to resolve the wrong verify.py during a real build.
        source = '''
import sys
import tempfile
from pathlib import Path
sys.path.insert(0, 'platform/embedded/nano')
import hps_context
before = sys.path[:]
(Path('out')).mkdir(exist_ok=True)
with tempfile.TemporaryDirectory(dir='out') as broken:
    try:hps_context.derive(broken)
    except (ValueError, OSError):pass
    else:raise AssertionError('invalid project accepted')
assert before == sys.path
sys.path.insert(0, 'platform/embedded/firmware')
import prepare_nano_owned
assert callable(prepare_nano_owned.function)
'''
        subprocess.run([sys.executable, '-c', source], cwd=ROOT,
                       env=dict(os.environ, PYTHONPATH=str(ROOT/'out/nano-bridge-diag-test-python')),
                       check=True, capture_output=True, timeout=15)

    def test_control_route_decoder_requires_compiler_hashes_exact_route_and_no_overlap(self):
        # These two connections are copied from the actual compiler handoff.
        # Only the decoder is under test; the project seal is explicitly mocked.
        fixture=ROOT/'tests/fixtures/nano/control-route.xml'
        for fault in ('pass','file','compiler','bridge','offset','overlap','duplicate','span','width'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as temp:
                project=Path(temp);hps=project/'board/hps';(hps/'project/qsys_top').mkdir(parents=True)
                xml=ET.fromstring(fixture.read_bytes())
                lw=next(c for c in xml if c.get('start')=='subsys_hps_agilex_hps.lwhps2fpga')
                endpoint=next(c for c in xml if c.get('start')=='subsys_periph_pb_cpu_0.m0')
                def parameter(c,name):return next(p.find('value') for p in c.findall('parameter') if p.get('name')==name)
                address_map=ET.fromstring(parameter(lw,'addressMapSysInfo').text)
                slot=next(s for s in address_map if s.get('name')=='subsys_periph.fes_embedded.avs')
                if fault=='bridge':parameter(lw,'baseAddress').text='0x100000'
                if fault=='offset':parameter(endpoint,'baseAddress').text='0x10500'
                if fault=='overlap':ET.SubElement(address_map,'slave',name='overlap',start='0x10440',end='0x10480')
                if fault=='duplicate':xml.append(ET.fromstring(ET.tostring(lw)))
                if fault=='span':slot.set('end','0x10600')
                if fault=='width':slot.set('datawidth','64')
                parameter(lw,'addressMapSysInfo').text=ET.tostring(address_map,encoding='unicode')
                raw=ET.tostring(xml);digest=hashlib.sha256(raw).hexdigest()
                (hps/'project/qsys_top/qsys_top.sopcinfo').write_bytes(raw+(b' ' if fault=='file' else b''))
                (hps/'generation.json').write_text(json.dumps({'top_system_handoff':'qsys_top/qsys_top.sopcinfo','sha256':digest}))
                (hps/'build.json').write_text(json.dumps({'generated_handoff_and_sdc_sha256':{
                    'qsys_top/qsys_top.sopcinfo':'f'*64 if fault=='compiler' else digest}}))
                with patch.object(context,'_derive',return_value=self.descriptor):
                    if fault=='pass':
                        mapping=context._control(project)
                        self.assertEqual(mapping['cpu_base'],0x20010400)
                        self.assertEqual(mapping['span'],256)
                        self.assertEqual(mapping['handoff_sha256'],digest)
                    else:
                        with self.assertRaises(ValueError):context._control(project)

    def test_control_map_cannot_be_added_omitted_or_changed_outside_dedicated_image(self):
        mapping=json.loads((ROOT/'platform/embedded/tamago/nativefabric/testdata/hps-control.json').read_bytes())
        record=dict(self.record,program='fpgacontext',hps_control=mapping,hps_control_sha256=context.digest(mapping))
        with patch.object(context,'derive',return_value=self.descriptor),patch.object(context,'control',return_value=mapping):
            self.assertEqual(context.checked_control(record),mapping)
            for changed in (dict(record,program='bootcontext'),dict(record,hps_control=None),
                            dict(record,hps_control_sha256='f'*64),
                            dict(record,hps_control=dict(mapping,cpu_base=0xff210400))):
                with self.subTest(changed=changed),self.assertRaises(ValueError):context.checked_control(changed)
            with self.assertRaises(ValueError):context.checked_control(dict(self.record,program='fpgacontext'))
            with self.assertRaises(ValueError):context.checked({'program':'fpgacontext'})
            self.assertIsNone(context.checked_control(self.record))
        header=cold.header({'sd_pio':False,'chain_sha256':'a'*64,'owner_seed':1,
                            'entry_image_sha256':'b'*64,'entry_image_size':128,
                            'hps_context':self.descriptor,'hps_context_sha256':context.digest(self.descriptor),
                            'hps_control':mapping,'hps_control_sha256':context.digest(mapping)})
        self.assertIn('#define FES_NANO_CONTROL_PROBE 1',header)
        self.assertIn('#define FES_NANO_CONTROL_BASE 0x20010400UL',header)
        self.assertIn('#define FES_NANO_CONTROL_BUILD {0x0ea033e5U,0xbddf4c77U,0x1d14a2bbU,0xe25bb65aU}',header)


if __name__ == '__main__':
    unittest.main()
