#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""Open FES Apple II diagnostic firmware and synthetic test disk.

Nothing here is derived from Apple's ROMs. The 16 KiB firmware image covers
the CPU's $C000-$FFFF window exactly as the shell's linked ROM does:

* $C600 (offset $0600) holds an open Disk II boot page for the built-in slot 6
  controller. It carries the documented Disk II slot signature bytes so slot
  scanners recognise it, then jumps into the diagnostic's own boot code.
* $D000-$FFFF holds a small test monitor: self tests of RAM and the language
  card, a character-set/text screen, keyboard echo, lo-res, hi-res and mixed
  test screens, and a clean-room Disk II seek/read-sector implementation
  (6-and-2 decode) used to boot the synthetic disk.

Commands (Apple II keyboard codes with bit 7 set): T text, L lo-res, H hi-res,
M mixed, B boot slot 6, S scan the slots for FES probe cards and call each one
(its result byte goes to $0380 + slot), R prepare a language-card warm-reset
test (press Ctrl-Reset, then use host Hold to return). Other keys are echoed. After each command the monitor
writes the command code to STAGE ($03F1); RESULT ($03F0) holds $11 after the
self tests, $A5 after a verified disk boot, $51/$52 before/after the R reset
test, or an $E0-$EF failure code.

The synthetic disk is a 143,360-byte DOS 3.3 order image. Logical sector 0
of track 0 (physical 0) is the stage-1 boot sector; physical sector 1 of
track 0 is stage 2. Every other file sector f contains an arithmetic
progression starting at (37*f + 5) mod 256 with step 11, so each sector holds
all 256 byte values and a wrong interleave, track or decode is detected.
"""

from __future__ import annotations

import argparse
import hashlib
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from asm6502 import assemble  # noqa: E402
from font import screen_code  # noqa: E402

FIRMWARE_BASE = 0xC000
FIRMWARE_SIZE = 0x4000
DISK_SIZE = 143360

TRANSLATE62 = bytes.fromhex(
    "96979a9b9d9e9fa6a7abacadaeafb2b3b4b5b6b7b9babbbcbdbebfcbcdcecfd3"
    "d6d7d9dadbdcdddedfe5e6e7e9eaebecedeeeff2f3f4f5f6f7f9fafbfcfdfeff"
)
DOS_SKEW = (0, 7, 14, 6, 13, 5, 12, 4, 11, 3, 10, 2, 9, 1, 8, 15)

RESULT = 0x03F0
STAGE = 0x03F1


def text_row_base(row: int) -> int:
    return 0x0400 + (row & 7) * 128 + (row >> 3) * 40


def hires_row_base(line: int) -> int:
    return 0x2000 + (line & 7) * 1024 + ((line >> 3) & 7) * 128 + (line >> 6) * 40


def screen_bytes(text: str, mode: str = "normal") -> str:
    return ", ".join(f"${screen_code(c, mode):02X}" for c in text)


def lores_byte(column: int, row: int) -> int:
    top = (column + row) & 15
    bottom = (column + row + 8) & 15
    return top | bottom << 4


HIRES_BANDS = ((0x00, 0x00), (0x55, 0x2A), (0x2A, 0x55), (0x7F, 0x7F),
               (0x80, 0x80), (0xD5, 0xAA), (0xAA, 0xD5), (0xFF, 0xFF))


def hires_byte(column: int) -> int:
    even, odd = HIRES_BANDS[column // 5]
    return even if column % 2 == 0 else odd


BANNER = [
    (0, 0, "FES APPLE II DIAGNOSTIC", "normal"),
    (0, 1, "INVERSE TEXT", "inverse"),
    (14, 1, "FLASHING TEXT", "flash"),
]
CHARSET_ROW = 3
MESSAGE_ROW = 20
DUMP_ROW = 12
KEYS_TEXT = "KEYS T L H M B S D R"
MIXED_TEXT = "MIXED MODE TEXT WINDOW"
DISK_PASS = "DISK BOOT OK"


LC_TEST = """
        .org $1000
        LDA $C081           ; read ROM, write RAM bank 2 (two reads)
        LDA $C081
        LDA #$5A
        STA $D000
        LDA #$3C
        STA $E000
        LDA $C089           ; read ROM, write RAM bank 1
        LDA $C089
        LDA #$A5
        STA $D000
        LDA $C080           ; read RAM bank 2, write protected
        LDA $D000
        CMP #$5A
        BNE FAIL
        LDA $E000
        CMP #$3C
        BNE FAIL
        LDA $C088           ; read RAM bank 1, write protected
        LDA #$00
        STA $D000
        LDA $D000
        CMP #$A5
        BNE FAIL
        LDA $C082           ; read ROM: $D000 is DTAB[0] = 0
        LDA $D000
        BNE FAIL
        CLC
        RTS
