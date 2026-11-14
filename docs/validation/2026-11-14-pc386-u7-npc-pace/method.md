# Method and tolerance, fixed before the Black Gate captures

Issue [#1853](https://github.com/DeanoC/fes/issues/1853), the last item of
[#1368](https://github.com/DeanoC/fes/issues/1368). This file is committed
before any pace capture on either side — before the reference is scouted for
a scene, and before the kit is claimed — so the pass rule and the reference
board cannot move afterwards. It follows the pattern
[the 14 November Ultima VI record](https://github.com/DeanoC/fes/pull/1838)
set: amendments are allowed, dated, and must say what had already been seen
when each was written.

What is already known and is **not** part of this file's commitment, because
it was settled before it was written: that The Black Gate now starts on the
reference, and how ([`summary.md`](summary.md), Part 1). No pace figure of any
kind has been read on either machine.

## What is being checked

`sources/misteross/docs/pc386-machine-design.md`'s acceptance table says, for
Ultima VII: The Black Gate, "386-class preset; NPC pace matches the
reference", and defines a reference as "recordings from period hardware or a
configured emulator, named in the acceptance record".

One check: **The Black Gate's NPC pace at preset 3 (386DX-25), kit against
reference.**

## The reference board, and the rule it was chosen by

### The rule

[#1838's `method.md`](https://github.com/DeanoC/fes/pull/1838) validated its
286 reference by placing it among the real machines of its class in TopBench
0.38a's own shipped database (`DATABASE.INI` on the benchmark drive), and
required the reference to sit mid-range among them. The same rule applies
here, with one stated reading, because the database holds **no 386DX at 25 MHz
at all**:

> **The comparable class is every AT-class real 386DX entry in that database,
> at whatever clock, with its score scaled to 25 MHz in proportion to its
> clock. The reference board must sit mid-range in the resulting distribution.
> "AT-class" excludes an accelerator card in an XT, as #1838 excluded one from
> its 286 class.**

Scaling by clock is the assumption this rule adds, and it is stated rather
than hidden: it treats TopBench's score as proportional to clock for a fixed
board design, which the database's one board entered at four clocks supports
(M326 at 16, 20, 33 and 40 MHz scores 50, 60, 90 and 104 — 3.12, 3.00, 2.73
and 2.60 points per MHz, falling slowly as the clock rises, so scaling *up*
from a lower clock slightly over-states and scaling *down* from a higher clock
slightly under-states).

Nothing below is tuned to a result: the band and the metric are set here, and
the classification rule for a miss is set here too.

### The distribution

18 AT-class real 386DX entries (19 with the Inboard 386 accelerator in an IBM
PC/XT, which is excluded). Scaled to 25 MHz and sorted:

```
31 32 34 42 48 54 54 57 57 57 59 60 61 65 66 68 75 78
```

minimum 31, lower quartile 47, **median 57**, upper quartile 65, maximum 78.
The four highest (65, 68, 75, 78) are all the same M326 motherboard at
different clocks; without it the maximum is 66. The distribution is bimodal:
three IBM PS/2 models at 1.25–1.38 points per MHz, then everything else from
1.68 upwards, with nothing between.

### The board

**`asus3863364k`** — 86Box's [SiS 310] ASUS 386/33-64K, run at 25 MHz with
its 64 KB cache enabled (the POST prints `Cache controller ... Enabled`), the
interpreter, and no FPU. [`reference.md`](reference.md) gives it in full. It
is chosen because it is a cached 386DX, which is what #1838's fifth amendment
requires of a reference for a preset defined as the Intel386 table at 25 MHz
with **zero wait states** (`sources/misteross/docs/i386-cpu-design.md`), and
because it is the board The Black Gate was made to run on, so the Part 1
configuration and the pace runs are the same machine.

**It does not meet the rule, and that is reported rather than worked around.**
It scores TopBench **40**, which is below the lower quartile of 47 and below
every entry of the upper cluster: only three of the eighteen AT-class entries
scale below it. It is not mid-range. Three things were checked before concluding that:

- `cpu_waitstates = 0`, the preset's own definition, scores **40**, the same
  as the board default. Wait states do not explain it.
- The uncached `ecs386` and the cached `asus3863364k` both scored 40 in
  #1838's work, so the cache does not explain it either.
- Five other 86Box 386DX boards (`award495`, `ecs386v`, `acc386`,
  `dataexpert386wb`, `asus386`) were set up for the same benchmark and are
  **not benchmarked**: each has an AMI BIOS that drops into SETUP on a blank
  CMOS, and giving each one a CMOS by hand was not worth it once the two
  wait-state settings showed the score does not move. They are listed as not
  measured, not as presumed 40s.

So the honest statement is that 86Box's 386DX at 25 MHz models a machine at
the slow end of the real range, and no board setting within it reaches the
middle. Every figure below is therefore reported together with the throughput
ratio, and a miss is classified by the rule in "If the check misses".

### Where the kit sits

The kit's 386DX-25 preset scored TopBench 68 in
[#1486](https://github.com/DeanoC/fes/pull/1486). That figure is from an older
seal, so **TopBench is re-run on the kit in the same claim as the pace
captures**, and the figure used is that one. Against the distribution above,
68 is above 16 of the 18 real entries, and above all 14 once the M326's four
are set aside; it beats every real 386DX-20 in the database (maximum 60) and
sits just under the slowest real 386DX-33 (71). That is the same shape as
[#1840](https://github.com/DeanoC/fes/issues/1840) found at 286-12, in a
milder form, and it is stated here because it qualifies any pace comparison
rather than being added to one afterwards.

## What is measured

**NPC pace** is **the displacement of a named non-player character per second
of wall-clock time, in a fixed scene, with no input sent during the window.**

- It is an NPC's movement, not the rate at which the picture changes. #1838's
  third amendment proposed the scene's update rate as a proxy, on the ground
  that The Black Gate has no frame limiter so the redraw rate is the game
  loop that walks the NPCs. That link was never measured, and this check does
  not assume it. The update rate is still recorded, as a second series, so
  that the link can be tested; the verdict rests on the displacement.
- Displacement is measured in the scene's own pixels, per side, from the
  captured frames: the NPC's sprite is tracked between frames and its path
  length over the window divided by the window's length in seconds. The
  analyser is committed with the record before the kit capture.
- The scene is fixed and reached the same way on both sides, and the paths
  from a cold boot to that scene are per side, as #1838's first amendment
  established for Ultima VI, because the two machines take different times
  through the game's menus.

## Tolerance

**The pace must be within ±10% of the reference's**, the band #1838 fixed for
a rate, for the reasons it gave: wider than the measurement's own spread,
wider than any clock offset between the two machines (here there is none —
both are nominally 25 MHz), and narrower than the step between the governor's
presets, so it cannot pass a machine running at the wrong one.

**Three windows a side**, and the spread between repeats on one machine is
reported. If the spread is not well inside the band, the check is not decided
by this method and the record says so, with the figures, as #1838's fourth
amendment required of the Ultima VI walk. Nothing is widened to admit a
result.

## Validity gates for a window

A window counts only if all of these hold, and the record says for each
window whether they did:

1. **No input** is sent during the window, on either side, and the mouse
   pointer is parked at the same place in the scene on both.
2. **86Box holds 100% emulation speed** for the whole window. The speed
   readout is captured alongside the picture; the title screen was already
   seen to dip to 97%, so this is not a formality.
3. The **capture rate is the same on both sides** (`REF_FPS` set to the kit's
   HDMI grab rate), and the measured pace is well under half of it.
4. The counted motion is the NPC's sprite moving, not palette cycling or
   another animation in the scene.
5. Only one 86Box runs on the host during a reference window.
6. Both sides run **the same drive image**: `u7bg-master.img`
   (SHA-256 `d9bfafd4…`) plus `UMAP.COM`, with a **fresh copy per run**,
   because starting a game writes to it. Each side's copy is digested before
   the run and the digests are given in the record.

## If the check misses

A miss is not by itself a core defect, because the reference is slower than
the kit by a measured amount. The classification is fixed here, before any
figure is read:

- Let *T* be the TopBench ratio, kit over reference, both measured in this
  work.
- If the pace ratio is within the band of *T*, the difference is the preset's
  throughput — the [#1840](https://github.com/DeanoC/fes/issues/1840) class of
  finding, not a Black Gate defect.
- If the pace ratio departs from *T*, the difference is specific to this
  title, and that is a core finding.
- A run at a second reference clock, chosen so its TopBench is near the kit's,
  may be taken to separate the two. **It cannot turn a miss into a pass**: the
  verdict of record is the kit against the 25 MHz reference at ±10%.

## What is recorded either way

The reference's and the kit's `UMAP.COM` maps, `MEM /C` output and TopBench
scores, taken in the same runs as the pace captures; the drive digests; the
per-window figures and their validity gates; and, if no figure survives the
gates, what would be needed to measure it, in the way #1838 recorded the
Ultima VI walk's three dead ends.

## Amendment, after scouting the reference and before any capture

Written after the reference was scouted for a scene and before any pace
window was captured on either side. **No pace figure of any kind had been
read when this was written**, on either machine, and none was read
afterwards; what had been seen is only the game's own front end on the
reference, listed below. The band, the metric and the board are unchanged.

1. **The route is START NEW GAME, not "Journey Onward" from the shipped
   save.** `method.md` above says the scene is "reached by Journey Onward
   from the save the game ships", inherited from #1838's third amendment.
   Scouting shows the kit's own command file for this title
   (`~/fes-scratch/u6ref/cmds/kit-u7-g1.jsonl`, written for the 14 November
   kit run) takes START NEW GAME and types a character name, so a new game is
   what the kit side is set up to reach and what both sides should use. A new
   game is also the stronger choice: it needs no save file on the drive image,
   so the two sides cannot differ by one.
2. **The title screen times out into the introduction after about 20
   seconds.** Every step between the menu appearing and the click has to fit
   inside that, which rules out grabbing a frame to check the aim before
   clicking. Any command file for this game must reach its click without a
   verification grab in between.
3. **The reference's emulation speed is not under this work's control.**
   Powerboat carried a load average above 50 from other sessions throughout
   the scouting, and 86Box's own readout fell as low as 17%. The validity
   gate above (100% for the whole window) already covers this, but it means a
   reference window can only be taken when the host is quiet, and the record
   must give the readout for each window.

## Second amendment: the walk cycle is the figure, after the reference's own repeats and before any kit capture

Written after three reference windows and **before the kit was claimed**. No
kit figure of any kind existed when this was written, and none could: the kit
had not been touched in this work. It changes which of two quantities the
verdict rests on, after seeing data, so it says exactly what was seen.

**What was seen.** The scene a new game opens in — Trinsic, between the world
appearing and the opening conversation starting, with no input sent — has one
townsperson pacing a fixed beat on the right of the view. Three reference
windows gave:

| run | window, world to conversation | displacement | walk cycle | 86Box held 100% |
| --- | --- | --- | --- | --- |
| m1 | 46.67 s | 0.04306 heights/s | 11.733 s | yes |
| m2 | 46.67 s | 0.05010 heights/s | 11.800 s | yes |
| m3 | 47.60 s | 0.04413 heights/s | 12.067 s | **no** |

m3 fails the validity gate above (86Box did not hold 100% for the whole
window) and is therefore not a window; it is given because it was run, and a
fourth was taken in its place.

**What changes.** Of the two valid windows, the **displacement** figures
differ by 16%, which is wider than the ±10% band, so the band cannot be
applied to it — the same situation #1838's fourth amendment met with Ultima
VI's walk, and the same answer: the figure is reported and the check is not
decided by it. The reason is the measurement, not the game: displacement is
summed from the centroid of changed pixels frame by frame, so it counts how
often the character is redrawn as much as how far it goes.

**The character's walk cycle does not have that problem.** Its beat is closed,
so the autocorrelation of its position has a sharp first peak at the time it
takes to walk the beat once, with harmonics at twice and three times that lag.
It needs no pixel scale and no event counting, and the two valid windows agree
to **0.6%**, sixteen times inside the band. So:

> **The verdict rests on the walk cycle: the kit's cycle must be within ±10%
> of the reference's.** The displacement figure and the window length are
> reported beside it, for whoever measures this next, and the check is called
> decided only if the cycle's own spread stays well inside the band on both
> sides.

The band itself does not move, the scene does not move, the validity gates do
not move, and the classification rule for a miss does not move. Three windows
a side still stand, and a window that fails a gate is not one.

## Third amendment: everything that changed after the kit figures were read

Written last, after both kit claims and the clock sweep. It changes no
quantity and no band; it exists so that the order of events is in this file
and not only in the summary. Each item says what it is.

1. **The classification rule is set aside, and what it would have returned is
   recorded.** The rule above compares the pace ratio with *T*, the TopBench
   ratio, and would have returned **title-specific**: *T* = 68/40 = 1.70 and
   the kit runs the scene 1.35× faster, well outside ±10% of *T*. It is set
   aside because its premise fails on the reference itself — across 25, 33.3
   and 40 MHz the reference's TopBench moves 20% while the game's pace moves
   39%, so a TopBench ratio cannot say what a pace ratio ought to be. **This
   was decided after the kit figures were read**, and the summary therefore
   reports the cause as *not classified* rather than claiming the answer the
   rule would have given or the opposite one.
2. **The lit-level mark was made adaptive**, after the kit capture, because
   the kit's HDMI path renders the same scene at a different grey level than
   86Box's window and a fixed level found no world frame in it. It is now
   taken halfway between each capture's own dark and bright plateaus. **The
   reference figures are unchanged by it** (m1 and m2 re-ran to the same
   world, conversation, cycle and pace figures to every digit).
3. **The analyser was committed after the kit capture, not before.** The
   method's text committed the quantity beforehand; `u7pace.py` itself was
   written against the reference windows and then run unchanged on both
   sides.
4. **Validity gate 3 is not met as written.** It asks for the same capture
   rate on both sides; the reference was captured at 15 fps and the kit at
   60 fps. The gate's purpose — that the measured rate be well under half the
   capture rate — is met with room to spare on both (a cycle of 9–12 s against
   15 and 60 frames a second), and the quantity is a period in seconds, which
   no capture rate enters. It is recorded as a deviation rather than waved
   through.
5. **The clock sweep and the reference's TopBench at 33.3 and 40 MHz were run
   after the kit figures**, to read the miss rather than to decide it. The
   verdict is the kit against the 25 MHz reference at ±10%, as fixed above.
6. **The speed gate's exclusions are reported from the readouts themselves.**
   The gate tests that the emulator's readout does not change within the
   window; reading the distinct strips shows what it was. m1, m2, m6 and the
   33.3 MHz sweep run read **100% for every frame**. m3, m4 and m5 vary — 85
   to 104%, 83 to 101% and 91 to 104%, at 100% for 55%, 90% and 73% of their
   frames — and are excluded. The 40 MHz sweep run reads 100% for 486 of its
   501 window frames and 101% for 15, so it fails the gate as written; a
   fast second shortens a measured period, so its true cycle is at or just
   above the 8.467 s measured, and the summary says so where it uses it.

## Correction to the second amendment

The second amendment moved the verdict from the quantity this file committed —
displacement per second — to the character's walk cycle, after seeing
reference repeats whose displacement spread failed the precommitted
spread rule. That is the move #1838's record was caught making and retracted:
a precommitted gate was replaced after repeats failed it. It is corrected here
in the same way, in place, and the correction is what to read.

**The committed check is not decided.** The rule above says it plainly: "If
the spread is not well inside the band, the check is not decided by this
method and the record says so, with the figures." Across the three reference
windows that passed the speed gate the displacement spreads **32.1%**, more
than three times the ±10% band, so the outcome of the check as committed is
**not decided by this method**, and no later quantity changes that. The
figures are reported either way.

There is a second, independent reason the committed quantity cannot carry a
verdict here, and it is a measurement defect rather than noise: the two
captures ran at **15 and 60 frames a second**, against validity gate 3, and
displacement is summed per frame, so the number depends on the capture rate.
Measured, the two quantities disagree outright — displacement gives a kit/
reference ratio of **1.096** where the cycle gives **0.740**. A figure that
turns over when the capture rate changes is of the rig, not of the game,
which is the lesson #1838's record drew from Ultima VI's walk.

**The walk cycle is therefore reported as a new quantity, not as a repaired
gate for the old one.** What can be said for it, and what cannot:

- It was fixed **before any kit capture**, so it cannot have been chosen to
  make the kit pass or fail; the second amendment is dated and says what had
  been seen.
- It is a period in seconds from the autocorrelation of a position, so no
  pixel scale and no capture rate enter it. Its quantisation is one frame —
  0.57% of the figure on the reference at 15 fps, 0.19% on the kit at 60 —
  far inside the band.
- It is nevertheless a quantity **adopted after the committed one failed its
  own spread rule**, and that is exactly the pattern this project retracts.
  So its figures are reported as a measurement, with the band applied to them
  for what that is worth, and **the check #1853 asks for is reported as not
  decided by the method as committed**. Whoever wants it decided should
  commit the cycle in advance and measure again; this record gives everything
  needed to do that, including the analyser.
