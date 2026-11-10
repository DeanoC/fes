#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Build the open ZX81 and ZX80 ROMs (cores/fes-zx81/openrom, openrom-zx80).

Writes <model>-openrom.bin (the machine-rom firmware: 8192 bytes for the
ZX81, 4096 for the ZX80), .hex (one byte per line, for FIRMWARE_INIT) and
.sym. Both character sets are generated from openrom/font.txt; the ZX80 set
reorders its glyphs into ZX80 codes.
"""
from __future__ import annotations

import argparse
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-zx81"
SOURCE = CORE / "openrom"
FONT = SOURCE / "font.txt"
sys.path.insert(0, str(ROOT / "scripts"))
import z80asm  # noqa: E402

ROM_SIZE = 8192
MODELS = {"zx81": (SOURCE, ROM_SIZE), "zx80": (CORE / "openrom-zx80", 4096)}

# ZX80 code -> (ZX81 glyph, inverted). The ZX81 set has no glyph with the
# lower-right quadrant; those ZX80 graphics are inverses of ZX81 ones.
ZX80_GLYPHS = [(0, 0), (11, 0), (5, 0), (3, 1), (1, 0), (2, 0), (4, 0), (7, 1),
               (6, 0), (8, 0), (9, 0), (10, 0), (12, 0), (13, 0), (14, 0), (15, 0),
               (16, 0), (17, 0), (22, 0), (21, 0), (23, 0), (24, 0), (20, 0), (18, 0),
               (19, 0), (25, 0), (26, 0), (27, 0)] + [(c, 0) for c in range(28, 64)]


def font_include(font: Path, model: str = "zx81") -> str:
    """font.txt (64 glyphs of eight '#'/'.' rows, in ZX81 code order) -> db
    lines in the model's code order."""
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
    order = ZX80_GLYPHS if model == "zx80" else [(c, 0) for c in range(64)]
    out = ["; Generated from font.txt"]
    for source, invert in order:
        values = ",".join(f"0x{int(r.replace('#', '1').replace('.', '0'), 2) ^ (0xff if invert else 0):02x}"
                          for r in glyphs[source])
        out.append(f"        db {values}")
    return "\n".join(out) + "\n"


def build(out_dir: Path, model: str = "zx81") -> bytes:
    source, size = MODELS[model]
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "font.inc").write_text(font_include(FONT, model))
    asm = z80asm.Assembler([out_dir])
    image = asm.assemble(source / "openrom.asm", size)
    name = f"{model}-openrom"
    (out_dir / f"{name}.bin").write_bytes(image)
    (out_dir / f"{name}.hex").write_text("".join(f"{b:02x}\n" for b in image))
    (out_dir / f"{name}.sym").write_text(
        "".join(f"{v:04x} {k}\n" for k, v in sorted(asm.symbols.items(), key=lambda kv: kv[1])))
    limit = asm.float_limit
    fixed = sum(1 for a in range(limit) if asm.fixed_owner[a] is not None)
    free = limit - fixed - sum(asm.float_size.values())
    print(f"{name}: {size} bytes, {free} bytes free below the character set")
    return image


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", choices=sorted(MODELS), default="zx81")
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    try:
        build(args.out or ROOT / "build" / f"{args.model}-openrom", args.model)
    except (z80asm.AsmError, ValueError) as exc:
        print(exc, file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
