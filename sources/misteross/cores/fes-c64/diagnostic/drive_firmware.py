#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Open 1541 motherboard diagnostic; no Commodore firmware or disk bytes."""
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
    lda #$1a
    sta $1802
    lda #$12
    sta $0001
    lda #$34
    sta $0701
    lda $2001
    cmp #$12
    bne fail
    lda $2701
    cmp #$34
    bne fail
    lda #$00
    sta $180b
    sta $1c0b
    lda #$c0
    sta $180e
    lda #$a0
    sta $1c0e
    lda #$00
    sta $02
    lda #$40
    sta $1804
    lda #$00
    sta $1805
    cli
wait_t1:
    lda $02
    beq wait_t1
    sei
    cmp #$01
    bne fail
    lda #$00
    sta $02
    lda #$40
    sta $1c08
    lda #$00
    sta $1c09
    cli
wait_t2:
    lda $02
    beq wait_t2
    sei
    cmp #$02
    bne fail
    lda #$a5
    sta $07ff
idle:
    jmp idle
fail:
    lda #$ff
    sta $07ff
    jmp fail
irq:
    pha
    lda $180d
    and #$40
    beq try_t2
    lda $1804
    lda #$01
    sta $02
    pla
    rti
try_t2:
    lda $1c0d
    and #$20
    beq irq_bad
    lda $1c08
    lda #$02
    sta $02
    pla
    rti
irq_bad:
    jmp fail
.org $fffa
    .word fail, reset, irq
"""

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    data = assemble(SOURCE, 0xc000, 16384)
    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir/'drive.hex').write_text(''.join(f'{v:02x}\n' for v in data))
    # Exercise the same mapped drive-ROM port as the complete motherboard.
    combined = bytes(20480) + data
    (args.output_dir/'combined-firmware.hex').write_text(
        ''.join(f'{v:02x}\n' for v in combined))

if __name__ == '__main__':
    main()