FAIL:   LDA $C082
        SEC
        RTS
"""


# Run from main RAM while the motherboard ROM is hidden. The language-card
# reset vector returns to WARM; a hardware reset must keep bank 1 readable and
# write-protected. The stack sentinel also catches writes during CPU RESET.
WARM_RESET_TEST = """
        .org $1200
        SEI
        CLD
        LDA #$5A
        STA $02F0
        LDX #$FF
STACK:  STA $0100,X
        DEX
        BNE STACK
        STA $0100
        LDA $C089
        LDA $C089
        LDA #$A5
        STA $D000
        LDA #<WARM
        STA $FFFC
        LDA #>WARM
        STA $FFFD
        LDA $C088           ; bank 1 RAM read, writes disabled
        LDA $C050           ; graphics, mixed, page 2, hi-res
        LDA $C053
        LDA $C055
        LDA $C057
        LDA $C058           ; annunciators 0/2 off, 1/3 on
        LDA $C05B
        LDA $C05C
        LDA $C05F
        LDA #$51
        STA $03F0
WAIT:   INC $03F2
        JMP WAIT
WARM:   LDA $02F0
        CMP #$5A
        BNE FAIL
        LDX #$FF
CHECK:  LDA $0100,X
        CMP #$5A
        BNE FAIL
        DEX
        BNE CHECK
        LDA $0100
        CMP #$5A
        BNE FAIL
        LDA #0
        STA $D000           ; warm reset must keep RAM write-protected
        LDA $D000
        CMP #$A5
        BNE FAIL
        LDA #$52
        STA $03F0
HALT:   JMP HALT
FAIL:   LDA #$E2
        STA $03F0
        JMP HALT
"""


def firmware_source() -> str:
    txt_lo = ", ".join(f"${text_row_base(r) & 0xFF:02X}" for r in range(24))
    txt_hi = ", ".join(f"${text_row_base(r) >> 8:02X}" for r in range(24))
    hgr_lo = ", ".join(f"${hires_row_base(y) & 0xFF:02X}" for y in range(192))
    hgr_hi = ", ".join(f"${hires_row_base(y) >> 8:02X}" for y in range(192))
    dtab = [0] * 256
    for value, nibble in enumerate(TRANSLATE62):
        dtab[nibble] = value
    dtab_bytes = "\n".join(
        "        .byte " + ", ".join(f"${b:02X}" for b in dtab[i:i + 16]) for i in range(0, 256, 16))
    banner = "\n".join(
        f"        LDA #{col}\n        STA CURX\n        LDA #{row}\n        STA CURY\n"
        f"        LDA #<MSG{i}\n        STA SRC\n        LDA #>MSG{i}\n        STA SRC+1\n        JSR PUTS"
        for i, (col, row, _text, _mode) in enumerate(BANNER))
    banner_msgs = "\n".join(
        f"MSG{i}:  .byte {screen_bytes(text, mode)}, 0" for i, (_c, _r, text, mode) in enumerate(BANNER))
    hires_row = ", ".join(f"${hires_byte(c):02X}" for c in range(40))
    lc = assemble(LC_TEST, 0x1000, 128, fill=0)
    lc = lc[:lc.rindex(b"\x38\x60") + 2]
    lc_bytes = "\n".join(
        "        .byte " + ", ".join(f"${b:02X}" for b in lc[i:i + 16]) for i in range(0, len(lc), 16))
    warm_reset = assemble(WARM_RESET_TEST, 0x1200, 256, fill=0)
    warm_reset_bytes = "\n".join(
        "        .byte " + ", ".join(f"${b:02X}" for b in warm_reset[i:i + 16])
        for i in range(0, len(warm_reset), 16))
    return f"""
