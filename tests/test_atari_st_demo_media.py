import importlib.util
from pathlib import Path
import struct
import unittest

SPEC = importlib.util.spec_from_file_location("demo_media", Path(__file__).parents[1] / "scripts/atari_st_demo_media.py")
media = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(media)


class DemoMediaTests(unittest.TestCase):
    def test_uncompressed_marker_and_alternating_sides(self):
        blocks = [bytes([value]) * 512 for value in (0xE5, 2, 3, 4)]
        data = struct.pack(">5H", 0xE0F, 1, 1, 0, 1)
        data += b"".join(struct.pack(">H", 512) + block for block in blocks)
        raw, geometry = media.decode_msa(data)
        self.assertEqual(raw, b"".join(blocks))
        self.assertEqual(geometry, (2, 2, 1))

    def test_actual_demo_geometry_is_not_admitted_by_size(self):
        for tracks, sides in ((80, 1), (82, 2)):
            track = b"\xe5\xaa" + struct.pack(">H", 5120)
            data = struct.pack(">5H", 0xE0F, 10, sides-1, 0, tracks-1)
            data += (struct.pack(">H", len(track)) + track) * tracks * sides
            report = media.inspect(data)
            self.assertFalse(report["current_fes_geometry_supported"])
            self.assertEqual(report["raw_bytes"], tracks * sides * 5120)

    def test_invalid_external_data_is_rejected(self):
        header = struct.pack(">5H", 0xE0F, 1, 0, 0, 0)
        valid = header + b"\x00\x04\xe5\xaa\x02\x00"
        for invalid in (valid[:-1], valid + b"x", header + b"\x00\x04\xe5\xaa\x00\x00",
                        header + b"\x00\x04\xe5\xaa\x02\x01", header + b"\x00\x01a",
                        header + b"\x00\x01\xe5", b"x" * 10,
                        struct.pack(">5H", 0xE0F, 1, 0, 1, 1) + valid[10:]):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                media.decode_msa(invalid)
        raw, geometry = media.decode_msa(valid)
        self.assertEqual(raw, b"\xaa" * 512)
        self.assertEqual(geometry, (1, 1, 1))


if __name__ == "__main__":
    unittest.main()
