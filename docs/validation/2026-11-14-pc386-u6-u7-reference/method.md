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