; ---------------------------------------------------------------- symbols
TXTPTR  = $06
CURX    = $08
CURY    = $09
TMP     = $0A
TMP2    = $0B
SRC     = $0C
IDX     = $0E
COL     = $0F
LINE    = $10
BUFP    = $26
DSLOT   = $2B
FVOL    = $2C
FTRK    = $2D
FSEC    = $2E
FCHK    = $2F
RETRY   = $30
HTRACK  = $31
SLOTN   = $32
PTR     = $33
HEXX    = $35
STUB    = $0340
SLOTRES = $0380
SECWANT = $3D
TRKWANT = $41
AUX     = $0300
RESULT  = ${RESULT:04X}
STAGE   = ${STAGE:04X}
KBD     = $C000
KBDSTRB = $C010
SPKR    = $C030

; ---------------------------------------------------------------- slot 6
        .org $C600
        LDX #$20            ; Disk II slot signature bytes ($Cn01=$20,
        LDY #$00            ; $Cn03=$00, $Cn05=$03, $Cn07=$3C)
        LDX #$03
        STX $3C
        LDX #$60
        JMP DBOOT

; ---------------------------------------------------------------- tables
        .org $D000
DTAB:
{dtab_bytes}
TXTLO:  .byte {txt_lo}
TXTHI:  .byte {txt_hi}
HGRLO:  .byte {hgr_lo}
HGRHI:  .byte {hgr_hi}
HROW:   .byte {hires_row}
{banner_msgs}
MSGRAM: .byte {screen_bytes("RAM OK")}, 0
MSGLC:  .byte {screen_bytes("LANGUAGE CARD OK")}, 0
MSGBAD: .byte {screen_bytes("FAILED")}, 0
MSGKEY: .byte {screen_bytes(KEYS_TEXT)}, 0
MSGMIX: .byte {screen_bytes(MIXED_TEXT)}, 0
MSGDSK: .byte {screen_bytes(DISK_PASS)}, 0
MSGDER: .byte {screen_bytes("DISK ERROR")}, 0

; Language card test, copied to $1000 and run from main RAM.
LCSRC:
{lc_bytes}
LCEND:
RESETSRC:
{warm_reset_bytes}

; ---------------------------------------------------------------- entry
        .org $E000
RESET:  CLD
        SEI
        LDX #$FF
        TXS
        LDA $C051
        LDA $C052
        LDA $C054
        LDA $C056
        LDA #$00
        STA RESULT
        STA STAGE
        JSR TEXTSCR
        JSR TESTRAM
        BCS SELFBAD
        LDA #0
        STA CURX
        LDA #{CHARSET_ROW + 5}
        STA CURY
        LDA #<MSGRAM
        STA SRC
        LDA #>MSGRAM
        STA SRC+1
        JSR PUTS
        JSR TESTLC
        BCS SELFBAD
        LDA #0
        STA CURX
        LDA #{CHARSET_ROW + 6}
        STA CURY
        LDA #<MSGLC
        STA SRC
        LDA #>MSGLC
        STA SRC+1
        JSR PUTS
        LDA #0
        STA CURX
        LDA #{CHARSET_ROW + 8}
        STA CURY
        LDA #<MSGKEY
        STA SRC
        LDA #>MSGKEY
        STA SRC+1
        JSR PUTS
        LDA #$11
        STA RESULT
        JMP MAIN
