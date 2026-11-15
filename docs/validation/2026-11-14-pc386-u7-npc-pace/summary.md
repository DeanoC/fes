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

## Status

Each outcome is stated here once. The parts below give the evidence and do not
restate the verdicts; nothing outside this table, and no heading, carries one.

| | outcome |
| --- | --- |
| The Black Gate starts on a 386DX-25 reference | **done** |
| A 386DX-25 board fixed by a rule committed before capture | **done**; the board that was fixed **does not meet** the rule, which is reported rather than worked around |
| How the kit's 386 preset scores against that rule's range | **stated**: TopBench 68, above fifteen of the eighteen real entries, inside the range |
| NPC pace, the quantity `method.md` committed | **not decided** — its spread across one machine's own repeats is not reproducible (32.1% on one gate-clean triple, 2.7% on another), which the method's own rule leaves undecided |
| NPC pace, the character's walk-cycle period | **measured**: kit 8.733 s against reference 11.800 s, a **26.0% miss** against the same ±10% band, three windows a side, 0.0% spread on both. A quantity the method did not commit, fixed before any kit capture |

## Part 1 — the game starts on a 386DX-25 reference

The Black Gate now boots, loads and reaches its title screen on the
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


## Part 3 — the pace

The Status table above gives the two outcomes; this part is the evidence for
them, and the two together are the whole answer — neither replaces the other.
The figures are in
[`pace.json`](pace.json) and the analyser that produced them is
[`u7pace.py`](u7pace.py), committed with the record.

**The check as committed is not decided**, and the reason is sharper than
"the spread was wide": **the spread is not reproducible**. Two sets of
reference windows, each with every gate met, give

| reference windows | displacement, heights/s | spread | kit/reference |
| --- | --- | --- | --- |
| m1, m2, m6 | 0.04306, 0.05010, 0.05941 | **32.1%** | 1.096 |
| r15a, r15b, r15d | 0.04716, 0.04712, 0.04592 | **2.7%** | 1.220 |

so whether the figure passes its own precommitted spread rule depends on which
three gate-clean windows you take, and the ratio it reports moves with them.
The walk cycle over the same two sets is 11.733/11.800/11.867 and
11.800/11.800/11.800. No cause was found: every window captured its full 1,800
frames, so it is not dropped frames, and the character band the detector picks
is the same in all of them. A figure like that cannot carry a band.
`method.md`'s correction to its second amendment retracts the substitution
that had let the verdict move off it.

**The measurement that can be made is the character's walk cycle**, fixed
before any kit capture — so it cannot have been chosen to make the kit pass or
fail — but adopted after the committed quantity failed its own spread rule,
which is the pattern this project retracts rather than endorses. Its figures
follow, with the band applied for what that is worth.

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

The verdict windows are **r15a, r15b, r15d** against **q1, q2, q3**, with
every gate met: no input, 86Box reading 100% for every frame against the
committed hash of the readout pixels ([`speed-100.png`](speed-100.png) holds
that rendering; `pace.json`'s `invocations` block says exactly what is
hashed), both sides derived at
**15 frames a second**, and both sides' drive copies digested before their
runs at the same **`41c7b9d8…`**.

| | reference, 86Box 386DX-25 | kit, preset 3 |
| --- | --- | --- |
| walk cycle | **11.800, 11.800, 11.800 s** | **8.733, 8.733, 8.733 s** |
| spread between windows | 0.00% | 0.00% |
| world to conversation | 46.67, 46.67, 46.67 s | 34.27, 34.33, 34.27 s |
| TopBench | 40 | 68 |

**The kit's cycle is 26.0% shorter than the reference's**, with no spread on
either side and a quantisation of one frame — 0.57% of the figure. Against a
±10% band that is a **miss by a wide margin, with no overlap between the two
sides' windows**. It is not `method.md`'s committed check decided; it is a
quantity the method did not commit, measured well. Whoever wants the check
decided should commit this quantity in advance and measure again — everything
needed to do that is in this record.

