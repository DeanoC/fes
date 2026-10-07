# SPDX-License-Identifier: MIT
"""Firmware-envelope, address coverage and provenance tests.

Actual CPU instructions, SDRAM commands and HDMI pixels are validated separately
with the existing FX68K/physical-memory boot harness, not a Python CPU stand-in.
"""
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import struct
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("memory_diagnostic", ROOT / "scripts/atari_st_memory_diagnostic.py")
diag = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(diag)


class MemoryDiagnosticTests(unittest.TestCase):
    def test_exact_rom_reset_vectors_and_deterministic_visible_stages(self):
        rom, labels = diag.firmware()
        self.assertEqual(len(rom), 196608)
        self.assertEqual(struct.unpack_from(">II", rom), (0x07FFF0, 0xFC0100))
        self.assertEqual(diag.firmware(), (rom, labels))
        self.assertLess(labels["failed_5"] + 4, diag.ROM_BASE + diag.ROM_SIZE)
        self.assertEqual(rom[-4:], b"\xff" * 4)
        self.assertEqual(set(diag.STAGES), {1, 2, 3, 4, 5})
        self.assertEqual(len({diag._glyph_rows(d) for d in range(6)}), 6)
        for digit in range(16):
            self.assertEqual(len(diag._glyph_rows(digit)), 28)
            self.assertTrue(all(0 <= row <= 0xffff for row in diag._glyph_rows(digit)))

    def test_addresses_cover_each_bit_banks_neighbors_and_avoid_display(self):
        addresses = set(diag.ADDRESSES)
        for bit in range(1, 19):
            self.assertTrue(any(address ^ (1 << bit) in addresses for address in addresses), f"untested RAM address bit {bit}")
        self.assertTrue(set(range(0x2000, 0x2020, 2)).issubset(addresses))
        self.assertEqual({(address >> 3) & 3 for address in addresses}, {0, 1, 2, 3})
        for address in (*addresses, *(neighbor for address in diag.PAIRS for neighbor in (address, address + 2))):
            self.assertEqual(address & 1, 0)
            self.assertTrue(8 <= address <= 0x7fffe)
            self.assertFalse(diag.SCREEN <= address < diag.SCREEN + 32000)
        for index in range(len(addresses)):
            value = 0x1234 ^ (index * 0x101)
            self.assertNotEqual(value >> 8, value & 255, "byte-read test cannot distinguish address bit0")
            upper_baseline = 0x5AA5 ^ (index * 0x101)
            new_upper = (0xC3 ^ (index * 7)) & 255
            self.assertNotEqual(new_upper, upper_baseline >> 8, "upper write must change data")
            lower_baseline = 0xA55A ^ (index * 0x101)
            new_lower = (0x3C ^ (index * 11)) & 255
            self.assertNotEqual(new_lower, lower_baseline & 255, "lower write must change data")
        self.assertEqual({(address >> 3) & 3 for index, address in enumerate(diag.ADDRESSES)
                          if ((0xC3 ^ (index * 7)) & 255) != ((0x5AA5 ^ (index * 0x101)) & 255)},
                         {0, 1, 2, 3}, "upper writes must expose both-lane corruption in every bank")
        self.assertEqual({(address >> 3) & 3 for index, address in enumerate(diag.ADDRESSES)
                          if ((0x3C ^ (index * 11)) & 255) != ((0xA55A ^ (index * 0x101)) >> 8)},
                         {0, 1, 2, 3}, "lower writes must expose both-lane corruption in every bank")

    def test_cli_writes_exact_image_and_hashes_current_source_bytes(self):
        with tempfile.TemporaryDirectory() as temp:
            rom, evidence = Path(temp) / "memory.rom", Path(temp) / "evidence.json"
            with contextlib.redirect_stdout(io.StringIO()) as stdout:
                self.assertEqual(diag.main(["--output", str(rom), "--record", str(evidence)]), 0)
            record = json.loads(evidence.read_text())
            self.assertEqual(record, json.loads(stdout.getvalue()))
            self.assertEqual(rom.read_bytes(), diag.firmware()[0])
            self.assertEqual(record["rom_sha256"], hashlib.sha256(rom.read_bytes()).hexdigest())
            self.assertFalse(record["hardware_acceptance"])
            self.assertEqual(record["pass_display"], "green 00")
            self.assertEqual(record["failure_display"], "red 01..05")
            for source in record["source_files"]:
                self.assertEqual(source["sha256"], hashlib.sha256((ROOT / source["path"]).read_bytes()).hexdigest())


if __name__ == "__main__":
    unittest.main()
