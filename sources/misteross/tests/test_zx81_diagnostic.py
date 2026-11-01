"""The open ZX81 diagnostic firmware honours the addresses zx81_machine.sv patches."""
from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

GENERATOR = Path(__file__).resolve().parents[1] / 'cores/fes-zx81/diagnostic/firmware.py'
SPEC = importlib.util.spec_from_file_location('zx81_diagnostic', GENERATOR)
firmware = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(firmware)
IMAGE = firmware.IMAGE


class ZX81DiagnosticTests(unittest.TestCase):
    def test_writes_hex_and_rom(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            subprocess.run([sys.executable, str(GENERATOR), '--output-dir', directory], check=True)
            rom = (Path(directory) / 'firmware.rom').read_bytes()
            lines = (Path(directory) / 'firmware.hex').read_text().split()
        self.assertEqual(rom, IMAGE)
        self.assertEqual(bytes(int(line, 16) for line in lines), IMAGE)

    def test_reset_and_line_interrupt(self) -> None:
        self.assertEqual(len(IMAGE), 8192)
        self.assertEqual(IMAGE[0:2], b'\xd3\xfd')          # out ($FD),a: NMI off
        handler = IMAGE[0x0038:0x004C]
        self.assertTrue(handler.endswith(b'\xed\x4f\xfb\xe9'))  # ld r,a / ei / jp (hl)

    def test_hook_addresses(self) -> None:
        self.assertEqual(IMAGE[0x0207], 0xC3)               # LOAD and SAVE exit
        self.assertEqual(IMAGE[0x02FB], 0x00)               # M1 arms SAVE
        self.assertEqual(IMAGE[0x0313:0x0316], b'\x21\x09\x40')  # ld hl,$4009
        self.assertEqual(IMAGE[0x031E], 0x5E)               # ld e,(hl)
        self.assertEqual(IMAGE[0x0347], 0xC3)               # replaced by the loader
        # Nothing else may run where the RTL watches M1 fetches.
        for start, end in ((0x020A, 0x02FB), (0x02FF, 0x0313), (0x0319, 0x031E),
                           (0x0322, 0x0347), (0x034A, 0x03C3)):
            self.assertEqual(IMAGE[start:end], bytes(end - start), hex(start))

    def test_display_file_executes_as_nops(self) -> None:
        dfile = firmware.display_file()
        self.assertEqual(len(dfile), 1 + firmware.ROWS * (firmware.COLUMNS + 1))
        self.assertEqual(dfile.count(0x76), firmware.ROWS + 1)
        self.assertEqual(dfile[0], 0x76)
        self.assertTrue(all(code == 0x76 or not code & 0x40 for code in dfile))
        self.assertIn(dfile, IMAGE)

    def test_glyphs_are_distinct(self) -> None:
        font = IMAGE[firmware.FONT:firmware.FONT + 512]
        glyphs = {font[code * 8:code * 8 + 8] for code in range(64)}
        self.assertEqual(len(glyphs), 64)
        self.assertEqual(font[0:8], bytes(8))               # space


if __name__ == '__main__':
    unittest.main()
