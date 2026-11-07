# C64 PAL game acceptance, 7 November 2026

Issue [#1531](https://github.com/DeanoC/fes/issues/1531): repeatable,
representative real-game acceptance for the C64 core, in PAL scope only, on
DE10-Nano. NTSC is out of scope.

The game and media matrix was
[published on the issue](https://github.com/DeanoC/fes/issues/1531#issuecomment-6707439997)
before the first kit claim. It has three titles, two independent library
launches each, at least 10 minutes of active play per case, and a declared
objective per case.

All runs were on Kit B. Kit A stayed held by Lander's #1490 restore all day.
Each case is compared with VICE 3.9 `x64sc` using the same ROMs and disks,
on semantic outcomes: screens reached, on-screen counters and course times,
and progress under input. No brightness or non-black checks are used.

**Result.**

| Case | Result | Notes |
| --- | --- | --- |
| 1 Kikstart: KERNAL load, both ports, split-screen, collisions, SID | **FAIL** (two-port objective) | Load, trainer, high-score file read, course completion and respawn all match VICE: course 1 Player 1 72.54 and course 2 79.42 on both kit and VICE, identical across launches. In two-joystick mode, though, both riders follow either port, which VICE doesn't do: [#1542](https://github.com/DeanoC/fes/issues/1542). |
| 2 Lode Runner: Brøderbund multi-file load, raw-sector level reads, joystick and keyboard | **PASS** | Both launches: the 1.5-minute original loader, the attract demo matching VICE (SCORE 2125), fire starting level 1, Ctrl+A costing a man (MEN 5→4), play scoring through input, game over and restart. Ctrl+U advanced to LEVEL 002 in r1 and in VICE; in r2 it landed in the death wipe (script timing). |
| 3 Metal Warrior 4: custom IRQ fastloader with its own 1541 drive code, live side-2 replacement | **PASS** | Run `mw2`, both launches. Fastloader on, the multicolour loading picture, then the side-2 prompt answered by a live replacement in the running session (same session and generation, unit ready). Side 2 loads through the game's own drive code into the game, followed by 600 s of play and a live eject (unit empty, same generation). Every scripted screen matches VICE. `mw1` r1 also reached the game; `mw1` r2 is a harness INCONCLUSIVE (see below). |

This record refers to #1531 and doesn't close it. What's still owed is
listed under [Owed](#owed).

## Package and kit

- **Package.** `fes.c64` 0.5.0, package
  `668a640d25bfdf7bbf464e93f879df78ecdfecb01cf3e3406ade2ab30f13d692`
  (archive `360eb61f…`, RBF `fdff714f…`, build `c8928c51534a5e75960d9da5bf25500a`).
  - It was sealed today from main `607922c63` with the registered producer
    (`build_fes_c64_oss.py`) and the core's lock (Yosys `3a5accbd`, nextpnr
    `5fe55f3d`, Mistral `8fcc4cb4`).
  - HIP router, placer seed 2. Seeds 5 and 4 ran out of their 600 s route
    lease, as they did for the 24 October seal.
  - Achieved against required: system 57.10/52.224 MHz, pixel
    74.84/74.25 MHz, audio 186.32/12.288 MHz.
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
- **Firmware.** 36 KiB, `01173de9…`, composed in memory from Deano's C64
  Forever 10 Plus ROM set. It's the same bytes as the 23 October Kit A
  diagnostic.
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

## Method

Each case ran from one script. A scripted part reaches the objectives:

- BASIC `LOAD"*",8,1` and `RUN` typed as keys
- menu keys
- joystick holds
- grabs at fixed points

Seeded pseudo-random joystick play follows, with an HDMI grab every 30 s.
The same script, mapped to VICE's input, produced the reference.

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
  - MW4 writes its config into file `00`.

  None of those writes are expected to persist on the kit. Users get no
  save and reload from a D64.

## Case 1: Kikstart (run `kik2`)

**Path.** KERNAL `LOAD"*",8,1` loads the cracked one-file version through
the native 1541 (about 55 s to `READY.`). Then:

1. `RUN`, then Space past the trainer screen.
2. `L` loads the high-score file from disk (a second KERNAL load).
3. `N` and `N` answer the trainer's two questions.
4. On the title, F1 F1 selects "two players, two joysticks" and F7 starts
   the split-screen race.

Every one of those screens matched VICE.

**Course times.** These are the race's own counters. Both kit launches gave
identical times:

| | Kit `kik2` r1 | Kit `kik2` r2 | VICE (twice) |
| --- | --- | --- | --- |
| Player 1, course 1 | 72.54 | 72.54 | 72.54 |
| Player 2, course 1 | 71.66 | 71.66 | 62.56 |
| Course 2, both riders | 79.42 | 79.42 | 79.42 |

Player 1's course 1 and both riders' course 2 match exactly. Player 2's
course 1 differs because of the two-port fault below. In both games, crashes
on obstacles were followed by respawn and the riders went on to the next
course.

**Two-joystick fault ([#1542](https://github.com/DeanoC/fes/issues/1542)).**
The isolation test, in run `lode1` (prefix `iso`), held each port Right for
8 s.

- In VICE, port 2 drives only Player 2 and port 1 only Player 1.
- On the kit, both riders follow either port.

A BASIC loop printing `PEEK(56320);PEEK(56321)` on the same claim and
package (run `mw1`, prefix `pp`) matched VICE for every port, direction and
fire combination. That rules out host routing, agent port mapping and a lost
first gamepad event. At the moments VICE was sampled, the game's CIA1 setup
matches the KERNAL's (`7f ff ff 00`). So the fault depends on how the game
accesses the ports during play, and the mechanism is still open in the
issue. The two-port objective fails. Single-port play is unaffected.

**Sustained play and audio.**

- `kik2` r1: 685 s in a race (about 235 s of held throttle, then three of
  four 150 s play blocks).
- `kik2` r2: about 535 s in a race. The first play block of each run sat on
  the title screen, because F7 was pressed while race 3 was still running,
  and r2 lost one more block the same way.
- SID music was present in 779 and 778 of the first 900 one-second windows,
  against 875 of 987 in VICE. The recorder's 900 s cap cut both kit
  recordings short.

A rerun with the fixed play script (hold the throttle until the race ends,
then F1 F1 F7 before each block) was queued and then cancelled to free Kit B
for the other sessions. The case fails on the two-port objective either way,
and #1542 needs a Kikstart rerun once it's fixed. So `kik2` r2 stays at
about 535 s of in-race play, under the 10-minute target.

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
| Ctrl+U (the game's level advance) | LEVEL 002 | Pressed during the death wipe; stayed LEVEL 001 | LEVEL 002 |
| Sustained play (600 s seeded port-2 play) | Gold and trapped-enemy scores; men lost; game over then restart by fire | SCORE 250 and 475 reached; game over then restart | Same pattern |

- SID effects were active in 120 and 112 of 764 one-second windows, against
  124 of 756 in VICE.
- Neither launch reached a qualifying high score, so the read-only disk
  never received a write attempt.
- **PASS:** load, joystick and keyboard, death and respawn, a level change
  (in r1), sustained play and relaunch.

## Case 3: Metal Warrior 4 (runs `mw1`, `mw2`)

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

- SID music was active in 715 and 724 of 900 one-second windows, against
  751 of 914 in VICE.
- **PASS:** custom drive-code loading on the native 1541, live disk
  replacement, eject, scrolling, sprites and raster screens, port-2 control,
  sustained play and relaunch.

`mw1` r1 also loaded side 2 after a live replacement (generation 32) and
reached the game. Two transient host-side `session` reads in `mw2` returned
503 "Cannot uniquely find the configured kit". The launches and play
weren't affected.

## Harness failures (not core defects)

These are kept so the attempts can be read in full.

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
  CANCEL then QUIT back to a clean `READY.` in r2. Those launches are
  INCONCLUSIVE for the in-game objective. `mw2` uses the corrected script.
- The audio recorder stops at 900 s, so runs longer than that have
  truncated audio.
- The 20 s videos are lossy x264. They show 60 fps capture (1200 frames)
  but don't prove frame-exact cadence.
- VICE reference runs early on attached Deano's library D64s read-write.
  Lode Runner's high-score sector and MW4's config byte were written. Both
  images were reconstructed exactly, verified against SHA-256s recorded
  before any run, and restored. From then on VICE only attached private
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

## Owed

- The two-joystick fault, [#1542](https://github.com/DeanoC/fes/issues/1542).
- A Kikstart rerun with the fixed play script once #1542 is fixed, for two
  launches with at least 10 minutes in a race.
- Independent review of this record, as #1531's closure requires.
- Deterministic regression cases:
  - The Kikstart course times and the Lode Runner attract score are
    deterministic on the kit, so they make natural host-simulation or CI
    candidates.
  - The scripts and drivers are kept private on Powerboat in
    `~/fes-scratch/c64acc`. Promoting them is follow-up work.
- Coordination with #946 (display and launcher), #593 (receipt attestation)
  and #1355 (named release-image acceptance). This record is a
  current-package hardware run and doesn't stand in for those.
