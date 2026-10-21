#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Original native 1541 media diagnostic, paired with firmware.py.

This fixed transaction is deliberately not DOS: the drive CPU reads track 1,
sector 0 through its VIA/GCR channel, decodes and checks the complete sector,
then sends six payload bytes over the physical IEC pins. The synthetic disk
uses zero IDs. ATN requests one bit, CLOCK acknowledges it and DATA carries it.
"""
from pathlib import Path
import argparse
from asm6502 import assemble

SOURCE = r"""
.org $c000
reset:
 sei
 cld
 ldx #$ff
 txs
 lda #$00
 sta $07ff
 sta $1800
 sta $1c03
 lda #$01
 sta $1c0b
 lda #$1a
 sta $1802
 lda #$6f
 sta $1c02
 lda #$ee
 sta $1c0c
 lda #$64
 sta $10
 sta $1c00
 ; The medium resets on track18. Move34 halfsteps to track1.
 ldx #34
seek:
 lda $10
 sec
 sbc #1
 and #3
 ora #$64
 sta $10
 sta $1c00
 ldy #0
settle:
 dey
 bne settle
 dex
 bne seek
 lda #1
 sta $07ff
header:
 jsr sync
 ldx #0
header_read:
 bvc header_read
 clv
 lda $1c01
 sta $0400,x
 inx
 cpx #10
 bne header_read
 ldx #0
header_compare:
 lda $0400,x
 cmp expected_header,x
 bne header
 inx
 cpx #10
 bne header_compare
 lda #2
 sta $07ff
 jsr sync
 ldx #0
data_page:
 bvc data_page
 clv
 lda $1c01
 sta $0400,x
 inx
 bne data_page
data_tail:
 bvc data_tail
 clv
 lda $1c01
 sta $0500,x
 inx
 cpx #69
 bne data_tail
 lda #3
 sta $07ff
 lda #0
 sta $20
 sta $22
 sta $26
 lda #4
 sta $21
 lda #6
 sta $27
decode:
 jsr nibble
 asl
 asl
 asl
 asl
 sta $25
 jsr nibble
 ora $25
 ldy #0
 sta ($26),y
 inc $26
 bne decoded_increment
 inc $27
decoded_increment:
 lda $27
 cmp #7
 bne decode
 lda $26
 cmp #4
 bne decode
 lda $0600
 cmp #7
 bne bad
 ldx #0
 lda #0
checksum:
 eor $0601,x
 inx
 bne checksum
 cmp $0701
 bne bad
 lda $0601
 bne bad
 lda $0602
 cmp #7
 bne bad
 lda #4
 sta $07ff
 lda #0
 sta $11
 lda #$18
 sta $13
send_byte:
 ldx $11
 lda $0603,x
 sta $12
 lda #8
 sta $14
send_bit:
 ; ATN is the host request; clock acknowledges and DATA carries one bit.
 ; PB4 mirrors ATN to cancel the1541 hardware attention gate.
 lda $13
 beq wait_atn_low
wait_atn_high:
 bit $1800
 bpl wait_atn_high
 jmp emit_bit
wait_atn_low:
 bit $1800
 bmi wait_atn_low
emit_bit:
 lsr $12
 lda $13
 bcs bit_one
 ora #2
bit_one:
 sta $1800
 lda $13
 eor #$18
 sta $13
 dec $14
 bne send_bit
 inc $11
 lda $11
 cmp #6
 bne send_byte
 lda #$a5
 sta $07ff
idle:
 jmp idle
bad:
 lda #$ff
 sta $07ff
 jmp bad
sync:
 bit $1c00
 bmi sync
 clv
sync_end:
 bit $1c00
 bpl sync_end
 clv
 rts
nibble:
 lda #0
 sta $23
 ldx #5
nibble_bit:
 lda $22
 bne have_bits
 ldy #0
 lda ($20),y
 sta $24
 inc $20
 bne pointer_ok
 inc $21
pointer_ok:
 lda #8
 sta $22
have_bits:
 asl $24
 rol $23
 dec $22
 dex
 bne nibble_bit
 ldx $23
 lda inverse,x
 cmp #$ff
 bne nibble_ok
 jmp bad
nibble_ok:
 rts
expected_header:
 .byte 82,84,181,41,75,82,148,165,85,85
inverse:
 .byte 255,255,255,255,255,255,255,255,255,8,0,1,255,12,4,5,255,255,2,3,255,15,6,7,255,9,10,11,255,13,14,255
.org $fffa
 .word bad,reset,bad
"""


def build() -> bytes:
    return assemble(SOURCE, 0xC000, 16384)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    image = build()
    (args.output_dir / "drive-media.bin").write_bytes(image)
    (args.output_dir / "drive-media.hex").write_text("".join(f"{b:02x}\n" for b in image))


if __name__ == "__main__":
    main()
