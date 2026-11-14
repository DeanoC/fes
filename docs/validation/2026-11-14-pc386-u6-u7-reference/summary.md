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
[`method.md`](method.md). It was then amended four times and corrected twice.
The first two amendments came before the only kit run that measured anything,
`g1`; the rest came after it, as the reference's own repeats were understood,
and no kit capture followed them. Each says what had been seen when it was
written. **The lute block, the pitch analyser and the ±5.9% band are as first
committed**, so the pitch verdict does not rest on anything written after the
kit was measured. The reference machines are in
[`reference.md`](reference.md).

## Result

| Check | Result |
| --- | --- |
| **Ultima VI, PC speaker pitch at the 286-12 preset** | **Fail.** The kit's notes are 12.7–12.9% below the reference's, 2.35 and 2.39 semitones flat. The band is ±5.9%, one semitone. Scaling the reference from its 12.5 MHz to the preset's nominal 12.0 leaves 9.0–9.3% and about 1.7 semitones, if the game keeps the same delay unit across that step. |
| **Ultima VI, walking speed at the 286-12 preset** | **Not decided**, and the method is the problem, not the kit: on the reference the turn rate follows how fast the keys are sent (56% higher at seventeen keys a second than at ten), and the step rate spreads 23% at one key rate where the band is ±10%. So no kit window was run with it. The figures are below for whoever measures it next. |
| **Ultima VII: The Black Gate, NPC pace at 386DX-25** | **Not done.** The game stops at its own memory check on the reference, and the reference board for a 386DX-25 is not settled; see below. |

So **#1368's last box is not ticked and the issue stays open**, and pc386 does
not reach tier 1 on this work. The pitch failure has its own issue.

## The reference, and that it is credible

86Box 6.0 (build 9001), a `award286` SCAT board with an Intel 286 at 12.5 MHz,
no FPU, IBM VGA, AdLib and the PC speaker, the drive behind an XT-IDE
Universal BIOS. Full settings and the reasons in [`reference.md`](reference.md).

