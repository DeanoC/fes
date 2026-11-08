# C64 PAL game acceptance, 7–8 November 2026

Issue [#1531](https://github.com/DeanoC/fes/issues/1531): repeatable,
representative real-game acceptance for the C64 core, in PAL scope only, on
DE10-Nano. NTSC is out of scope.

The game and media matrix was
[published on the issue](https://github.com/DeanoC/fes/issues/1531#issuecomment-6707439997)
before the first kit claim. It has three titles, two independent library
launches each, at least 10 minutes of active play per case, and a declared
objective per case.

All runs were on Kit B. On 7 November Kit A stayed held by Lander's #1490 restore; the 8 November Kikstart rerun also used Kit B.
Each case is compared with VICE 3.9 `x64sc` using the same ROMs and disks,
on semantic outcomes: screens reached, on-screen counters and course times,
and progress under input. No brightness or non-black checks are used.

**Result.**

| Case | Result | Notes |
| --- | --- | --- |
| 1 Kikstart: KERNAL load, both ports, split-screen, collisions, SID | **PASS** | Run `kik4` (8 Nov), both launches. KERNAL load, trainer, high-score file read, two players with two joysticks: each port drives only its own rider, and course 1 times are P1 72.54 and P2 59.64 on both launches and in VICE. About 754 s of race time per launch (derivation in case 1), and the race completes back to the title. The 7 Nov `kik2` two-joystick failure was a harness timing error ([#1542](https://github.com/DeanoC/fes/issues/1542), closed as not an FES defect). |
| 2 Lode Runner: Brøderbund multi-file load, raw-sector level reads, joystick and keyboard | **PASS** | Run `lode1`, both launches: the 1.5-minute original loader, the attract demo matching VICE (SCORE 2125), fire starting level 1, Ctrl+A costing a man (MEN 5→4), Ctrl+U advancing to LEVEL 002 in r1 and r2 (corrected 8 Nov; see case 2), play scoring through input, game over and restart. |
| 3 Metal Warrior 4: custom IRQ fastloader with its own 1541 drive code, live side-2 replacement | **PASS** | Run `mw2`, both launches. Fastloader on, the multicolour loading picture, then the side-2 prompt answered by a live replacement in the running session (same session and generation, unit ready). Side 2 loads through the game's own drive code into the game, followed by 600 s of play. The live eject (unit empty, same generation) passed in `mw2` r1 and in both launches of the 8 Nov rerun `mw3`, which repeated the whole case and carried the cadence captures. Every scripted screen matches VICE on inspection. `mw1` is a superseded attempt (see below). |

Audio and video cadence passed within tolerances declared before the
analysis ([Cadence and audio continuity](#cadence-and-audio-continuity)).
The [Regression checks](#regression-checks) section lists what runs in CI
and the bounded manual procedure for the rest.

## Package and kit

- **Package.** `fes.c64` 0.5.0, package
  `668a640d25bfdf7bbf464e93f879df78ecdfecb01cf3e3406ade2ab30f13d692`
  (archive `360eb61f…`, RBF `fdff714f…`, build `c8928c51534a5e75960d9da5bf25500a`).
  - It was sealed on 7 November from main `607922c63` with the registered
    producer (`build_fes_c64_oss.py`) and the core's lock (Yosys `3a5accbd`,
    nextpnr `5fe55f3d`, Mistral `8fcc4cb4`; full commits in the JSON). The
    producer's `build-summary.json`, `timing.json` and `manifest.toml` are
    kept privately on Powerboat and hashed in the JSON (`seal_record`).
  - HIP router, placer seed 2. Seeds 5 and 4 ran out of their 600 s route
    lease, as they did for the 24 October seal.
  - Achieved against required: system 57.10/52.224 MHz, pixel
    74.84/74.25 MHz, audio 186.32/12.288 MHz.
  - On 8 November, after merging main `66af9a662`, all 103 pinned producer
    inputs in `build-inputs.json` still hash identically, so the seal still
    stands for main.
  - The 24 October package `3d5bfc91…` isn't main's tree any more: `top.v`
    and the producer have changed since then, and its archive no longer
    exists on Powerboat.
- **Kit.** Kit B on its baseline: main `bb51f445`, software `a8cd30c4`, OS
  image `f904961e`.
  - Every run was core-only: nothing installed, no image or boot change, no
    reboot.
  - Each claim was logged in `kitB-STATUS.md`. Each release checked the same
    image, previous, pending, idle RBF, launcher, image list, quirk and
    boot_id keys as the pre-run probe, plus an idle agent and a free lease.
  - The FogCast data directories matched the pre-run listing; no run left a
    new path.
- **Host.** A private `fogcast-api` built from main `607922c63`, with its own
  HOME and a 0600 config that names only Kit B. Remote input on, capture off.
  The shared Powerboat host wasn't touched. Each case went through the
  normal library path:
  1. import the package and check compatibility (`compatible: true`)
  2. upload the firmware and bind it as the entry's ROM (`c64-firmware`)
  3. create the library entry and select the D64 as its disk
  4. `session/launch`
  5. input through `session/input/event`
  6. `session/live-media` for a live replacement
  7. `session/stop`, then a relaunch of the same entry
- **Machine.** PAL C64: 6510 at 0.985248 MHz, 312×63-cycle raster.
  - The SID is a 6581 with the package's default fixed-point
    state-variable filter. The opt-in `RESID_FILTER` path is off and isn't
    covered here.
  - Video is the native 384×272 PAL frame scaled into fixed 720p60 HDMI.
  - Capture used Kit B's 4KPRO at 1920×1080 and 60 fps. Audio was HDMI
    digital, 48 kHz stereo, with a constant −88 LSB DC offset that the
    measurements remove. No analogue measurement was made.
- **Firmware.** 36 KiB, `01173de9…`, from Deano's C64 Forever 10 Plus ROM
  set: the four files below concatenated in this order (8, 8, 4 and 16 KiB),
  the core's `c64-firmware` layout. Recomposing them on 8 November gave the
  same SHA-256. It's the same bytes as the 23 October Kit A diagnostic.
  - BASIC 901226-01 (`89878cea…`)
  - KERNAL 901227-03, revision 3 (`83c60d47…`)
  - character ROM 901225-01 (`fd0d53b8…`)
  - 1541-II drive ROM (`326c289c…`)
- **Media.** From Deano's licensed C64 Forever 10 Plus; Metal Warrior 4 is
  also Covert BitOps freeware.
  - Kikstart `91f42076…`
  - Lode Runner `e7eb5220…`
  - Metal Warrior 4 side 1 `4530b1f1…` and side 2 `a8b083af…`

  Nothing from a game, ROM, frame or audio capture is committed. Hashes are
  in [kit-runs.json](kit-runs.json).
- **Reference.** VICE 3.9 `x64sc`, built headless on Powerboat from the
  release tarball, configured to match:
  - PAL and a 6581 (reSID)
  - true drive emulation of a 1541-II
  - the same four ROM files
  - private copies of the D64s

  Input went through its binary monitor. A small local patch,
  [vice-3.9-binary-monitor.patch](vice-3.9-binary-monitor.patch), adds two
  commands, keyboard-matrix set and joystick set (hashes in the JSON),
  because stock `joyport set` drives only the I/O-simulation device. The
  reference filter is reSID's, not the kit's state-variable filter, so audio
  is compared only for presence and pattern of activity.

  Each VICE run is listed in the JSON's `vice_runs` with its script, start
  time, event-log hash, audio hash and the hash of every grabbed frame as a
  raw VIC colour-index buffer. Kit frames are 1920×1080 HDMI captures, so kit
  and VICE frames are compared by inspection, not by hash. The criteria are
  the same screen and on-screen text, equal counters and course times, and
  the same rider or player response to the same input. Counters and times
  must be equal; there's no tolerance on them.

  Audio activity (`tools/aud.py`) counts 1 s windows above −50 dBFS RMS after
  removing DC. No tolerance was declared before the runs, so the counts are
  evidence that the SID plays in the same pattern, not a pass criterion. The
  largest kit-to-VICE difference in the active fraction is 2.7 points (MW4
  r1).

## Method

Each case ran from one script. A scripted part reaches the objectives:

- BASIC `LOAD"*",8,1` and `RUN` typed as keys
- menu keys
- joystick holds
- grabs at fixed points

Seeded pseudo-random joystick play follows, with an HDMI grab every 30 s.
The same script, mapped to VICE's input, produced the reference.

The exact scripts are committed: `inputs/kit-<run>.jsonl` for the kit
(`kik1` used the `kik2` script) and `inputs/vice-*.jsonl` for VICE. The
driver and helpers are in [tools/](tools/). The JSON binds each run to its
script hash, driver hash, event-log hash and play seeds. The seed fixes the
sequence of joystick moves, but each hold lasts a random 0.15–0.7 s of wall
time, so play repeats in order, not in exact timing.

Each case ran twice in one claim, as two library launches with a Stop
between them; the second is the relaunch check. HDMI audio was recorded for
each launch and a 20 s 60 fps video taken at the end.

**Not exercised:**

- **Guest reset.** Neither the shell nor the host exposes a C64 reset, so
  Stop and relaunch is the reset path. Power cycling isn't claimed.
- **Save and reload: N/A.** Native D64 is read-only. On the kit:
  - Kikstart's trainer offers to save high scores, but no run reached a
    qualifying time.
  - Lode Runner writes high scores to sector 12/7 at game over. In VICE
    that write lands in the image; on the kit, disk writes aren't
    supported, so the table doesn't persist.
  - MW4 writes its config into file `00`. On the kit the game carried on to
    the loading picture with the fastloader on and showed no error. Whether
    the drive code attempted the write can't be seen from HDMI.

  None of those writes are expected to persist on the kit. Users get no
  save and reload from a D64.

## Case 1: Kikstart (runs `kik4`, then the earlier `kik2`)

**Path.** KERNAL `LOAD"*",8,1` loads the cracked one-file version through
the native 1541 (about 55 s to `READY.`). Then:

1. `RUN`, then Space past the trainer screen.
2. `L` loads the high-score file from disk (a second KERNAL load).
3. `N` and `N` answer the trainer's two questions.
4. On the title, F1 F1 selects "two players, two joysticks" and F7 starts
   the split-screen race.

Every one of those screens matched VICE.

**Two joysticks (`kik4`, 8 Nov, both launches).** Kikstart ignores both
joysticks while the riders crash at the first wall and recover, about 6–14 s
into the race. A press inside that window does nothing on the kit or in
VICE; see #1542's closing comment. The script therefore works outside it:

1. Port 2 is held Right from before F7, through the start, for 14 s.
2. Port 1 is held Right for 8 s, starting at race time ~15 s.
3. Both ports are then held until the race ends.

| | Kit r1 | Kit r2 | VICE |
| --- | --- | --- | --- |
| Port 2 held 14 s | Player 2 at the Mr. Chip lorry; Player 1 coasts to the tyres | Same | Same |
| Then port 1 held 8 s | Player 1 reaches the lorry; Player 2 is ahead at the fence | Same | Same |
| Course 1: Player 1 / Player 2 | 72.54 / 59.64 | 72.54 / 59.64 | 72.54 / 59.64 |
| Race completes, back to the title | Yes | Yes | Yes |

**Sustained play.** After the race, four 150 s blocks of seeded play
alternate between port 1 and port 2. Each block starts with F1 F1 F7, so
both players have joysticks. All 16 play grabs in each launch are mid-race.

Race time comes from the event log. Counting from each F7, the scripted race
is in progress at the course-2 grab, 133.8 s (r1) and 133.7 s (r2) after F7.
The four play blocks run 620.7 s (r1) and 620.0 s (r2) from their F7 to
their end. That's about 754 s per launch. The scripted race ended somewhere
between the course-2 grab and the after-race grab of the title; that tail
isn't counted. The first version of this record said 850 s, which assumed
the scripted race lasted until the after-race grab.

Crashes and respawns happen throughout. The port isolation evidence is the
scripted check above, where one held port moves only its own rider, and the
`mw1` `pp` probe below. The simulation bench in
[Regression checks](#regression-checks) checks the same wiring on every
nightly run.

SID music was active in 911 of 1037 and 915 of 1038 one-second windows,
against 899 of 1015 in VICE.

Both launches are fresh library launches with a Stop between them. Kit B
was claimed at 13:43 and released at 14:20 on 8 November, with the baseline
verified.

**The earlier attempt (`kik2`, 7 Nov).**

- Course times matched VICE where the inputs reached the game at the same
  game time: Player 1 course 1 72.54, course 2 79.42.
- The two-joystick check pressed each port at a fixed delay after F7. On
  the kit the race starts about 1.4 s sooner after F7 than in VICE, so the
  presses landed in the crash window, where Kikstart ignores both
  joysticks. The riders then looked as if they followed the same input.
- A BASIC `PEEK(56320);PEEK(56321)` probe on the same package (run `mw1`,
  prefix `pp`) matched VICE for every port and direction.
- The fix session traced the game's port reads and the RTL, then reproduced
  VICE's course times with corrected timing. It closed #1542 as not an FES
  defect.

`kik2` is kept as a harness timing error. It also had shorter sustained play
(about 685 s and 535 s in a race), because the old play script pressed F7
while race 3 was still running.

## Case 2: Lode Runner (run `lode1`, launches r1 and r2)

**Path.** `LOAD"*",8,1` loads the 3-block boot program. `RUN` starts
Brøderbund's own multi-file loader, which shows "please wait approximately
1.5 mins" and asks for a joystick in port 2. The game keeps its 150 levels
as raw sectors on tracks 3–12 and reads them by direct block access, so
play exercises the drive throughout.

| Check | Kit r1 | Kit r2 | VICE |
| --- | --- | --- | --- |
| Load and attract demo | Attract SCORE 2125, LEVEL 001 | Same | Same |
| Fire on port 2 starts level 1 | MEN 005, LEVEL 001 | Same | Same |
| Ctrl+A (the game's abort) | MEN 004, respawn | Same | Same |
| Ctrl+U (the game's level advance) | Level-end wipe, then LEVEL 002 | Same | Same: level-end wipe, then LEVEL 002 with MEN 004 and SCORE 0 |
| Sustained play (600 s seeded port-2 play) | Gold and trapped-enemy scores; men lost; game over then restart by fire | SCORE 150, 250 and 475 reached; game over then restart | Same pattern |

**Level advance, corrected 8 November.** The first version of this record
said Ctrl+U reached LEVEL 002 only in r1, and that in r2 it landed in a
death wipe and stayed at LEVEL 001. The independent review of the record
prompted a frame-by-frame recheck, which shows the two launches behaved the
same:

- The `r1-06-ctrl-u` and `r2-06-ctrl-u` grabs, about 9 s after Ctrl+U, both
  show the level-end wipe over level 1 with MEN 004 and LEVEL 001. No man is
  lost, so this isn't a death wipe.
- The first play grabs, `r1-p-000` and `r2-p-000` (30 s later), both show
  the level 2 layout with SCORE 0000000, MEN 004 and LEVEL 002. Random play
  can't finish level 1 without collecting gold, so SCORE 0 means Ctrl+U made
  the change. In r2, LEVEL 002 stays on screen for the next two grabs (SCORE
  150, MEN 002 and MEN 001).

VICE shows the same sequence at its `ref-06-ctrl-u` and `ref-p-000` grabs.
The frame hashes are in the JSON (`level_advance` and `vice_runs`). Case 2
therefore meets its declared objective in both launches.

- All 19 play grabs in each launch show the play field and status line,
  some during the wipe after a death or a game over. None is at the title
  or attract demo.
- SID effects were active in 120 and 112 of 764 one-second windows, against
  124 of 756 in VICE.
- Neither launch reached a qualifying high score, so the read-only disk
  never received a write attempt.
- **PASS:** the loader, joystick and keyboard, death and respawn, the level
  advance, sustained play and relaunch passed in both launches.

## Case 3: Metal Warrior 4 (runs `mw1`, `mw2`, `mw3`)

**Path.**

1. `LOAD"*",8,1` and `RUN` load the configuration menu.
2. The menu sets "FASTLOADER 1541/71/81/FD/HD" ON, so the game's own drive
   code runs on the native 1541.
3. "Load the game" shows the multicolour bitmap loading picture, then
   "PLEASE INSERT DISK SIDE 2 AND PRESS FIRE".
4. Side 2 goes in through `session/live-media`, and fire continues the load
   from side 2.

| Check | `mw2` r1 | `mw2` r2 | VICE |
| --- | --- | --- | --- |
| Config menu, fastloader ON | Yes | Yes | Yes |
| Loading picture, then the side-2 prompt | Yes | Yes | Yes |
| Live replacement (`session/live-media`) | 200, same session, generation 40 before and after, unit `ready` | 200, same session, generation 41, unit `ready` | Monitor `attach` |
| Fire: side 2 loads, then the credits and the difficulty menu | Yes (`r1-07-difficulty` `d4120284…`) | Yes | Yes |
| EASY: level loads, "WHAT'S THIS ALL ABOUT?" | Yes (`r1-08-ingame` `48d5406b…`) | Yes | Yes, the same scene and text |
| Play under port 2 (600 s seeded) | Player walks and the screen scrolls through the yard into the building; game menus (CARRY, FISTS, CREDITS) open | Same | Same |
| Live eject (`live-media/clear`) | 200, same session, generation 40, unit `empty` | Not scripted | n/a |
| Stop, then relaunch with side 1 | Idle, then a fresh generation | Idle | n/a |

- All 19 play grabs in each launch are in the game. r1 includes the
  in-game CARRY, START/SAVE/LOAD and FISTS menus.
- SID music was active in 715 and 724 of 900 one-second windows, against
  751 of 914 in VICE. Both kit recordings stop at the 900 s recorder cap;
  the VICE recording ran to 914 s.
- **PASS:** custom drive-code loading on the native 1541, live disk
  replacement, scrolling, sprites and raster screens, port-2 control,
  sustained play and relaunch, in both launches. The live eject passed in
  r1; it wasn't scripted in r2.

**Rerun `mw3` (8 Nov, Kit B, 17:34–18:07).** The `mw2` script with the eject
added to r2 and the play split into 240 s under a lossless cadence capture
and 360 s with grabs (`inputs/kit-mw3.jsonl`). Both launches went BASIC,
configuration menu, fastloader ON, loading picture, side-2 prompt, live
replacement (200, same session, generation 18 and 19 kept, unit `ready`),
credits, difficulty, EASY and into the game. All 22 play grabs are in the
game. After play, `live-media/clear` returned 200 in the same session and
generation with unit `empty` in **both** launches, then Stop went idle. So
the eject now passes in three launches (`mw2` r1, `mw3` r1 and r2), and
case 3 meets every per-run item in two independent launches of one run.

`mw1` r1 also loaded side 2 after a live replacement (generation 32) and
reached the game, through random play rather than the script. Two
transient host-side `session` reads in `mw2` returned 503 "Cannot uniquely
find the configured kit". The launches and play weren't affected.

## Harness failures (not core defects)

These are kept so the attempts can be read in full.

- `kik2` two-joystick check (7 Nov): presses landed in Kikstart's crash
  window. See case 1 and #1542.
- `kik1` (18:22–18:23): the ROM binding sent no ROM id and was rejected
  before launch. The kit was released unchanged.
- `kik2`, first launch: the driver typed `LOAD` with Shift held, which
  gives PETSCII graphics on a C64, and BASIC answered `?SYNTAX ERROR`. The
  driver was corrected live, the session stopped, and both counted runs are
  relaunches.
- `mw1`: the port-probe text failed at `(`, missing from the driver's key
  map. The probe was re-typed key by key at the end of the same claim.
- `mw1` r1: the live eject (`session/live-media/clear`) sent `{}`, but the
  host requires an empty body, so the request was rejected before the kit.
  The eject was never exercised in that launch. In `mw2` it ran and passed.
- `mw1` r1 and r2: the script sent two fires after the credits where three
  are needed (menu, START, EASY). The scripted part ended at the difficulty
  menu, and the random play chose the outcome: EASY into the game in r1, and
  CANCEL then QUIT back to a clean `READY.` in r2. r1 reached the game
  outside the script; r2 is INCONCLUSIVE. `mw1` is superseded by `mw2`,
  which uses the corrected script.
- `lode1` launch 1: a Kikstart port-isolation attempt made before Lode
  Runner in the same claim. Its presses also fell in the crash window
  (#1542). It's superseded by `kik4`.
- The audio recorder stopped at 900 s until the 8 November driver raised
  the cap to 1800 s. The `kik2` and `mw2` recordings are cut at 900 s; the
  `lode1` launches ended before the cap (764 s), and `kik4` (about 1038 s)
  ran under the new cap. Each launch's recorded seconds and a truncation
  flag are in the JSON.
- The 20 s videos are lossy x264. They show 60 fps capture (1200 frames)
  but don't prove frame-exact cadence. Run `mw3` measures cadence from
  lossless captures instead.
- VICE reference runs early on attached Deano's library D64s read-write.
  Lode Runner's high-score sector and MW4's config byte were written. Both
  images were reconstructed by inverting those writes, checked against the
  64-bit SHA-256 prefixes recorded before any run, and restored. Their full
  SHA-256s are in the JSON. From then on VICE only attached private
  copies. The references used here were all started from the original
  bytes.

## Not covered

These matrix exclusions stand, and nothing here extrapolates to them:

- tape and cartridges
- G64, copy-protected and non-standard GCR disks
- 40-track and error-info D64
- disk writes and saves
- second drives, 1571 and 1581
- fastload cartridges, REU and GEOS
- NTSC
- light pen and paddles
- analogue audio
- the opt-in reSID filter

Three titles are a bounded sample of the KERNAL, raw-sector and
custom-drive-code loader classes. They aren't a general compatibility
claim.

## Cadence and audio continuity

Measured in run `mw3`, both launches, during MW4 play. The tolerances were
written down at 17:39 on 8 November, while the run was in progress and
before any capture was analysed (they're in the JSON's `cadence_and_audio`).

**Video.** Each launch has a 240 s lossless capture: Kit B's 4KPRO HDMI
input scaled to 480×270 gray and written raw at its own frame rate, as
#1530's `cad1`. [tools/cadence.py](tools/cadence.py) gives #1530's metrics.
It adds a picture rule (any pixel more than 4 levels from the previous
frame), because the capture path adds ±1 level of noise, so almost every
frame differs at the byte level.

| | r1 | r2 | Tolerance |
| --- | --- | --- | --- |
| Frames, fps | 14385, 59.94 | 14397, 59.99 | 60 ± 0.5 |
| Black frames | 0 | 0 | 0 |
| Updates/s, #1530 rule (whole capture / walking 150–240 s) | 16.0 / 34.2 | 8.5 / 14.6 | — |
| Picture updates/s (whole / walking) | 16.2 / 34.5 | 9.5 / 17.0 | at most 50.1 |
| Longest static run, frames (whole / walking) | 1902 / 368 | 2161 / 871 | game states only |
| Steady scrolling: time, picture changes/s | 25.3 s, 49.7 | 6.6 s, 50.2 | C64 PAL 50.125 Hz |
| Repeat spacing in steady scrolling, output frames | 6 (209), 7 (20), 2–5 (15) | 6 (55), 7 (5), 4 (1) | — |

- The long static runs are the opening dialogue, which waits for fire
  between lines. The 12 frames sampled every 20 s show it, then walking.
- The core shows complete 50 Hz frames in 60 Hz output. While the
  background scrolls, the picture changes in a `11112` pattern: one repeat
  every 6 output frames, with an occasional 7 because PAL runs at 50.125 Hz
  against 60 Hz. [tools/scrollgap.py](tools/scrollgap.py) finds those
  stretches from the per-frame changed-pixel count.
- Inside steady scrolling, r1 has 4 longer gaps, of 3 to 6 frames, and r2
  has none. The background shift around each of those 4 goes from −1 to +1
  capture pixels per update: the player turned round and the game stopped
  scrolling. None shows the double step a dropped PAL frame would leave.
- Video **PASS** in both launches.

**Audio.** [tools/audiogap.py](tools/audiogap.py) counts dropouts in each
whole launch recording. A dropout is a run of at least 1 ms of identical
stereo samples, inside an active second, cutting into a signal that was
moving fast (mean step over the 8 samples before it above 32 LSB, and a
step into the run above 8 LSB).

| | `mw3` r1 | `mw3` r2 | VICE MW4 | Tolerance |
| --- | --- | --- | --- | --- |
| Dropouts | 0 | 0 | 0 | 0 |
| Samples against recorder wall time | −0.05 % | −0.08 % | — | within 0.2 % |
| Active 1 s windows | 749/923 (81.1 %) | 749/924 (81.1 %) | 751/914 (82.2 %) | within 5 points |

- Unlike reSID, the kit's digital SID holds an exactly constant value when
  no voice sounds. The first version of the rule flagged that, so the rule
  was tightened twice after the tolerances were declared:
  - Pass 1: a quiet tail stepping 1 LSB every few milliseconds was flagged.
    Mostly fixed by requiring a fast-moving signal before the run.
  - Pass 2: one event at 209 s in r1 remained, the output ringing down onto
    its silence level. Fixed by requiring a large step into the run.

  Every flagged event was inspected. With 10 injected 5 ms freezes in 60 s
  of `mw2` music, the final rule finds 8. The 2 it misses froze a slowly
  moving signal. `mw2` r1 also has 0 dropouts.
- Audio **PASS** in both launches, as digital HDMI audio. Analogue output
  isn't measured.

## Regression checks

**In CI.** `make -C sources/misteross sim-fes-c64-joyports` runs the real
`c64_machine` with a RAM program that leaves CIA1 as the KERNAL does after a
key scan (DDRA `$FF`, PA `$FF`, DDRB `$00`) and copies `$DC00` and `$DC01`
in a loop. The bench drives the `fes.gamepad.ports` bits and checks 18
cases:

- idle
- each of the five switches on player 0 (C64 port 2, `$DC00`) and on player
  1 (C64 port 1, `$DC01`) alone, with the other register unchanged
- B, Select and Start alone on each player, reaching neither port
- both players at once with different switches, and every switch on both
- a held key with all columns high, which reads nowhere
- port 2 fire with a key in matrix column 4, which reads in `$DC01`, as the
  real wired-AND matrix does

The target is in `sim-fes-c64-system-group`, so it runs in the nightly full
suite and whenever `sim-fes-c64` runs. With the two ports' bits swapped in
`c64_machine.sv` as a negative control, it fails 13 of the 18 cases.
Player 0 reaching the shell's port 0 is the mailbox contract
([computer I/O](../../../sources/mister-packages/docs/computer-io.md),
opcode 4), so together these cover the kit's "player 0 is port 2, player 1
is port 1" path below the host.

The open native-drive diagnostic (`sim-fes-c64-machine`) already covers the
1541 GCR-to-IEC transfer in CI.

**Manual, with private bytes.** The game cases need the Commodore ROMs and
the D64s, which are licensed to Deano and can't go into CI. They stay
bounded manual checks:

- **Kit.** [tools/run-c64.sh](tools/run-c64.sh) claims a kit under the
  kit rules, checks its baseline, runs
  [tools/c64run.py](tools/c64run.py) with a plan naming the package,
  firmware and D64s, cleans the FogCast data it created and releases the
  kit. Each `inputs/kit-<run>.jsonl` is a complete run of about 30–37
  minutes for two launches. `run-c64.sh` also calls Powerboat-only helpers
  (the kit ssh wrappers and lease probe from the pc386 acceptance tools), so
  it's a receipt of what ran, not a portable tool.
- **VICE.** [tools/vref.py](tools/vref.py) runs `x64sc` with the patch in
  this folder and an `inputs/vice-*.jsonl` script, on private copies of the
  D64s. [tools/gen.py](tools/gen.py) generates both kit and VICE scripts
  from one spec.
- **What to compare.** For Kikstart, the course 1 times (P1 72.54 and P2
  59.64 with the `kik4` script) and the two-joystick table in case 1. For
  Lode Runner, the attract SCORE 2125, MEN 5→4 on Ctrl+A and LEVEL 002 with
  SCORE 0 after Ctrl+U. For MW4, the side-2 prompt, the live replacement
  keeping the session and generation, and the "WHAT'S THIS ALL ABOUT?"
  scene.

Named grabs are taken at fixed times, not when a state is detected, so a
grab can show the screen before or after the one its label names. For
example, `mw2` `r2-03-picture` has the same hash as `r1-05-swapped`. Read a
label as a time in the script, not a checked state.

## Independent review

On 8 November a fresh reviewer was given only issue #1531, its matrix
comment, this record, the `fes.c64` status row and the record's tests, and
asked for gaps against the issue's closure terms, internal
inconsistencies, claims stronger than the evidence, and anything that
couldn't be reproduced. It found 12 issues. This revision addresses them:

- **Verdicts disagreed** (the JSON said PASS for Lode Runner, while this
  summary and the status row said QUALIFIED). Rechecking the frames showed
  the level advance passed in both launches; see case 2.
- **The MW4 eject was presented as covering both launches.** It's now
  stated per launch, and the rerun `mw3` ejected in both launches.
- **Input sequences and seeds weren't recorded.** Scripts, driver,
  event-log hashes and seeds are now committed or bound.
- **The VICE comparison had nothing behind it.** `vice_runs` now binds it,
  and the criteria are stated.
- **Sustained-play grabs weren't classified.** All three cases now have
  per-launch counts.
- **The audio cap statement was wrong.** It now names the runs it cut.
- **The 850 s figure couldn't be derived.** It's replaced by the 754 s
  derivation.
- **The `mw1` verdict was stated two ways.** Reconciled.
- **The superseded `lode1` launch 1 wasn't listed.** Added to the harness
  failures.
- **The library restore was overstated.** Now says 64-bit prefixes.
- **The status row was stronger than the record.** Qualified with the open
  items.
- **Bindings were missing.** Full toolchain commits, the firmware recipe,
  the seal record hashes and where the raw artifacts live are now recorded.

The reviewer's other notes, on the MW4 config write and the frame labels,
are answered in the save paragraph under Method and under Regression
checks. It also found that audio and video cadence was neither measured nor
claimed. Run `mw3` measured it (see
[Cadence and audio continuity](#cadence-and-audio-continuity)).

## Owed

Nothing from #1531's matrix. The MW4 eject and the audio/video cadence,
which the first revision of this PR listed here, passed in `mw3`. These
stay with their own issues, and this record doesn't stand in for them:

- #946 (display and launcher)
- #593 (receipt attestation)
- #1355 (named release-image acceptance)
