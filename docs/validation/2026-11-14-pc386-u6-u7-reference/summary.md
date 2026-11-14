# Ultima VI and Ultima VII against a reference, 14 November 2026

Issue [#1368](https://github.com/DeanoC/fes/issues/1368)'s last item, and the
two lines `docs/core-status.md` still lists as open for the games:

- Ultima VI's PC speaker pitch and walking speed under the 286-class preset;
- Ultima VII: The Black Gate's NPC pace under the 386-class preset.

`sources/misteross/docs/pc386-machine-design.md`'s acceptance table asks for
each of these "against the reference", and says a reference is "recordings
from period hardware or a configured emulator, named in the acceptance
record". None had been named, which is why
[the 10 November rerun](../2026-11-10-pc386-game-acceptance-rerun/summary.md)
left them open. This record names one, checks it against real hardware, and
measures what it can with it.

The method and the pass rule were fixed before any kit capture, in
[`method.md`](method.md), and amended four times, each time before the next kit
capture and each time saying what had been seen. The reference machines are in
[`reference.md`](reference.md).

## Result

| Check | Result |
| --- | --- |
| **Ultima VI, PC speaker pitch at the 286-12 preset** | **Fail.** The kit's notes are 12.7–12.9% below the reference's, 2.35 and 2.39 semitones flat; 9.0–9.3% and about 1.7 semitones even after the reference is scaled from 12.5 to 12.0 MHz. The band is ±5.9%, one semitone. |
| **Ultima VI, walking speed at the 286-12 preset** | **Not decided**, and the method is the problem, not the kit: on the reference the figure follows how fast the keys are sent, so it is not the game's rate. No kit window was run with it. The figures are below for whoever measures it next. |
| **Ultima VII: The Black Gate, NPC pace at 386DX-25** | **Not done.** The game stops at its own memory check on the reference, and the reference board for a 386DX-25 is not settled; see below. |

So **#1368's last box is not ticked and the issue stays open**, and pc386 does
not reach tier 1 on this work. The pitch failure has its own issue.

## The reference, and that it is credible

86Box 6.0 (build 9001), a `award286` SCAT board with an Intel 286 at 12.5 MHz,
no FPU, IBM VGA, AdLib and the PC speaker, the drive behind an XT-IDE
Universal BIOS. Full settings and the reasons in [`reference.md`](reference.md).

TopBench 0.38a, from the same benchmark drive the kit's presets were measured
on, scores it **23**. Real 286 boards at 12 and 12.5 MHz in TopBench's own
database — it ships with the benchmark — score **21, 22, 22, 23, 25 and 27**.
The reference sits in the middle of them. The kit's 286-12 preset scores
**33**, above every real 286 of that clock in the database, which is the
difference these checks are about: the preset is a 386 core paced to a table,
not a 286. CHKCPU refuses to run on the reference at all ("requires a 386 or
higher CPU") and calls the kit's 286-12 preset a "classic 386 at 12.8 MHz".

## Package and kit

- **Package `fc01c956…`**, a Quartus 17.0.2 seal (seed 1, CUDA toolchain slot)
  of **main `1b4601831`**, built on `chip-30x0-ai-dev` in a clean detached
  worktree. `verify_pc386_quartus_seal.py` passes it against that revision
  (`SEAL_OK`, 119 bound inputs). Timing passes: worst setup +0.468 ns, hold
  +0.090 ns; system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz. 36,189 of
  41,910 ALMs. Its lockstep report is the one this tree's lockstep produced
  (`--netlist`, default suite, complete corpus), run on the sealing host.
- **Kit B**, core-only on its baseline (image `179affbb…`, software
  `7ddb0bfb…`). Nothing installed, no reboot, no host change. The claim and
  the restore are in Kit B's status file.
- **Disk.** `u6nm.img` (`e28bc5a2…`): main's FreeDOS games image plus Ultima VI
  from Deano's eXoDOS copy with its music off, the same disk the 10 November
  rerun used. The reference's copy differs in four files, listed in
  `method.md`; `ULTIMA6\` is byte-identical.

## Ultima VI: PC speaker pitch

Ultima VI's lute is played by fixed key presses — the note for key `6` thirty
times, then key `1` — and its effects come from the game toggling port 61h in
CPU-timed loops, so the note that comes out is a measurement of the machine.
Both sides play the same notes from the same point in the game. The pitch of
each note is the autocorrelation peak of the middle of it; the figures are in
[`pitch.json`](pitch.json). The lute block, this analyser and the ±5.9% band
are as first committed, and have not changed since; the kit's capture was taken
in run `g1`, before `method.md`'s third amendment, which changed only the walk
and the reference's memory.

| | kit, 286-12 | reference, 286 at 12.5 MHz | reference scaled to 12.0 MHz |
| --- | --- | --- | --- |
| key `6` | **840.72 Hz** (31 notes, 840.42–840.90) | **962.76 Hz** (two runs: 962.29, 963.23) | 924.25 Hz |
| key `1` | **499.79 Hz** (31 notes, 499.65–499.87) | **573.91 Hz** (two runs: 574.28, 573.54) | 550.95 Hz |
| kit below the reference | | **12.68% and 12.91%** (2.35 and 2.39 semitones) | **9.04% and 9.29%** (1.64 and 1.69 semitones) |

**Verdict: fail.** The band is ±5.9%, one semitone, and the kit is outside it
on both notes however the reference's 4.2% clock offset is treated. The
measurement is far finer than the band: the kit's notes spread 0.05% and the
reference's 1%, and the kit's figures are the same as the 10 November run's
(840.7 and 499.8 Hz) on a different seal, so this is the machine and not the
day.

**What is not established is the cause.** The pc386 design document says
Ultima VI's effects "toggle it in CPU-timed loops" and names the 720 ns `isa8`
charge on each `OUT` as "one reason for the 286-class preset", so the I/O
charge is a candidate; the governor's instruction pacing is another, and
Ultima VI's delay loop costs a 386 a different number of clocks than a 286
whatever the table says. Nothing here separates them. The issue filed from
this record asks for that.

## Ultima VI: walking speed

What was measured **on the reference**, from the same place in a new game,
after passing turns until the opening fight in Lord British's throne room is
over. The matching kit windows were not run: the measurement did not survive
its own checks, so no kit time was spent on it.

- **steps a second**, counted as map-view scrolls while the Avatar is walked
  around a one-tile square (down, right, up, left) by taps at ten a second;
- **turns a second**, counted as text-panel lines while the turn is passed
  with the space bar, which cannot be blocked by anything in the way.

The reference's own three repeats of each (`walk.json`), with the emulation
holding 100% through all but one window. The three walk windows all used the
same loop, at a nominal ten keys a second; of the pass windows, the first sent
its keys at about seventeen a second and the other two at ten:

| | run 1 | run 2 | run 3 | range |
| --- | --- | --- | --- | --- |
| steps a second | 2.66 | 1.96 | 1.89 | 1.89–2.66 |
| turns a second | 1.55 | 1.14 | 1.11 | 1.11–1.55 |

Two things are wrong with this, and both have to be fixed before anything can
be judged against a ±10% band:

- **The pass figure follows the key rate.** At ten keys a second the two
  windows agree, 1.14 and 1.11 turns a second; at about seventeen the same
  machine gives 1.55. Ultima VI takes a turn only when it next looks at the
  keyboard and drops what is queued meanwhile, so what comes out is partly the
  rate the keys arrive at.
- **The walk has an outlier at the same key rate.** All three windows used the
  same loop, and gave 2.66, 1.96 and 1.89 steps a second. The last two agree to
  4%; the first does not, and nothing here says why.

A sound measurement would have to drive the keys from the guest's own timer, or
find a figure inside the game that counts turns.

That is `method.md`'s fourth amendment, written before the kit run that would
have been compared with these numbers, and corrected there as it is here.

For the record, the kit's own figures on this package, from run `g1`, which
walked with a held key during the opening fight rather than after it: 11 steps
in 45 s with a median gap of 1.57 s, which is the same as the 10 November
run's 1.47 s. Those are not comparable with the reference's numbers above —
different input, and a fight in progress — and they are not treated as a
verdict. The kit's preset was read back from the machine record as preset 1,
frequency override 0, turbo 0, before the game was loaded.

**This check therefore stays open**, with a method that does not work and the
figures that show why.

## Ultima VII: The Black Gate, NPC pace

Not done. The Black Gate prints "Ultima VII requires 561144 bytes of DOS
memory!" on the reference and exits. The free figure itself was not read there,
but the cause is plain from the boot: the games image's UMB provider reports
"no RAM found at C800. Aborted!", because a real 386 board has nothing behind
C800–DFFF where the kit's machine has RAM, so HIMEMX and the mouse driver load
low instead of high. An attempt to move the XT-IDE BIOS to C400h was made by
writing a `[PC/AT XTIDE]` section into the configuration; the POST banner was
not read back, so it is not known whether it moved, and it would not have
helped in any case, because there is still no RAM to make upper memory out of.
The next thing to try is `DOS=HIGH` with MS-DOS 6.22's `HIMEM.SYS`, as Ultima
VI's reference does and as the kit's own menu entry 2 offers; that was not
reached today.

The reference machine for it was also not settled. The board used for the
benchmark above, a 1988 C&T 386/AT with no cache, scores 40 against the kit
preset's 68, where TopBench's database has real 20–25 MHz 386 machines from 25
to 60. A verdict on NPC pace should not rest on a board chosen after the fact,
so the board has to be fixed first, by the same rule that validated the 286
one.

## What this does not cover

- **The cause of the pitch failure** (above).
- **Walking speed**, for want of a measurement that repeats (above).
- **NPC pace** (above).
- The reference runs Ultima VI with a different DOS kernel build and 548 KiB
  free against the kit's 573 KiB (`method.md`). Neither is in the game's
  timing path, but neither was varied to prove it.
- One reference window in six dipped below 100% emulation speed for 60 frames
  of 1,350; it is the one that gives the highest pass rate (1.55 a second), and
  it is reported as measured.
- **TopBench validates the reference's CPU, not its I/O timing**, and the
  note's frequency is set by how long each `OUT` to port 61h takes as much as
  by the CPU. The kit is flat although its CPU benchmarks faster, so the
  difference is not a CPU-speed difference, and the reference's standing
  against real 286 boards does not by itself settle what the right note is.