SELFBAD:
        LDA #<MSGBAD
        STA SRC
        LDA #>MSGBAD
        STA SRC+1
        JSR PUTS
        LDA #$E1
        STA RESULT
HANG:   JMP HANG

; Command loop -------------------------------------------------------------
MAIN:   LDA #0
        STA CURX
        LDA #{MESSAGE_ROW}
        STA CURY
MLOOP:  LDA KBD
        BPL MLOOP
        STA KBDSTRB
        STA TMP2
        CMP #$D4            ; T
        BNE M1
        JSR TEXTSCR
        JMP MDONE
M1:     CMP #$CC            ; L
        BNE M2
        JSR LORES
        JMP MDONE
M2:     CMP #$C8            ; H
        BNE M3
        JSR HIRES
        JMP MDONE
M3:     CMP #$CD            ; M
        BNE M4
        JSR MIXED
        JMP MDONE
M4:     CMP #$C2            ; B
        BNE M5
        LDA $C600           ; touch the slot ROM page, then boot it
        JMP $C600
M5:     CMP #$D3            ; S
        BNE M6
        JSR SLOTSCAN
        JMP MDONE
M6:     CMP #$C4            ; D
        BNE M7
        JSR DUMP
        JMP MDONE
M7:     CMP #$D2            ; R: main-RAM warm-reset test
        BNE M8
        LDX #0
RCOPY:  LDA RESETSRC,X
        STA $1200,X
        INX
        BNE RCOPY
        JMP $1200
M8:     JSR PUTC            ; echo any other key
        LDA SPKR            ; and click the speaker twice
        LDA SPKR
MDONE:  LDA TMP2
        STA STAGE
        JMP MLOOP

; Slot scan: call every card whose page ends in the probe signature ---------
; X = slot * 16 and Y = slot on entry; the card's A result goes to SLOTRES+n.
SLOTSCAN:
        LDA #0
        STA CURX
        LDY #1
SS1:    STY SLOTN
        TYA
        ORA #$C0
        STA PTR+1
        LDA #$F8
        STA PTR
        LDY #0
SS2:    LDA (PTR),Y
        CMP PROBESIG,Y
        BNE SSNEXT
        INY
        CPY #8
        BNE SS2
        LDA #$20            ; JSR $Cn00 ; RTS stub in RAM
        STA STUB
        LDA #$00
        STA STUB+1
        LDA PTR+1
        STA STUB+2
        LDA #$60
        STA STUB+3
        LDA SLOTN
        ASL
        ASL
        ASL
        ASL
        TAX
        LDY SLOTN
        JSR STUB
        LDY SLOTN
        STA SLOTRES,Y
        LDA #0
        STA CURX
        INC CURY
SSNEXT: LDY SLOTN
        INY
        CPY #8
        BNE SS1
        RTS
PROBESIG: .byte "FESPROBE"

; Dump: one row per slot 1-7 from DUMP_ROW: "n " then the eight bytes the CPU
; reads at $CnF8-$CnFF. Except for the Disk II slot it then prints the card
; registers at $C0n1 and $C0n2, the $C0n0 read-back after writing $5A, and
; $C800/$CBFF after a $Cn00 access claims $C800 and writes $C3/$3C to them.
; Reading $CFFF releases $C800 again. All values are hex.
DUMP:   LDA #{DUMP_ROW}
        STA CURY
        LDY #1
DM1:    STY SLOTN
        LDA #0
        STA CURX
        TYA
        ORA #$B0
        JSR PUTC
        LDA #$A0
        JSR PUTC
        LDA SLOTN
        ORA #$C0
        STA PTR+1
        LDA #$F8
        STA PTR
        LDY #0
