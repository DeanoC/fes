# Spectrum game acceptance on Kit B, 7–8 November 2026

Issue [#1530](https://github.com/DeanoC/fes/issues/1530). Six real games ran
on the four Spectrum profiles, on fresh seals of main, on Kit B, and were
compared against Fuse. The matrix was published on the issue
([comment](https://github.com/DeanoC/fes/issues/1530#issuecomment-6707017631))
before any kit run. One change was made after review, and it was posted before
its runs ([amendment](https://github.com/DeanoC/fes/issues/1530#issuecomment-6721883046)).
C4's published objective asks for a fight, and Batman: The Caped Crusader
never met an enemy. With Deano's approval, C4 became Renegade, also a +3 disk.
The Batman runs stay as +3 disk-load evidence.

**Result.** The matrix is met, so this closes #1530:

- Every case has two passing runs, and each of those runs reached its
  declared objective.
- Each case's passing runs cover the controls the matrix names.
- Each passing run had 10 minutes or more of active play after the load. Time
  the game spends on its own name entry doesn't count as play.

| Case | Profile | Game, media | Controls | Verdict | Passing runs |
| --- | --- | --- | --- | --- | --- |
| C1 | 48K, `fes.spectrum` | Manic Miner, `.tzx` | keyboard | **pass** | `c1r1`, `c1r2` |
| C2 | 48K, `fes.spectrum` | Bubble Bobble, `.tzx` (Firebird turbo) | Kempston, Sinclair | **pass** | `c2r6` (Kempston), `c2r4` (Sinclair) |
| C3 | 128K/+2, `fes.spectrum128` | Bubble Dizzy 128K, `.z80` | Sinclair, Kempston | **pass** | `c3r7` (Sinclair), `c3r8` (Kempston) |
| C4 | +2A/+3, `fes.spectrum-plus3` | Renegade, `.dsk` (amended; Batman `.dsk` superseded) | Kempston, keyboard | **pass** | `c4r5` (Kempston), `c4r6` (keyboard) |
| C5 | Pentagon, `fes.pentagon` | Bomb Jack 2 (TR-DOS), `.scl` | keyboard, Kempston | **pass** | `c5r1`, `c5r3` (keyboard), `c5r4` (Kempston) |

**No core defects were found, and no issues were filed.** Every miss traced
to the test driver, to the title's own behaviour, or to Powerboat:

- **Manic Miner's in-game tune toggles with ENTER.** Fuse does the same.
- **Bubble Bobble's round 1 wasn't cleared by blind input.** Fuse cleared it
  with the same first pattern only once. The Fuse-chosen pattern cleared it in
  two of three valid kit runs.
- **Batman's control menu appears about 11 s after the part is chosen.** Fuse
  shows the same.
- **Bubble Dizzy's name entry needs fire.** This was a gap in the driver's
  pattern.
- **Bomb Jack 2's Kempston key is J, not D.** Fuse confirms it.
- **One run started without its media.** Powerboat's media disk failed to read
  the file once.
- **One run started without its package.** The driver's package lookup still
  pointed at the deleted seal worktree (`c3r5`).
- **The first C3 passes fell short of 10 minutes of play.** The game's own
  name entry took part of each window. C3 was rerun with 14-minute windows.
- **Batman met no enemy.** C4 was amended to Renegade.
- **Renegade's default keys aren't usable.** The keyboard run sets them through
  the game's own Define Keys first.

Failed attempts are kept below and in [`kit-runs.json`](kit-runs.json).

## Packages

The 26 Oct seals were cut from `9da56232`. Main's `top.v` has since gained the
shared shell's new ports, so those seals don't stand for main. Each model was
therefore resealed from main `607922c63`:

- The producer was `scripts/build_fes_spectrum_oss.py --gpu-router HIP --model …`.
- It ran outside `gpu-run`. The producer refuses the wrapper, because its
  nextpnr calls take their own GPU lease. Wrapped, it fails with "source list
  outside audited closure" (#881).
- Each seal passed every clock.

| Package | Package ID | `.fcore` SHA-256 | Build | Seed | System / pixel / audio MHz (target 52.22 / 74.25 / 12.29) | COMB / FF / M10K |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.6.0 | `5013dde2…` | `c4f0757d…` | `496f3050…` | 5 | 63.44 / 96.92 / 181.72 | 9451 / 4821 / 129 |
| `fes.spectrum128` 0.4.0 | `85665cca…` | `d32449dc…` | `91d3c00f…` | 5 | 68.90 / 94.07 / 189.25 | 10554 / 5092 / 225 |
| `fes.spectrum-plus3` 0.5.0 | `f362eb96…` | `e5a9f724…` | `b0431001…` | 2 | 56.51 / 77.96 / 238.83 | 14574 / 6650 / 257 |
| `fes.pentagon` 0.5.0 | `abb2f085…` | `7a5d52e0…` | `1e0e38a3…` | 5 | 61.40 / 93.79 / 151.79 | 12141 / 6273 / 241 |

**Why these seals stand for main `4b33ec559`.** Between `607922c63` and
`4b33ec559`, main changed 11 files, all in pc386, i386 or docs. None of them is
among the 64 source inputs listed in the four `build-inputs.json` files.

## Firmware and media

All firmware and game media are Deano's privately held copies, used on
Powerboat and the kit only. Nothing copyrighted is committed: no ROM, media or
frame. The record keeps hashes and measurements.

| Item | SHA-256 |
| --- | --- |
| 48K ROM (`spectrum-firmware`) | `d55daa43…` |
| 128 ROM (`spectrum128-firmware`) | `c1ff621d…` |
| +3 ROMs (`spectrum-plus3-firmware`) | `ee8218fa…` |
| Pentagon 128 + 48 BASIC + TR-DOS 5.03 (`pentagon-firmware`) | `3ef7872a…` |
| Manic Miner (Bug-Byte 1983) `.tzx` | `c2fc1173…` |
| Bubble Bobble (Firebird 1987) `.tzx` | `bbc71c2c…` |
| Bubble Dizzy (Codemasters 1991, 128K) `.z80` | `46753d76…` |
| Batman: The Caped Crusader (Ocean 1988) `.dsk` | `b40f6137…` |
| Renegade (Imagine 1987) `.dsk` | `4648cae0…` |
| Bomb Jack 2 (Elite 1987, Boys Soft TR-DOS release) `.scl` | `f327b317…` |

## Method

**Kit path.** Kit B ran core-only on its baseline software:

- Baseline: software `a8cd30c4`, from main `bb51f445`, OS `f904961e`.
- Nothing was installed, the image wasn't changed and the kit wasn't rebooted.
- The host's compatibility check accepted each package on that baseline.

A private FogCast host (the baseline's own `bb51f445` binary, private state, in
a container) targeted Kit B only. It ran the normal library flow for each run:

- Import the package and link the firmware.
- Create a fresh library game and select the media.
- Launch, then send keyboard and pad input through `/api/v1/session/input`.
- Stop, then relaunch the same library game.

Pads map as follows:

- Controller 0 is the core's built-in Kempston port.
- Controller 1 is Sinclair joystick 1.

**Machine reset.** Neither the host nor the kit menu has a machine-reset control
for computer sessions. So Stop followed by a relaunch stands for a cold reset
(each relaunch reboots to the ROM's first screen). Deaths, game overs and
in-game restarts stand for soft resets.

**Capture.** Frames and video came from Kit B's HDMI capture on Powerboat:

- Frames: 1920×1080 grabs, reduced to the 320×240 Spectrum picture and mapped
  to the 15 Spectrum colours. BRIGHT is folded, because the capture doesn't
  keep the two levels apart.
- Video: 60 fps windows of 20 s, stored as 480×270 grey.
- Audio: per-second RMS of the captured audio. The floor is about 97.

**Reference.** Fuse 1.6.0 (libretro `e997e2bc`, the project's Spectrum oracle)
ran the same media headless, under a private harness:

- It takes key presses and Kempston on controller port 0.
- It can loop a kit step pattern.
- It dumps frames and per-second audio RMS.

The comparisons are by 8×8 cells of the 256×192 paper area, by game state
(menu, room, round, lives, score) and by input outcome. No brightness or
"not black" check is used.

**Per run.** Each run did the following:

- Launch the library game, then load the media.
- Run a scripted control-menu path.
- Probe the input, then take a 20 s 60 fps cadence capture.
- Play a looped step pattern for 600 s, grabbing a frame every 15 s.
- Stop, relaunch, and grab again.

A few early runs were laid out differently:

- `c1r1` played in three legs.
- The loop was 560 s in `c1r2` and 570 s in `c2r1`.

Some later runs had longer loops:

- The C3 reruns `c3r7` and `c3r8` looped for 840 s, so that gameplay without
  the name entry passes 10 minutes.
- The Renegade runs `c4r5` and `c4r6` looped for 690 s.

Each run's play window (first game input to the end of play) is in
[`kit-runs.json`](kit-runs.json).

## C1 Manic Miner, 48K: pass (`c1r1`, `c1r2`)

1. **Load.** `LOAD ""` from the library tape, with the turbo load muted. The
   title showed within 60 s in `c1r1` and by 100 s in `c1r2` (still on
   "Bytes: mm1" at 60 s).
2. **Title.** The title matches Fuse frame 11500 in 733/768 (`c1r1`) and
   739/768 (`c1r2`) cells. The top 16 rows match 512/512; the rest is the
   scroller and the animated Willy.
3. **Input probe (`c1r1`).** The kit gives the same outcome as Fuse:
   - Walk, jump, then walk right into the guardian loses a life (2 → 1).
   - Willy respawns in Central Cavern.
   - AIR drains at the same rate (2 px in about 1.4 s).
4. **Play.** Ten minutes of play reached deaths, game over, the title and
   restarts.
   - The cavern matches Fuse frame 11699 in 483/512 and 479/512 cells of the
     play field. The difference is the moving sprites and AIR.
5. **Stop and relaunch.** Both ended back at the start:
   - Stop left the session and agent idle.
   - The relaunch cold-booted to "© 1982 Sinclair Research Ltd".
   - It then reloaded to the title, and ENTER restarted the cavern with two
     spare lives.
6. **Cadence.** Both runs had no black frames, and the longest still run was
   5 frames:

   | Run | Updates | Capture SHA-256 |
   | --- | --- | --- |
   | `c1r1` | 25.1/s | `ee88126c…` |
   | `c1r2` | 23.0/s | `0f461953…` |

7. **Audio.** The title tune measured 4500–6500 RMS, against Fuse's 4800–5100.
   - In `c1r1` the in-game tune seemed silent. Fuse shows why: ENTER toggles the
     tune off and on in play, and the play loop pressed ENTER.
   - So `c1r2` started with a 0.3 s ENTER tap and then listened for 15 s with
     no input. The tune played steadily at 753–854 RMS.

## C2 Bubble Bobble, 48K: pass (`c2r6`, `c2r4`)

**Load and menus.** The Firebird turbo loader reached the control menu in under 3 minutes. The
control menus match Fuse exactly, 768/768 cells each:

- The player 1 menu, then the player 2 menu.
- The title, "1 OR 2 TO PLAY".

**Declared objective.** Trap and pop enemies, lose a life, and clear round 1 so
that round 2 starts.

| Run | Controls | Pattern | Round 1 cleared | Verdict |
| --- | --- | --- | --- | --- |
| `c2r1` | Kempston | first blind pattern | no: round 1 only, for 9 min 56 s (4 s short of 10 min); pops (score to 1150), deaths, game overs, restarts | fail (objective) |
| `c2r2` | Sinclair | first blind pattern | no: same | fail (objective) |
| `c2r3` | Kempston | Fuse-chosen pattern 17 | no: round 1 only for 600 s; score to 9110 | fail (objective) |
| `c2r4` | Sinclair | Fuse-chosen pattern 17 | **yes**: round 2 starts about 6¼ min in; the kit's round 2 matches Fuse's in 711/768 cells; game over in round 2 | **pass** |
| `c2r5` | Kempston | Fuse-chosen pattern 17 | none: the driver couldn't read the `.tzx` from Powerboat's media disk, so the game launched with no tape and never loaded; the file read correctly 17 min later | invalid, not counted |
| `c2r6` | Kempston | Fuse-chosen pattern 17 | **yes**: round 1 cleared about 1½ min in and again near the end; the kit's round 2 matches Fuse's in 716/768 cells; game overs and restarts | **pass** |

**Pattern 17.** Blind input rarely clears round 1, so after `c2r1` a search in
Fuse picked a looped pattern that does:

- With cursor keys and step jitter, it cleared round 1 in 12 of 12 runs.
- With Kempston at eight start offsets, it cleared in 7 of 8 ten-minute runs.
- It still cleared in 7 of 8 with the kit's measured step latency added. On the
  kit, one loop takes 4.93 s against 4.5 s in Fuse.

The kit cleared round 1 in two of the three valid runs with it, close to Fuse's
rate. Fuse also cleared round 1 with
the first blind pattern in one run: it picked up the umbrella and skipped to
round 5.

**Black frames.** Each Bubble Bobble cadence capture begins with the loop's "1"
press. Each holds one black block right after it, which is the game's own
blank at game start (`c2r1` was checked frame by frame). In `c2r1`,
`c2r2`, `c2r3` and `c2r6` the block lasts 26–30 frames at 60 fps (0.43–0.5 s). Fuse blanks
for exactly as long as "1" is held: 12 frames for a 0.1 s press and 23 frames
(0.46 s) for the kit's 0.3 s press. In `c2r4` it lasts 49 frames (0.82 s). That fits a late key release, but the
driver doesn't log event times, so it isn't checked. Holding the Sinclair fire
key during the blank doesn't lengthen it in Fuse. There are no other black
frames.

**Audio.** In play the kit and Fuse (with the same pattern) agree closely:

| Measure | Kit `c2r1` | Fuse |
| --- | --- | --- |
| Median | 594 | 528 |
| p90 | 2018 | 2033 |
| Maximum | 2542 | 2507 |
| Active seconds | 62% | 64% |

## C3 Bubble Dizzy, 128K: pass (`c3r7`, `c3r8`)

**Load and menu.** The `.z80` snapshot launches to the menu. The AY title music
measured 2678–2958 RMS against Fuse's 2454–2940 (up to 2969 and 3024 in the
reruns). The menu option chooses the stick: 2 for Sinclair, 3 for Kempston.
"0" starts the game.

**Declared objective.** Rise on the bubbles, lose a life, and continue.

**Play time.** Each game over leads to the game's own name entry, which the
input loop drives. Name entry isn't play, so it doesn't count toward the 10
minutes. The first runs had 10-minute windows, and their gameplay fell short.
The reruns `c3r7` and `c3r8` had 14-minute windows.

The table splits each run's window into gameplay and name entry, counted from
the 15 s grabs:

| Run | Controls | Gameplay | Name entry | Verdict |
| --- | --- | --- | --- | --- |
| `c3r1` | Sinclair | 6¾ min: two games, scores to 77,300 | 3 min: "HMRRRR…" entered, back to the menu | fail (gameplay under 10 min) |
| `c3r2` | Kempston | 3¼ min: score to 79,400 | 6½ min, stuck: no letter could be entered | inconclusive (driver) |
| `c3r3` | Sinclair | about 8 min 40 s: two games, scores to 42,400 and 151,200 | 1¾ min: the name was entered and is listed in the high-score table | fail (gameplay under 10 min) |
| `c3r4` | Kempston | about 8 min 15 s: two games, scores to 54,800 and 92,900, GAME OVER | 2¼ min: the name was entered with Kempston fire and is listed | fail (gameplay under 10 min) |
| `c3r5` | Sinclair | none: the driver's package lookup pointed at the deleted seal worktree, so nothing launched | none | invalid, not counted |
| `c3r7` | Sinclair | about 12 min (48 of 56 grabs): lives lost, two games end, each followed by the ship cut-scene and "GET READY, DIZZY! ABYSS 1"; a third game under way at the end | 2 min (8 grabs), names entered with Sinclair fire | **pass** |
| `c3r8` | Kempston | about 11¼ min (45 of 56 grabs): lives lost, games end and restart through the cut-scene | 2¾ min (11 grabs), names entered with Kempston fire | **pass** |

**Why `c3r2` stuck.** The play pattern had no fire step:

- Sinclair fire is key 0, and the pattern pressed 0, so `c3r1` could enter
  letters.
- Under Kempston, nothing in the pattern selected a letter.

A fire step was added for the reruns.

**Cadence.** No black frames in `c3r7` or `c3r8`, at 13.7 and 13.6 updates/s.

## C4 Renegade, +3: pass (`c4r5`, `c4r6`)

**Why this title.** C4 was published as Batman: The Caped Crusader, with the
objective "walk Batman through at least one room exit, and fight". Batman met
no enemy: in Fuse, three 10-minute input patterns stayed in the Batcave rooms.
Reaching a fight needs puzzle routing, which blind input won't do. With
Deano's approval, the [amendment](https://github.com/DeanoC/fes/issues/1530#issuecomment-6721883046),
posted before any Renegade run, swapped C4 to Renegade. It is a single-sided
+3 disk whose first screen is a fight.

**Load and menus.** The disk boots through the +3 menu's Loader to the title,
the high scores and the control menu. On the kit, both runs' control menus
and relaunch menus match Fuse frame 1450 in 768/768 cells.

**Declared objective.** Fight the gang on the first stage, with each knock-down
scoring 50. Lose lives, reach game over and restart.

**Controls.**

- `c4r5` chose Kempston (menu 4) and started with fire.
- Renegade's default keyboard keys aren't usable: in Fuse no letter or digit
  moves the player, and S pauses. So `c4r6` used the game's Define Keys (menu
  5) to set left O, right P, down A, up Q, hit M and pause H, then started
  with 1.
- On the kit, the Define Keys screens before and after match Fuse frames 2050
  and 2650 in 768/768 cells, and the first stage matches Fuse frame 2060 in
  768/768.

| Run | Controls | Result | Verdict |
| --- | --- | --- | --- |
| `c4r5` | Kempston | about 11 min of fighting (44 of 46 grabs; the other 2 are the high-score screen between games): knock-downs, scores up to 500 per game, the energy bar drains, lives lost, three GAME OVERs with the high-score screen and restarts | **pass** |
| `c4r6` | keyboard | about 11½ min of fighting (46 of 46 grabs): knock-downs, scores up to 650 per game, lives lost, GAME OVER and restarts | **pass** |

**Fuse with the same inputs.** Ten-minute runs scored up to 550 per game on
Kempston and up to 1650 on keyboard. Each looped through game over and
restart in the same way.

**The game-over wipe.** One `c4r5` grab shows white bands across the
background under "GAME OVER". That is Renegade's own screen wipe. Fuse draws
the same bands at frame 6456 of a game over.

**Audio.** Sound was on in 63% and 64% of seconds, against Fuse's 62% and 63%
with the same patterns. The level is about 0.8× Fuse's: median 782 and 717
against 959 and 918, p90 about 1800 against 2120.

**Cadence.** No black frames, at 15.0 and 23.8 updates/s.

## C4, superseded: Batman: The Caped Crusader, +3 disk loads

These runs no longer count toward C4's objective. They still stand as +3 disk
evidence, for a second disk title through the Loader.

**Load and menus.** The disk boots through the +3 menu's Loader to the part
menu. The control menu matches Fuse frame 2550 in 768/768 cells, and the
Batcave start matches Fuse frame 2700 in 742/768 (Batman's sprite position
differs).

**Published objective.** Walk Batman through at least one room exit (a scene
change), and fight.

| Run | Controls | Result | Verdict |
| --- | --- | --- | --- |
| `c4r1` | Kempston | sat at the control menu for all 600 s | inconclusive (driver) |
| `c4r2` | Sinclair | same | inconclusive (driver) |
| `c4r3` | Kempston | The Batcave → "WAY OUT…" in the first minute, then moving in that room for 10 min; no enemy, so no fight | superseded (no fight) |
| `c4r4` | Sinclair | The Batcave → the next room the other way, then moving in it for 10 min; no enemy, so no fight | superseded (no fight) |

**Why `c4r1` and `c4r2` stuck.** They sent the control key 3 s after choosing
part 1, while the loading picture and its music still showed. In Fuse the
control menu appears 11 s after the part key (frames 1600 → 2150). The reruns
wait 15 s.

**Audio.** The menu music plays, and play is silent on the kit and in Fuse.

**Black frames.** None in any +3 capture. (#1003 was an earlier +3 HDMI
dropout.)

## C5 Bomb Jack 2, Pentagon: pass (`c5r1`, `c5r3`, `c5r4`)

**Load and menu.** The `.scl` boots through TR-DOS: "Version by BOYS SOFT'93",
"INFINITE LIVES Y/N?" (N), then the control menu and "1" to start.

**Declared objective.** Move, jump and fight, then lose a life and continue.

| Run | Controls | Result | Verdict |
| --- | --- | --- | --- |
| `c5r1` | keyboard (K) | Jack moves around the platforms, scores 500, loses lives; game over ("ИГРА ОКОНЧЕНА"), the high-score table, restart | **pass** |
| `c5r2` | Kempston, chosen with D | Jack barely moves; a few points; lives lost | inconclusive (driver) |
| `c5r3` | keyboard (K) | Jack moves to the upper and right platforms, scores 500, loses lives; game over, restarts | **pass** |
| `c5r4` | Kempston, chosen with J | Jack moves round the platforms on Kempston, scores 1200; game overs ("ИГРА ОКОНЧЕНА"), restarts | **pass** |

**Fight.** Both the keyboard and Kempston patterns include fire presses. In
Fuse, ten minutes of the keyboard pattern scored up to 700 per game. With its
M, Z and X presses replaced by pauses, the same pattern scored at most 400.
The kit's passing runs reached 500, 500 and 1200. The 15 s grabs don't catch a
single punch frame, so this is the fight evidence: score above what movement
alone earns in Fuse.

**Why `c5r2` failed.** The menu letters are KOI-7 Cyrillic. In Fuse with
working Kempston, only J of the 27 keys tried moves Jack: this release's
Kempston key is J. With D, Jack barely moves in Fuse as well. The rerun selects
J.

**Audio.** The kit's sound is on in 45% of seconds against Fuse's 49% with the
same keyboard pattern, at about 0.45× Fuse's level.

## Observations, not failures

- **Relative loudness.** Kit-to-Fuse RMS ratios, measured with the same media
  and patterns:

  | Sound | Ratio |
  | --- | --- |
  | Manic Miner title tune | about 1.1× |
  | Bubble Bobble play | about 1.0× |
  | Bubble Dizzy AY title | about 1.0× |
  | Manic Miner in-game tune | about 0.45× |
  | Bomb Jack 2 effects | about 0.45× |
  | Renegade play | about 0.8× |

  A plain scale factor would be constant, so this isn't one. The cause isn't
  known, and it wasn't investigated.
- **Load-time sound.** The turbo loads were muted, as designed. Native-speed
  loading is #1252.
- **After Stop.** A grab 3 s after Stop sometimes still shows the last game
  frame. The next launch always cold-booted.

## Kit housekeeping

- **Kit and baseline.** Kit B, core-only on baseline software `a8cd30c4`. No
  install, no image change and no reboot.
- **Claim and release.** Each run took the kit's lock and claimed it in
  `kitB-STATUS.md`. It released only after these checks:
  - The post-run snapshot matched the probe: image keys, `launcher.json`, the
    FAT idle hash and `usb-storage.quirks=152d:0583:i`.
  - The agent was idle with a free lease.
- **Cleanup.** Every run's new FogCast data paths were removed and checked: 0
  left after each run. Kit B was left FREE at 09:29 EET on 8 November, and again
  at 16:06 EET after the review reruns (`c3r5`–`c4r6`).
- **Run windows.** In [`kit-runs.json`](kit-runs.json).
- **Kept on Powerboat.** Frames, captures, logs, the Fuse harness and the seal
  artifacts are in `~/fes-scratch/spec1530`. The private host's ROM and media
  copies were deleted.
