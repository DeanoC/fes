"""DE25 async-input recipe bindings for canonical producer model IDs."""
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from boards.de25nano.scanout import async_inputs


class AsyncInputBindingTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='de25-async-binding-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.fit = self.root / 'signoff'
        self.hps = self.fit / 'combined/hps'
        (self.hps / 'project/qdb').mkdir(parents=True)
        (self.hps / 'project/qdb/fixture.qdb').write_bytes(b'compiled database fixture')
        (self.hps / 'project/output_files').mkdir()
        for name in ('generation.json', 'linux-boot.core.rbf', 'project/golden_top.qsf',
                     'project/output_files/golden_top.tq.drc.signoff.rpt',
                     'project/output_files/golden_top.sta.summary'):
            path = self.hps / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(('fixture: ' + name).encode())

    def test_spectrum_128_binds_to_the_spectrum_computer_recipe(self):
        record = json.dumps({'format': 1, 'recipe':
            'boards/de25nano/producers/build_spectrum_package.py', 'revision': 'a' * 40,
            'parameters': {'core_id': 'fes.spectrum128'}}).encode()
        identity = hashlib.sha256(record).hexdigest()[:32]
        (self.root / 'build-inputs.json').write_bytes(record)
        (self.hps / 'inputs.json').write_text(json.dumps({
            'fes_revision': 'a' * 40, 'computer': {'build_id': identity}}))

        result = async_inputs.bindings(self.fit)

        self.assertEqual(result['build_record_sha256'], hashlib.sha256(record).hexdigest())

    def test_ramtest_accepts_only_its_own_review_recipe(self):
        record = {'format': 1, 'recipe': 'boards/de25nano/producers/build_ramtest_package.py',
                  'revision': 'a' * 40, 'parameters': {
                      'core_id': 'fes.ramtest', 'standard_core': True,
                      'programming_profile': 'de25-linux-computer-v1'}}
        raw = json.dumps(record).encode()
        identity = hashlib.sha256(raw).hexdigest()[:32]
        (self.root / 'build-inputs.json').write_bytes(raw)
        inputs = {'fes_revision': 'a' * 40, 'standard_zx81': {'build_id': identity},
                  'scanout': {'build_identity_scheme': 'canonical-standard-computer-v2',
                              'build_id': identity}}
        (self.hps / 'inputs.json').write_text(json.dumps(inputs))
        self.assertEqual(async_inputs.bindings(self.fit)['build_record_sha256'], hashlib.sha256(raw).hexdigest())
        inputs['scanout']['build_id'] = '0' * 32
        (self.hps / 'inputs.json').write_text(json.dumps(inputs))
        with self.assertRaisesRegex(async_inputs.BuildError, 'standard profile/scanout identity mismatch'):
            async_inputs.bindings(self.fit)
        record['recipe'] = 'boards/de25nano/producers/build_spectrum_package.py'
        (self.root / 'build-inputs.json').write_text(json.dumps(record))
        with self.assertRaisesRegex(async_inputs.BuildError, 'unsupported canonical input-review core/recipe'):
            async_inputs.bindings(self.fit)

    def test_unknown_core_cannot_reuse_the_spectrum_recipe(self):
        record = json.dumps({'format': 1, 'recipe':
            'boards/de25nano/producers/build_spectrum_package.py', 'revision': 'a' * 40,
            'parameters': {'core_id': 'fes.spectrum128-other'}}).encode()
        (self.root / 'build-inputs.json').write_bytes(record)
        with self.assertRaisesRegex(async_inputs.BuildError, 'unsupported canonical input-review core/recipe'):
            async_inputs.bindings(self.fit)


if __name__ == '__main__':
    unittest.main()
