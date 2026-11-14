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

## Part 2 — the reference board, settled before any capture

**Done, and the answer is uncomfortable rather than convenient.**

[`method.md`](method.md) was committed before any pace capture, with the board
rule, the board, the metric and the band. It is pushed ahead of every capture
so the order is in the branch's history and not only in its prose.

### The rule, and the number it gives

#1838 validated its 286 reference by placing it mid-range among the real
machines of its class in TopBench 0.38a's shipped database. That database
holds **no 386DX at 25 MHz at all**, so the rule needs a reading, and
`method.md` fixes one: take every AT-class real 386DX entry at whatever clock
and scale its score to 25 MHz by its clock.
[`topbench_band.py`](topbench_band.py) does exactly that and
[`topbench-386dx.json`](topbench-386dx.json) is its output, so the figures can
be rederived. Eighteen entries, scaled to 25 MHz:

```
31 32 34 42 48 54 54 57 57 57 59 60 61 65 66 68 75 78
```

minimum 31, lower quartile 47, **median 57**, upper quartile 65, maximum 78.
The four highest are one M326 motherboard entered at four clocks; without it
the maximum is 66. The distribution is bimodal — three IBM PS/2 models at
1.25–1.38 points per MHz, then everything else from 1.68 up, with nothing in
between.

### The reference does not meet it

`asus3863364k` at 25 MHz, cached, scores TopBench **40** — fourth lowest of
the nineteen, below the lower quartile, and in the gap between the two
clusters rather than inside either. Three checks before concluding that:

| setting | TopBench |
| --- | --- |
| `cpu_waitstates = 0` (the preset's own definition) | 40 |
| `cpu_waitstates = 1` | 40 |
| board default | 40 |

and #1838 had already found the uncached `ecs386` and the cached
`asus3863364k` both at 40. Five further 86Box 386DX boards (`award495`,
`ecs386v`, `acc386`, `dataexpert386wb`, `asus386`) were staged for the same
benchmark and are **not measured**: each has an AMI BIOS that drops into SETUP
on a blank CMOS. They are listed as unmeasured, not presumed.

So 86Box's 386DX at 25 MHz models a machine at the slow end of the real range,
and nothing inside it reaches the middle. That is stated rather than worked
around, and `method.md` fixes in advance how a pace miss would be classified
against the throughput ratio.

### Where the kit's preset sits, which is the part #1853 asked for

The kit's 386DX-25 preset scored TopBench 68 in
[#1486](https://github.com/DeanoC/fes/pull/1486) — an older seal, so
`method.md` requires it re-measured in the same claim as the pace captures.
Taken at face value for now, 68 is above sixteen of the eighteen real entries,
and above all fourteen once the M326's four are set aside. It beats every real
386DX-20 in the database (maximum 60) and sits just under the slowest real
386DX-33 (71).

**That is the same shape as [#1840](https://github.com/DeanoC/fes/issues/1840)
found at 286-12, in a milder form.** At 286-12 the preset scored above *every*
real 286 of its clock; at 386DX-25 it scores above most but not all real
386DXs once they are scaled, and it stays inside the range. It is a
qualification a pace comparison needs, not a second defect, and on this
evidence it does not warrant its own issue.

The reference is therefore slower than the kit by a measured factor of
**68/40 = 1.7**, and any CPU-bound pace figure will differ by about that much
before anything about the core is in question.
