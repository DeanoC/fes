"""Conformance checks for the two-source, sealed format-4 fixture corpus."""

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
FIXTURES = ROOT / "testdata/core-bundle-v4"


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def reject_number(value):
    raise ValueError("noninteger JSON number")


class CoreBundleV4Fixtures(unittest.TestCase):
    def test_fixture_contract(self):
        manifest_schema = json.loads((ROOT / "schema/core-bundle-v4.json").read_text())
        map_schema = json.loads((ROOT / "schema/rom-map-v1.json").read_text())
        Draft202012Validator.check_schema(manifest_schema)
        Draft202012Validator.check_schema(map_schema)
        cases = json.loads((FIXTURES / "cases.json").read_text())
        self.assertEqual(len(cases), len({case["name"] for case in cases}))
        self.assertGreaterEqual(len(cases), 10)
        for case in cases:
            with self.subTest(case=case["name"]):
                manifest, payload, mapping = (
                    (FIXTURES / case[key]).read_bytes()
                    for key in ("manifest", "payload", "rom_map")
                )
                parsed = tomllib.loads(manifest.decode("utf-8"))
                errors = list(Draft202012Validator(manifest_schema).iter_errors(parsed))
                if case.get("archive_members", ["manifest.toml", "core.rbf", "rom-map.json"]) != [
                    "manifest.toml", "core.rbf", "rom-map.json"
                ]:
                    errors.append("archive members")
                descriptor = parsed.get("rom_map", {})
                if descriptor.get("size") != len(mapping) or descriptor.get("sha256") != hashlib.sha256(mapping).hexdigest():
                    errors.append("map binding")
                if parsed.get("payload", {}).get("size") != len(payload) or parsed.get("payload", {}).get("sha256") != hashlib.sha256(payload).hexdigest():
                    errors.append("payload binding")
                roms = parsed.get("roms", [])
                if len(roms) == 2:
                    if [r.get("role") for r in roms] != ["firmware", "cartridge"]:
                        errors.append("ordered roles")
                    if len({r.get("id") for r in roms}) != 2:
                        errors.append("unique IDs")
                    if any(type(r.get(k)) is not int for r in roms for k in ("source_size", "source_offset")):
                        errors.append("exact integer")
                    elif roms[0]["source_offset"] != 0 or roms[1]["source_offset"] != roms[0]["source_size"] or sum(r["source_size"] for r in roms) > 262144:
                        errors.append("source coverage")
                try:
                    value = json.loads(mapping.decode("utf-8"), object_pairs_hook=unique_pairs,
                                       parse_float=reject_number, parse_constant=reject_number)
                    map_errors = list(Draft202012Validator(map_schema).iter_errors(value))
                    errors.extend(map_errors)
                    if not map_errors:
                        total = sum(r["source_size"] for r in roms) if len(roms) == 2 else None
                        if value["base_sha256"] != hashlib.sha256(payload).hexdigest() or value["source_size"] != total:
                            errors.append("map source/base binding")
                        blocks = value["blocks"]
                        if sorted(b["source_offset"] for b in blocks) != list(range(0, value["source_size"], 1024)):
                            errors.append("map block coverage")
                        bels = [b["bel"] for b in blocks]
                        bits = [bit for b in blocks for word in b["word_bits"] for bit in word]
                        if len(set(bels)) != len(bels) or len(set(bits)) != len(bits):
                            errors.append("duplicate map destination")
                except (ValueError, UnicodeDecodeError) as exc:
                    errors.append(str(exc))
                self.assertEqual(case["valid"], not errors, errors)
                if case["valid"]:
                    digest = hashlib.sha256(b"FES-CORE-PACKAGE-4\n")
                    for data in (manifest, payload, mapping):
                        digest.update(struct.pack("<Q", len(data)))
                        digest.update(data)
                    self.assertEqual(case["package_id"], digest.hexdigest())

    def test_generator_has_no_drift(self):
        subprocess.run([sys.executable, ROOT / "scripts/core_bundle_v4_fixtures.py", "--check"], check=True)


if __name__ == "__main__":
    unittest.main()
