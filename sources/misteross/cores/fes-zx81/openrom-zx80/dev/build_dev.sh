#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-or-later
# Development build of the open ZX80 ROM: an 8 KiB image whose code may
# spill past the character set (it does not fit 4 KiB yet), for the fast
# harness only. Writes build/zx80-openrom-dev/dev.{bin,sym}.
set -e
cd "$(dirname "$0")/../../../.."
OUT=build/zx80-openrom-dev
mkdir -p "$OUT"
python3 -c "
import sys; sys.path.insert(0, 'scripts')
import build_zx81_openrom as b
open('$OUT/font.inc', 'w').write(b.font_include(b.FONT, 'zx80'))"
python3 scripts/z80asm.py -I "$OUT" --size 0x2000 --float-limit 0x2000 \
    cores/fes-zx81/openrom-zx80/openrom.asm "$OUT/dev.bin" --symbols "$OUT/dev.sym"
echo "$OUT/dev.bin"
