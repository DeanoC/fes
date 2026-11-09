# Atari ST game acceptance on main's package, 8 November 2026

Issue [#1528](https://github.com/DeanoC/fes/issues/1528). This record reruns the
cases of the [7 November record](../2026-11-07-atari-st-game-acceptance/summary.md)
on a package sealed from main. The earlier runs used factory package `2dd2271c`
(fes `f53eba76`), because main's seal stopped at the producer's read audit until
[#1544](https://github.com/DeanoC/fes/pull/1544) merged.

Both runs, `m1` and `m2`, were on Kit A, each in one claim. They used the
same TOS 1.04, the same private disks and the same private host as the
earlier record. Hashes are in [`kit-runs.json`](kit-runs.json).

**Result.** All four cases pass two runs on main's package. Each run has at
least 10 minutes of play and a declared objective:

- joystick (Xenon)
- mouse (Gomoku and Renju)
- keyboard with a disk save (The Lurking Horror)
- a disk swap inside a running session (Terramex)

No core defect was found. Two Lurking Horror attempts were spoiled by the
harness and aren't counted (see Case 3). Their cause, late key-ups from the
host's input path, is filed as
[#1582](https://github.com/DeanoC/fes/issues/1582).

| Case | `m1` | `m2` | Case |
| --- | --- | --- | --- |
| 1. Xenon (joystick) | **PASS**: 11:02 of play, score 250, lives 3 to 2 | **PASS**: 11:00 of play, score 500, then 750 | **PASS** |
| 2b. Gomoku and Renju (mouse) | **PASS**: 8 launches, 7 won by White, 1 stopped by the player script's cap with no result | **PASS**: 6 launches, all won by White, 14:21 of board play | **PASS** |
| 3. The Lurking Horror (keyboard, save) | **PASS** (entry `m1b`): SAVE, two Stop/relaunch/RESTOREs, about 11 min of play; the saved disk equals Hatari's except one directory time byte | **PASS** (entry `m2b`, checked launch and typing): SAVE, two Stop/relaunch/RESTOREs, about 13.5 min of play; saved at 5/22, so not byte-compared with Hatari | **PASS** |
| 4. Terramex (disk swap, joystick) | **PASS**: two launches, each swapped to disk 2 at the game's prompt, about 11 min of play each | **PASS**: the same | **PASS** |

This PR refers to #1528 and doesn't close it. Still owed:

- A swap from tenfoot. The ST package has no session display, and tenfoot has no ST
  swap control, so case 4 swaps through the host API.
- A gameplay reference for Xenon and Gomoku. Hatari's command interface has
  no joystick or mouse-move events.
- Protected originals. [#1541](https://github.com/DeanoC/fes/issues/1541)
  tracks running IPF images directly.

## Package

- **Package.** `2b54eb672a057cc13bdaa0af12dda30bae657772e3ae4ae465afa9deba8957b4`
  (`.fcore` SHA-256 `d1558723…`), `fes.atari-st` 0.2.0, build ID
  `15f63d4f3fa0477295133328f5e3dea7`, from fes `25204901d` (main, 8 November).
- **Toolchain.** Mistral `8fcc4cb4`, nextpnr-mistral `5fe55f3d`, Yosys
  `3a5accbd` and the HIP router. Built-in Direct video.
- **Seal.** Placer seed 4, route pass. Achieved against required:

  | Clock | Achieved | Required |
  | --- | --- | --- |
  | System | 58.75 MHz | 52.22 MHz |
  | Pixel | 83.06 MHz | 74.25 MHz |
  | Audio | 139.08 MHz | 12.29 MHz |

- **How it was built.** The producer ran outside `gpu-run`, which is how
  #1544 says it must run. It leases a GPU for each nextpnr call itself.
- **Main has moved since #1544.** #1544's PR sealed main `4b33ec559` as
  `1f3b21b7…`, and this seal of later main is a different package.
- **Firmware.** TOS 1.04 UK, SHA-256 `c005b0ae…`, selected per title through
  the library ROM slot.
- **Kit A.** Baseline software `4efd34bf` with image `31b39063`, run
  core-only: nothing was installed, the image wasn't changed and no run
  rebooted the kit.
- **Host.** The private Powerboat FogCast host from the earlier record
  (promoted `bb51f445` `fogcast-api`, private HOME), targeting Kit A's `dev`
  only. It used library Play, the session input events, the library disk
  insert and Stop. The shared host was not used.
- **Capture.** Kit A's ShadowCast. The runners read `live.png` (960×540,
  2 fps); play was recorded at 640×360 h264, with stereo 48 kHz audio.

**Play time** is time in the game: from the first game frame (for Gomoku, the
first board frame) to the last input of that game or session, summed over
the run. Boots, desktop launches and idle gaps aren't counted.

## Case 1: Xenon

Library Play, then fire on the title starts a game. `xplay.py` from the earlier
record then sent joystick moves and fire bursts for 11 minutes, with a frame
saved each minute.

- **`m1`, 18:01–18:12.** Score 250; one life lost.
- **`m2`, 20:23–20:34.** Score 500, then 750.

The playfield, panel and scores look as they did on the factory package's
runs. Each disk record still holds the unchanged base image.

## Case 2b: Gomoku and Renju

Each game starts with Stop, then library Play, then `GOMOKU.PRG` through
File > Open on the desktop. The record's
[`gomoku.py`](../2026-11-07-atari-st-game-acceptance/gomoku.py) plays White
with mouse packets.

- **`m1` (18:13–19:10).** Eight launches.
  - Seven ended in "Game won by White".
  - Game 1 reached 84 White stones and was stopped by the player script's
    15-minute cap. Its final board (84 White, 85 Black) has no five in a
    row for either side, so it was a long game, not a missed result.
  - The earlier `white_moves` value was a logged-move count (82); the
    final-board census is 84 (+2), and the record does not explain the extra
    stone beyond the +1 seen in each won game.
  - Board play totals 39:44.
- **`m2` (20:34–21:01).** Six launches, all "Game won by White", with 14,
  17, 9, 25, 27 and 26 White stones on the final board. Board play 14:21.
- **Counts.** Stone counts are read from each game's final board, where the
  winner's five in a row is also checked. `m1`'s seven wins have 12, 29, 10,
  7, 35, 24 and 19 White stones.
- **First board frame.** It is byte-identical after all 14 launches
  (`4db2dd70…`), the same frame as on the factory package.
- **Speed.** In `m1` the player script took 10–15 s per move, against about
  6 s on the factory package. The recordings show the ST isn't slower:
  counting stones frame by frame, Black's reply appears within about 1 s of
  White's stone on both packages. The time went to the player script on
  Powerboat, whose load average was 55–66 during `m1`.

## Case 3: The Lurking Horror

The flow, the same as in the earlier record:

1. Library Play reaches the GEM desktop.
2. File > Open opens drive A, then `LURKING.PRG`.
3. The script ([`lh-script.txt`](../2026-11-07-atari-st-game-acceptance/lh-script.txt))
   logs in, follows the dream, takes the stone and wakes in the Terminal
   Room.
4. SAVE `FES1528`.
5. Stop, library relaunch and RESTORE, played twice
   ([`lh-play2.txt`](lh-play2.txt), [`lh-play3.txt`](lh-play3.txt)).

### Counted runs

- **`m1`, entry `m1b` (19:32–19:48).**
  - **The game opened every time.** Each frame after File > Open shows the
    title (19:32:07, 19:38:03 and 19:45:58). The window at 19:38 listed
    `FES1528.SAV`, `LURKING.PRG` and `STORY.DAT`.
  - **The save.** SAVE answered "Okay." at 5/19.
  - **Both restores.** Each answered "Okay." with the stone in inventory, at
    5/20 and then 5/35. About 11 minutes of play.
  - **Byte match.** The saved disk equals Hatari's natural-timer saves `sd2`
    and `sd3` except offset 5686, the `FES1528.SAV` directory time. The
    other Hatari saves differ in that byte plus 4 bytes, which are the
    urchin's room. The earlier record explains those 4 bytes: the game seeds
    its random numbers from the system timer when it starts.
  - **Not fully checked.** Some keys in this run went through the old typing
    path (`lhtype.py`, kept on Powerboat), so not every command was
    length-checked. The checked typing came with `m2b`.
- **`m2`, entry `m2b` (21:26–21:45), run with [`lhrun.py`](lhrun.py).**
  - **Checked launch.** Before File > Open, the runner reads the drive-A
    window: with a third icon present, `LURKING.PRG` is the second icon,
    otherwise the first. After opening, the game's solid status bar must be
    on screen (1.00 here, against 0.11 for the GEM text viewer), or the run
    stops. Sessions 1 to 3 passed this at 21:26:47, 21:34:26 and 21:42:12.
  - **Checked typing.** Each command is typed without Enter, and the input
    row is measured on the frame: it must grow by exactly the command's
    length.
    - A repeat is erased with Backspace and retyped; about 10 commands
      needed a retry.
    - A `[MORE]` page is cleared with space first.
    - A few Enter repeats gave harmless "I beg your pardon?" lines.
  - **The filename.** SAVE and RESTORE open the GEM file selector, so the
    filename goes through [`selname.py`](selname.py): Esc, type the name,
    check the Selection field, Enter.
    - For the SAVE, the field read `FES1528` by eye. The script's counter
      had also counted the text cursor bar, and it was fixed before the two
      RESTOREs, which passed it on the first try.
  - **Outcome.** SAVE at 5/22 answered "Okay."; the restores led to 5/55
    and 5/38, with the stone. About 13.5 minutes of play.
  - **No Hatari byte match.** The save was made 3 moves past the reference
    script's 5/19, so it differs from the Hatari saves by 23–24 bytes. The
    typing log shows the script's commands in order, with retries only
    before Enter, and the cause of the extra moves wasn't isolated.
  - Run 1's byte match carries that comparison for this case.

### Harness-invalid, not counted

- **`m1`, entry `m1-e625024857d2` (19:13–19:18).**
  - **What went wrong.** Late key-ups made TOS auto-repeat keys ("sit on
    ccccccchair", "turrn on pc"), so the script never reached the dream.
  - **The save.** It wrote a different game state, 166 bytes from Hatari's.
  - This happened before the typing check existed.
- **`m2`, entry `m2-37e82fd75d55` (21:03–21:16).**
  - **Session 1 ran the game.** But key repeats garbled the save line
    ("savvve", "FEES1555555528"), so no save file was written. The disk
    record is still the base image.
  - **Sessions 2 and 3 opened the wrong file.** Each clicked the second icon
    without reading the window. With no save file, that icon is `STORY.DAT`,
    which opened in the GEM text viewer (21:08:02 and 21:15:11). Deano saw
    it on the kit.
  - **Stopped.** The runner was stopped at 21:16:22, and the case was rerun
    as `m2b` with the checks above.

### Late key-ups (#1582)

Each key goes to the host as two requests, down and then up. Most keys
took about 0.08 s from the down request's start to the up request's reply,
but 20–30% took 0.2–0.5 s; in `m2`, 68 of 527 keys took 0.3 s or more. A
hold that long reaches TOS's auto-repeat delay.

The repeats line up with those keys. In "takkke sssssstonne", the 'k', 's'
and 'n' were the three keys whose up request took 0.33–0.44 s, and every
key that took 0.08 s arrived once.

`m2` ran at a Powerboat load average of about 4, so the delay is in the
host-to-kit path. `st_ikbd.sv` changed on main (#897, #913), but the core is
ruled out on two counts:

- 3 × 41 keys typed in a quiet spell, with every up reply within 0.33 s,
  arrived exactly.
- Every repeat matches a late key-up.

## Case 4: Terramex disk swap

Disk 2 is its own library entry. At the game's "Please Insert Disk 2" prompt,
the host's library disk insert put it into unit 0 of the running session, and
the game went on to its title and play. [`tmplay3.sh`](tmplay3.sh) played for
11 minutes, restarting from the title when a game ended.

| Run | Launch | Swap | Play |
| --- | --- | --- | --- |
| `m1` | 1 | 19:51:26, rc 0, 10.1 s; unit 0 bound to disk 2, same session (generation 38) | 19:52–20:03 (11:04), score 50 |
| `m1` | 2, after Stop and relaunch (unit 0 back on disk 1) | 20:06:20, rc 0, 9.9 s | 20:06–20:17 (11:02) |
| `m2` | 1 | as above | 21:48–21:59 (11:09) |
| `m2` | 2, after Stop and relaunch | as above | 22:03–22:14 (11:13) |

- **Same prompt every time.** The 60 s and 100 s prompt frames are
  byte-identical across all four launches.
- **Disks unchanged.** Both disk records still hold their base images after
  each run.

## Kit housekeeping

- **Claims.** `m1` (17:56–20:18) and `m2` (20:19–22:15) were each logged in
  `kitA-STATUS.md`. Each claim was released only after the post-run snapshot
  matched the probe and the agent was idle with a free lease.
- **Clean-up.** Each run removed the 7 trees it created: one empty
  `core-data` namespace directory and six disk records. The post-run file
  listings equal the pre-run ones.
- **Final state.** Kit A was left FREE at 22:15 on software `4efd34bf` and
  image `31b39063`, with no reboot. Kit B wasn't used.
- **What stays on Powerboat.** These stay in `~/fes-scratch/st1528`:
  - captures (`.mkv`, `.wav`, frames)
  - the copied disk records
  - the per-key timing logs
  - `keytest.py`, `lhtype.py` and the seal tree
- **Not committed.** No disk, ROM or frame is committed.
