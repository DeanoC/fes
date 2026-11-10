# pc386 game acceptance rerun on main, 10 November 2026

Issue [#1368](https://github.com/DeanoC/fes/issues/1368), part of the M5
tracker [#1267](https://github.com/DeanoC/fes/issues/1267). This rerun covers
#1368's checks again on a fresh seal of main: the ones done in
[#1481](../2026-11-04-pc386-games-smoke/summary.md),
[#1486](../2026-11-04-pc386-m5-game-checks/summary.md) and
[#1520](../2026-11-06-pc386-game-acceptance/summary.md), plus the FreeDOS date
and time check. That check was blocked until the kits ran an image with
[#1279](https://github.com/DeanoC/fes/issues/1279)'s NTP. The Serpent Isle
reload issue [#1494](https://github.com/DeanoC/fes/issues/1494) has its own
record: [`2026-11-10-pc386-si-reload-presets`](../2026-11-10-pc386-si-reload-presets/summary.md).

**Result.** Every check rerun here passed, and FreeDOS date and time now pass.
The exception is one Serpent Isle launch in eight, which showed a black
picture throughout ([#1494](https://github.com/DeanoC/fes/issues/1494),
reproduced):

| #1368 item | Result | Run |
| --- | --- | --- |
| Rerun on main's seal: FreeDOS, Wolfenstein 3D, Ultima VI | **Pass** | `t1` |
| FreeDOS date and time | **Pass**: DOS shows the kit's date and time | `t1` |
| Wolfenstein 3D, a level played through | Not rerun: passed in #1520 (`w1`) | none |
| Wolfenstein 3D above 30 fps | **Pass**: 60 new pictures a second while turning, at 60 fps capture | `t1` |
| Ultima VI under the 286 preset: speaker pitch | **Pass**: 70/70 notes, every key within 0.33% of #1481 | `t1` |
| Ultima VI under the 286 preset: walking speed against a reference | **Open**: still no period or emulator reference | none |

| The Black Gate, save then load after a relaunch (#1520's check) | **Pass**: same place, slot listed | `b2` |
| Serpent Isle, save then load after a relaunch (#1520's check) | **Pass in 7 of 8 launches**; 1 launch showed a black picture throughout (#1494) | #1494 run `k1` |

Still open on #1368, so this PR refers to it and doesn't close it:

- Ultima VI walking speed at 286-12 against a period or emulator reference.
  Powerboat has no DOSBox or 86Box, and no period recording was found.
- Ultima VII NPC pace (listed by #1486). It also has no reference.

## Package

- **Package.** `650bc477a12c458181f6eeca24d56fc71a0db37d1002f6a2d58869871a8c8f93`
  (file SHA-256 `a76a6114…`), a Quartus 17.0.2 seal (seed 1) of main
  `65ec1f22a`, built in a clean detached worktree with
  `scripts/build_fes_pc386_quartus.py`.
  [`seal-summary.json`](seal-summary.json) is its `build-summary.json`.
- **Timing.** Every analysis passed. Worst slacks: setup +0.694 ns,
  hold +0.099 ns, recovery +7.125 ns, removal +0.500 ns and minimum pulse
  width +1.122 ns. Clocks: system 61.44 MHz, pixel 74.27 MHz, audio
  12.29 MHz.
- **Resources.** 36,050 of 41,910 ALMs, 268 of 553 RAM blocks, 38 DSP
  blocks.
- **Why this seal stands for main.** All 114 sealed inputs hash identically
  in this branch's tree. Main has moved to `f4fff82d9` since, and that commit
  (#1636) changes nothing under `sources/misteross`.
- **Since #1520's seal** (`254b2bf5`, of `317310c81`), the tree gained, among
  others: Phase 4 slices 5 and 6/6b (indirect prediction, early ALU, load
  forwarding), the one-write-port return stack (#1638), Phase 5 slices 1 and
  2 (paging, debug registers, call gates), privilege transitions for ring-3
  DPMI hosts (#1552), the EMS board (#1556), the KF8253 wrap fix (#1549), the
  BIOS 198x year (#1558), the ATA SRST signature fix (#1596) and the VGA BIOS
  AH=00h DX fix (#1590).

## Kit and disks

- **Kit.** Kit B, core-only on its baseline: image `cd284ca3…`, software
  bundle `7ddb0bfb…`. Nothing was installed, and there was no reboot. The
  claim, the restore check and the release are logged in Kit B's status file.
  The kit clock read correct UTC (it runs `S21fes-time`).
- **Disks.** Each is main's FreeDOS games image `e69194ec…` (built with
  `scripts/build_pc386_freedos_image.py --profile games` on main `e835d545d`;
  the builder hasn't changed since) plus one game from Deano's eXoDOS 6.04
  zips, or the scrub disk. Each run attached scratch copies. Nothing from a
  game is committed.

| Disk | SHA-256 | Notes |
| --- | --- | --- |
| Scrub | `8538495a…` | SCRUBX, all regions; the shared kit image |
| Wolfenstein 3D | `fdd1604a…` | `C:\WOLF3D` |
| Ultima VI | `4e6d75e6…` | eXoDOS config, AdLib music |
| Ultima VI, no music | `e28bc5a2…` | `U6_NOMUSIC=1`, `CONFIG.U6` `75d79845…`, for the lute |
| DOOM | `9a38b183…` | `C:\DOOM` |
| The Black Gate | `d9bfafd4…` | `C:\ULTIMA7` |
| Serpent Isle | `f32f047a…` | `C:\SERPENT`, plain-SB `SERPENT.CFG` `13ad7bdb…` (#1426) |

- **Presets.** Every game ran at `max` (preset 0), except Ultima VI with no
  music at 286-12 (preset 1) and The Black Gate at 386DX-25 (preset 3).
  Sound Blaster is 220h, IRQ 7, DMA 1 everywhere.
- **Capture.** Kit B's 4KPRO now captures at 1920x1080, **60 fps** (720
  frames in 12 s), with audio from ALSA at 48 kHz. Frame counts use
  480x270 grey video. A "new picture" is a frame that differs from the one
  before in more than 0.5% of the picture by more than 24 levels.
- **Plan.** Run `t1` replays #1481's run `s2` op for op (672 ops), with a
  SCRUBX scrub before each game, plus one `kitdate` op that reads the kit's
  clock just after DOS prints its date and time.

## Run `t1` (Kit B, 18:26–19:11 EET)

### FreeDOS (`max`)

**Pass.**

- **Boot.** UMBM C800-EFFF, HimemX 3.38 ("Fast A20 method used"), and
  CuteMouse "Installed at PS/2 port" (frame `bebc7051`).
- **Memory.** `MEM` shows 580K conventional free (594,256 bytes), 15,201K
  XMS free and an 81K largest UMB (frame `0c06cbb8`). #1481's games image
  showed 575K.
- **Sound Blaster.** `BLASTER=A220 I7 D1 T3`.
- **Date and time: pass.** DOS shows "Current date is Tue 11-10-2026" and
  "Current time is 4:27:48.56 pm" (frame `f9cfe9a2`, committed as
  [`kit-t1-date-time.png`](kit-t1-date-time.png)). The `kitdate` op, sent
  right after the frame grab, read the kit at 2026-11-10 16:27:55 UTC. So DOS
  is within the 7 s between the two reads of the kit's time. The kit's local
  zone is UTC, and
  [`computer-io.md`](../../../sources/libmister-runtime/docs/computer-io.md)
  says `fes.rtc.wall-clock` carries the kit's local time.
- **Persistence.** `C:\SMOKE.TXT`, written before a Stop, reads back after
  the relaunch (`fes1368-t1`). The Wolfenstein 3D save `SAVEGAM0.WL6` is
  listed with its date, 11-10-26 4:29p (frame `fc06773b`). So DOS also stamps
  files with the right date and time.

### Wolfenstein 3D (`max`)

**Pass.**

- **Start-up.** The sign-on detects the mouse and Sound Blaster, with
  1,000K of XMS (frame `a08f57d7`). Menus, then E1L1 (frame `41d1511c`).
- **Play.** It moves, turns and fires (ammo 8 → 5).
- **Frame rate.** Over the held turn, 60, 60 and 60 new pictures in the
  three full seconds, at 60 fps capture. 60 Hz output is the limit. No
  partly updated frames in 720.
- **Audio.** 62 of 75 seconds over −50 dB, median −43.6 dB, peak −27.8 dB.
- **Save.** F2 in slot 1. After a Stop, a scrub and a relaunch, Load Game
  lists `smoke`, and the loaded game has ammo 5 in the same view (frame
  `3eefede3`).

### Ultima VI

- **At `max`, eXoDOS config: pass.**
  - The menu (frame `e931dee6`), character creation, then the game.
  - Music throughout: median −36.9 dB, peak −22.8 dB.
  - **Save.** Ctrl-S, Y (frame `b723ebf4`). After a Stop, a scrub and a
    relaunch, Journey Onward puts the party in the same room (frame
    `58a88fe3`).
- **Lute at 286-12, no music: pass.**
  - **Method.** As in #1481: Use the lute, then keys `6` ×30, `1` ×31, then
    `2`–`9` and `0`. The analyser is #1472's `u6_tones.py`: notes are runs
    of 5 ms frames over RMS 300, and the pitch is the envelope's rising-edge
    rate.
  - **Notes.** All 70 keys gave exactly one note, 100–105 ms long, with a
    PIT carrier at 18.8–20.2 kHz. Four more effects came after Esc, as in
    #1472 (917 Hz, about 3.4 kHz, 587 Hz, about 1.2 kHz).
  - **Envelope pitch** (median, with range where there are more notes),
    against #1481's `s2`:

    | Key | `t1` | `s2` | Change |
    | --- | ---: | ---: | ---: |
    | `1` | 501.9 Hz (499.9–505.1, n=31) | 502.2 Hz | −0.06% |
    | `6` | 839.9 Hz (838.0–843.5, n=31) | 840.0 Hz | −0.01% |
    | `2` | 565.5 | 565.4 | +0.02% |
    | `3` | 634.2 | 636.3 | −0.33% |
    | `4` | 675.4 | 675.2 | +0.03% |
    | `5` | 748.5 | 750.3 | −0.24% |
    | `7` | 950.5 | 949.4 | +0.12% |
    | `8` | 1000.1 | 998.4 | +0.17% |
    | `9` | 1106.6 | 1104.1 | +0.23% |
    | `0` | 1232.7 | 1231.4 | +0.11% |

    WAV `50e64a2d…`. So the CPU front-end and Phase 5 changes since #1481
    haven't moved the 286-12 preset's speaker timing.
  - **Walk.** The party walked out of the throne room (frame `51bb659d`).
    Still no reference to compare it with.

### DOOM (`max`)

**Pass.**

- **Start-up.** DOS/4GW's banner, then "DOOM System Startup v1.9",
  "registered version", and R_Init completes (frames `0ace54c9`,
  `a52bebaf`).
- **Demos.** 581 new pictures in 20 s, 29.1 a second (17–35 per second) at
  60 fps capture. #1486 measured 19.9 at `max`. DOOM caps at 35.
- **Play.** E1M1 moves, turns and fires (bullets 50 → 48): 18, 35, 35, 32
  and 20 new pictures in the seconds of movement.
- **Save and reload.** F2, slot 1, "game saved" (frame `762ada39`). After a
  Stop, a scrub and a relaunch, F3 loads it with 48 bullets and 100% health
  in the same view (frame `ffdc0d03`).
- **`-timedemo demo3`.** "timed 3863 gametics in 3379 realtics" (frame
  `0891c7c1`): 3863 × 35 / 3379 = **40.0 fps** at `max`. #1481 measured
  26.7 (5062 realtics).
- **Audio.** 81 of 102 seconds over −50 dB, peak −19.6 dB.

### The Black Gate (386DX-25) and Serpent Isle (`max`): intros

**Pass.** Each plays its introduction with music to the main menu: The
Black Gate's skull and Moongate scenes, then the menu (frame `6efc1e18`).
Serpent Isle's castle and Guardian scenes, then the menu (frames `10562c0d`,
`900b3954`).

- **Not determined: one black grab.** The Black Gate's last grab, `bg21`
  (10 s after the menu, 30 s after the grab before), is the flat black frame
  `4c6d2c8d`. In #1481 the same grab showed the introduction again. The menu
  goes back to its introduction when left idle, and its scene changes fade
  through black, so one black grab can be a fade. It can also be #1494's
  black picture, whose frames have the same hash. This run had no grab after
  it, so it doesn't tell which. In #1494's run `k2`, three more Black Gate
  launches at 386DX-25 all showed their introduction.
- **Audio.** The Black Gate 215 of 235 s over −50 dB, Serpent Isle 137 of
  149 s.

## Run `b2`: The Black Gate save and load (Kit B, 19:49–19:58 EET)

`b2` replays #1520's `b1` op for op on this package, at `max`, with the
disk and game record renamed.

**Pass.**

1. A new game from the menu (name "Avatar", frame `c751ba1f`), the Trinsic
   opening, Finnigan's question answered, then a walk south-east to the town
   wall.
2. `S`, slot 1 saved as "fesb2" (frame `3b940a47`). Frame at the save
   point: `37a2f014`.
3. Stop, scrub, relaunch, `ULTIMA7`, Esc, Journey Onward.
4. The game comes back at the save point (frame `7acdff93`). 0.18% of
   pixels differ from the save-point frame, which is the mouse cursor.
   `S` lists "fesb2" in slot 1 (frame `88951708`). The Avatar walks on
   (frame `75d015a5`).

This run checks the save by its frames only. #1520 also matched the save
file's FLEX entries against `GAMEDAT`; that wasn't repeated.

## Serpent Isle save and load

Serpent Isle's save and load ran in #1494's run `k1` on this package: a new
game, the ship scene, a save to slot 1 ("fesk1"), then 8 relaunches at
four presets, each loading the save with Journey Onward. In 7 launches the
ship scene came back within 3–6 s. In one (486DX2-66, the first of two
launches there) the picture was black from the introduction on, through the
whole launch, while the game ran. That is #1494; its record has the details.

## Not rerun here

- **Wolfenstein 3D E1L1 and DOOM E1M1 played to their exits.** They passed
  in #1520 (`w1` and `d1`) on seal `254b2bf5`, which predates the CPU changes
  listed under Package. Each exit took about 45 minutes of leg-by-leg
  steering, read from debug position readouts, and this rerun didn't repeat
  them. So on this seal the levels were entered and played (Wolfenstein 3D
  above, DOOM's E1M1 and save), but not played to their exits.
- **The `BLASTER` change to 240h/IRQ 5** and DOOM's SETUP at that setting
  (#1486).

## Kit housekeeping

Every run claimed Kit B in its status file, checked the baseline before
and after (image `cd284ca3…`, software `7ddb0bfb…`, no pending image, same
boot), and released it with the agent idle and the lease free. After each
run, a housekeeping run removed the FogCast data trees that run created
(game records, media and staged disk images). The shared scrub image was
kept:

| Run | Kit B | Housekeeping |
| --- | --- | --- |
| `t1` | 18:26–19:11 EET | 17 trees, 19:12 |
| `k1` (#1494) | 19:12–19:47 | 5 trees, 19:49 |
| `b2` | 19:49–19:58 | 5 trees, 19:59 |
| `k2` (#1494) | 19:59–20:26 | 6 trees, 20:27 |

`b2`'s before-listing of the data directory came back empty, so its first
removal list named whole top-level directories. The housekeeping script
refused it. The list was then rebuilt against the listing `k1`'s housekeeping
left, and that run removed only `b2`'s 5 trees. The runner now stops if the
before-listing is empty.
