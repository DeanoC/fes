"""Independent decoder for the fes.ata-disk.delta 1.0 golden vector."""
import hashlib
import json
from pathlib import Path
import struct
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = ROOT / "testdata/media-data-v1/ata-disk-delta.json"
CHUNK = 65536


def decode_delta(data, core_id, game_id, base_id):
    if len(data) < 174 or data[:8] != b"FESDELT1":
        raise ValueError("envelope")
    if hashlib.sha256(data[:-32]).digest() != data[-32:]:
        raise ValueError("checksum")
    if (data[8:40], data[40:72], data[72:104]) != (hashlib.sha256(core_id.encode()).digest(),
                                                   hashlib.sha256(game_id.encode()).digest(), bytes.fromhex(base_id)):
        raise ValueError("binding")
    unit, major, minor, length, base, chunk, count = struct.unpack_from("<HHHHIII", data, 104)
    if (unit, major, minor, length, chunk) != (0, 1, 0, 18, CHUNK) or data[124:142] != b"fes.ata-disk.delta":
        raise ValueError("layout")
    if base % 512 or not 1 << 20 <= base <= 128 << 20:
        raise ValueError("base size")
    if len(data) != 174 + 36 * count:
        raise ValueError("length")
    entries, previous = [], -1
    for at in range(142, 142 + 36 * count, 36):
        index = struct.unpack_from("<I", data, at)[0]
        if index <= previous or index * CHUNK >= base:
            raise ValueError("index")
        previous = index
        entries.append((index, data[at + 4:at + 36].hex()))
    return base, entries


class AtaDiskDelta(unittest.TestCase):
    def test_golden_vector(self):
        v = json.loads(FIXTURE.read_text())
        record = bytes.fromhex(v["record_hex"])
        base, entries = decode_delta(record, v["core_id"], v["game_id"], v["base_media_id"])
        self.assertEqual(base, v["base_size"])
        self.assertEqual(entries, [(c["index"], c["sha256"]) for c in v["chunks"]])
        image = bytearray((at * 7 + (at >> 11)) & 255 for at in range(base))
        self.assertEqual(hashlib.sha256(image).hexdigest(), v["base_media_id"])
        for c in v["chunks"]:
            data = bytes((c["index"] * 31 + at * 13 + (at >> 8)) & 255 for at in range(c["bytes"]))
            self.assertEqual(hashlib.sha256(data).hexdigest(), c["sha256"])
            self.assertEqual(c["file"], "chunk-" + c["sha256"])
            self.assertEqual(c["bytes"], min(CHUNK, base - c["index"] * CHUNK))
            image[c["index"] * CHUNK:c["index"] * CHUNK + len(data)] = data
        self.assertEqual(hashlib.sha256(image).hexdigest(), v["restored_sha256"])
        self.assertEqual(zlib.crc32(image), v["restored_crc32"])
        self.assertEqual(hashlib.sha256(record).hexdigest(), v["revision"])
        namespace = "\0".join(["fes-media-data-v1", v["core_id"], v["game_id"], "0", v["base_media_id"]])
        self.assertEqual(hashlib.sha256(namespace.encode()).hexdigest(), v["namespace"])
        for row in v["invalid"]:
            with self.subTest(name=row["name"]), self.assertRaises(ValueError):
                decode_delta(bytes.fromhex(row["hex"]), v["core_id"], v["game_id"], v["base_media_id"])


if __name__ == "__main__":
    unittest.main()
