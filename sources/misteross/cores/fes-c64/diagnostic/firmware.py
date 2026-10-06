#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Open 16 KiB C64 firmware: an 8 KiB BASIC window and an 8 KiB KERNAL window.

No Commodore ROM is used. The KERNAL window is a diagnostic that checks RAM,
the linked BASIC signature, VIC and character generator, both cartridge
sockets, the joystick and keyboard, then LOADs BOOT from the built-in 1541
over the IEC bus and checks CIA1 IRQ and CIA2 NMI delivery. Bytes 0..8191 map
at $A000 and bytes 8192..16383 at $E000.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from asm6502 import assemble

BASIC = bytes(b"FESBASIC") + bytes(8192 - 8)

KERNAL = r"""
reset:
        sei
        cld
        ldx #$ff
        txs
        lda #$37
        sta $01
        lda #$2f
        sta $00
        lda #$00
        sta $02
        sta $17
        lda #$03
        sta $dd00
        lda #$3f
        sta $dd02
        lda #$a5
        sta $0200
        cmp $0200
        beq ram_ok
        lda #$01
        jmp fail
ram_ok:
        lda $a000
        cmp #'F'
        beq basic_ok
        lda #$02
        jmp fail
basic_ok:
        lda #$1b
        sta $d011
        lda #$14
        sta $d018
        lda #$c8
        sta $d016
        lda #$02
        sta $d020
        lda #$06
        sta $d021
        lda $d020
        and #$0f
        cmp #$02
        beq vic_ok
        lda #$03
        jmp fail
vic_ok:
        ldx #$00
paint:
        lda msg,x
        sta $0400,x
        lda #$01
        sta $d800,x
        inx
        cpx #$07
        bne paint
        lda $d800
        and #$0f
        cmp #$01
        beq color_ok
        lda #$03
        jmp fail
color_ok:
        lda #$33
        sta $01
        lda $d000
        cmp #$3c
        php
        lda #$37
        sta $01
        plp
        beq char_ok
        lda #$04
        jmp fail
char_ok:
        lda $8000
        cmp #'F'
        bne cart_bad
        lda $8001
        cmp #'E'
        bne cart_bad
        lda $8003
        cmp #'1'
        beq cart_ok
cart_bad:
        lda #$05
        jmp fail
cart_ok:
        lda #$5a
        sta $de00
        cmp $de00
        bne io_bad
        lda $de01
        cmp #$c6
        beq io_ok
io_bad:
        lda #$06
        jmp fail
io_ok:
        lda #$00
        sta $c001
        sta $dc02
        sta $dc03
        lda #$07
        sta $c000
wait_joy:
        lda $dc00
        and #$1f
        cmp #$1e
        bne wait_joy
joy_ok:
        lda #$ff
        sta $dc02
        lda #$fb
        sta $dc00
        lda #$08
        sta $c000
wait_key:
        lda $dc01
        and #$02
        bne wait_key
key_ok:
        lda #$00
        sta $d400
        lda #$10
        sta $d401
        lda #$00
        sta $d402
        lda #$08
        sta $d403
        lda #$80
        sta $d406
        lda #$41
        sta $d404
        lda #$0f
        sta $d418
        jsr load_boot
        lda $17
        bne disk_bad
        lda $0800
        cmp #$01
        bne disk_bad
        lda $0801
        cmp #$08
        bne disk_bad
        lda $0802
        cmp #$11
        bne disk_bad
        lda $0803
        cmp #$22
        bne disk_bad
        lda $0804
        cmp #$33
        bne disk_bad
        lda $0805
        cmp #$44
        beq disk_ok
disk_bad:
        lda #$09
        jmp fail
disk_ok:
        jsr check_interrupts
        lda #$05
        sta $d020
        lda #$ff
        sta $c000
        lda #$00
        sta $c001
hang:   jmp hang
fail:
        sta $c000
        lda #$01
        sta $c001
        jmp hang

check_interrupts:
        lda #$00
        sta $19
        sta $1a
        sta $1b
        sta $1c
        lda #$20
        sta $dc06
        lda #$00
        sta $dc07
        lda #$82
        sta $dc0d
        lda #$09
        sta $dc0f
        cli
        ldx #$ff
wait_irq:
        lda $19
        bne irq_done
        dex
        bne wait_irq
irq_bad:
        sei
        lda #$0a
        jmp fail
irq_done:
        sei
        cmp #$01
        bne irq_bad
        lda $1b
        cmp #$82
        bne irq_bad
        lda $1a
        bne irq_bad
        lda #$20
        sta $dd04
        lda #$00
        sta $dd05
        lda #$81
        sta $dd0d
        lda #$09
        sta $dd0e
        ldx #$ff
wait_nmi:
        lda $1a
        bne nmi_done
        dex
        bne wait_nmi
nmi_bad:
        lda #$0b
        jmp fail
nmi_done:
        cmp #$01
        bne nmi_bad
        lda $1c
        cmp #$81
        bne nmi_bad
        lda $19
        cmp #$01
        bne nmi_bad
        rts

irq_handler:
        pha
        lda $dc0d
        sta $1b
        inc $19
        pla
        rti

nmi_handler:
        pha
        lda $dd0d
        sta $1c
        inc $1a
        pla
        rti

load_boot:
        lda #$00
        sta $17
        sta $14
        jsr atn_on
        lda #$28
        jsr send_byte
        lda #$f0
        jsr send_byte
        jsr atn_off
        lda #'B'
        jsr send_byte
        lda #'O'
        jsr send_byte
        lda #'O'
        jsr send_byte
        lda #'T'
        jsr send_byte
        jsr atn_on
        lda #$3f
        jsr send_byte
        lda #$48
        jsr send_byte
        lda #$60
        jsr send_byte
        jsr atn_off
        ldx #$00
recv_loop:
        jsr recv_byte
        sta $0800,x
        inx
        lda $17
        bne load_done
        lda $14
        beq recv_loop
load_done:
        rts

atn_on:
        lda $02
        ora #$08
        sta $02
        rts
atn_off:
        lda $02
        and #$f7
        sta $02
        rts

port:
        lda $02
        and #$08
        ora #$03
        rts

send_byte:
        sta $13
        jsr port
        ora #$10
        sta $dd00
        jsr wait_data_high
        lda $17
        bne send_done
        ldx #$08
send_bit:
        jsr port
        ora #$10
        lsr $13
        bcc send_zero
        ora #$20
send_zero:
        sta $dd00
        jsr delay
        and #$ef
        sta $dd00
        jsr delay
        dex
        bne send_bit
        jsr wait_data_low
        lda $17
        bne send_done
        jsr port
        ora #$10
        sta $dd00
send_done:
        rts

recv_byte:
        lda #$00
        sta $14
        jsr port
        ora #$20
        sta $dd00
        jsr wait_clk_low
        lda $17
        bne recv_done
        jsr port
        sta $dd00
        ldy #40
eoi_watch:
        lda $dd00
        and #$40
        bne recv_bits
        jsr delay
        dey
        bne eoi_watch
        lda #$01
        sta $14
        jsr port
        ora #$20
        sta $dd00
        jsr delay
        and #$df
        sta $dd00
recv_bits:
        lda #$08
        sta $15
recv_bit:
        jsr wait_clk_low
        jsr wait_clk_high
        lda $dd00
        asl
        ror $13
        dec $15
        bne recv_bit
        jsr port
        ora #$20
        sta $dd00
        lda $13
recv_done:
        rts

wait_data_high:
        ldy #0
wdh:
        lda $dd00
        bpl wdh_ok
        iny
        bne wdh
        lda $17
        bne wdh_done
        lda #$01
        sta $17
wdh_done:
        rts
wdh_ok:
        rts

wait_data_low:
        ldy #0
wdl:
        lda $dd00
        bmi wdl_ok
        iny
        bne wdl
        lda $17
        bne wdl_done
        lda #$02
        sta $17
wdl_done:
        rts
wdl_ok:
        rts

wait_clk_low:
        ldy #0
wcl:
        lda $dd00
        and #$40
        bne wcl_ok
        iny
        bne wcl
        lda $17
        bne wcl_done
        lda #$03
        sta $17
wcl_done:
        rts
wcl_ok:
        rts

wait_clk_high:
        ldy #0
wch:
        lda $dd00
        and #$40
        beq wch_ok
        iny
        bne wch
        lda $17
        bne wch_done
        lda #$04
        sta $17
wch_done:
        rts
wch_ok:
        rts

delay:
        pha
        lda #$08
        sta $18
delay_loop:
        dec $18
        bne delay_loop
        pla
        rts

msg:
        .byte 6, 5, 19, 32, 3, 54, 52

        .org $fffa
        .word nmi_handler
        .word reset
        .word irq_handler
"""


def build(*, without_cartridges: bool = False) -> bytes:
    source = KERNAL
    if without_cartridges:
        # Keep every other check identical when qualifying a vacant shell.
        start = source.index("char_ok:\n") + len("char_ok:\n")
        end = source.index("io_ok:\n", start)
        source = source[:start] + source[end:]
    kernal = assemble(source, 0xE000, 8192)
    if len(BASIC) != 8192 or len(kernal) != 8192:
        raise RuntimeError("firmware windows must be 8 KiB")
    if kernal[0x1FFC] != 0x00 or kernal[0x1FFD] != 0xE0:
        raise RuntimeError("reset vector is not $E000")
    return BASIC + kernal


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--without-cartridges", action="store_true",
                        help="skip only cartridge checks for a vacant sealed shell")
    args = parser.parse_args()
    image = build(without_cartridges=args.without_cartridges)
    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir / "firmware.bin").write_bytes(image)
    (args.output_dir / "firmware.hex").write_text("".join(f"{byte:02X}\n" for byte in image))


if __name__ == "__main__":
    main()
