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
[`method.md`](method.md). It was then amended six times and corrected four
times. The first two amendments came before the only kit run that measured
anything, `g1`; the rest came after it, as the reference's own repeats were
understood and the rig itself was measured, and no kit capture followed them. Each says what had been seen when it was
written. **The lute block, the pitch analyser and the ±5.9% band are as first
committed**, so the pitch verdict does not rest on anything written after the
kit was measured. The reference machines are in
[`reference.md`](reference.md).

## Result

| Check | Result |
| --- | --- |
| <a id="pitch-result"></a>**Ultima VI, PC speaker pitch at the 286-12 preset** | **Fail**, on three kit captures and five reference ones. The kit's notes are 12.7–12.9% below the reference's, 2.35 and 2.38 semitones flat. The band is ±5.9%, one semitone. Scaling the reference from its 12.5 MHz to the preset's nominal 12.0 leaves 9.0–9.3% and about 1.7 semitones, if the game keeps the same delay unit across that step. |
| <a id="walk-result"></a>**Ultima VI, walking speed at the 286-12 preset** | **Open: not measurable by any method tried here**, and the methods are why, not the kit. Three were tried and all failed on one cause: the game steps when it next polls the keyboard and drops what is queued, so the rate follows how the keys arrive. Keying harder makes it walk *slower* — 2.94 steps a second at 10 keys a second, 2.06 and 2.07 at 12.3 and 25 — so the steady over-keyed figure is a suppressed rate, not a plateau. Two routes stay open: the game's own step count rather than motion in frames, the surer one, and a sweep of the unsampled key rates below 12.3 a second on both machines. |
| <a id="npc-result"></a>**Ultima VII: The Black Gate, NPC pace at 386DX-25** | **Not established**, and **not** for want of a method: no pace was ever measured, because the game does not start on the reference. It stops at its own memory check, which is a configuration problem with a named cause and routes left to try. Carried forward as [#1853](https://github.com/DeanoC/fes/issues/1853). |

The table is the status for each check; the sections below keep the evidence
and method limits behind it. #1368's last box is not ticked on this work, so
pc386 does not reach tier 1 here.

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

**Does the preset's own calibration make this verdict unsafe?** No, and the
record says so rather than leaving it to be wondered about. The 286-12 preset
scores TopBench 33 where the AT-class real 286 boards of its clock score 21 to
27 — 22% to 57% above them, and 43% above the emulated reference's 23
([#1840](https://github.com/DeanoC/fes/issues/1840)). That is a CPU-throughput
finding, and it cuts the other way for pitch: a faster CPU would raise the
note, and the kit's note is lower. Whether the two have a common cause in the
governor's clock table is open; they are not the same measurement. It is the
286-12 preset's figure, so what it qualifies is **Ultima VI's walking speed**:
a walk measured against a preset that is itself 22% to 57% above the real
boards of its clock would not mean what it appears to, whatever the
measurement's own quality. The Black Gate runs at preset 3 and has its own
question on its own evidence, below. Both are said again where those checks
are.

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
  `7ddb0bfb…`) for all three runs. Nothing was installed and no image,
  boot-config or host change was made in any of them. Each claim and restore is
  in Kit B's status file.
  - `g1`, the pitch and walk measurement: no reboot. The wrapper's own shell
    was killed after it had claimed the kit (it was mistaken for a run still
    waiting for the lock), so the run finished on its own and the restore check
    — remove the run's data trees, snapshot, compare with the pre-run probe,
    release — was run by hand from the same script's tail and logged at 11:53.
  - `g3`, the three lute captures: no reboot, wrapper restore, released 15:56.
  - `g4`, the walk run that measured nothing: **this one did need a reboot.**
    It left the lease blocked with the core loaded, and a warm reboot cleared
    it, at Deano's word. That is the one hardware action in this record beyond
    loading cores, and the section on the walk gives the cause and the
    verification that followed it.
- **Disk.** `u6nm.img` (`e28bc5a2…`): the FreeDOS games image `e69194ec…`,
  built from `scripts/build_pc386_freedos_image.py --profile games` on 26
  October and carrying FreeDOS kernel 2043, where main's lock now pins 2046, plus Ultima VI
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
| key `6` | **840.69 Hz** (three captures, 840.65–840.70) | **962.95 Hz** (five captures, 962.29–963.23) | 924.43 Hz |
| key `1` | **499.78 Hz** (three captures, 499.77–499.79) | **573.56 Hz** (five captures, 573.26–574.28) | 550.62 Hz |
| kit below the reference | | **12.70% and 12.86%** (2.35 and 2.38 semitones) | **9.06% and 9.23%** (1.64 and 1.68 semitones) |

The [pitch result](#pitch-result) uses the ±5.9% band, one semitone, against
the reference measured at 12.5 MHz. Scaling it to the preset's nominal 12.0 MHz
holds while the game's delay unit does not change across that step (see below).
The measurement is far finer than the band: the kit's three captures agree to
0.006% and the reference's five to 0.18%, and the kit's figures are the same as
the 10 November run's on a different seal, through this record's own analyser —
840.70 against 840.69 Hz and 499.78 against 499.78 — so this is the machine and
not the day.

**Both sides now have the three repeats the method asked for**, and more:
three kit captures on this package, taken as three plays inside one Use in run
`g3` on Kit B, and five reference captures — `m3`, `m4`, and three plays inside
one Use in run `m6`. `pitch.json` also keeps the earlier kit captures `g1` and
the 10 November `t1`, which is on a different seal. An earlier reference run
was made and its lute landed in character creation, so it holds no notes.

The repeats do what the method asked of them. The three kit captures spread
**0.006%** on key `6` and **0.004%** on key `1`; the five reference captures
spread 0.10% and 0.18%. The band is ±5.9% and the difference being measured is
12.7%, so it is between 70 and 2,000 times either side's own spread.

**The reference was running at full speed for each lute play.** 86Box shows its
emulation speed in the window's tool bar, and the grab taken at the end of each
lute play — `u6-played` in `m3` and `m4`, `u6-after3` in `m6` — reads 100%. It
would not change the verdict either way: a reference held below real time can
only read flat, which narrows the gap the kit is being judged on.

**The condition is not new, and most of it predates #1453.**
[#1474's record](../2026-11-04-pc386-speaker-kit/summary.md) measured the same
two keys on Kit B on 4 November, before and after #1453's ISA charges: 859.0
and 511.8 Hz before, 839.0 and 501.5 Hz after, and gives that step as a ratio
measured with
one estimator throughout: 0.9767 for key `6` and 0.9799 for key `1`. Applied to
today's figures, the kit before #1453 would have been 860.8 and 510.0 Hz, a
shortfall against this reference of **10.6% and 11.1%**, against 12.70% and
12.86% today. So #1453 added about two points (2.1 and 1.8) to roughly eleven
that were already there.

**4 November's absolute pitches are not comparable with this record's**: they
come from `u6_tones.py`'s envelope estimator where this record uses
autocorrelation, which is why only #1474's ratio is used above. Repeatability,
same analyser on both, is this record's own two kit captures, four days and two
seals apart: **840.70 and 840.69 Hz** for key `6`, **499.78 and 499.78** for
key `1`. Nothing recent regressed, and what is new here is the reference.

The two keys' shortfalls agree to 0.2% (ratios 1.1454 and 1.1476), which looks
like one multiplier on the loop's time per pass rather than anything per-note
(it is not the PIT's input clock: #1472 measured the carrier at 18.5–20.2 kHz
at divisor 60, where a clock slow enough to explain this would put it near
17.3 kHz) — against
which #1474 found #1453's own drop was *not* uniform, 2.01% at 512 Hz rising to
2.59% at 1,265 Hz, and left that unexplained.

**What is not established is the cause.** There is already a model for this
loop: [#1472's record](../2026-11-04-pc386-speaker-kit/summary.md) puts one
pass of the routine's loop at about 89 clocks plus about 475 for each of the
game's own delay units, so #1453's ISA charges — 14 clocks on the pass's port
61h `IN` and `OUT` — drop the pitch 2.42% at one delay unit, which is what was
measured then (2.01, 2.33 and 2.59% over three keys). That accounts for **at
most** two and a half points of the twelve and a half here, and only on the
kit: what the reference pays for the same two port accesses was not measured.
So most of the difference is elsewhere: the governor's pacing of the rest of the pass,
and the pass costing a 386 a different number of clocks than a 286 whatever the
table says. Nothing here separates them, and no lute capture at another preset
exists to split them with (#1481 measured the lute at 286-12 only).

The direction of the error says one thing for certain and suggests another.
What it settles: the kit's CPU benchmarks *faster* than the reference's —
TopBench 33 against 23, which is
[#1840](https://github.com/DeanoC/fes/issues/1840) — while its notes come out
*lower*, so **a uniform CPU speed-up is not the cause**.

What it does not settle is what is: a hypothesis put by another session is
that the loop is paced by its I/O cost rather than by instruction throughput,
which would make the governor's ISA charges the cause. The model above does not
support that on its own — before #1453 the pass carried no ISA charge at all,
so the eleven points that predate it had nothing there to be over-charged, and
nearly the whole pass is the delay loop. A delay loop whose instructions cost
the kit's 386 a different number of clocks than a 286 fits the same evidence.

A cheap test would separate them, and is named in #1837 rather than run here:
play the lute at preset 1 under two different frequency overrides, which the
machine record already takes. If the pitch follows the override, the loop is
CPU-timed and the clock table is where to look; if it does not, the I/O charges
are. The issue filed from
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
  difference. Two of them reading *exactly* the same is quantisation, not
  agreement: at 30 fps a 10-frame median gap moves 10% — the whole band — for
  one frame either way, and the 23% is three frames. Whoever measures this next
  should capture at 60 fps, which Kit A's path and the reference both allow.

**That dependence is itself the finding**: anyone measuring Ultima VI's pace by
sending keys and counting what moves will measure their own key timing unless
they deal with it first.

### What was done about it, and how far it got

A small DOS program, [`keypace.asm`](keypace.asm), was written to measure what a
driven guest actually receives: it reads keys through INT 16h, the way Ultima VI
does, and records the BIOS tick at each one. Run on the reference (run `kp4`) it
gives the rig's delivery, which nothing here had measured before:

| what the driver sent | keys the guest got | rate |
| --- | --- | --- |
| hold a key for 10 s | **1** | — |
| 40 taps, 0.25 s apart | 40 | 2.3/s |
| burst of 60, 80 ms apart | 60 | 12.3/s |
| burst of 60, 40 ms apart | 60 | 25.0/s |
| burst of 60, 20 ms apart | 60 | 48.8/s |

So the rig can key the reference far faster than the game steps, with nothing
dropped — which makes the repair in `method.md`'s sixth amendment possible:
**saturate the input on both sides and read where the game's step rate stops
rising**. It also shows a held key must never be used: it reaches the
reference as one keystroke, where the kit's keyboard repeats.

That amendment also writes down two validity gates the windows above never
stated — the party out of combat in a grab taken immediately before each
window, and 86Box's own speed indicator at 100% for every frame of it.

**The first run under the new design, `bw3`, is void because its route ran out
of floor.** Each leg walked one direction for several seconds: one found no
wall and gave six steps, another none at all, and the grabs show the Avatar
crossing out of the throne room into the next room in the first. A saturated
walk covers a room in seconds, so a straight leg cannot be the route. (The four
windows keyed at 25/s did hold 100% for all 240 frames and pass the speed gate;
the four at 48.8/s ran at 71-101% under other work on the shared host and would
have failed it.) An earlier reading of this record blamed the step counter for
merging fast steps; the counts do not support that — `scroll_events.py` finds 8
events on the window where `walk_rate.py` found 6, a strict threshold rather
than a merge.

### What over-keying showed

Two things changed before a saturated walk could be read, both in `method.md`'s
seventh amendment. The route went back to the one-tile square the earlier
windows used, because a saturated walk crosses a room in seconds and a straight
leg runs out of floor. And the step counter was replaced:
[`scroll_events.py`](scroll_events.py) tests the whole map view's mean absolute
difference against 2 levels, where `walk_rate.py` asks that a fifth of its
pixels each move more than 40 — too strict for a view that is mostly dark
stone. On the three slow windows the two counters agree to within one frame of
median gap (3.33/3.00/2.50 against 3.00/3.00/2.31), so the new one is sound.

Run `sq1` on the reference, six windows of ten seconds, all out of combat and
all at 100% emulation speed:

| keys a second | steps a second, from the median gap | mean |
| --- | --- | --- |
| 25.0 | 2.143, 2.308, 1.765 | 2.07 |
| 12.3 | 1.818, 2.143, 2.222 | 2.06 |
| 10 (the earlier windows, same analyser) | 3.33, 3.00, 2.50 | 2.94 |

**Keying harder makes the game walk slower.** The over-keyed figure is steady
— 12.3 and 25 a second agree to 0.53% — but it is steady at a rate the game can
be *held down to*, not the rate it can reach: excess queued keys suppress
steps. Two machines compared at that point are being compared under a rig
artefact, not on walking speed.

That is not a proof that no usable plateau exists. The rates tried here are 10,
12.3 and 25 a second, and the peak lies somewhere below 12.3 — nothing was
sampled below 10, nor in the gap between 10 and 12.3, and the response could
still be rising inside either. Finding the maximum would mean sweeping
everything under 12.3 a second on both sides, that gap included, for a rate at
which the step rate stops rising before the suppression sets in.

**What is established is that the three methods tried here cannot measure it**,
all on one cause — the game steps when it next looks at the keyboard and drops
what is queued meanwhile. A held key reaches the two sides differently; a
matched tap rate measures the tap rate; over-keying measures a suppressed rate.
The surer fix is a count the game itself keeps, read from memory on each side.
Neither that nor the sweep was available here, and the band is not applied to a
number neither side could defend.

The windows' own spread would have been a problem in any case: 1.77 to 2.31 at
one key rate. The three-window means carry standard errors of 7.8% and 6.0%,
so a comparison between two such means carries √(7.8² + 6.0²) ≈ **9.8%** —
level with the ±10% band on its own, before any difference between the
machines is measured.

**The matching kit run, `g4`, measured nothing.** It inserted `KEYPACE.COM`
before the game, but the kit image boots to a menu rather than a DOS prompt, so
that typing went into the menu and everything after it desynchronised; the
grabs show COMMAND.COM answering "Bad command or filename" to the mangled
lines, including `ultima6\`, and all six windows are a static DOS prompt. The
run also left Kit B with its lease blocked and the core still loaded: the
command file was missing its final `quit`, so the runner sat in its idle wait,
and the run's post-run tail was then run by hand too early, removing the run's
data trees while the core was loaded. The agent's own cleanup then failed with
HTTP 500 — most likely because of that early removal, though that is not shown.
The agent reported "operator recovery required"; **a warm reboot cleared it**,
at Deano's word, and the kit was restored, verified and released. Sending a
stop was tried first and cannot be done — the agent refuses a takeover on a
blocked lease with HTTP 409, so there is no token to stop with — and the
narrower `fes-kit-software restart-service fogcast-kit` was not available. The
reboot works because the lease status is in-memory only
(`sources/FogCast/internal/kitlease/manager.go`: `kitlease.New` starts free with
a fresh generation, and nothing reads a persisted lease back), so any restart of
the agent clears it. A lease blocked by a *maintenance reservation* would not
clear this way, since those persist; the reason string tells the two apart.

After the boot: lease free with a new generation and no owner — the stale one
did not survive — agent idle with no core loaded, image `179affbb` good with no
pending and `state.previous` unchanged, bundle `7ddb0bfb` selected and active,
`usb-storage.quirks=152d:0583:i` present in both `u-boot.txt` and
`/proc/cmdline`, and the FogCast data byte-identical to before the run. No
reimage, image or boot-config change, no I2C and no power cycle. The incident
and the recovery are logged in `~/kit-status/kitB-STATUS.md`.

That run would not have changed the conclusion. It was to produce the kit's
half of a comparison whose reference half turned out not to mean what it looked
like.

Even then, the figure would have to be read against
[#1840](https://github.com/DeanoC/fes/issues/1840): the 286-12 preset the walk
would be measured at scores 22% to 57% above the AT-class real 286 boards of
its clock, so
matching the reference's walking speed at it would not mean the preset is
right, and missing it would not say which of the two was wrong.

That is `method.md`'s fourth amendment, written before the kit run that would
have been compared with these numbers, and corrected there as it is here.

For the record, the kit's own figures on this package, from run `g1`, which
walked with a held key during the opening fight rather than after it: 11 steps
in 45 s with a median gap of 1.57 s, close to the 10 November run's 1.47 s. Those are not comparable with the reference's numbers above —
different input, and a fight in progress — and they are not treated as a
verdict. The kit's preset was read back from the machine record as preset 1,
frequency override 0, turbo 0, before the game was loaded.

See the [walking-speed result](#walk-result) for the check's status and the
routes left to try.

## Ultima VII: The Black Gate, NPC pace

The [NPC-pace result](#npc-result) gives this check's status. The Black Gate
does not start on the reference: it
prints "Ultima VII requires 561144 bytes of DOS memory!" and exits. The free figure itself was not read there,
but the cause is plain from the boot: the games image's UMB provider reports
"no RAM found at C800. Aborted!", because a real 386 board has nothing behind
C800–DFFF where the kit's machine has RAM, so HIMEMX and the mouse driver load
low instead of high. An attempt to move the XT-IDE BIOS to C400h was made by
writing a `[PC/AT XTIDE]` section into the configuration; the POST banner was
not read back, so it is not known whether it moved, and it would not have
helped in any case, because there is still no RAM to make upper memory out of.
Three ways round it were tried on a cached 386DX-25 board and none worked:

| what was booted | free conventional memory | what The Black Gate did |
| --- | --- | --- |
| the games image's own config (`u7b`) | not read | the 561,144-byte error |
| MS-DOS 6.22 `HIMEM.SYS` with `DOS=HIGH` (`u7c`) | 600,128 bytes | the same error |
| the same without `DOS=HIGH` (`u7d`, `ctl`) | 557,632 bytes | the same error, 3,512 bytes short |

The middle row is the informative one: with **more** than the 561,144 bytes it
asks for, it still refuses. That fits the kit's own boot menu, which labels its
`DOS=HIGH` entry "More memory: **not for Ultima VII**" — the game's Voodoo
memory manager wants the HMA free. So `DOS=HIGH` is ruled out rather than
untried, and the remaining 3,512 bytes have to come from somewhere that is not
the HMA. Trimming `FILES`, `BUFFERS` and the environment was tried and
abandoned: the edited image would not boot, and it would have made the
reference's DOS differ from the kit's in a way that matters to a game doing
this much disk access.

What is left is to reproduce the kit's memory map — an 86Box ISA memory
expansion board putting RAM behind C800–DFFF, so the games image's own UMB
provider loads the drivers high there exactly as it does on the kit. That is
the like-for-like route and it was not reached today.

**This is now [#1853](https://github.com/DeanoC/fes/issues/1853)**, with its
own session, and the ruled-out routes below are its starting point rather than
work still open here.

**Two things, then, for whoever picks this up**, in order: that memory board,
which is a configuration change on the reference and should unblock the game;
and the reference board itself, which `method.md`'s fifth amendment now fixes
as a cached 386DX-25. Settling the board is the smaller of the two — the cached
and uncached boards both score TopBench 40, so the choice does not decide the
comparison — but it has to be stated before a pace figure is read, not after.
Neither is a dead end; both are work that was not reached. Any change made to
the reference to get the game running must be named in the record beside the
figure, because a reference configured unlike the kit's machine is a difference
to declare rather than absorb.

The reference machine for it was also not settled. The board used for the
benchmark above, a 1988 C&T 386/AT with no cache, scores 40 against the kit
preset's 68, where TopBench's database has real 20–25 MHz 386 machines from 25
to 60. A verdict on NPC pace should not rest on a board chosen after the fact,
so the board has to be fixed first, by the same rule that validated the 286
one: mid-range among that database's real machines of the class. That rule is
harder to apply here — the database has no 386DX at 25 MHz at all, its 386DX
and 386SX machines at 20–25 MHz score 25 to 60, and its one 25 MHz entry with
on-chip cache (an IBM 386SLC) scores 96.

`method.md`'s fifth amendment fixes the rule from the preset's own definition —
zero wait states means a cached board — and a cached 386DX-25 (`asus3863364k`,
POST "Cache controller ... Enabled") was benchmarked for it: it scores **40**,
the same as the uncached board. The two are therefore not far enough apart for
the board choice to decide the comparison, which removes one objection to
whatever figure a working run eventually produces, though it does not place
either board among real machines.

The 386DX-25 preset has the same question as
[#1840](https://github.com/DeanoC/fes/issues/1840) asks of 286-12 — it scores
68 ([#1486](https://github.com/DeanoC/fes/pull/1486)) — but on weaker evidence,
because of that gap in the database. It is not filed, and settling the board
would be the first step towards being able to.

## What this does not cover

- The cause behind the [pitch result](#pitch-result).
- The [walking-speed result](#walk-result), with the method observations above.
- The [NPC-pace result](#npc-result), with the reference setup above.
- The reference runs Ultima VI with a different DOS kernel build and 548 KiB
  free against the kit's 573 KiB (`method.md`). Neither is in the game's
  timing path, but neither was varied to prove it.
- One reference window in six dipped below 100% emulation speed for 60 frames
  of 1,350; it is `p1`, the one that gives the highest pass rate (1.67 a
  second), and it is reported as measured. `p1` also differs from the other two
  in more than its key rate: its keys went in bursts of four from one `xdotool`
  call, where `p2` and `p3` sent them singly.
- **TopBench validates the reference's CPU, not its I/O timing**, and the
  note's frequency is set by how long each `OUT` to port 61h takes as much as
  by the CPU. The kit is flat although its CPU benchmarks faster, so the
  difference is not a CPU-speed difference, and the reference's standing
  against real 286 boards does not by itself settle what the right note is.