**Both sides are at one rate, and getting there took the long way.** The gate
asks for the reference to be captured at the kit's 60 fps. Three runs at 60
(`r60a`, `r60b`, `r60c`) **all failed the 100% gate**: `x11grab` at 60 fps on
a 667×496 window costs 86Box its real-time margin on this host, so gates 2 and
3 cannot both be met by capturing the reference faster. The gate's purpose —
one rate on both sides — is met the other way instead, by **deriving the kit's
captures at 15 fps**, keeping every fourth frame; `u7pace.py --decimate`. That
is derivation, not capture, and it is reported as such.

The rate turns out not to matter, which is measured rather than argued:

| | at 60 fps | at 15 fps |
| --- | --- | --- |
| reference | 11.850, 11.833, 11.717 s (gate failed) | 11.800, 11.800, 11.800 s |
| kit, the same three captures | 8.733, 8.733, 8.733 s | 8.733, 8.733, 8.733 s |

The kit's figures are the same captures read at the two rates and do not move
at all; the reference's two sets agree to within their quantisation.

A second quantity, measured the same way and never used for the verdict,
agrees: the time from the world appearing to the conversation starting is
34.29 s on the kit against 46.67 s on the reference, a ratio of **0.735**
where the cycle's is **0.740**. **It is not independent corroboration**, and
the arithmetic says why: the window is 3.96 cycles on the reference and 3.93
on the kit, so the conversation fires after a fixed number of the character's
cycles and the two figures are one timeline read twice. What it does show is
that the detector found the same timeline on two different captures.

### What two failed rate figures say about measuring this game

This is the second attempt to put a per-second rate on an Ultima scene and the
second to fail, and the two failed the same way — on the spread **within one
condition on one machine**:

