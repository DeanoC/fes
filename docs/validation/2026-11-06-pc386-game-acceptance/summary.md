# pc386 game acceptance checks owed on #1368, 6 November 2026

Issue [#1368](https://github.com/DeanoC/fes/issues/1368), part of the M5
tracker [#1267](https://github.com/DeanoC/fes/issues/1267). This record
follows [#1481](../2026-11-04-pc386-games-smoke/summary.md) and
[#1486](../2026-11-04-pc386-m5-game-checks/summary.md), and covers the
gameplay checks they left owed. Four Kit B runs, `b1`, `i1`, `d1` and `w1`,
used one sealed package of main's pc386 tree, at the `max` preset.

**Result.** Every owed gameplay check passed:

| Check | Result | Run |
| --- | --- | --- |
| Ultima VII: The Black Gate, save then load after a relaunch | **Pass**: same place, slot listed, save file matches the game state | `b1` |
| Ultima VII Part Two: Serpent Isle, save then load after a relaunch | **Pass**: same place, slot listed, save file matches the game state | `i1` |
| DOOM v1.9, a level played to its exit | **Pass**: E1M1 exit switch, tally, then E1M2 starts | `d1` |
| Wolfenstein 3D, a level played through | **Pass**: E1L1 elevator, tally, then floor 2 starts | `w1` |
| FreeDOS date and time | **Blocked** on [#1279](https://github.com/DeanoC/fes/issues/1279): Kit B still runs image `f904961e` | none |

These items on #1368 are still open, and this run doesn't close them:

- Ultima VI walking speed at the 286-12 preset against a period or emulator
  reference. The speaker pitch was done in #1481, and there is still no
  reference.
- Ultima VII NPC pace, which #1486 listed with save and load. It also has no
  reference, and this run didn't measure it.

So this PR refers to #1368 and doesn't close it.

## Package

- **Package.** `254b2bf566fe1e134e635dc9e9a685829de6574d399337ca4dcf76e1ac59e9b1`
  (file SHA-256 `2bbc94ec…`), the Quartus 17.0.2 seal (seed 1) that #1515
  made of its merge commit `317310c81`.
- **Timing.** Every analysis passed. Worst slacks: setup +0.410 ns,
  hold +0.102 ns, recovery +6.781 ns, removal +0.618 ns and minimum pulse
  width +1.122 ns. Clocks: system 61.44 MHz, pixel 74.27 MHz.
- **Why this seal stands for main.** All 112 sealed inputs in its
  `build-summary.json` hash identically on main `5e201aa3b`, and the
  inputs were checked again just before the first claim. Between
  `317310c81` and `5e201aa3b` only documentation changed.
- **Content.** Phase 4 slices 2–4 (BTB, return stack and the store-to-load
  check) and the #1504 VGA BIOS scroll fix.

## Disks

All four disks are main's FreeDOS games image plus one game. The image was
built with `scripts/build_pc386_freedos_image.py --profile games`, giving
`36f50d52…`, which includes MOUSEDEF. Each game comes from Deano's eXoDOS 6.04
zips. Nothing from a game is committed.

| Disk | SHA-256 | Game zip SHA-256 | Notes |
| --- | --- | --- | --- |
| Black Gate | `19365b10…` | `75e6eb68…` | `C:\ULTIMA7` |
| Serpent Isle | `f10ab1f9…` | `5601ca4e…` | `C:\SERPENT`, with SERPENT.CFG and TEST.CFG replaced by the plain Sound Blaster config (`13ad7bdb…`) from #1426 |
| DOOM | `a6270d18…` | `06ee36b4…` | `C:\DOOM`, top-level files; DOOM.WAD `ff2c301b…` |
| Wolfenstein 3D | `d5236fa4…` | `f451d853…` | `C:\WOLF3D` (the Activision 1.4 release) |

Each game launch came after a SCRUBX scrub (`8538495a…`), and so did every
relaunch. The kit's baseline software predates #1499, so a load doesn't
clear guest RAM by itself.

## Ultima VII: The Black Gate (`b1`)

1. A new game: the introduction was skipped to the menu, then Start New Game,
   the name "Avatar" and Journey Onward.
2. The Trinsic opening. Iolo's line ends, the view stays still (#1504's fix),
   and Finnigan's "Dost thou accept?" takes a click on Yes.
3. The Avatar walked south-east to a spot by the town wall, so the save point
   differs from the start.
4. `S` opens the save dialog, and slot 1 was saved as "fes1368".
   - Frame at the save point: `f6e14b18…`.
5. Stop, scrub, then a relaunch of the same library game and disk.
   `ULTIMA7`, Esc to the menu, then Journey Onward.
6. The game comes back at the save point.
   - Loaded frame: `e8084d54…`.
   - 0.15% of pixels differ from the save-point frame, and that is the mouse
     cursor.
   - `S` lists "fes1368" in slot 1 (frame `c3cbe88e…`).
   - The Avatar then walks on (frame `1cca37e9…`).
7. **The save file on the disk delta.** The delta was applied to the disk
   image on Powerboat:
   - `ULTIMA7\GAME00.U7` (SHA-256 starts `039c2f7e4ebb4055`) is new, and its name field
     reads "fes1368".
   - All 175 of its FLEX entries match the `GAMEDAT` files byte for byte, so
     the slot holds the game state the relaunched game read.
   - The extracted files were deleted afterwards.

## Ultima VII Part Two: Serpent Isle (`i1`)

1. A new game: Start New Game, the name "Avatar" and Journey Onward.
2. The ship scene. Iolo's speech was clicked through, then the Avatar walked
   to the ship's prow.
3. The save:
   - The first SAVE click landed on the dialog's last slot, so that save
     dropped its name and didn't save.
   - The second try saved slot 1 as "fes1368si".
4. Stop, scrub, relaunch, `SERPENT`, Esc, then Journey Onward. The game comes
   back on the prow.
   - The whole frame differs by 22.7% from the frame taken at the save.
     Two frames taken at that same spot before the relaunch already differ
     by 23.5%, because of the animated surf and moving cloud shadows.
   - Over a 300×300 crop around the Avatar, the loaded frame differs by 0.8%
     from the first save-point frame.
   - Frames: save points `ada15482…` and `2e5299ff…`; loaded `2a0a76ef…`.
   - `S` lists "fes1368si" in slot 1 (frame `a56f4c7e…`).
5. **The save file on the disk delta.** `SERPENT\GAME00.U7` (SHA-256 starts `4200586af8b45908`)
   is named "fes1368si", and all 177 of its FLEX entries match `GAMEDAT`.

## DOOM v1.9 (`d1`)

DOOM was started from its menu: New Game, Knee-Deep in the Dead, Hurt Me
Plenty. It was played with `IDDQD` (god mode) and the pistol only. No warp,
no-clip or monster switches were used.

- **Route.**
  1. North out of the start room.
  2. East through the door to the computer room, and around the computer
     stand.
  3. East through the hall's exit to the lift.
  4. Down onto the zigzag over the nukage.
  5. South down the corridor and through its door into the exit hall.
  6. Through the exit door to the switch at x = 2912.
- **How it was steered.** `IDMYPOS` after each leg gave the exact position and
  angle. The legs were planned from the map's line and sector data, read on
  Powerboat; none of it is committed.
- **Monsters.** Zombies and imps blocked the way several times and were shot.
- **Exit.** The switch press was captured at 60 fps, and the capture shows
  the level ending:
  - The tally: "Hangar Finished", kills 100%, items 16%, secrets 0%, time
    40:04 (frame `b3b57cf8…`).
  - Space then gives "Entering Nuclear Plant", the screen melt and E1M2 (frame
    `f06f29b7…`).
  - Walking in E1M2 gives 28 new pictures in the second of movement.
- **Captures** (480×270 gray at 60 fps):
  - `exit60c`: 2400 frames, `2dfb36e1…`.
  - `e1m2`: 1800 frames, `3f77cb0a…`.

## Wolfenstein 3D (`w1`)

The game was started as `wolf3d -goobers`, then New Game, episode 1, "Can I
play, Daddy?".

- **Debug keys.** Left Shift+Alt+Backspace enabled them ("Debugging keys are
  now available!").
  - Tab+G turned god mode on (frame `7a7956c3…`).
  - Tab+F gave the exact position and angle after each leg (first readout
    X 1933312, Y 3768320, A 0, which is the start tile facing east;
    frame `e8edd922…`).
- **Route.** E1L1, 126 tiles to the normal exit (the elevator).
  It was planned by a grid search over the level's own wall and object
  planes, read on Powerboat.
  - It passes through 11 doors and then the elevator door.
  - Seven guards were shot where they stood in the way.
  - Turns used mouse counts (8 a degree with this CONFIG.WL6). Walks used
    held Up (5.61 tiles a second).
- **Exit.** The elevator switch press was captured at 60 fps.
  - "Floor 1 completed": time 30:03, kill ratio 63% (frame `f533d12b…`).
  - "Get Psyched!", then floor 2 starts, and walking moves the view (frame
    `7d3d64cb…`).
- **Captures:**
  - `wexit60c`: 1800 frames, `65d91498…`.
  - `wfloor2`: 1199 frames, `7ba8fa76…`.

## Observations, not failures

- **Black capture on some Kit B loads.** In run `w1`, 4 of 9 loads captured
  pure black (maximum level 0) for the whole load, BIOS and DOS prompt
  included.
  - A later load of the same package and disk, in the same session,
    captured normally.
  - The game's audio still played during the black loads, and runs `b1`, `i1`
    and `d1` (10 loads) had none.
  - This looks like the 4KPRO capture or the HDMI link not locking, rather
    than the machine. It wasn't investigated.
- **The two games read the mouse differently.**
  - In Wolfenstein 3D the counts turned the view exactly, at 8 counts a
    degree.
  - In DOOM, turns by mouse counts didn't match the counts sent. So DOOM
    turned with held arrow keys (about 54° for a 0.5 s hold) and was checked
    with `IDMYPOS` after each turn. This wasn't investigated.
- **Driving notes.**
  - Monsters standing in the path stop movement, so a leg that falls short
    usually means something is in the way.
  - A second Use on an open Wolfenstein 3D door closes it.
  - DOOM's walk momentum carries about 100 units past a short tap.

## Kit housekeeping

- **Runs.** Kit B, core-only on baseline software `a8cd30c4` (main
  `bb51f445`, OS `f904961e`): no install, no image change, no reboot.
  - Each run claimed the kit in its status file and released it only after
    the post-run snapshot matched the probe and the agent was idle with a
    free lease.
  - Run windows (EET): `b1` 18:52–19:07, `i1` 19:08–19:21, `d1` 19:26–20:11,
    `w1` 20:12–20:58.
- **Cleanup.** After each run, a housekeeping pass removed exactly the
  FogCast data trees that the run created, each checked against the run's
  new-path list: 5, 5, 5 and 10 trees. The shared scrub image was kept.
  Kit B was left FREE at 20:58.
- **What stays on Powerboat.** Frames, captures, disk deltas, run logs and the
  route tools are in `~/fes-scratch/acc1368`.
