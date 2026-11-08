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
| 1 Kikstart: KERNAL load, both ports, split-screen, collisions, SID | **PASS** | Run `kik4` (8 Nov), both launches. KERNAL load, trainer, high-score file read, two players with two joysticks: each port drives only its own rider, and course 1 times are P1 72.54 and P2 59.64 on both launches and in VICE. 850 s or more in a race per launch, and the race completes back to the title. The 7 Nov `kik2` two-joystick failure was a harness timing error ([#1542](https://github.com/DeanoC/fes/issues/1542), closed as not an FES defect). |
| 2 Lode Runner: Brøderbund multi-file load, raw-sector level reads, joystick and keyboard | **QUALIFIED** | Both launches: the 1.5-minute original loader, the attract demo matching VICE (SCORE 2125), fire starting level 1, Ctrl+A costing a man (MEN 5→4), play scoring through input, game over and restart. Ctrl+U advanced to LEVEL 002 only in r1; in r2 it landed in the death wipe and stayed at LEVEL 001. The predeclared two-launch level-advance objective was not met. |
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
both players have joysticks. All 16 play grabs in each launch are mid-race,
for about 850 s in a race per launch. In the play grabs the two riders are
at different points of the course, so each port drives only its own rider.
Crashes and respawns happen throughout.

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
| Ctrl+U (the game's level advance) | LEVEL 002 | Pressed during the death wipe; stayed LEVEL 001 | LEVEL 002 |
| Sustained play (600 s seeded port-2 play) | Gold and trapped-enemy scores; men lost; game over then restart by fire | SCORE 250 and 475 reached; game over then restart | Same pattern |

- SID effects were active in 120 and 112 of 764 one-second windows, against
  124 of 756 in VICE.
- Neither launch reached a qualifying high score, so the read-only disk
  never received a write attempt.
- **QUALIFIED:** the loader, joystick and keyboard, death and respawn,
  sustained play and relaunch passed in both launches. Ctrl+U reached LEVEL
  002 only in r1; in r2 it landed during the death wipe and stayed at LEVEL
  001, so the predeclared two-launch level-advance objective was not met.

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
