import importlib.util
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("demo_media", Path(__file__).parents[1] / "scripts/atari_st_demo_media.py")
media = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(media)


class DemoMediaTests(unittest.TestCase):
    def sample(self, tracks=2, sides=2, sectors=1):
        blocks = [bytes([index % 256]) * (sectors * 512)
                  for index in range(tracks * sides)]
        data = struct.pack(">5H", 0xE0F, sectors, sides - 1, 0, tracks - 1)
        data += b"".join(struct.pack(">H", len(block)) + block for block in blocks)
        return data, b"".join(blocks)

    def test_conversion_preserves_exact_geometry_and_track_order(self):
        with tempfile.TemporaryDirectory() as directory:
            source, output = Path(directory) / "demo.msa", Path(directory) / "demo.st"
            for geometry in ((80, 1, 10), (82, 2, 10), (2, 2, 1)):
                with self.subTest(geometry=geometry):
                    data, raw = self.sample(*geometry)
                    source.write_bytes(data)
                    output.write_bytes(b"old output")
                    report = media.convert_msa(source, output)
                    self.assertEqual(output.read_bytes(), raw)
                    self.assertEqual(source.read_bytes(), data)
                    self.assertEqual(report, media.inspect(data))
                    self.assertEqual(report["raw_bytes"], len(raw))
                    self.assertEqual(sorted(path.name for path in Path(directory).iterdir()),
                                     ["demo.msa", "demo.st"])

    def test_failed_validation_never_creates_or_replaces_output(self):
        data, _ = self.sample()
        header = struct.pack(">5H", 0xE0F, 1, 0, 0, 0)
        invalids = (data[:-1], data + b"trailing", header + b"\x00\x04\xe5\xaa\x00\x00",
                    header + b"\x00\x04\xe5\xaa\x02\x01", b"x" * 10,
                    b"x" * (media.MAX_BYTES + 1))
        with tempfile.TemporaryDirectory() as directory:
            source, output = Path(directory) / "demo.msa", Path(directory) / "demo.st"
            for invalid in invalids:
                for exists in (False, True):
                    with self.subTest(length=len(invalid), existing_output=exists):
                        source.write_bytes(invalid)
                        if exists:
                            output.write_bytes(b"keep existing")
                        else:
                            output.unlink(missing_ok=True)
                        with self.assertRaises(ValueError):
                            media.convert_msa(source, output)
                        if exists:
                            self.assertEqual(output.read_bytes(), b"keep existing")
                        else:
                            self.assertFalse(output.exists())
                        self.assertFalse(list(Path(directory).glob(".demo.st.*")))

    def test_input_cannot_be_overwritten_through_path_or_inode_alias(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "demo.msa"
            data, _ = self.sample()
            source.write_bytes(data)
            symlink, hardlink = Path(directory) / "link.st", Path(directory) / "hard.st"
            symlink.symlink_to(source)
            os.link(source, hardlink)
            for output in (source, source.parent / "." / source.name, symlink, hardlink):
                with self.subTest(output=output), self.assertRaisesRegex(ValueError, "overwrite"):
                    media.convert_msa(source, output)
                self.assertEqual(source.read_bytes(), data)

    def test_publication_failure_removes_temporary_and_preserves_previous_output(self):
        with tempfile.TemporaryDirectory() as directory:
            source, output = Path(directory) / "demo.msa", Path(directory) / "demo.st"
            source.write_bytes(self.sample()[0])
            output.write_bytes(b"keep existing")
            with mock.patch.object(media.os, "replace", side_effect=OSError("publication failed")):
                with self.assertRaisesRegex(OSError, "publication failed"):
                    media.convert_msa(source, output)
            self.assertEqual(output.read_bytes(), b"keep existing")
            self.assertFalse(list(Path(directory).glob(".demo.st.*")))

    def test_cli_converts_and_reports_validation_errors(self):
        with tempfile.TemporaryDirectory() as directory:
            source, output = Path(directory) / "demo.msa", Path(directory) / "demo.st"
            data, raw = self.sample()
            source.write_bytes(data)
            command = [sys.executable, str(SPEC.origin), str(source), "--raw-output", str(output)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('"raw_bytes": 2048', result.stdout)
            self.assertEqual(output.read_bytes(), raw)
            source.write_bytes(data[:-1])
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn("truncated MSA track", result.stderr)
            self.assertNotIn("Traceback", result.stderr)
            self.assertEqual(output.read_bytes(), raw)

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
            self.assertTrue(report["geometry_extension_supported"])
            self.assertEqual(report["raw_bytes"], tracks * sides * 5120)

    def test_report_distinguishes_legacy_extension_and_offline_geometry(self):
        for geometry in ((80, 2, 9), (80, 1, 9), (81, 2, 10), (82, 1, 10),
                         (79, 2, 10), (83, 2, 9), (80, 2, 11), (2, 2, 1)):
            with self.subTest(geometry=geometry):
                report = media.inspect(self.sample(*geometry)[0])
                self.assertEqual(report["current_fes_geometry_supported"], geometry == (80, 2, 9))
                tracks, sides, sectors = geometry
                self.assertEqual(report["geometry_extension_supported"],
                                 80 <= tracks <= 82 and 1 <= sides <= 2 and 9 <= sectors <= 10)

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