DM2:    LDA (PTR),Y
        JSR PUTHEX
        INY
        CPY #8
        BNE DM2
        LDA SLOTN
        CMP #6
        BEQ DM3
        LDA #$A0
        JSR PUTC
        LDA SLOTN
        ASL
        ASL
        ASL
        ASL
        TAX
        LDA $C081,X
        JSR PUTHEX
        LDA $C082,X
        JSR PUTHEX
        LDA #$A0
        JSR PUTC
        LDA #$5A
        STA $C080,X
        LDA #$00
        LDA $C080,X
        JSR PUTHEX
        LDA #$A0
        JSR PUTC
        LDY #0
        STY PTR
        LDA (PTR),Y         ; $Cn00 access claims $C800 for this slot
        LDA #$C3
        STA $C800
        LDA #$3C
        STA $CBFF
        LDA $C800
        JSR PUTHEX
        LDA $CBFF
        JSR PUTHEX
        LDA $CFFF           ; release $C800
DM3:    INC CURY
        LDY SLOTN
        INY
        CPY #8
        BEQ DM4
        JMP DM1
DM4:    RTS
; PUTHEX: print A as two hex digits. Preserves X and Y.
PUTHEX: PHA
        LSR
        LSR
        LSR
        LSR
        JSR PUTNIB
        PLA
        AND #$0F
PUTNIB: STX HEXX
        TAX
        LDA HEXDIG,X
        LDX HEXX
        JMP PUTC
HEXDIG: .byte {screen_bytes("0123456789ABCDEF")}

; Screens --------------------------------------------------------------------
TEXTSCR:
        LDA $C051
        LDA $C052
        LDA $C054
        JSR CLEAR
{banner}
        ; full character set: 64 normal glyphs then 64 inverse glyphs
        LDA #{CHARSET_ROW}
        STA CURY
        LDA #0
        STA CURX
        LDX #0
TS1:    TXA
        ORA #$80
        JSR PUTC
        INX
        CPX #64
        BNE TS1
        LDX #0
TS2:    TXA
        JSR PUTC
        INX
        CPX #64
        BNE TS2
        LDA #0              ; echo continues on the message row
        STA CURX
        LDA #{MESSAGE_ROW}
        STA CURY
        RTS

CLEAR:  LDA #$04
        STA TXTPTR+1
        LDA #$00
        STA TXTPTR
        TAY
        LDA #$A0
CL1:    STA (TXTPTR),Y
        INY
        BNE CL1
        INC TXTPTR+1
        LDX TXTPTR+1
        CPX #$08
        BNE CL1
        LDA #0
        STA CURX
        STA CURY
        RTS

; PUTS: print the zero-terminated screen-code string at (SRC).
PUTS:   LDY #0
PS1:    LDA (SRC),Y
        BEQ PS2
        JSR PUTC
        INY
        BNE PS1
PS2:    RTS

; PUTC: store screen code A at CURX,CURY and advance. Preserves X and Y.
PUTC:   STA TMP
        STY IDX
        STX COL
        LDX CURY
        LDA TXTLO,X
        STA TXTPTR
        LDA TXTHI,X
        STA TXTPTR+1
        LDY CURX
        LDA TMP
        STA (TXTPTR),Y
        INC CURX
        LDA CURX
        CMP #40
        BCC PC1
        LDA #0
        STA CURX
        INC CURY
        LDA CURY
        CMP #24
        BCC PC1
        LDA #0
        STA CURY
PC1:    LDY IDX
        LDX COL
        RTS

; Lo-res: top block colour (column+row) & 15, bottom block +8.
LORES:  LDA $C050
        LDA $C052
        LDA $C054
        LDA $C056
        LDX #0
LR1:    LDA TXTLO,X
        STA TXTPTR
        LDA TXTHI,X
        STA TXTPTR+1
        LDY #0
