"""Cross-reader cold-start package fixtures; no programming admission."""
import hashlib
import json
from pathlib import Path
import struct
import subprocess
import sys
import tomllib
import unittest
from jsonschema import Draft202012Validator
ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / 'testdata/core-bundle-v5'

class StaticPackageFixtures(unittest.TestCase):
    def test_schema_and_sealed_two_image_cases(self):
        schema = json.loads((ROOT / 'schema/core-bundle-v5.json').read_text())
        Draft202012Validator.check_schema(schema)
        validator = Draft202012Validator(schema)
        cases = json.loads((FIXTURES / 'cases.json').read_text())
        self.assertEqual(len(cases), len({c['name'] for c in cases}))
        self.assertGreaterEqual(len(cases), 18)
        for case in cases:
            with self.subTest(case=case['name']):
                manifest, base, persona = ((FIXTURES / case[k]).read_bytes() for k in ('manifest', 'payload', 'persona'))
                d = tomllib.loads(manifest.decode())
                errors = list(validator.iter_errors(d))
                selected = d.get('static_expansion', {})
                if any(type(selected.get(k)) is not int for k in ('size', 'hardware_id')):
                    errors.append('exact integer type')
                if selected.get('size') != len(persona) or selected.get('sha256') != hashlib.sha256(persona).hexdigest():
                    errors.append('persona binding')
                if d['payload']['size'] != len(base) or d['payload']['sha256'] != hashlib.sha256(base).hexdigest():
                    errors.append('base binding')
                self.assertEqual(not errors, case['valid'], [str(e) for e in errors])
                if case['valid']:
                    digest = hashlib.sha256(b'FES-CORE-PACKAGE-5\n')
                    for value in (manifest, base, persona):
                        digest.update(struct.pack('<Q', len(value)))
                        digest.update(value)
                    self.assertEqual(digest.hexdigest(), case['package_id'])
    def test_static_asset_and_composition_golden_bindings(self):
        asset_bytes = (FIXTURES / 'expansions/ram16k/manifest.json').read_bytes()
        asset = json.loads(asset_bytes)
        self.assertEqual(asset['format'], 2)
        self.assertEqual(asset['cart_sha256'], hashlib.sha256((FIXTURES / 'expansions/ram16k/cart.rbf').read_bytes()).hexdigest())
        asset_id = hashlib.sha256(b'fes-expansion-v2\x00' + asset_bytes).hexdigest()
        manifest = tomllib.loads((FIXTURES / 'manifests/valid-basic.toml').read_text())
        for field in ('control_build_id', 'root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256', 'pof0_hash'):
            self.assertEqual(asset['partial'][field], manifest['static_expansion'][field])
        vectors = json.loads((FIXTURES / 'compositions.json').read_text())
        self.assertEqual([v['name'] for v in vectors], ['empty', 'ram16k'])
        for vector in vectors:
            c = vector['composition']
            self.assertEqual(c['expansion_id'] if 'expansion_id' in c else '', asset_id if vector['name'] == 'ram16k' else '')
            self.assertEqual(c['package_id'], asset['shell_package_id'])
            self.assertEqual(c['base_sha256'], asset['shell_sha256'])
            values = [c['package_id'], c.get('expansion_id', ''), c['base_sha256'], str(c['base_size']), c['persona_sha256'], str(c['persona_size']), c['control_build_id'], str(c['hardware_id'])]
            self.assertEqual(c['composition_id'], hashlib.sha256(b'fes-de25-static-composition-v1\x00' + '\x00'.join(values).encode()).hexdigest())
    def test_generated_bytes_current(self):
        subprocess.run([sys.executable, str(ROOT / 'scripts/core_bundle_v5_fixtures.py'), '--check'], check=True)
