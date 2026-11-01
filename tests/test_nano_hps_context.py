"""Source-context identity and omission failures, without programming hardware."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

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
            for args in (['bootcontext'],
                         ['bootcontext', '--board', 'de25-nano', '--profile', str(NANO/'probe-profile.json')],
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


if __name__ == '__main__':
    unittest.main()
