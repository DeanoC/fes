# Method and tolerance, fixed before the kit runs

Issue [#1368](https://github.com/DeanoC/fes/issues/1368)'s last item, and the
two lines `docs/core-status.md` still lists as open for the games. This file
is committed before any kit capture, so the pass rule cannot move afterwards.

## What is being checked

`sources/misteross/docs/pc386-machine-design.md`'s acceptance table says, for
Ultima VI, "PC speaker effects play at the reference pitch under the 286-class
preset; walking speed matches the reference", and for The Black Gate "386-class
preset; NPC pace matches the reference". It also says: *"Reference" means
recordings from period hardware or a configured emulator, named in the
acceptance record.* No reference had been named, which is why
[the 10 November rerun](../2026-11-10-pc386-game-acceptance-rerun/summary.md)
left these open.

Three checks, all against the reference:

1. **Ultima VI walking speed** at preset 1 (286-12).
2. **Ultima VI PC speaker pitch** at preset 1. The 10 November rerun passed
   pitch kit-against-kit (70/70 notes within 0.33% of #1481); this adds the
   comparison against the reference that the contract asks for.
3. **Ultima VII: The Black Gate NPC pace** at preset 3 (386DX-25).

## The reference

**86Box 6.0 (build 9001)**, the Linux x86-64 AppImage of the v6.0 release, with
the 86Box ROM set. 86Box models period machines at board level — its own CPU
timings, ISA bus and VGA — rather than a cycle budget, so a run of it *is* a
run of a machine of the class the preset names. That is what the contract asks
for and what DOSBox-style emulation cannot give: the pc386 design document
already calls DOSBox's cycle figures for these titles "a starting point, not
the answer".

Neither 86Box nor its ROM set is committed; both stay in `~/fes-scratch/u6ref`
on Powerboat, with the game drives.

| | Ultima VI | The Black Gate |
| --- | --- | --- |
| Machine | `award286` (SCAT 286 board, Award 286 Modular BIOS B3.11.03) | see the record |
| CPU | Intel 286, 12.5 MHz, interpreter, no FPU | see the record |
| Memory | 1 MiB (640 KiB conventional) | see the record |
| Video | IBM VGA (`vga`) | IBM VGA (`vga`) |
| Sound | AdLib (`adlib`), PC speaker | see the record |
| Disk | XT-IDE Universal BIOS (AT) + the run's drive image | as Ultima VI |

**The 12.5 MHz point.** 86Box's 286 family offers 12.5 MHz, not 12.0: asking
for 12 MHz writes 12.5 back. The reference therefore runs 4.2% above the
preset's nominal clock, which makes the reference *faster*, so a kit that is
faster than the reference is faster still against a true 286-12. The band
below is wider than that offset.

**Same drive, same game.** Each side runs the same disk image, built by
`scripts/build_pc386_game_drive.py` from Deano's eXoDOS copies: `u6nm.img`
(`e28bc5a2…`, the FreeDOS games image plus Ultima VI with music off) and
`a-u7bg.img`. The reference copy differs in exactly three files, because
FreeDOS's 386 kernel refuses to load on a 286:

- `KERNEL.SYS`: FreeDOS kernel 2046's 8086 FAT16 build
  (`ke2046_86f16.zip/bin/kernel.sys`, SHA-256 `00962622…`) in place of the
  386 build the image ships. Same kernel, same version, different CPU build.
- `FDCONFIG.SYS` and `AUTOEXEC.BAT`: the kit's menu entry 1 (DOS in
  conventional memory) without HIMEMX, UMBM and the FES-specific `FESENV`,
  none of which a 286 or a machine outside FES can run.

Everything under `ULTIMA6\` and `ULTIMA7\` is byte-identical to the kit's.

**Same sequence.** Both sides replay the same command file: the kit through
`hil_games.py`, the reference through `refbox.py`, which takes the same
commands. The Ultima VI sequence is the one the 10 November rerun ran (run
`t1`): boot, `ultima6`, character creation with fixed keys, the lute, then the
held walk.

## What is measured

- **Walking speed.** One held direction key, 6 s on `down` then 6 s on
  `right`, captured at 30 fps. A frame counts as a map scroll when more than
  20% of the map view's pixels change by more than 40 grey levels; consecutive
  scroll frames are one step. This is the kit's own detector
  (`~/fes-scratch/games/walk_steps.py`), with the map view given per side in
  that side's pixels. The figure is **steps per second from the median gap
  between steps**, which ignores the first step (typematic delay differs
  between the two input paths) and any step the detector misses.
- **Speaker pitch.** The lute's notes, played by fixed key presses, from the
  captured audio. The fundamental of each loud note comes from the
  autocorrelation of its window, not the 25 Hz histogram bins
  `~/fes-scratch/games/tones.py` used, because the band below is finer than
  those bins.
- **NPC pace.** Defined in the record from the reference runs, before the kit
  run: a countable rate in a fixed scene reached by "Journey Onward" from the
  save the game ships, measured the same way on both sides.

## Tolerance

**Each rate must be within ±10% of the reference's, and each note's pitch
within ±5.9%.**

- The band has to be wider than the measurement's own spread. Three repeats
  run on each side and the record gives the spread; if it is not well inside
  the band, the check is not decided and says so.
- It has to be wider than the reference's 4.2% clock offset (above).
- It has to be narrower than the step between presets, or it would pass a
  machine running at the wrong one. The governor's table
  ([record](../2026-11-01-pc386-m5-governor/summary.md)) gives 27,463 ticks at
  286-12 against 39,946 at 386SX-16, a 45% step, and 65,387 at 386DX-25.
  ±10% cannot pass a neighbour.
- **5.9% for pitch is one semitone**, the smallest interval that is musically
  a different note. A pitch error below it is not a wrong note; above it is.
  The earlier pitch work resolved a 2.31% shift, so the method is finer than
  this band.

Nothing here is tuned afterwards: no frequency override is set in this work,
and the band is not widened to admit a result.

## Amendment, before the kit runs

Three things in the plan above did not survive contact with the two machines.
They are corrected here, before any kit capture, with the reasons.

1. **Not one shared command file: one shared *measured block*.** The reference
   is slower through Ultima VI's introduction than the kit is, so the kit's
   fixed sleeps land in different places there, and the kit's single `Esc`
   after the last story page quits the game on the reference. The path from a
   cold boot to the start of play is therefore per side; everything that is
   measured — the lute's notes and the walk — is the same command block on
   both, and both reach the same place in the game.
2. **A longer, four-legged walk, repeated three times.** The 10 November kit
   capture holds 9 steps in 14 s with gaps from 11 to 134 frames, because
   Ultima VI is turn-based and the start of a new game is a fight: each step
   waits for the other turns. Six seconds of one direction cannot be judged
   against a ±10% band. The measured block is now, three times over: five
   single steps 3 s apart (which checks the step detector counts five), then
   four held legs of 10 s — down, right, up, left — captured for 45 s. The
   figures are the step rate over each leg set and the median step gap.
3. **Ultima VII: what is captured, and what is derived from it.** The Black
   Gate is mouse-driven and its save is reached through its menu, so the
   measured block is three 30 s captures of the scene reached by "Journey
   Onward" with no input, then the same clicks on both sides to clear the
   conversation, then three more. The rate taken from them is the scene's
   update rate — the new pictures a second the game draws — because The Black
   Gate has no frame limiter, so that rate is the game loop that walks the
   NPCs. The record says so where it gives the figure.

**What had already been seen.** Before this amendment: the 10 November kit
capture's step figures (above), the kit's lute notes from the same run (key
`6` at 840.7 Hz, key `1` at 499.8 Hz), and that the reference plays Ultima
VI's introduction more slowly than the kit. No reference walk or lute figure
existed yet. The tolerance is unchanged.

## Second amendment, still before the kit runs

**The reference needs an XMS driver to run Ultima VI at all.** On the 286 the
games image leaves 505 KiB free, against 573 KiB on the kit, because FreeCOM
0.86 cannot swap itself out without XMS and the image's HIMEMX is 386-only.
Ultima VI's introduction then plays to its end and `GAME.EXE` drops straight
back to the prompt. The reference disk therefore also carries **MS-DOS 6.22's
`HIMEM.SYS`** (29,136 bytes, taken from the `wfw-msdos` drive in
`~/fes-drives/pc386`, never committed) with `DOS=HIGH`, which gives 591 KiB
free — 18 KiB more than the kit, not less. Memory does not pace the game; this
only lets it start.

So the reference disk differs from the kit's `u6nm.img` in five files:
`KERNEL.SYS` (kernel 2046's 8086 build in place of its 386 build),
`HIMEM.SYS` (added), `FDCONFIG.SYS`, `AUTOEXEC.BAT`, and nothing else.
`ULTIMA6\` is byte-identical.

**The reference is checked against real 286 machines.** TopBench's own
database, on the benchmark drive, holds real 286 boards at 12 and 12.5 MHz
scoring 21, 22, 22, 23, 25 and 27. The 86Box reference scores **23**, in the
middle of them, so `cpu_waitstates = 1` and the rest of the board's defaults
are period-typical. (The kit's 286-12 preset scores 33, which is above every
real 286 at that clock in the database; that is the thing being measured, not
a fault of the reference.) For the 386 the reference board, a 1988 C&T 386/AT
with no cache and shadow RAM off, scores **40** where the database's real
386DX/SX machines at 20–25 MHz score 25 to 60, so it sits inside the range but
on the slow side; the record says so where it gives The Black Gate's figure.

## Third amendment: the route, and the reference's memory as it finally stands

Written after the first kit run (`g1`, 11:38–11:50, which measured the lute and
a held walk) and before the kit run the walk verdict rests on. What had been
seen by then: `g1`'s lute notes (840.7 and 499.8 Hz, the same as 10 November),
the reference's lute notes from run `m3` (962 and 574 Hz), and that a held key
steps once on the reference and repeatedly on the kit. No reference walk rate
existed, because of exactly that.

1. **Taps, not a held key, on both sides.** 86Box's keyboard does not repeat a
   key held with the host's own auto-repeat off, so a 10 s held leg stepped
   once. The kit's keyboard does repeat, at the BIOS typematic rate. Both sides
   therefore tap the key about 6.7 times a second, which is far above the rate
   either machine can take a step at (under one a second), so what is measured
   is the game's step time and not the input path. `g1`'s held walk stays in
   the record as a cross-check.
2. **A one-tile square, not four long legs.** Ten seconds in one direction ends
   against a wall in Lord British's throne room, where a new game starts: in
   `m3` and `g1` whole legs stalled and some loops counted one step. The route
   is now down, right, up, left, repeated, which returns to the same tile, so
   nothing is ever walked into. The calibration is now five single steps that
   alternate down and up, 3 s apart, for the same reason.
3. **The reference's memory, as it finally stands.** MS-DOS 6.22's `HIMEM.SYS`
   **without** `DOS=HIGH`, which leaves **548 KiB** free against the kit's
   573 KiB. `DOS=HIGH` leaves 591 KiB and then `GAME.EXE`, which is EXEPACKed,
   fails with "Packed file is corrupt", the known EXEPACK fault when a packed
   program loads too low; and the image's own layout leaves 505 KiB, where
   `GAME.EXE` quits to the prompt without a word.
4. **Four files differ on the reference disk**, not five: `KERNEL.SYS`,
   `HIMEM.SYS` (added), `FDCONFIG.SYS` and `AUTOEXEC.BAT`. The image carries
   FreeDOS kernel **2043**'s 386 FAT16 build (46,256 bytes, SHA-256
   `932c0c15…`); the reference carries kernel **2046**'s 8086 FAT16 build
   (46,298 bytes, SHA-256 `00962622…`), because no 8086 build of 2043 was to
   hand and the DOS kernel is not in the game's timing path. `ULTIMA6\` is
   byte-identical on both.

The metrics and the band do not move: each rate within ±10% of the
reference's, each note within ±5.9%.

## Fourth amendment: the band cannot be applied to the walk

Written after the reference's own repeats, before the kit run that would be
compared with them, and it changes a rule after seeing data, so it says exactly
what was seen and why.

With the opening fight passed off and the same input on every repeat, the
reference's three walk windows gave 2.66, 1.96 and 1.89 steps a second, and
three windows of continuous passing gave 1.55, 1.14 and 1.11 turns a second.
The emulation held 100% through all but one of them. **The spread between
repeats of the same measurement on the same machine is about 40%, four times
the ±10% band.** Ultima VI is turn-based and the party and the castle's people
share the turn with the Avatar, so how long a turn takes depends on what else
is in view, and a step is only taken when the game next looks at the keyboard.

So the band stays as it is, and the walk is reported against it only if it can
be: **the kit's three windows and the reference's three are given in full, and
the comparison is called decided only where the two ranges do not overlap.**
Where they overlap, the record says the check is not decided by this method and
why, and the figures stand for whoever measures it next. Nothing is widened to
admit a result.

The pitch measurement is unaffected: its spread is 0.05% on the kit and 1% on
the reference, both far inside ±5.9%.

## Correction to the fourth amendment

The fourth amendment said the walk could not be judged because repeats on one
machine spread about 40%. On a closer look at those six windows that is not
what they show, and the correction matters because it changes what a later
measurement has to fix.

The first window of each pair sent its keys at about 17 a second and the other
two at 10. At the same key rate the repeats agree: 1.14 and 1.11 turns a
second, 1.96 and 1.89 steps. At the higher key rate the same machine gives
1.55 turns and 2.66 steps. **The figure follows the input rate, so it is not
the game's rate**, and no band can be applied to it. Ultima VI takes a step or
a turn only when it next looks at the keyboard and drops what is queued
meanwhile, so what is being measured is partly the key timing.

The conclusion stands — the walk is not decided and the kit windows were not
run — but the reason is this, not the spread.
