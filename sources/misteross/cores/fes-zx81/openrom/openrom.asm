; SPDX-License-Identifier: GPL-2.0-or-later
; Open ZX81 ROM: an original 8 KiB firmware for the ZX81 family. It keeps
; the hardware interface and published entry points ZX81 software uses and
; implements BASIC from scratch.

        include "sysvars.inc"
        include "hw.asm"

; Everything else is placed by the assembler below the character set.
        floatlimit 0x1e00
        autofloat
        include "core.asm"
        include "print.asm"
        include "fp.asm"
        include "expr.asm"
        include "stmt.asm"
        include "edit.asm"
        include "tables.asm"
        endautofloat

        org 0x1e00
        include "font.inc"           ; generated from font.txt by build_zx81_openrom.py
