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
FIXTURES = ROOT / 'testdata' / 'core-bundle-v3'


def unique_pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError('duplicate key')
        result[key] = value
    return result


def reject_number(value):
    raise ValueError('noninteger number')


class CoreBundleV3Fixtures(unittest.TestCase):
    def test_fixture_contract(self):
        manifest_schema=json.loads((ROOT/'schema/core-bundle-v3.json').read_text())
        map_schema=json.loads((ROOT/'schema/rom-map-v1.json').read_text())
        for schema in (manifest_schema,map_schema):
            Draft202012Validator.check_schema(schema)
        cases=json.loads((FIXTURES/'cases.json').read_text())
        self.assertEqual(len(cases),len({c['name'] for c in cases}))
        for case in cases:
            with self.subTest(case=case['name']):
                manifest,payload,mapping=((FIXTURES/case[k]).read_bytes() for k in ('manifest','payload','rom_map'))
                errors=[]
                parsed=tomllib.loads(manifest.decode())
                errors.extend(Draft202012Validator(manifest_schema).iter_errors(parsed))
                rom=parsed.get('rom',{})
                if rom.get('size') != len(mapping) or rom.get('sha256') != hashlib.sha256(mapping).hexdigest():
                    errors.append('map binding')
                for key in ('size','source_size'):
                    if type(rom.get(key)) is not int:
                        errors.append('exact integer required')
                try:
                    value=json.loads(mapping.decode(),object_pairs_hook=unique_pairs,parse_float=reject_number,parse_constant=reject_number)
                    map_errors=list(Draft202012Validator(map_schema).iter_errors(value))
                    errors.extend(map_errors)
                    if not map_errors:
                        if type(value['format']) is not int:
                            errors.append('map format integer')
                        if value['base_sha256'] != hashlib.sha256(payload).hexdigest() or value['source_size'] != rom.get('source_size'):
                            errors.append('map payload/source binding')
                        blocks=value['blocks']
                        if len(blocks)*1024 != value['source_size']:
                            errors.append('block count')
                        if sorted(b['source_offset'] for b in blocks) != list(range(0,value['source_size'],1024)):
                            errors.append('source coverage')
                        bels=[b['bel'] for b in blocks]
                        if len(set(bels)) != len(bels) or any(len(b.encode())>64 for b in bels):
                            errors.append('BEL bound or duplicate')
                        bits=[bit for b in blocks for word in b['word_bits'] for bit in word]
                        if len(set(bits)) != len(bits) or any(type(b) is not int for b in bits):
                            errors.append('destination duplicate or type')
                except (ValueError,UnicodeDecodeError) as exc:
                    errors.append(str(exc))
                self.assertEqual(case['valid'],not errors, errors)
                if case['valid']:
                    digest=hashlib.sha256(b'FES-CORE-PACKAGE-3\n')
                    for data in (manifest,payload,mapping):
                        digest.update(struct.pack('<Q',len(data)));digest.update(data)
                    self.assertEqual(case['package_id'],digest.hexdigest())
                else:
                    self.assertIn(case['validation_layer'],('rom_map','package'))

    def test_generator_has_no_drift(self):
        subprocess.run([sys.executable,ROOT/'scripts/core_bundle_v3_fixtures.py','--check'],check=True)
