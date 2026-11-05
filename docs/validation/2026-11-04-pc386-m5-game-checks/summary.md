# pc386 M5 game checks at both presets, 4 November 2026

Trackers [#1267](https://github.com/DeanoC/fes/issues/1267) (M5) and
[#1368](https://github.com/DeanoC/fes/issues/1368). This run follows
[#1481's smoke run](../2026-11-04-pc386-games-smoke/summary.md) on the same
seal, and covers the checks that record left owed.

**Result.** Kit B run `r1` passed the FreeDOS leftovers, measured each preset
against real machines, and recorded Wolfenstein 3D and DOOM frame rates
through a 60 fps capture. It also found one failure, and some checks were not
done:

- **Passed.**
  - FreeDOS: `A20TEST` passes every check. `BLASTER` follows the machine
    setting to 240h, IRQ 5, and DOOM plays effects and music there after
    its SETUP is rerun.
  - Presets: TopBench and CHKCPU at 286-12, 386DX-25 and `max`.
  - Wolfenstein 3D: about 59–60 fps at `max` (the 60 Hz output's limit),
    and 24 fps at 386DX-25.
  - DOOM: up to 39 new pictures a second in play at `max`, and
    `-timedemo demo3` at 5.6 fps at 386DX-25.
  - Ultima VII: both games play their introductions to the main menu at
    386DX-25.
- **New failure,
  [#1485](https://github.com/DeanoC/fes/issues/1485).** The Ultima VII
  engine's menus stop taking mouse clicks and keys, at `max` as well as at
  386DX-25. So neither Ultima VII game could start a game, and their saves
  and NPC pace were not checked.
  - The CuteMouse driver counts every click at both presets (M33 below), so
    the clicks get lost above the driver.
  - This widens [#1427](https://github.com/DeanoC/fes/issues/1427), which
    saw the fault only at 386DX-25.
  - One Black Gate launch at `max` also stalled outright in its
    introduction after a scrub. That is recorded on
    [#1433](https://github.com/DeanoC/fes/issues/1433).
- **Not done.**
  - Wolfenstein 3D: a level played through. A scripted route was started
    but abandoned.
  - DOOM: a level played to its exit.
  - FreeDOS: date and time, blocked by #1279.

## Inputs

- **Package.** `fes.pc386` `499f4805ca07ddc1…` (file `b69ca3f9…`), #1481's
  Quartus 17.0.2 seal of main `ee7ea9bb4`. All 109 of its sealed inputs hash
  identically on main `d646bcd7e`, checked at 20:30 and again before the
  claim. So no reseal was needed. Timing is in #1481's
  [seal summary](../2026-11-04-pc386-games-smoke/seal-summary.json).
- **Kit.** Kit B, run `r1`, claimed 20:45 and released 22:22 EET. It ran
  core-only on the baseline software `a8cd30c4` (main `bb51f445`) and OS
  `f904961e`.
  - Nothing was installed, and there was no image or boot change and no
    reboot.
  - The runner checked the snapshot keys before and after, including
    `usb-storage.quirks`.
  - Kit A was tried first, as `r1` at 20:32. Its FAT has 23 MiB free and
    FogCast keeps one other staged disk, so it refused the first 33.5 MB
    disk. Nothing ran there, and its status file says so.
- **Capture.** Kit B's 4KPRO through `fes-4kpro-hil`. Since this evening its
  fan-out reads the card at 60 fps.
  - **Rate.** 240 frames arrived in 3.99 s. The loopback still advertises
    30 fps, so the runner's video capture uses `-fps_mode passthrough`, and
    each 8 s sample holds 479–480 frames.
  - **Audio.** ALSA `hw:CARD=D4KPRO` at 48 kHz. Levels are the RMS of
    each 100 ms window in dBFS, with the DC offset left in, so silence reads
    −47.9.
- **Disks.** The games are user-supplied, so every disk stays outside the
  repo.
  - **Base.** Main's `build_pc386_freedos_image.py --profile games` gives
    `b23fba2e…`, the same image #1481 used.
  - **Tools.** `A20TEST.COM`, `INPUT.COM` and `M33.COM` were added to the
    root of a copy of the base, with TopBench and CHKCPU in `C:\TOPBENCH`
    and `C:\CHKCPU`. The copy is `f7929ece…`.
  - **Games.** Each game disk is that copy plus one eXoDOS 6.04 zip from
    Deano's share:
    - Wolfenstein 3D `acc0f2b1…`
    - DOOM `fa8ec703…`
    - The Black Gate `5eba6445…`
    - Serpent Isle `a72e3d4e…`, with #1443's plain-SB `SERPENT.CFG`
  - **Scrub.** The SCRUBX disk is `8538495a…`.
  - [kit-runs.json](kit-runs.json) gives every hash, the zips and their
    file counts.
- **Tools on the disks.**
  - **`A20TEST.COM`** `61173821…` is assembled from main's
    `cores/fes-pc386/sim/a20_test.asm`. #1391's build was `5efd0ac3…`; the
    source has changed since.
  - **`M33.COM`** `a57fda05…` is a 285-byte probe written for #1427 and
    kept in scratch. Each key press prints CuteMouse's left and right press
    and release counts since the last line (INT 33h functions 5 and 6),
    then the button state and position (function 3).
  - **TopBench and CHKCPU** come from PhilsComputerLab's DOS Benchmark Pack
    v1.5 on Deano's share. They were put on scratch disks only, and only
    their scores are given here.
- **Presets.** Each library entry was new, and its settings were written
  before its first load. Sound Blaster was 220h, IRQ 7, DMA 1 everywhere
  except `r1-fd240`.

  | Entry | Preset | Sound Blaster |
  | --- | --- | --- |
  | `r1-fd240` | 0, `max` | 240h, IRQ 5, DMA 1 |
  | `r1-bgmax`, `r1-w3dmax`, `r1-doommax`, `r1-simax` | 0, `max` | 220h, IRQ 7 |
  | `r1-bg25`, `r1-w3d25`, `r1-doom25`, `r1-si25` | 3, 386DX-25 | 220h, IRQ 7 |
  | `r1-fd286` | 1, 286-12 | 220h, IRQ 7 |

  "Both presets" is read here as `max` and 386DX-25 for every game, and
  286-12 as well for the benchmarks. The design names 286-12 only for
  Ultima VI, which #1481 already ran at both.
- **Scrub.** A SCRUBX scrub ran before every game launch, 12 in all. Each
  ended `DONE flags=0000000F`.

## FreeDOS

All checks pass.

- **A20.** At the prompt, `A20TEST` prints every check `ok`:
  - `xms-start` and `xms-query`;
  - `xms-global` seven times and `xms-lock` six times;
  - `A20 DONE ok`.

  HimemX's banner says "Fast A20 method used"
  ([frame](kit-r1-a20test.png)). These are the games image's checks in
  `sim_fes_pc386.py --suite a20`.
- **`BLASTER` at 240h, IRQ 5.** The entry's record holds `sb_base` 576 and
  `sb_irq` 5. `set` shows `BLASTER=A240 I5 D1 T3`, and `MEM` still shows
  575K free ([frame](kit-r1-blaster-240-mem.png)).
- **A game's sound after its setup is rerun at 240h, IRQ 5.**
  - **Setup.** DOOM's `SETUP.EXE` was run from `C:\DOOM`. It set Sound
    Blaster for effects at port 240, IRQ 5 and DMA 1, with 8 channels, and
    Sound Blaster for music at port 240. Then "Save parameters and launch
    DOOM".
  - **Config.** `DEFAULT.CFG` then reads `snd_sfxdevice 3`,
    `snd_musicdevice 3`, `snd_sbport 576`, `snd_sbirq 5` and
    `snd_sbdma 1`. DOOM's start-up prints no sound error.
  - **Sound.** All three launches play effects and music:
    - launched by SETUP: the title music at about −40 dB, then the demos at
      −17.5 to −22 dB a second;
    - launched by `doom`: 30 s of demos at a median of −22.4 dB;
    - launched by SETUP again: 30 s of demos at a median of −22.1 dB.

    #1481 measured −22 to −31 dB at 220h, IRQ 7.
- **Not done: date and time.** This needs a kit image with NTP (#1279).

## Presets against real machines

Each benchmark ran at the DOS prompt, in a fresh entry. TopBench's figures
are its Realtime benchmark's. Its database entry with the nearest score is
named as a guide only: it matches on score alone, and the sub-scores show
how different the machines are.

| Preset | CHKCPU | TopBench | MemTest | MemEA | Opcodes | VidMem | 3DGames | Nearest entry (its score) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 286-12 | classic 386, 12.8 MHz, real mode | 33 | 597 µs | 173 µs | 199 µs | 329 µs | 191 µs | IBM PS/1 Model 2121, 386SX-16 (33) |
| 386DX-25 | classic 386, 26.4 MHz | 68 | 250 µs | 52 µs | 79 µs | 267 µs | 82 µs | Northstar Slimline 333, 386DX-33 (71) |
| `max` | classic 386, 62.4 MHz | 298 | 21 µs | 20 µs | 19 µs | 101 µs | 22 µs | Packard Bell Pulsar 16 (modified), Pentium MMX 233 (309) |

- **286-12.** It benchmarks as a 386SX-16, faster than a real 286-12 would.
- **386DX-25.** It scores close to a 386DX-33. Its memory and video timings
  (MemTest 250 µs, VidMem 267 µs) are slower than that entry's, and its
  opcode times are about the same.
- **`max`.** Its overall score is close to a Pentium MMX 233. Its memory
  timings are slower than that machine's and its opcode times are about
  three times its 6 µs.

## Wolfenstein 3D

- **Frame rate.** Each sample was a 6 s held turn with the keyboard on
  E1L1, captured at 60 fps. Frames were compared over the 3D view.
  - **At `max`.** In each full second with textured walls in view, 59 of
    the 60 captured frames were new pictures. A second facing a plain wall
    counts fewer, because its frames barely change.
  - **At 386DX-25.** 24, 24, 23, 24 and 24 new pictures a second, steadily.
  - **So:** about 59–60 fps at `max`, which is the 60 Hz output's limit,
    and 24 fps at 386DX-25.
- **Start-up at 386DX-25.** The menus are visibly slower, and one Enter was
  dropped during a menu fade.
- **Mouse turning is exact.** Wolfenstein 3D reads the mouse by INT 33h
  function 0Bh. 5,760 counts make one full turn (16 a degree).
  - **Lag.** The input stream delivered about 40 packets a second here.
    So a turn sent faster than that is applied late, but it is applied in
    full.
  - **Check.** After +360°, +90° and −90° had settled, the view was
    byte-identical to the start view.
- **Not done: a level played through.**
  - **Plan.** A route to E1L1's elevator was planned from the map, about
    127 tiles and 13 doors. Mouse counts were used for exact turns and
    steps.
  - **What went wrong.**
    - A turn queued before the forward moves made the first legs start from
      the wrong heading.
    - An Esc used as a pause dropped its resume during the menu fade, so
      later mouse motion went to the menu.
    - After that, the dead reckoning didn't match the views.
  - The route was abandoned at the start room to keep kit time for the
    other rows. This stays owed on #1368.

## DOOM

- **Frame rate at `max`, at 60 fps.**
  - **Demos.** 397 new pictures in 20 s: 19.9 a second, 15–26 a second.
    This matches #1481's 20.1 a second at 30 fps, so the demos were
    CPU-limited below 30 all along.
  - **Play.** During a turn in E1M1's north corridor (a simpler view),
    32, 39 and 33 new pictures a second. The partial-frame heuristic flags
    some of those frames, so counts above DOOM's 35 fps cap include frames
    caught mid-flip. The view drops to 19–25 a second where more is in
    sight.
- **`-timedemo demo3` at 386DX-25.** "timed 3863 gametics in 24196
  realtics" (frame `2fcc5501`): 3863 × 35 / 24196 = 5.6 fps. #1481
  measured 26.7 fps at `max`.
- **Play at `max`.** About 3 minutes on E1M1 at Hurt Me Plenty. From the
  start room the run went to the east window, then up the north corridor to
  its door and back, using the automap to place itself.
  - **Controls.** Held keys moved and turned, and Use and Tab worked.
  - **Audio.** −19.9 dB at its peak.
  - **Not done.** The level was not played to its exit: the corridor's end
    door did not open from where the player stood.

## Ultima VII: The Black Gate

**Failed: [#1485](https://github.com/DeanoC/fes/issues/1485).** No new game
could be started at either preset.

- **At 386DX-25.** The introduction plays to the main menu (frame
  `6efc1e18`). On the menu, 0 of 3 clicks on Start New Game landed (frame
  `6cf6e3ba`).
- **At `max`.** Four launches, each after a scrub, showed three different
  states:
  1. **Intro replay stall.** The first launch reached the menu. While the
     mouse moved, the menu went back to its introduction after about 20 s.
     The replay froze on its first frame (`ecb9eeae`): 0 new pictures in
     6 s and in 4 s at 60 fps, and audio at the −47.9 floor. Keys and
     clicks changed nothing for 2 minutes, so the machine was stopped.
     This is recorded on #1433. Later replays in the same session, one
     chosen from the menu and one after 25 s idle, played normally.
  2. **Menu stops taking input.** Seen on the third and fourth launches,
     with the menu music playing and the cursor following the mouse.
     Clicks on Start New Game did nothing, Enter did nothing, and the
     menu no longer went back to its introduction:
     - 9 of 9 clicks missed on the fourth launch, over 40 s (frames
       `99b7a9af`, then `e09f760a` after Enter);
     - presses of 0.16 s, 0.3 s and 1.5 s all missed on the third.
  3. **Character screen.** On the second launch, a click opened the
     character screen. After typing a name, a click on Journey Onward
     made the mouse cursor vanish.
     - After that no click, Esc or Enter acted.
     - Typed letters still reached the name field (frame `a4fe6f17`).
       This is the state #1427 describes at 386DX-25.
     - #1409's `max` run on seal `c7e06e04` also needed a second click,
       96 s after the first, to leave this screen.
- **Clicks over the whole run.** Clicks aimed at View Introduction started
  it 3 of 3 times. Clicks on Start New Game opened the character screen 1
  time in 13 at `max` and 0 in 3 at 386DX-25. Clicks on Journey Onward
  started 0 games in 2.
- **CuteMouse sees every click (M33).** At the DOS prompt, M33 ran in the
  same entries. Both presets give the same counts:
  - **Presses.** Each left press of 0.16 s, 0.3 s and 1.5 s counts as one
    press and one release, and a right press counts the same.
  - **Held button.** Function 3 reports it (B=1) while it is down.
  - **Position.** It moves exactly with the packets.

  Frames: [max](kit-r1-m33-max.png), [386DX-25](kit-r1-m33-386dx25.png).
  The press counts reach the INT 33h driver intact, at either speed.
- **How the game reads the mouse.** MAINMENU.EXE and U7.EXE call INT 33h
  only for functions 3, 4, 7 and 8, found by a byte search of the copies:
  they poll the button state and set the position and range. Neither
  installs an event handler.
- **Not checked:**
  - NPC pace, and save and load across a relaunch, both blocked by #1485.
  - The pace reference is also still missing (#1368).

## Ultima VII Part Two: Serpent Isle

**Failed: [#1485](https://github.com/DeanoC/fes/issues/1485).**

- **At `max`.** The menu came up 14 s after Esc skipped the introduction.
  0 of 5 clicks on Start New Game landed, and Enter did nothing (frames
  `58beee5a` and `d8957e74`). This is the same as The Black Gate's second
  state.
- **At 386DX-25.** The introduction plays, at 5.5 new pictures a second in
  a 10 s sample. Its audio has a median of −31.5 dB, with peaks to
  −12.5 dB. Esc leads to the main menu (frame `99817b25`).
- **Not checked:** save and load (#1485).

## Kit housekeeping

- **Trees.** Run `r1` created 25 top-level FogCast data trees on Kit B:
  11 game records, 11 ATA disk deltas and 3 staged disk images. It also
  staged the shared scrub image again, which is kept.
- **First removal, 22:37.** It halted before removing anything, because
  FogCast had already evicted all three staged disk images while #1433's
  run `p1` staged its own (22:22–22:37). The kit was checked unchanged and
  released.
- **Second removal, 22:38.** It removed the 22 remaining trees, each
  checked against the run's `kit-new-paths.txt`, and nothing else. The
  snapshot keys matched before and after, and the agent was idle with a
  free lease.
- **Status file.** Kit B's status file logs both removals.
- **Kit A.** The attempt there added nothing, so it needed no cleanup.

## Frames

Only DOS screens are committed. Game frames and benchmark screens stay on
Powerboat (`~/fes-scratch/m5done/runs/r1/fr`), and their SHA-256s are in
[kit-runs.json](kit-runs.json).

| File | Content |
| --- | --- |
| [kit-r1-a20test.png](kit-r1-a20test.png) | `A20TEST`: every check `ok`, then `A20 DONE ok`. |
| [kit-r1-blaster-240-mem.png](kit-r1-blaster-240-mem.png) | `set` with `BLASTER=A240 I5 D1 T3`, and `MEM` with 575K free, in `r1-fd240`. |
| [kit-r1-m33-max.png](kit-r1-m33-max.png) | M33 at `max`: lines `a`–`j`, every press counted, the held button `B=1`. |
| [kit-r1-m33-386dx25.png](kit-r1-m33-386dx25.png) | M33 at 386DX-25: lines `a`–`h`, the same counts. |
