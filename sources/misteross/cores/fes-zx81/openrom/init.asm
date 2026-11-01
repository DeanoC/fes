; SPDX-License-Identifier: GPL-2.0-or-later
; Power-on: size RAM in 1 KiB steps (a write that disturbs $4000 is a
; mirror of the first block), then NEW.

init:   ld hl,0x4000
        xor a
        ld (hl),a
        ld h,0x44
.probe: ld (hl),0x5a
        ld a,(hl)
        cp 0x5a
        jr nz,.top
        ld a,(0x4000)
        and a
        jr nz,.top
        ld a,h
        add a,4
        ld h,a
        cp 0x80
        jr nz,.probe
.top:   ld sp,hl                ; NEW clears the rest
        ld (RAMTOP),hl
        jp new_entry
