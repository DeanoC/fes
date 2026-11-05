"""Exercise the real assembler and closed DE25 firmware capture."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25_firmware_test", HERE / "firmware.py")
F = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(F)


class FirmwareTests(unittest.TestCase):
    def test_captured_assembly_initializes_the_exact_reachable_window(self):
        sources = {name: (F.ROOT / name).read_bytes() for name in F.INPUTS}
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "firmware"
            files, record = F.generate(sources, output)
            rom = (output / "assembled/zx81-openrom.bin").read_bytes()
            words = bytes(int(word, 16) for word in files["zx8x.hex"].split())
            self.assertEqual(words, rom + b"\xff" * 8192)
            self.assertEqual(len(rom), 8192)
            self.assertEqual(record["rom_sha256"], F.hashlib.sha256(rom).hexdigest())
            # Parse every explicit MIF row independently of the HEX serializer.
            rows = files["zx8x.mif"].decode().partition("CONTENT BEGIN\n")[2].splitlines()[:-1]
            self.assertEqual(len(rows), 16384)
            self.assertEqual([int(row.split(':')[0], 16) for row in rows], list(range(16384)))
            self.assertEqual(bytes(int(row.split(':')[1].strip(' ;'), 16) for row in rows), words)
            self.assertEqual(set(record["source_sha256"]), set(sources))
            self.assertIn(b"Zx81Newline", F.simulation_header(record))
            # Editing the captured font changes the actual reachable ROM,
            # without consulting or modifying the checkout's font.
            font = F.PREFIX + "cores/fes-zx81/openrom/font.txt"
            changed = dict(sources)
            changed[font] = changed[font].replace(b"........", b"#.......", 1)
            self.assertNotEqual(changed[font], sources[font])
            other, other_record = F.generate(changed, Path(tmp) / "changed")
            self.assertNotEqual(other["zx8x.hex"], files["zx8x.hex"])
            self.assertNotEqual(other_record["rom_sha256"], record["rom_sha256"])
        self.assertEqual(sources[font], (F.ROOT / font).read_bytes())

    def test_current_capture_excludes_removed_rom_and_historical_capture_keeps_it(self):
        current = {path.relative_to(F.ROOT).as_posix() for path in F.source_paths()}
        self.assertEqual(current, set(F.INPUTS))
        original = "786e6383b32617af8baeb32e9de6cf99231a19f9"
        historical = {path.relative_to(F.ROOT).as_posix() for path in F.source_paths(original)}
        self.assertEqual(historical, set(F.LEGACY))


if __name__ == "__main__":
    unittest.main()
