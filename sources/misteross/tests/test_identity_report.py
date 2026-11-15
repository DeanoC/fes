import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from tools.identity import compare, report
from scripts.export_core_package import PackageExportError, encode_build_record


def fields():
    return {
        'format': 2, 'repository': 'https://example.invalid/fes', 'revision': 'a' * 40,
        'recipe': 'scripts/build.py', 'recipe_sha256': 'b' * 64,
        'abi_definition': 'abi/core.json', 'abi_definition_sha256': 'c' * 64,
        'source_path': '.', 'source_roots': ['abi', 'cores', 'scripts'],
        'source_inputs': {'scripts/build.py': 'b' * 64, 'abi/core.json': 'c' * 64,
                          'cores/core.sv': 'd' * 64, 'cores/toolchain.lock': 'e' * 64,
                          'scripts/export_core_package.py': 'f' * 64},
        'dependencies': {}, 'tools': {'yosys': 'old'},
        'parameters': {'seed': 1, 'execution_sha256': '0' * 64, 'gpu_backend': 'hip'},
    }


class IdentityReportTests(unittest.TestCase):
    def test_reporting_preserves_original_build_id_and_record(self):
        original = encode_build_record(fields())
        functional = {key: value for key, value in fields().items()
                      if key not in ('repository', 'revision', 'source_path')}
        encoded = json.dumps(functional, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
        legacy_id = hashlib.sha256(b'fes-functional-inputs-v2\0' + encoded).hexdigest()[:32]
        result = report(original)
        self.assertEqual(result['build_id'], legacy_id)
        self.assertEqual(result['record_sha256'], hashlib.sha256(original).hexdigest())
        self.assertEqual(original, encode_build_record(fields()))

    def test_provenance_changes_only_exact_record_digest(self):
        before = fields()
        after = dict(before, revision='1' * 40, source_path='sources/misteross',
                     repository='https://example.invalid/another-repository')
        result = compare(encode_build_record(before), encode_build_record(after))
        self.assertFalse(result['design_inputs_changed'])
        self.assertFalse(result['toolchain_inputs_changed'])
        self.assertTrue(result['current_build_id_matches'])
        self.assertNotEqual(result['built_with']['record_sha256'], result['candidate']['record_sha256'])

    def test_toolchain_drift_is_advisory_but_still_changes_production_id(self):
        original = fields()
        for group, key, value in (
            ('tools', 'yosys', 'new'),
            ('source_inputs', 'cores/toolchain.lock', '1' * 64),
            ('parameters', 'execution_sha256', '2' * 64),
            ('parameters', 'gpu_backend', 'cuda'),
        ):
            with self.subTest(group=group, key=key):
                changed = copy.deepcopy(original)
                changed[group][key] = value
                result = compare(encode_build_record(original), encode_build_record(changed))
                self.assertFalse(result['design_inputs_changed'])
                self.assertTrue(result['toolchain_change_only'])
                self.assertFalse(result['current_build_id_matches'])

    def test_design_and_unclassified_inputs_remain_conservative(self):
        original = fields()
        for group, key, value in (
            ('source_inputs', 'cores/core.sv', '1' * 64),
            ('source_inputs', 'scripts/export_core_package.py', '1' * 64),
            ('source_inputs', 'cores/new.mem', '1' * 64),
            ('parameters', 'seed', 2),
            ('parameters', 'pnr_recipe_sha256', '1' * 64),
            ('parameters', 'unknown_parameter', True),
        ):
            changed = copy.deepcopy(original)
            changed[group][key] = value
            result = compare(encode_build_record(original), encode_build_record(changed))
            self.assertTrue(result['design_inputs_changed'], (group, key))
            self.assertFalse(result['toolchain_change_only'])
        changed = copy.deepcopy(original)
        changed['abi_definition_sha256'] = changed['source_inputs']['abi/core.json'] = '1' * 64
        self.assertTrue(compare(encode_build_record(original), encode_build_record(changed))['design_inputs_changed'])

    def test_cli_reports_without_modifying_inputs_and_rejects_bad_records(self):
        script = Path(__file__).resolve().parents[1] / 'tools/identity.py'
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'build-inputs.json'
            original = encode_build_record(fields())
            path.write_bytes(original)
            completed = subprocess.run([sys.executable, str(script), str(path), '--current', str(path)],
                                       capture_output=True, text=True, check=True)
            self.assertTrue(json.loads(completed.stdout)['current_build_id_matches'])
            self.assertEqual(path.read_bytes(), original)
            for invalid in (b'{}', original + b' ', b'x' * 65537):
                path.write_bytes(invalid)
                result = subprocess.run([sys.executable, str(script), str(path)], capture_output=True, text=True)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stdout, '')
                self.assertIn('identity report:', result.stderr)

    def test_legacy_records_cannot_claim_separated_source_identity(self):
        legacy = fields()
        legacy['format'] = 1
        for key in ('source_inputs', 'source_roots', 'source_path'):
            del legacy[key]
        with self.assertRaisesRegex(PackageExportError, 'format-2'):
            report(encode_build_record(legacy))
