#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Open 36 KiB C64/1541 firmware: BASIC, KERNAL, characters and drive ROM.

No Commodore ROM is used. The KERNAL window is a diagnostic that checks RAM,
the linked BASIC signature, VIC and character generator, both cartridge
sockets, the joystick and keyboard, then receives a checked disk-sector payload
from the native 1541 through the physical IEC pins. This original diagnostic
transaction is not DOS or KERNAL LOAD. CIA1 IRQ and CIA2 NMI are also checked.
Bytes 0..8191 map at $A000, bytes 8192..16383 at $E000, bytes 16384..20479
form character ROM, and the final 16 KiB map at $C000 in the drive.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from asm6502 import assemble
from drive_media_firmware import build as drive_media_image
import importlib.util
_font_spec=importlib.util.spec_from_file_location("fes_c64_character_font",Path(__file__).with_name("font.py"))
_font=importlib.util.module_from_spec(_font_spec)
_font_spec.loader.exec_module(_font)
character_image=_font.image

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
        jsr show_stage
wait_joy:
        lda $dc00
        and #$1f
        cmp #$1e
        bne wait_joy
joy_ok:
        lda #$ff
        sta $dc02
        lda #$fd
        sta $dc00
        lda #$08
        sta $c000
        jsr show_stage
wait_key:
        lda $dc01
        and #$04
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
        ; Exercise real CPU read stalls from eight expanded sprite DMAs.
        ; X=0 keeps these sprites outside the native display window.
        ldx #$0f
        lda #$14
sprite_y_setup:
        sta $d000,x
        dex
        dex
        bpl sprite_y_setup
        lda #$ff
        sta $d017
        sta $d015
        jsr check_interrupts
        ; The demo's fastloader leaves VIC bank pins as inputs. Their pull-ups
        ; select bank zero even when the CIA output latch holds zero.
        lda #$00
        sta $dd00
        lda #$3c
        sta $dd02
        ; Black border and screen. A green bar is the 720p letterbox and is
        ; not this test. The pass mark is the word PASS in the active picture.
        lda #$00
        sta $d020
        sta $d021
        ldx #$00
blank_status:
        lda #$20
        sta $0400, x
        inx
        ; The status text is $0400..$0407. The stage digit is $0408.
        cpx #$09
        bne blank_status
        ldx #$00
paint_pass:
        lda pass_row, x
        sta $05b8, x
        sta $05e0, x
        sta $0608, x
        lda #$01
        sta $d9b8, x
        sta $d9e0, x
        sta $da08, x
        inx
        cpx #40
        bne paint_pass
        lda #$ff
        sta $c000
        lda #$00
        sta $c001
        jmp hang
fail:
        sta $c000
        jsr show_stage
        lda #$01
        sta $c001
        jmp hang
hang:
        jmp hang

show_stage:
        lda $c000
        clc
        adc #$30
        sta $0408
        lda #$01
        sta $d808
        rts

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
        jsr check_vic_irq
        rts

check_vic_irq:
        lda #$00
        sta $1d
        sta $1e
        lda $d011
        and #$7f
        sta $d011
        lda #$20
        sta $d012
        lda #$01
        sta $d019
        sta $d01a
        cli
        ldy #$ff
vic_wait_outer:
        ldx #$ff
vic_wait:
        lda $1d
        bne vic_irq_done
        dex
        bne vic_wait
        dey
        bne vic_wait_outer
vic_irq_bad:
        sei
        lda #$0c
        jmp fail
vic_irq_done:
        sei
        cmp #$01
        bne vic_irq_bad
        lda $1e
        and #$81
        cmp #$81
        bne vic_irq_bad
        lda $d019
        and #$81
        bne vic_irq_bad
        lda #$00
        sta $d01a
        rts

irq_handler:
        pha
        lda $d019
        and #$81
        cmp #$81
        bne irq_cia
        lda $d019
        sta $1e
        inc $d019
        inc $1d
        pla
        rti
irq_cia:
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
        ; Original diagnostic transfer: ATN request, CLK acknowledgement,
        ; DATA payload, all through the actual CIA/VIA IEC pins.
        lda #$09
        sta $c000
        jsr show_stage
        lda #$00
        sta $17
        lda #$08
        sta $14
        ldx #$00
native_byte:
        lda #$00
        sta $15
        ldy #$08
native_bit:
        lda $14
        ora #$03
        sta $dd00
        lda #$00
        sta $16
        sta $18
        lda $14
        beq native_high
native_low:
        lda $dd00
        and #$40
        beq native_sample
        dec $16
        bne native_low
        dec $18
        bne native_low
        jmp native_timeout
native_high:
        lda $dd00
        and #$40
        bne native_sample
        dec $16
        bne native_high
        dec $18
        bne native_high
native_timeout:
        lda #$80
        sta $17
        rts
native_sample:
        lda $dd00
        asl
        ror $15
        lda $14
        eor #$08
        sta $14
        dey
        bne native_bit
        lda $15
        sta $0800,x
        inx
        cpx #$06
        bne native_byte
        rts

msg:
        .byte 6, 5, 19, 32, 3, 54, 52

pass_row:
        .byte 16, 1, 19, 19, 32
        .byte 16, 1, 19, 19, 32
        .byte 16, 1, 19, 19, 32
        .byte 16, 1, 19, 19, 32
        .byte 16, 1, 19, 19, 32
        .byte 16, 1, 19, 19, 32
        .byte 16, 1, 19, 19, 32
        .byte 16, 1, 19, 19, 32

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
    return BASIC + kernal + character_image() + drive_media_image()


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
    (args.output_dir / "rom-test.hex").write_text("".join(f"{(i*37+(i>>8)*11+(i>>10)*17)&255:02x}\n" for i in range(36864)))


if __name__ == "__main__":
    main()