LR2:    STX TMP
        TYA
        CLC
        ADC TMP
        AND #$0F
        STA TMP
        CLC
        ADC #8
        AND #$0F
        ASL
        ASL
        ASL
        ASL
        ORA TMP
        STA (TXTPTR),Y
        INY
        CPY #40
        BNE LR2
        INX
        CPX #24
        BNE LR1
        RTS

; Hi-res page 1: eight five-byte colour bands on every line.
HIRES:  LDA $C050
        LDA $C052
        LDA $C054
        LDA $C057
        LDX #0
HR1:    LDA HGRLO,X
        STA TXTPTR
        LDA HGRHI,X
        STA TXTPTR+1
        LDY #39
HR2:    LDA HROW,Y
        STA (TXTPTR),Y
        DEY
        BPL HR2
        INX
        CPX #192
        BNE HR1
        RTS

; Mixed: lo-res top with a text window in the last four rows.
MIXED:  JSR LORES
        LDX #{MESSAGE_ROW}
MX1:    LDA TXTLO,X
        STA TXTPTR
        LDA TXTHI,X
        STA TXTPTR+1
        LDY #39
        LDA #$A0
MX2:    STA (TXTPTR),Y
        DEY
        BPL MX2
        INX
        CPX #24
        BNE MX1
        LDA #0
        STA CURX
        LDA #{MESSAGE_ROW + 1}
        STA CURY
        LDA #<MSGMIX
        STA SRC
        LDA #>MSGMIX
        STA SRC+1
        JSR PUTS
        LDA $C053
        RTS

; Self tests -----------------------------------------------------------------
; RAM: write and verify (page EOR offset) in pages $08-$0B, $20-$21, $BF.
TESTRAM:
        LDX #0
TR1:    LDA RAMPAGES,X
        BEQ TROK
        STA TXTPTR+1
        LDA #0
        STA TXTPTR
        LDY #0
TR2:    TYA
        EOR TXTPTR+1
        STA (TXTPTR),Y
        INY
        BNE TR2
TR3:    TYA
        EOR TXTPTR+1
        CMP (TXTPTR),Y
        BNE TRBAD
        INY
        BNE TR3
        INX
        JMP TR1
TROK:   CLC
        RTS
TRBAD:  SEC
        RTS
RAMPAGES: .byte $08, $09, $0A, $0B, $20, $21, $BF, 0

TESTLC: LDX #LCEND-LCSRC-1
TL1:    LDA LCSRC,X
        STA $1000,X
        DEX
        BPL TL1
        JMP $1000

; Disk II ------------------------------------------------------------------
; Clean-room boot and sector reader for the built-in slot 6 controller.
DBOOT:  STX DSLOT
        LDA $C08E,X         ; Q7 low: read mode
        LDA $C08C,X         ; Q6 low
        LDA $C08A,X         ; drive 1
        LDA $C089,X         ; motor on
        LDA #80             ; unknown head position: recalibrate from 80
        STA HTRACK
        LDA #0
        JSR SEEK
        LDA #0
        STA TRKWANT
        STA SECWANT
        STA BUFP
        LDA #$08
        STA BUFP+1
DB1:    JSR READSECT
        BCS DB1
        LDX DSLOT
        JMP $0801

; SEEK: move the head from HTRACK to half-track A, one phase at a time.
SEEK:   STA TMP
SK1:    LDA HTRACK
        CMP TMP
        BEQ SKDONE
        BCS SKOUT
        CLC
        ADC #1
        JMP SKSTEP
SKOUT:  SEC
        SBC #1
SKSTEP: STA TMP2
        AND #3
        JSR PHON
        JSR SKWAIT
        LDA HTRACK
        AND #3
        JSR PHOFF
        JSR SKWAIT
        LDA TMP2
        STA HTRACK
        JMP SK1
SKDONE: LDX DSLOT
        RTS
PHON:   ASL
        ORA DSLOT
        TAX
        LDA $C081,X
        RTS