TopBench 0.38a, from the same benchmark drive the kit's presets were measured
on, scores it **23**. TopBench's own database — it ships with the benchmark —
holds 27 real 286 machines; the seven at 12 to 12.5 MHz score **16, 21, 22, 22,
23, 25 and 27**, the 16 being an accelerator card in an IBM PC/XT rather than
an AT-class board. The reference sits in the middle of them. The kit's 286-12
preset scores **33**, above every real 286 of that clock in the database and
level with its 16 MHz boards (33, 36, 42), which is the difference these checks
are about: the preset is a 386 core paced to a table, not a 286. CHKCPU refuses to run on the reference at all ("requires a 386 or
higher CPU") and calls the kit's 286-12 preset a "classic 386 at 12.8 MHz".

**Evidence class**, as `AGENTS.md` asks it be named: exact-artifact hardware
acceptance for the kit side — the sealed package `fc01c956…` on Kit B, with
the load read back from the run's own record — beside a host-only measurement
of the reference.

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
  the restore are in Kit B's status file. The wrapper's own shell was killed
  after it had claimed the kit (it was mistaken for a run still waiting for the
  lock), so the run finished on its own and the restore check — remove the
  run's data trees, snapshot, compare with the pre-run probe, release — was run
  by hand from the same script's tail and logged as such at 11:53.
- **Disk.** `u6nm.img` (`e28bc5a2…`): main's FreeDOS games image (`e69194ec…`,
  from `scripts/build_pc386_freedos_image.py --profile games`) plus Ultima VI
  from Deano's eXoDOS copy with its music off, put together by the 10 November
  run's own `make_games_disk.py`; it is the disk that run used. The reference's copy differs in four files, listed in
  `method.md`; `ULTIMA6\` is byte-identical.

## What the kit was actually running

The HIL driver reports `rc=0` even when a kit refuses a core at admission, so a
run can produce frames while nothing of the intended core ever loaded (seen on
Kit B the same morning, on another session's hand-packed `.fcore`: HTTP 422
`INVALID_ARCHIVE`, driver `rc=0`, plausible frames). Run `g1`'s
`result.json` was read back for this record rather than trusted:

- `package` is `fc01c95686d0…`, the seal above;
- the Ultima VI load (`seq 7`) returned `state: active` in 11.38 s with
  `fes.pc386.machine 1.0` among its active interfaces, and no `error` anywhere
  in the file;
- the disk it staged hashes `e28bc5a24d41…`, which is `u6nm.img`'s own
  SHA-256, and the kit replied with the same digest;
- the settings written before the load read back as preset 1, frequency
  override 0, turbo 0.

**Nothing in this record is judged from black or missing frames.** Kit B's
capture card freezes its last frame when the signal drops
([#1823](https://github.com/DeanoC/fes/issues/1823), an open intermittent
black-HDMI defect after a kit-software activation), so it cannot be used to
count them; the pitch figures are audio, and the walk figures count motion in a
picture that is running throughout. One of `g1`'s 32 grabs is fully black,
`u6-u00` at 11:40:10, five seconds after `ultima6` was typed; the 10 November
run's grab at the same point is black and the same size, so it is the game
starting up rather than anything of #1823's. It is not one of the captures
anything is measured from, and none of those went black.

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
on both notes against the reference as measured, at 12.5 MHz. It is still
outside it against the reference scaled to the preset's nominal 12.0 MHz,
which holds while the game's delay unit does not change across that step (see
below). The
measurement is far finer than the band: the kit's notes spread 0.05% and the
reference's 1%, and the kit's figures are the same as the 10 November run's
(840.7 and 499.8 Hz) on a different seal, so this is the machine and not the
day.

**This is two captures a side, not the three the method asked for**, and
`pitch.json` names all four: one kit capture on this package (`g1`) with the 10
November capture on package `650bc477` behind it, and two reference captures
(`m3`, `m4`). A third reference run was made and its lute landed in character
creation, so it holds no notes. The method's reason for three was to show the
spread is well inside the band, and it is — 0.05% on the kit and 0.1% between
the two reference runs, against ±5.9%, with the difference being measured 100
times larger than either — but the shortfall is a deviation and is named here
rather than left to be noticed. Three kit captures were queued for Kit B and
not run, because the kit was held by another session.

**What is not established is the cause.** There is already a model for this
loop: [#1472's record](../2026-11-04-pc386-speaker-kit/summary.md) puts one
pass of the routine's loop at about 89 clocks plus about 475 for each of the
game's own delay units, so #1453's ISA charges — 14 clocks on the pass's port
61h `IN` and `OUT` — drop the pitch 2.42% at one delay unit, which is what was
measured then (1.99, 2.34 and 2.59% over three keys). That accounts for **at
most** two and a half points of the twelve and a half here, and only on the
kit: what the reference pays for the same two port accesses was not measured.
So most of the difference is elsewhere: the governor's pacing of the rest of the pass,
and the pass costing a 386 a different number of clocks than a 286 whatever the
table says. Nothing here separates them, and no lute capture at another preset
exists to split them with (#1481 measured the lute at 286-12 only). The issue filed from
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

All figures are the method's own: the rate from the median gap between steps or
turns. `walk.json` gives `(count − 1) / span` beside each for comparison.

| | run 1 | run 2 | run 3 | range |
| --- | --- | --- | --- | --- |
| steps a second | 3.00 | 3.00 | 2.31 | 2.31–3.00 |
| turns a second | 1.67 | 1.11 | 1.03 | 1.03–1.67 |

Two things are wrong with this, and both have to be fixed before anything can
be judged against a ±10% band:

- **The pass figure follows the key rate.** At ten keys a second the two
  windows agree to 7%, 1.11 and 1.03 turns a second; at about seventeen the
  same machine gives 1.67, 56% higher. Ultima VI takes a turn only when it next
  looks at the keyboard and drops what is queued meanwhile, so what comes out
  is partly the rate the keys arrive at.
- **The walk's own spread is still twice the band.** All three windows used the
  same key loop: two give exactly 3.00 steps a second and the third 2.31, a
  23% spread where the band is ±10%, and nothing here says what makes the
  difference.

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
- **Walking speed**, for want of a measurement that is of the game rather than
  of the key timing, and with one window unexplained at the same key rate
  (above).
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
