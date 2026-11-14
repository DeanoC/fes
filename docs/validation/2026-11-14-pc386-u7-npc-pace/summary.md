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

`asus3863364k` at 25 MHz, cached, scores TopBench **40**. Only three of the
eighteen AT-class entries scale below it (31, 32 and 34, all IBM PS/2
models), it is below the lower quartile of 47, and it falls in the gap
between the two clusters rather than inside either. Three checks before concluding that:

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
Taken at face value for now, 68 is above fifteen of the eighteen real entries
(the M326 scales to 68.2 at 33 MHz, 75.0 at 20 and 78.1 at 16), and above all
fourteen once the M326's four are set aside. It beats every real
386DX-20 in the database (maximum 60) and sits just under the slowest real
386DX-33 (71).

**That is the same shape as [#1840](https://github.com/DeanoC/fes/issues/1840)
found at 286-12, in a milder form.** At 286-12 the preset scored above *every*
real 286 of its clock; at 386DX-25 it scores above most but not all real
386DXs once they are scaled, and it stays inside the range. It is a
qualification a pace comparison needs, not a second defect, and on this
evidence it does not warrant its own issue.

The reference is therefore slower than the kit by a measured factor of
**68/40 = 1.7** on this benchmark. `method.md` expected to use that ratio to
classify a pace miss; Part 3 measures the reference across three clocks and
finds that its TopBench score and the game's pace do not move together, so the
ratio cannot be used that way. It is given here as what it is — a benchmark
ratio — and Part 3 says why it does not carry over.


## Part 3 — the pace, measured: **fail, by 26%**

**Measured on both sides, three windows each, against the band
[`method.md`](method.md) fixed before any capture.** The figures are in
[`pace.json`](pace.json) and the analyser that produced them is
[`u7pace.py`](u7pace.py), committed with the record.

### The scene and the quantity

A new game opens in Trinsic. Between the world first appearing and Iolo's
opening conversation starting, no input is sent, and one townsperson paces a
fixed beat on the right of the view. The quantity is **that character's walk
cycle**: its beat is closed, so the autocorrelation of its position has a
sharp first peak at the time it takes to walk the beat once, with harmonics at
twice and three times that lag. It needs no pixel scale, so the two sides'
different captures — 86Box's window at 15 fps through `x11grab`, the kit's
HDMI at 60 fps through the capture card — cannot enter it.

**The front end needs no mouse.** That is what made this measurable at all,
and the earlier record's assumption to the contrary is what had stopped it:
`Esc` leaves the introduction, `Down` and `Enter` work the title menu and the
character screen, `Enter` pages the conversation, and **`Tab` selects a
conversation keyword and `Enter` picks it**. The same command block drives
both machines.

### The figures

| | reference, 86Box 386DX-25 | kit, preset 3 |
| --- | --- | --- |
| walk cycle | **11.733, 11.800, 11.867 s** | **8.733, 8.733, 8.733 s** |
| mean | 11.800 s | 8.733 s |
| spread between windows | 1.14% | 0.00% |
| world to conversation | 46.67, 46.67, 46.60 s | 34.33, 34.35, 34.35 s |
| TopBench, same runs | 40 | 68 |

**The kit's cycle is 26.0% shorter than the reference's, against a ±10% band.
That is a fail**, and the spread on each side is far inside the band — 1.1%
and 0.0% — so the method decides it rather than leaving it open.

A second quantity, measured the same way and never used for the verdict,
agrees: the time from the world appearing to the conversation starting is
34.34 s on the kit against 46.65 s on the reference, a ratio of **0.736**
where the cycle's is **0.740**. Two independent timings of the same scene give
the same answer to half a percent.

**The displacement figure `method.md` first committed is not usable**, as its
second amendment said before any kit capture: across the three reference
windows it spreads 32% (0.043, 0.050, 0.059 heights/s) while the kit's three
agree to 0.04%. It is in `pace.json` and nothing rests on it.

### Reading the miss

`method.md` fixed the classification before any figure was read: compare the
pace ratio with *T*, the TopBench ratio. **That rule cannot be applied, and
the reason is a finding about the reference, not about the kit.** Its premise
is that the game's pace and TopBench move together. On the reference they do
not:

| 86Box 386DX, same board | TopBench | walk cycle |
| --- | --- | --- |
| 25.0 MHz | 40 | 11.800 s |
| 33.3 MHz | 46 | 9.467 s |
| 40.0 MHz | 48 | ≤ 8.467 s (the window failed the 100% gate, so the true cycle is shorter) |

A 60% rise in clock moves TopBench by 20% and the game's pace by 39%. So the
two are not proportional even within one machine, and the ratio of two
machines' TopBench scores cannot be used to say what their pace ratio ought to
be. The rule is reported as not applicable rather than applied anyway.

**What the sweep does give is a sharper statement than the rule would have.**
The kit's 8.733 s sits between the reference at 33.3 MHz (9.467 s) and at
40 MHz (≤8.467 s):

> **At the 386DX-25 preset the kit paces The Black Gate's world like an 86Box
> 386DX at about 37 MHz — roughly 1.35× the pace of the 386DX-25 the preset
> names.**

That is the same shape as [#1840](https://github.com/DeanoC/fes/issues/1840)
at 286-12 and as Part 2's benchmark reading, now measured in a game rather
than in a benchmark, and it is a fault of the preset's pacing rather than of
anything specific to this title. Two things keep it from being a complete
account, and both are stated rather than glossed:

- **The reference is at the slow end of its class.** Part 2 measured it at
  TopBench 40 where AT-class real 386DXs scale to 31–78 with a median of 57.
  A mid-range reference would narrow the gap; how far is not measured, and
  86Box cannot be made to score 57 at any clock — 48 at 40 MHz is the most it
  gives.
- **The kit's TopBench of 68 is above every 86Box 386DX at any clock.** So the
  kit is not simply "a faster 386DX" on both axes by the same factor: it
  benchmarks 70% above the reference and paces this game 35% above it.

### The kit runs

Two claims of Kit B, both core-only on the baseline, both claimed and released
through `~/fes-scratch/u6ref/kit/run-u6ref.sh` with the status file as the
record, both with a final `quit` in the command file and no hand-run tail —
the two things that wedged the kit on 14 November.

- `u7pace` (23:20–23:40): window `p1`, plus the kit's `UMAP.COM` map and
  `MEM /C`. **Its second and third windows are not at preset 3 and are not
  used**: the governor `settings` call names a game, and the command file set
  it for the first game only, so the later ones ran with no preset. TopBench
  in that run read **303**, which is what gave it away. The lesson is in the
  record because it would be easy to repeat: a preset is per game, not per
  claim.
- `u7pace2` (23:48–00:07): windows `q1`, `q2`, `q3`, each with its own
  `settings` before its `load`, and TopBench at preset 3 reading **68** —
  [#1486](https://github.com/DeanoC/fes/pull/1486)'s figure, re-measured on
  this seal in the same claim as the pace windows, as `method.md` required.

Both runs verified from `result.json` before any figure was trusted: no
`error` on any command, every `load` reporting `state: active`, the staged
disk digest matching, and the wrapper's own before/after listings agreeing.

### The seal and the disk

The package is `fc01c956…`, a Quartus 17.0.2 seal of main `1b4601831` — the
same seal [#1838](https://github.com/DeanoC/fes/pull/1838)'s kit runs used, so
the two records' kit figures are of the same build. It is **not** exact-head
for main: `sources/misteross/cores/fes-common/rtl/fes_hps_ddr_guard.v` has
changed since, so the figures above are for revision `1b4601831`.

Both sides run the kit's own `u7bg-master.img` (SHA-256 `d9bfafd4…`) with
`UMAP.COM` copied in, a fresh copy per run. The copies differ in exactly two
bytes — the FAT directory entry's timestamp for `UMAP.COM`, in both copies of
the table — and in nothing else; the reference's windows ran digests
`cff7e4b0…` (m1, m2) and `41c7b9d8…` (m6 and the clock sweep), the kit's
`41c7b9d8…`.

### The memory map, side by side

`UMAP.COM` on both machines, which is the Part 1 claim checked rather than
asserted:

```
reference           kit
A000 ...........     A000 ................
B000 ....WWWWWWWW    B000 ........WWWWWWWW
C000 Rrrrr...WWWW…   C000 RrrrrrrrWWWWWWWW
D000 WWWWWWWWWWWW…   D000 WWWWWWWWWWWWWWWW
E000 ...........     E000 ................
F000 rrrrrr-----r…   F000 rrrrrrrrrr--rr-r
```

C800–DFFF is 96 KiB of writable RAM on both, which is what the games image's
UMB provider asks for. They differ above C500, where the reference reads open
bus and the kit reads the body of its VGA BIOS, and in the BIOS at F000;
neither is in the game's path. Free conventional memory is 595,312 bytes on
the reference; the kit's `MEM /C` is in the run's `p1-mem.png`.

## What this record does not cover

- **Why the preset paces this game at about 37 MHz** rather than 25. The
  measurement says it does; the cause is not traced here, and whether it
  shares one with [#1840](https://github.com/DeanoC/fes/issues/1840) and
  [#1837](https://github.com/DeanoC/fes/issues/1837) is open.
- **A mid-range reference.** 86Box's 386DX scores 40 at 25 MHz and 48 at
  40 MHz, so it cannot be brought to the real median of 57 by clock alone. How
  much of the 26% a mid-range reference would absorb is not measured.
- **Five 86Box 386DX boards** (Part 2) are unmeasured, not measured and equal.
- **The reference's emulation speed depends on Powerboat's load.** Three of
  six 25 MHz windows failed the 100% gate and are excluded; their cycles
  (12.067, 11.867, 11.867 s) are in `pace.json` and agree with the three that
  passed, but they are not part of the verdict.
- **The displacement metric** `method.md` first committed, which its second
  amendment set aside before any kit capture.
- The reference's DOS layout is like the kit's, not identical: `reference.md`
  names every difference.