PHOFF:  ASL
        ORA DSLOT
        TAX
        LDA $C080,X
        RTS
SKWAIT: LDY #$20
SW1:    DEY
        BNE SW1
        RTS

; READSECT: read TRKWANT/SECWANT (physical) into (BUFP). Carry set on error.
READSECT:
        LDX DSLOT
        LDA #0
        STA RETRY
RS1:    JSR RDADDR
        BCS RS2
        LDA FTRK
        CMP TRKWANT
        BNE RS2
        LDA FSEC
        CMP SECWANT
        BNE RS2
        JSR RDDATA
        BCC RS3
RS2:    DEC RETRY
        BNE RS1
        SEC
        RTS
RS3:    JSR POSTNIB
        CLC
        RTS

; RDADDR: next address field into FVOL..FCHK. Carry set on bad checksum.
RDADDR:
RA1:    LDA $C08C,X
        BPL RA1
RA2:    CMP #$D5
        BNE RA1
RA3:    LDA $C08C,X
        BPL RA3
        CMP #$AA
        BNE RA2
RA4:    LDA $C08C,X
        BPL RA4
        CMP #$96
        BNE RA1
        LDY #0
RA5:    LDA $C08C,X
        BPL RA5
        SEC
        ROL
        STA TMP
RA6:    LDA $C08C,X
        BPL RA6
        AND TMP
        STA FVOL,Y
        INY
        CPY #4
        BNE RA5
        LDA FVOL
        EOR FTRK
        EOR FSEC
        EOR FCHK
        CMP #1
        RTS

; RDDATA: 342 XOR-chained values and checksum. Carry set on error.
RDDATA: LDY #32
RD1:    DEY
        BEQ RDBAD
RD2:    LDA $C08C,X
        BPL RD2
RD3:    CMP #$D5
        BNE RD1
RD4:    LDA $C08C,X
        BPL RD4
        CMP #$AA
        BNE RD3
RD5:    LDA $C08C,X
        BPL RD5
        CMP #$AD
        BNE RD1
        LDA #86
        STA IDX
        LDA #0
RD6:    LDY $C08C,X
        BPL RD6
        EOR DTAB,Y
        DEC IDX
        LDY IDX
        STA AUX,Y
        BNE RD6
RD7:    STY IDX
RD8:    LDY $C08C,X
        BPL RD8
        EOR DTAB,Y
        LDY IDX
        STA (BUFP),Y
        INY
        BNE RD7
RD9:    LDY $C08C,X
        BPL RD9
        EOR DTAB,Y
        BNE RDBAD
        CLC
        RTS
RDBAD:  SEC
        RTS

; POSTNIB: merge the two-bit pieces back into the eight-bit bytes.
POSTNIB:
        LDY #0
PN1:    LDX #86
PN2:    DEX
        BMI PN1
        LDA (BUFP),Y
        LSR AUX,X
        ROL
        LSR AUX,X
        ROL
        STA (BUFP),Y
        INY
        BNE PN2
        LDX DSLOT
        RTS

; ---------------------------------------------------------------- vectors
        .org $FE00
        JMP READSECT        ; $FE00
        JMP SEEK            ; $FE03
        JMP PUTS            ; $FE06
        JMP CLEAR           ; $FE09
        JMP PUTC            ; $FE0C
        .org $FF58
IORTS:  RTS                 ; conventional known RTS used for slot discovery
NMI:    RTI
IRQ:    RTI
        .org $FFFA
        .word NMI, RESET, IRQ
"""


def build_firmware() -> bytes:
    image = assemble(firmware_source(), FIRMWARE_BASE, FIRMWARE_SIZE)
    return image


def sector_base(file_sector: int) -> int:
    return (37 * file_sector + 5) & 0xFF


def progression_sector(file_sector: int) -> bytes:
    start = sector_base(file_sector)
    return bytes((start + 11 * j) & 0xFF for j in range(256))


# Sectors stage 2 loads and verifies: (track, physical sector, load page).
DISK_READS = [(0, 3, 0x0B)] + [(17, p, 0x40 + p) for p in range(0, 16, 2)] + [(34, 15, 0x50)]


def stage1_source() -> str:
    return """
