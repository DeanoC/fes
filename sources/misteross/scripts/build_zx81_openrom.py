#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Build the open ZX81 ROM (cores/fes-zx81/openrom) as an 8192-byte image.

Writes zx81-openrom.bin (the machine-rom firmware), .hex (one byte per line,
for FIRMWARE_INIT) and .sym. The character set is generated from font.txt.
"""
from __future__ import annotations

import argparse
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "cores" / "fes-zx81" / "openrom"
sys.path.insert(0, str(ROOT / "scripts"))
import z80asm  # noqa: E402

ROM_SIZE = 8192


def font_include(font: Path) -> str:
    """font.txt (64 glyphs of eight '#'/'.' rows, in code order) -> db lines."""
    # Comments start "# "; glyph rows are only "#" and ".".
    lines = [line for line in font.read_text().splitlines() if not line.startswith("# ")]
    glyphs: dict[int, list[str]] = {}
    i = 0
    while i < len(lines):
        if not lines[i].strip():
            i += 1
            continue
        code = int(lines[i].split()[0])
        rows = lines[i + 1:i + 9]
        if len(rows) != 8 or any(len(r) != 8 or set(r) - {"#", "."} for r in rows):
            raise ValueError(f"glyph {code}: expected eight rows of eight '#' or '.'")
        glyphs[code] = rows
        i += 9
    if sorted(glyphs) != list(range(64)):
        raise ValueError("font.txt must define glyphs 0 to 63")
    out = ["; Generated from font.txt"]
    for code in range(64):
        values = ",".join(f"0x{int(r.replace('#', '1').replace('.', '0'), 2):02x}" for r in glyphs[code])
        out.append(f"        db {values}")
    return "\n".join(out) + "\n"


def build(out_dir: Path) -> bytes:
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "font.inc").write_text(font_include(SOURCE / "font.txt"))
    asm = z80asm.Assembler([out_dir])
    image = asm.assemble(SOURCE / "openrom.asm", ROM_SIZE)
    (out_dir / "zx81-openrom.bin").write_bytes(image)
    (out_dir / "zx81-openrom.hex").write_text("".join(f"{b:02x}\n" for b in image))
    (out_dir / "zx81-openrom.sym").write_text(
        "".join(f"{v:04x} {k}\n" for k, v in sorted(asm.symbols.items(), key=lambda kv: kv[1])))
    limit = asm.float_limit
    fixed = sum(1 for a in range(limit) if asm.fixed_owner[a] is not None)
    free = limit - fixed - sum(asm.float_size.values())
    print(f"zx81-openrom: {ROM_SIZE} bytes, {free} bytes free below the character set")
    return image


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, default=ROOT / "build" / "zx81-openrom")
    args = parser.parse_args()
    try:
        build(args.out)
    except (z80asm.AsmError, ValueError) as exc:
        print(exc, file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