- [#1368](https://github.com/DeanoC/fes/issues/1368)'s Ultima VI walk: the
  rate followed how the harness keyed the game, and over-keying settled on a
  suppressed rate that looked like a plateau; within-rate spreads of 1.765 to
  2.308 steps a second.
- this record's Ultima VII displacement: a spread that is not reproducible,
  32.1% across one gate-clean triple and 2.7% across another.

**The conclusion #1368 drew from its failure does not carry over, and this
record is the evidence against it.** It concluded that the walk needs a count
the game itself keeps, read from memory on each side, rather than motion
counted in frames. For NPC pace that is not necessary: the walk cycle is read
from the same frames, by the same analyser, on the same captures, and repeats
to **0.0%** on both machines.

The difference is not frames against memory. It is **rate against period**. A
rate summed per frame carries the capture rate and the detector's sensitivity
into the number — which is why displacement disagrees with the cycle about how
much faster the kit is, and why its spread moves with which windows you pick.
A period taken from the autocorrelation of a position carries neither: it is
the time between two recurrences of the same picture, and a detector that
finds those recurrences at all gets the same answer however often it looks.

So the observable to commit in advance, for this title and probably for any
scene with a repeating actor, is **a period of something the game repeats**,
not a rate of something the harness counts. Reading a counter out of guest
memory would also work and would be stronger still, but this record shows it
is not required to get a figure that repeats.

### Reading the miss

`method.md` fixed the classification before any figure was read: compare the
pace ratio with *T*, the TopBench ratio. **Applied as written it returns
"specific to this title"** — *T* = 68/40 = 1.70 and the kit runs the scene
1.35× faster, well outside ±10% of *T*. **The rule is nonetheless set aside,
and the cause is reported as not classified**, because its premise fails on
the reference itself: it assumes the game's pace and TopBench move together,
and on the reference they do not.

| 86Box 386DX, same board | TopBench | walk cycle |
| --- | --- | --- |
| 25.0 MHz | 40 | 11.800 s |
| 33.3 MHz | 46 | 9.467 s |
| 40.0 MHz | 48 | 8.467 s (its window read 101% for one second in 501 frames, so the true cycle is at or just above this; it fails the gate as written) |

A 60% rise in clock moves TopBench by 20% and the game's pace by 39%. So the
two are not proportional even within one machine, and the ratio of two
machines' TopBench scores cannot be used to say what their pace ratio ought to
be. The rule is reported as not applicable rather than applied anyway, and
`method.md`'s third amendment records that this was decided after the kit
figures were read, together with what the rule would have returned.

The three sweep points also fit **cycle ≈ 2.9 s + 222/MHz**, so about 2.9 s of
the 25 MHz cycle — a quarter of it — does not move with the clock at all.
Whatever that part is, it is not the CPU, and it is not attributed here: the
reference's `hdd_01_speed` was never varied, so disk timing is among the
candidates. It is one more reason the TopBench ratio is the wrong instrument.

**What the sweep does give is a sharper statement than the rule would have.**
The kit's 8.733 s sits between the reference at 33.3 MHz (9.467 s) and at
40 MHz (≤8.467 s):

> **At the 386DX-25 preset the kit paces The Black Gate's world like
> 86Box's 386DX at about 38 MHz — between its 33.3 and 40 MHz points, and
> about 1.35× the pace of 86Box's 386DX-25.**

Read it as about 86Box's 386 model, not about a real 386DX-25: Part 2 measured
that model at TopBench 40 where the real range scaled to 25 MHz is 31–78.

It is the same *shape* as [#1840](https://github.com/DeanoC/fes/issues/1840)
at 286-12 and as Part 2's benchmark reading — the kit running ahead of the
machine its preset names — now seen in a game rather than in a benchmark.
**Whether the cause is the preset's pacing or something particular to this
title is not settled by one title**, and the rule that was meant to settle it
is the one set aside above. Two further things keep this from being a complete
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

Both runs verified from `result.json` and `acks.jsonl` before any figure was
trusted, item by item: **no `error` on any command**; **every `load` reporting
`state: active`**; the **stage events naming `u7bg-umap.img` with SHA-256
`41c7b9d8…` and the kit's `reply_sha256` matching it** in both runs; the
governor's own reply showing `"preset": 0` before and **`"preset": 3` after**
for each of `q1-u7bg`, `q2-u7bg`, `q3-u7bg` and `qb-bench`; and the wrapper's
before/after/clean listings agreeing with nothing left and nothing gone.

### The seal and the disk

The package is `fc01c956…`, a Quartus 17.0.2 seal of main `1b4601831` — the
same seal [#1838](https://github.com/DeanoC/fes/pull/1838)'s kit runs used, so
the two records' kit figures are of the same build. It is **not** exact-head
for main: `sources/misteross/cores/fes-common/rtl/fes_hps_ddr_guard.v` has
changed since, so the figures above are for revision `1b4601831`.

Both sides run the kit's own `u7bg-master.img` (SHA-256 `d9bfafd4…`) with
`UMAP.COM` copied in, a fresh copy per run, each digested before it ran. **In
the verdict windows the two sides' copies are byte-identical, `41c7b9d8…`
both.** Earlier reference runs used copies made with a separate `mcopy`, which
differ from it in exactly two bytes — the FAT directory entry's timestamp for
`UMAP.COM`, in both copies of the table, checked with `cmp -l` — and one of
them, `m1`, was never digested at all before it ran. That is why those runs
are prior runs here and not the verdict set: `r15a`, `r15b` and `r15d` were
each copied from the digested `u7bg-umap.img` and digested again before
running.

### The memory map, side by side

`UMAP.COM` on both machines, in full, which is the Part 1 claim checked rather
than asserted. The reference, from `runs/r1/fr/r1-umap.png`:

```
UMAP A000-FFFF 4K R=rom W=ram r=ro .=ff -=00 ==same
A000 ................
B000 ........WWWWWWWW
C000 Rrrrr...WWWWWWWW
D000 WWWWWWWWWWWWWWWW
E000 ................
F000 rrrrrr-----rrrrr
```

and the kit, from `kit/runs/u7pace/fr/p1-umap.png`:

```
UMAP A000-FFFF 4K blocks R=rom W=ram r=ro .=ff -=00
A000 ................
B000 ........WWWWWWWW
C000 RrrrrrrrWWWWWWWW
D000 WWWWWWWWWWWWWWWW
E000 ................
F000 rrrrrrrrrr--rr-r
```

The reference map above is from a re-run with the corrected probe. The first
build of `UMAP.COM` printed `-` for any block that was uniform and not `FF`,
although its legend said `-` meant `00`; it now tests zero explicitly and
prints `=` for a block uniform at some other value. Re-run, **the reference's
map is unchanged**, so its five `-` blocks at F000 really are zero-filled. The
kit's map was taken with the first build and has not been re-run, so its three
`-` blocks at F000 are "uniform, value not distinguished". They are in the
BIOS, nothing in this record depends on them, and the `W` blocks that matter
come from the write test, which the fault never touched.

**C800–DFFF is 96 KiB of writable RAM on both**, which is what the games
image's UMB provider asks for, and both print `UMBM: Upper Memory Blocks:
C800-DFFF` with no refusal. They differ at C500–C7FF, where the reference
reads open bus and the kit reads the body of its VGA BIOS, and in the BIOS at
F000; neither is in the game's path.

`MEM /C` agrees as closely:

| | reference | kit |
| --- | --- | --- |
| free conventional | 595,312 bytes | **594,272 bytes** |
| largest program | 595,296 | 594,256 |
| upper memory, total / free | 96K / 84K | 96K / 84K |
| loaded high | HIMEMX, COMMAND, CTMOUSE | HIMEMX, COMMAND, CTMOUSE |
| extended (XMS) free | 7,009K | 15,201K |

The two differ by **1,040 bytes of conventional memory, 0.17%**, and carry the
same drivers in the same place. The kit has twice the extended memory, which
the game does not use: it refuses to start at all when an XMS manager takes
the HMA, which is why both run the image's menu entry 1.

## What this record does not cover

- **Why the preset paces this game at about 37 MHz** rather than 25. The
  measurement says it does; the cause is not traced here, and whether it
  shares one with [#1840](https://github.com/DeanoC/fes/issues/1840) and
  [#1837](https://github.com/DeanoC/fes/issues/1837) is open.
- **A mid-range reference.** 86Box's 386DX scores 40 at 25 MHz and 48 at
  40 MHz, so it cannot be brought to the real median of 57 by clock alone. How
  much of the 26% a mid-range reference would absorb is not measured.
- **Five 86Box 386DX boards** (Part 2) are unmeasured, not measured and equal.
- **The reference's emulation speed depends on Powerboat's load.** Of eleven
  25 MHz reference windows run, five failed the 100% gate and are excluded
  (`m3`, `m4`, `m5`, `r15c`, and all three 60 fps attempts); their cycles are
  in `pace.json` and agree with the ones that passed, but they are not part of
  the verdict. The gate now checks the readout's value, not only that it held
  still: every frame of a counted window matches the SHA-256 of
  the readout pixels (above). The excluded 15 fps runs ranged 85–104%,
  83–101% and 91–104%.
- **The part of the reference's cycle that does not scale with clock.** The
  sweep fits cycle ≈ 2.9 s + 222/MHz, so roughly a quarter of the 25 MHz
  cycle is clock-independent and unattributed. The reference's
  `hdd_01_speed` (`1997_5400rpm`) was never varied, so disk timing is among
  the candidates and was not ruled out. One 25 MHz run at another disk speed
  would test it.
- **Whether the miss is the preset or the title.** One title cannot separate
  them, and the rule that was meant to is set aside (above).
- **The reference's TopBench figures (40, 46, 48) are from separate benchmark
  machines**, not from the pace runs: the same board and clock, but with the
  XT-IDE controller and the benchmark drive, because the pace reference's
  CMOS drive type is cut for the games image's geometry. The kit's 68 *is*
  from the same claim as its pace windows.
- **How the one capture rate is reached.** The reference cannot be captured
  at the kit's 60 fps on this host without losing the 100% gate (three runs
  tried, all failed), so the kit's captures are *derived* at 15 fps instead.
  The figure does not move with the rate, which is measured above, but the
  gate as written asks for capture and this is derivation.
- **Why the displacement spread is not reproducible.** 32.1% across one
  gate-clean triple and 2.7% across another, with full frame counts and the
  same detector band in all of them. No cause was found.
- **The displacement metric** `method.md` first committed, which its second
  amendment set aside before any kit capture.
- The reference's DOS layout is like the kit's, not identical: `reference.md`
  names every difference.