DSLOT   = $2B
BUFP    = $26
SECWANT = $3D
TRKWANT = $41
READSECT = $FE00
        .org $0800
        .byte $01
BOOT1:  STX DSLOT
        LDA #$B1
        STA $03F1
        LDA #0
        STA TRKWANT
        STA BUFP
        LDA #1
        STA SECWANT
        LDA #$09
        STA BUFP+1
B1:     JSR READSECT
        BCS B1
        JMP $0900
"""


def stage2_source() -> str:
    reads = "\n".join(f"        .byte {t}, {p}, ${page:02X}, ${sector_base(t * 16 + DOS_SKEW[p]):02X}"
                      for t, p, page in DISK_READS)
    return f"""
DSLOT   = $2B
BUFP    = $26
SECWANT = $3D
TRKWANT = $41
HTRACK  = $31
PTR     = $1E
NUM     = $1D
EXPECT  = $1C
READSECT = $FE00
SEEK    = $FE03
PUTS    = $FE06
CURX    = $08
CURY    = $09
SRC     = $0C
        .org $0900
STAGE2: LDA #$B2
        STA $03F1
        LDA #0
        STA NUM
S1:     LDA NUM
        ASL
        ASL
        TAX
        LDA READS,X
        CMP #$FF
        BEQ SPASS
        STA TRKWANT
        ASL
        JSR SEEK
        LDA NUM
        ASL
        ASL
        TAX
        LDA READS+1,X
        STA SECWANT
        LDA READS+2,X
        STA BUFP+1
        STA PTR+1
        LDA READS+3,X
        STA EXPECT
        LDA #0
        STA BUFP
        STA PTR
        JSR READSECT
        BCS SFAIL
        LDY #0
        LDA EXPECT
S2:     CMP (PTR),Y
        BNE SFAIL
        CLC
        ADC #11
        INY
        BNE S2
        INC NUM
        JMP S1
SPASS:  LDA #0
        STA CURX
        LDA #22
        STA CURY
        LDA #<MSGOK
        STA SRC
        LDA #>MSGOK
        STA SRC+1
        JSR PUTS
        LDA #$A5
        STA $03F0
        LDA #$C2
        STA $03F1
HALT:   JMP HALT
SFAIL:  LDA NUM
        ORA #$E0
        STA $03F0
        JMP HALT
MSGOK:  .byte {screen_bytes(DISK_PASS)}, 0
READS:
{reads}
        .byte $FF
"""


def build_disk() -> bytes:
    disk = bytearray(DISK_SIZE)
    for f in range(DISK_SIZE // 256):
        disk[f * 256:(f + 1) * 256] = progression_sector(f)
    stage1 = assemble(stage1_source(), 0x0800, 256, fill=0)
    stage2 = assemble(stage2_source(), 0x0900, 256, fill=0)
    disk[0:256] = stage1
    stage2_file = DOS_SKEW[1]
    disk[stage2_file * 256:(stage2_file + 1) * 256] = stage2
    return bytes(disk)


def write_hex(path: Path, data: bytes) -> None:
    path.write_text("".join(f"{value:02x}\n" for value in data))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    firmware = build_firmware()
    disk = build_disk()
    (args.output_dir / "firmware.bin").write_bytes(firmware)
    write_hex(args.output_dir / "firmware.hex", firmware)
    (args.output_dir / "disk.dsk").write_bytes(disk)
    write_hex(args.output_dir / "disk.hex", disk)
    for name, data in (("firmware.bin", firmware), ("disk.dsk", disk)):
        print(f"{name} {len(data)} {hashlib.sha256(data).hexdigest()}")


if __name__ == "__main__":
    main()
