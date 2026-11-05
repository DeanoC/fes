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
    def test_generated_bytes_current(self):
        subprocess.run([sys.executable, str(ROOT / 'scripts/core_bundle_v5_fixtures.py'), '--check'], check=True)
