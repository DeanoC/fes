#!/usr/bin/env python3
"""Read the fes.ramtest status screen from an HDMI frame.

The core draws 40x30 characters of its 8x8 font (rtl/ram_font.v) at 3x into a
960x720 playfield centred in 1280x720. A capture at another size is the same
picture scaled, so each glyph pixel is sampled at its centre and the 8x8 bit
pattern is matched to the font. Prints the 30 text rows; an unknown glyph is
'?' and its nearest match is used when within two bits.
"""
import re
import subprocess
import sys
from pathlib import Path

FONT = Path(sys.argv[2]) if len(sys.argv) > 2 else None


def load_font(path: Path) -> dict[int, str]:
    table = {0: " "}
    for char, bits in re.findall(r'"(.)": bitmap = 64\'h([0-9A-F]{16});', path.read_text()):
        table[int(bits, 16)] = char
    return table


def frame(path: str) -> tuple[int, int, bytes]:
    probe = subprocess.run(["ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries",
                            "stream=width,height", "-of", "csv=p=0", path],
                           check=True, capture_output=True, text=True).stdout.strip()
    width, height = (int(v) for v in probe.split(",")[:2])
    raw = subprocess.run(["ffmpeg", "-loglevel", "error", "-i", path, "-f", "rawvideo",
                          "-pix_fmt", "rgb24", "-"], check=True, capture_output=True).stdout
    return width, height, raw


def read_screen(path: str, font: dict[int, str]) -> list[str]:
    width, height, raw = frame(path)
    scale = width / 1280.0
    unit = 3 * scale

    def ink(x: float, y: float) -> bool:
        px, py = min(int(x), width - 1), min(int(y), height - 1)
        r, g, b = raw[(py * width + px) * 3:(py * width + px) * 3 + 3]
        return max(r, g, b) > 0x60

    def glyph(left: float, top: float, row: int, column: int) -> int:
        bits = 0
        for gy in range(8):
            for gx in range(8):
                x = left + (column * 8 + gx + 0.5) * unit
                y = top + (row * 8 + gy + 0.5) * unit
                bits = (bits << 1) | ink(x, y)
        return bits

    # The display pipeline delays the text by a few pixels: find the shift
    # (in 720p pixels) at which the most cells are exact font glyphs.
    best_shift, best_score = (0, 0), -1
    for dx in range(0, 16):
        for dy in range(0, 3):
            left, top = (160 + dx) * scale, dy * scale
            score = sum(glyph(left, top, row, column) in font
                        for row in range(0, 30, 3) for column in range(0, 40, 3))
            if score > best_score:
                best_shift, best_score = (dx, dy), score
    left, top = (160 + best_shift[0]) * scale, best_shift[1] * scale

    rows = []
    for row in range(30):
        text = []
        for column in range(40):
            bits = glyph(left, top, row, column)
            char = font.get(bits)
            if char is None:
                best = min(font, key=lambda k: bin(k ^ bits).count("1"))
                char = font[best] if bin(best ^ bits).count("1") <= 2 else "?"
            text.append(char)
        rows.append("".join(text).rstrip())
    return rows


def main() -> int:
    font_path = FONT or Path(__file__).resolve().parent / "ram_font.v"
    font = load_font(font_path)
    for line in read_screen(sys.argv[1], font):
        print(line)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
