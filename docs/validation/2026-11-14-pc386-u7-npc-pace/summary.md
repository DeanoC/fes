# Ultima VII: The Black Gate's NPC pace against a 386DX-25 reference, 14 November 2026

Issue [#1853](https://github.com/DeanoC/fes/issues/1853), the last unticked
item of [#1368](https://github.com/DeanoC/fes/issues/1368).

`sources/misteross/docs/pc386-machine-design.md`'s acceptance table asks that
The Black Gate's "NPC pace matches the reference" under the 386-class preset.
[The 14 November Ultima VI record](https://github.com/DeanoC/fes/pull/1838)
(`docs/validation/2026-11-14-pc386-u6-u7-reference/`) named a reference for
Ultima VI and measured what it could, but reached no figure here, because the
game would not start on the reference at all. That blocker is what this record
clears first.

## Part 1 — the game starts on a 386DX-25 reference

**Done.** The Black Gate now boots, loads and reaches its title screen on the
reference, off the kit's own games image with the image's own boot
configuration unchanged.

### What was actually in the way

The earlier record had the cause right in outline — a real 386 board has
nothing behind C800–DFFF, where the kit's machine has RAM — and named the
route left to try, an ISA memory expansion board. Taking that route alone does
not work, and the reason only shows up if the memory map is **read from inside
the guest** instead of inferred from the emulator's device list.

`UMAP.COM` ([`umap.asm`](umap.asm), assembled with the NASM 2.16.03 the repo
pins) walks A000:0000 to F800:0000 in 4 KiB blocks and prints what each block
is: an option ROM (`AA55` signature), writable RAM, read-only non-uniform,
open bus (reads FF), or all zeroes. It was run before and after every
configuration change. On the reference as the earlier record left it:

```
UMAP A000-FFFF 4K blocks R=rom W=ram r=ro .=ff -=00
A000 ................
B000 ........WWWWWWWW
C000 Rrrrr...Rr......
D000 ................
E000 ................
F000 rrrrrr-----rrrrr
```

`R` at C800 is the **XT-IDE Universal BIOS option ROM**, which 86Box's
`xtide_at` device places at C8000 and which — unlike its PC/XT sibling — has
no configurable BIOS address: its device dialog offers only a BIOS revision.
So C800 was not empty, it was occupied, and the games image's
`UMBM.EXE /I=C800-DFFF` was refusing for the reason it prints: it tests C800
for RAM, finds ROM, and aborts. Adding the memory card on its own reproduces
that exactly — the card's RAM appears from CA00 up and C800 stays ROM:

```
C000 Rrrrr...RrWWWWWW
D000 WWWWWWWWWWWWWWWW
```

### The configuration that works

Two changes, not one:

1. **Replace the XT-IDE controller with a plain AT IDE controller**
   (`hdc_1 = ide_isa`, "[ISA16] PC/AT IDE Controller"), which has no option
   ROM, and declare the disk in CMOS instead, as a period AT owner would:
   SETUP's user-definable **drive type 47**, 65 cylinders / 16 heads /
   63 sectors, matching the image's `63, 16, 65` geometry. The XT-IDE card was
   only ever there because the 1988 boards do not auto-detect an IDE disk; the
   CMOS user type is the period answer to the same problem and costs no
   address space.
2. **Add an ISA memory expansion card** — 86Box's `genericat`, "Generic PC/AT
   Memory Expansion", `size = 96`, `start = 800` — putting 96 KiB of RAM at
   C8000–DFFFF, exactly the range the image's UMB provider asks for.

The configuration keys matter and are easy to get wrong, which cost several
runs here: the card is selected by **`isamem0_type`** in `[Other peripherals]`
(zero-based, although the GUI calls it "Card 1"), and configured in a section
named **`[Generic PC/AT Memory Expansion #1]`** (one-based) with keys `size`
and `start`, both in KiB. Hand-written `[PC/AT XTIDE #1] bios_addr` sections,
which the earlier record tried and could not confirm, have no effect for a
different reason: that device has no such option at all.

### What the guest sees now

```
UMAP A000-FFFF 4K blocks R=rom W=ram r=ro .=ff -=00
A000 ................
B000 ........WWWWWWWW
C000 Rrrrr...WWWWWWWW
D000 WWWWWWWWWWWWWWWW
E000 ................
F000 rrrrrr-----rrrrr
```

C800–DFFF is 96 KiB of writable RAM, and the image's own boot — menu entry 1,
unchanged — reports:

```
UMBM: Upper Memory Blocks: C800-DFFF
UMBM: XMS host not found, installing uXMS
```

with HIMEMX and CTMOUSE loaded high, **595,312 bytes** of free conventional
memory against the 561,144 the game demands, and `ULTIMA7.EXE` reaching its
title screen and intro. No file on the drive image is changed: the reference
runs the kit's `u7bg-master.img` (SHA-256 `d9bfafd4…`, the image the kit's own
acceptance runs use) with `UMAP.COM` copied in beside it for the probe.

Evidence: `~/fes-scratch/u6ref/runs/r1/` on Powerboat (`r1-umap.png`,
`r1-mem.png`, `r1-u7a.png`), from a fully scripted run of
`u7pace-run.sh r1 vm/u7ref cmds/r1.jsonl`.

### What this does *not* claim

The reference's DOS memory layout is now like the kit's, not identical to it:
both load HIMEMX and the mouse driver into UMBs at C800, and the free
conventional figure is compared side by side in the pace section below. Any
difference is named there rather than absorbed.
