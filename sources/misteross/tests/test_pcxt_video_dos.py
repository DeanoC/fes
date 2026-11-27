"""Unit tests for MDA/Hercules DOS binaries, frame checks, and game harness staging."""
from __future__ import annotations

from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))

import pcxt_fat12 as fat12  # noqa: E402
import pcxt_run_dos as run_dos  # noqa: E402
import pcxt_video_tests_asm as vasm  # noqa: E402
import sim_fes_pcxt as sim  # noqa: E402
import sim_fes_pcxt_video_dos as video_dos  # noqa: E402


def _paint(buf: bytearray, x: int, y: int, rgb: bytes) -> None:
    if 0 <= x < 1280 and 0 <= y < 720:
        off = (y * 1280 + x) * 3
        buf[off:off + 3] = rgb


def _synth_mda_frame(mda_font: bytes, tint: int = 0) -> tuple[bytes, bytes]:
    off_col, norm_col, bright_col = video_dos.TINT_PALETTE[tint]
    rows: dict[int, tuple[bytes, int]] = {
        0: (b'FES MDA PASS', 0x07),
        2: (b'BRIGHT NORMAL 0F', 0x0f),
        3: (b'UNDERLINE ATTR 01', 0x01),
        4: (b'BRIGHT UNDERLINE 09', 0x09),
        5: (b'REVERSE VIDEO 70', 0x70),
        6: (b'HIDDEN', 0x00),
        7: (bytes([0xc9] + [0xcd] * 10 + [0xbb]), 0x07),
    }
    cells = bytearray(80 * 25 * 2)
    for i in range(0, len(cells), 2):
        cells[i] = 0x20
        cells[i + 1] = 0x07
    for r, (text, attr) in rows.items():
        for col, ch in enumerate(text):
            cells[2 * (r * 80 + col)] = ch
            cells[2 * (r * 80 + col) + 1] = attr

    frame = bytearray(1280 * 720 * 3)
    for py in range(720):
        sy = min(349, (py * 350) // 720)
        row_idx = sy // 14
        gy = sy % 14
        for hx in range(960):
            sx = (hx * 720) // 960
            col_idx = sx // 9
            gx = sx % 9
            ch = cells[2 * (row_idx * 80 + col_idx)]
            attr = cells[2 * (row_idx * 80 + col_idx) + 1]
            pat = mda_font[ch * 16 + gy]
            ink = bool(pat & (0x80 >> gx)) if gx < 8 else ((0xc0 <= ch <= 0xdf) and bool(pat & 0x01))
            if (attr & 0x77) == 0x01 and gy == 13:
                ink = True
            if (attr & 0x77) == 0x70:
                col = off_col if ink else norm_col
            elif (attr & 0x77) == 0x00:
                col = off_col
            else:
                col = (bright_col if (attr & 0x08) else norm_col) if ink else off_col
            _paint(frame, 160 + hx, py, col)
    return bytes(frame), bytes(cells)


def _synth_hgc_frame(page: int, tint: int = 0) -> bytes:
    off_col, norm_col, _ = video_dos.TINT_PALETTE[tint]
    frame = bytearray(1280 * 720 * 3)
    for py in range(720):
        sy = min(347, (py * 348) // 720)
        for hx in range(960):
            sx = (hx * 720) // 960
            lit = False
            if sy in (0, 347):
                lit = True
            elif page == 0:
                if sy == 1:
                    lit = bool(0xaa & (0x80 >> (sx & 7)))
                elif sy == 2:
                    lit = bool(0xcc & (0x80 >> (sx & 7)))
                elif sy == 3:
                    lit = bool(0xf0 & (0x80 >> (sx & 7)))
                elif 40 <= sy <= 139 and (sx == 0 or sx == 719 or 64 <= sx <= 191):
                    lit = True
            else:
                if 200 <= sy <= 299 and (sx == 0 or sx == 719 or 448 <= sx <= 575):
                    lit = True
            _paint(frame, 160 + hx, py, norm_col if lit else off_col)
    return bytes(frame)


class VideoDosTests(unittest.TestCase):
    def test_com_binaries_are_valid(self) -> None:
        mda = vasm.build_mdatest_com()
        hgc = vasm.build_hgctest_com()
        self.assertGreater(len(mda), 64)
        self.assertGreater(len(hgc), 128)
        self.assertLess(len(mda), 65000)
        self.assertLess(len(hgc), 65000)
        # MDATEST writes attribute+char words cleanly into B000; check its sequence & PASS/FAIL strings.
        for ch in b'FES MDA PASS':
            self.assertIn(bytes([0xb8, ch, 0x07, 0xab]), mda)
        self.assertIn(b'FES MDA FAIL', mda)
        self.assertIn(b'FES HGC PASS', hgc)

    def test_verify_mda_synthetic_and_negative(self) -> None:
        mda_font = sim.mda_font()
        for tint in (0, 1, 2):
            frame, cells = _synth_mda_frame(mda_font, tint=tint)
            text_proof = video_dos.verify_mda_text_frame(frame, cells, mda_font, b'FES MDA PASS', tint=tint)
            self.assertGreaterEqual(float(text_proof['score']), 0.99)
            attr_proof = video_dos.verify_mda_attributes(frame, mda_font, tint=tint)
            self.assertEqual(attr_proof['hidden_lit'], 0)
            self.assertGreater(attr_proof['ninth_dot_hits'], 0)

        bad = bytearray(_synth_mda_frame(mda_font, tint=0)[0])
        _, cells = _synth_mda_frame(mda_font, tint=0)
        # Wipe top-right margin to break pillarbox check or blank out row 0 to fail glyph check
        for y in range(28):
            for x in range(160, 400):
                _paint(bad, x, y, b'\x00\x00\x00')
        with self.assertRaises(ValueError):
            video_dos.verify_mda_text_frame(bytes(bad), cells, mda_font, b'FES MDA PASS', tint=0)

    def test_verify_hgc_synthetic_and_negative(self) -> None:
        p0 = _synth_hgc_frame(0, tint=1)
        p1 = _synth_hgc_frame(1, tint=1)
        proof0 = video_dos.verify_hgc_page_frame(p0, page=0, tint=1)
        proof1 = video_dos.verify_hgc_page_frame(p1, page=1, tint=1)
        self.assertGreater(proof0['page0_block'], 0.99)
        self.assertLess(proof0['page1_block'], 0.01)
        self.assertGreater(proof1['page1_block'], 0.99)
        self.assertLess(proof1['page0_block'], 0.01)
        with self.assertRaises(ValueError):
            video_dos.verify_hgc_page_frame(p0, page=1, tint=1)

    def test_run_dos_staging_and_args(self) -> None:
        args = run_dos.parse_args(['--video', 'hercules', '--tint', 'amber', '--out', '/tmp/out', '--command', 'TEST.COM'])
        self.assertEqual(args.video, 'hercules')
        self.assertEqual(args.tint, 'amber')
        with tempfile.TemporaryDirectory() as td:
            td_path = Path(td)
            game_dir = td_path / 'game'
            (game_dir / 'DATA').mkdir(parents=True)
            (game_dir / 'GAME.EXE').write_bytes(b'MZ' + b'\x00' * 100)
            (game_dir / 'DATA/LVL1.DAT').write_bytes(b'LEVEL1')
            extra = td_path / 'README.TXT'
            extra.write_bytes(b'hello')
            img = run_dos.stage_floppy_image(
                add_files=[extra],
                add_dir=game_dir,
                command='GAME.EXE HERC',
            )
            self.assertEqual(fat12.read_file(img, 'GAME.EXE'), b'MZ' + b'\x00' * 100)
            self.assertEqual(fat12.read_file(img, 'DATA/LVL1.DAT'), b'LEVEL1')
            self.assertEqual(fat12.read_file(img, 'README.TXT'), b'hello')
            self.assertIn(b'GAME.EXE HERC', fat12.read_file(img, 'AUTOEXEC.BAT'))


if __name__ == '__main__':
    unittest.main()
