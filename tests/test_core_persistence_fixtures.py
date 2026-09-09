"""Independent checks of the bounded core-data record and GP wire fixtures."""
import hashlib
import json
from pathlib import Path
import struct
import unittest

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "testdata/core-persistence-v1"


def decode_record(data, core_id):
    if not 81 <= len(data) <= 688 or data[:8] != b"FESDATA1":
        raise ValueError("invalid envelope")
    if hashlib.sha256(data[:-32]).digest() != data[-32:]:
        raise ValueError("checksum")
    if data[8:40] != hashlib.sha256(core_id.encode()).digest():
        raise ValueError("core identity")
    length, major, minor, count = struct.unpack_from("<HHHH", data, 40)
    if not 1 <= length <= 96 or not 1 <= count <= 256:
        raise ValueError("length")
    if len(data) != 48 + length + count * 2 + 32:
        raise ValueError("length")
    if data[48:48+length] != b"fes.pong.progress" or (major, minor, count) != (1, 0, 2):
        raise ValueError("layout")
    words = list(struct.unpack_from("<HH", data, 48 + length))
    if words[0] > 2:
        raise ValueError("setting")
    return words


class CorePersistenceFixtures(unittest.TestCase):
    def test_records_match_independent_decoder(self):
        fixture = json.loads((FIXTURES / "records.json").read_text())
        self.assertEqual(fixture["core_id"], "fes.pong")
        names = set()
        for row in fixture["valid"]:
            names.add(row["name"])
            data = bytes.fromhex(row["hex"])
            with self.subTest(name=row["name"]):
                self.assertEqual(decode_record(data, fixture["core_id"]), row["words"])
                self.assertEqual(hashlib.sha256(data).hexdigest(), row["revision"])
        self.assertEqual(names, {"defaults", "fast-rally", "saturated-rally"})
        for row in fixture["invalid"]:
            with self.subTest(name=row["name"]), self.assertRaises(ValueError):
                decode_record(bytes.fromhex(row["hex"]), fixture["core_id"])
        self.assertGreaterEqual(len(fixture["invalid"]), 10)

    def test_wire_fields_and_toggle_sequence(self):
        fixture = json.loads((FIXTURES / "exchanges.json").read_text())
        toggle = fixture["initial_request_toggle"]
        self.assertFalse(toggle)
        names = set()
        for row in fixture["exchanges"]:
            with self.subTest(name=row["name"]):
                names.add(row["name"])
                fields = row["opcode"] << 24 | row["index"] << 16 | row["argument"]
                self.assertEqual(row["gpo"], [fields | (int(toggle) << 31), fields | (int(not toggle) << 31)])
                toggle = not toggle
                self.assertEqual(row["gpi"], 0xf5000000 | (int(toggle) << 23) | (int(row["error"]) << 22) | row["data"])
        self.assertTrue({"incomplete-commit", "invalid-speed-commit", "freeze", "freeze-again", "snapshot-rally", "resume", "read-after-resume"} <= names)


if __name__ == "__main__":
    unittest.main()
