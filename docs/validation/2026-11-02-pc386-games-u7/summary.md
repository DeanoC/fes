# pc386 games image for Ultima VII, 2 November 2026

[#1369](https://github.com/DeanoC/fes/issues/1369), its games half; PR
[#1409](https://github.com/DeanoC/fes/pull/1409). #1373 closed the CPU half
(protected-mode entry and unreal mode), and #1391 made the BIOS hand DOS a
machine with A20 off.

The FreeDOS games image gets the memory Ultima VII needs: all extended memory
as XMS (HIMEMX without `/MAX=0`), the machine's upper RAM as upper memory
blocks (UMBM), and the kernel kept low. This record covers the PR's last
steps: main merged, `HIMEMX /METHOD:KBC` dropped, the first seal of main's
combined #1373 + #1391 tree, and the kit runs on that package.

**Result.**
- With #1391's BIOS, HIMEMX picks the port 92h method by itself ("Fast A20
  method used", in simulation and on Kit B). XMS 04h (global disable A20)
  returns success and turns A20 off, which is what Ultima VII's Voodoo memory
  manager needs as it exits. `/METHOD:KBC` is gone.
- The seal of the merged head passes every analysis: setup +1.131 ns (pixel),
  hold +0.092 ns, 28,211 ALMs.
- On Kit B, The Black Gate plays its introduction to the main menu and starts
  a new game in Trinsic. Wolfenstein 3D runs with all of its XMS. Ultima VI
  plays through character creation into the game, at max and at the 286-12
  preset.
- Two checks did not pass, and neither comes from this PR's change:
  - Serpent Isle's loader exits at once and leaves all XMS allocated, under
    either A20 method, with XMS capped at 2048K, and with Bochs as the CPU in
    the machine sim ([#1426](https://github.com/DeanoC/fes/issues/1426); it
    may be a bad copy).
  - At the 386DX-25 preset, The Black Gate's character screen takes keys but
    no mouse clicks, so the new game doesn't start
    ([#1427](https://github.com/DeanoC/fes/issues/1427)).

## Inputs

- **Branch.** `deano/pc386-games-u7`: the PR's two commits on `011a16bf8`,
  main `3d2e17f84` merged in `3f2e47ebe` (plain merge, no conflicts), and
  `6d4c86227`, which drops `/METHOD:KBC`.
- **Seal.** A clean checkout of `3f2e47ebe` (see [Seal](#seal)).
- **Games image.** `build_pc386_freedos_image.py --profile games` at
  `6d4c86227`: SHA-256 `504015dc38358c21…`. `FDCONFIG.SYS`:

  ```
  FILES=20
  BUFFERS=20
  DOS=UMB
  DEVICE=C:\FDOS\BIN\UMBM.EXE /I=C800-EFFF
  DEVICEHIGH=C:\FDOS\BIN\HIMEMX.EXE /VERBOSE
  SHELL=C:\COMMAND.COM C:\ /E:1024 /P=C:\AUTOEXEC.BAT
  ```

- **Firmware.** BIOS `fd0df1f9f64d3f20…` (#1391), VGA BIOS `69401dc8…`.
- **Games.** Deano's eXoDOS 6.04 zips, read in place on Powerboat and copied
  onto copies of the games image with mtools. No game bytes are in this
  repository; the disks stay in a 0700 scratch directory and were evicted
  from the kit after the runs. Paths and hashes are in
  [`kit-runs.json`](kit-runs.json):
  - The Black Gate: `Ultima VII Part 1 - The Black Gate + Forge of Virtue (1992).zip`,
    `75e6eb68…`, 399 files in `C:\ULTIMA7`;
  - Serpent Isle: `Ultima VII Part 2 - Serpent Isle + The Silver Seed (1993).zip`,
    `5601ca4e…`, 433 files in `C:\SERPENT`;
  - Wolfenstein 3D: `Wolfenstein 3D (1992).zip`, `f451d853…` (the census
    zip), 11 files;
  - Ultima VI: `Ultima VI - The False Prophet (1990).zip`, `5ec54a04…` (the
    census zip), 324 files.

  Each disk boots to the prompt with the image's own configuration; the kit
  driver types the commands.
- **Tools.** Verilator 5.032; Quartus Prime Lite 17.0.2 with the pc386
  lock's Yosys `0a7927bf`; NASM 2.16.03 from the test386 runner.

## The change

- `build_pc386_freedos_image.py`: the games profile's HIMEMX line loses
  `/METHOD:KBC`.
- `A20TEST.COM`'s mode F, for the games image, now checks what Voodoo needs.
  Before, it allowed for `DOS=HIGH`'s kernel lock; the image dropped
  `DOS=HIGH`, so nothing but the program holds an XMS enable:
  - `xms-start`: A20 is off when the program starts;
  - `xms-query`: XMS 07h agrees with memory;
  - `xms-global`: 03h turns A20 on, and 04h turns it off and returns success;
  - `xms-lock`: 05h and 06h likewise.

  The `xms-global` and `xms-lock` steps are now shared with mode X (MS-DOS).
- The `a20` suite runs the games image on the RTL CPU as well, now that the
  CPU has protected mode.
- The core README and the image script's docstring say HIMEMX picks its own
  method. The machine design doc never named the switch, so it is unchanged.

## Checks

| Check | Result |
| --- | --- |
| `a20` suite, oracle CPU ([report](a20-report.json)) | Pass. Boot image: the 27 checks as before. Games image: `xms-start`, `xms-query`, `xms-global` (3 + 3), `xms-lock` (3 + 3), `DONE`, all ok. HIMEMX: "Fast A20 method used". |
| `a20` suite, RTL CPU ([report](a20-rtl-report.json)) | Pass, both images, every line ok, with 0 oracle steps on either: what `rtl_failures` now requires of this run (Codex's P1, fixed in `21fc68592`; the run predates the fix, and its report passes the new check). Boot image: the 27 checks, one CPU reset. The four INT 15h AH=87h moves enter protected mode on the RTL (4 entries, 68 retirements with PE set), where #1391's run on main's CPU handed them to the oracle. Games image: the 15 checks, and HIMEMX says "Fast A20 method used"; 1 protected-mode entry (HIMEMX's). |
| RTL machine sim, games image, `mem` typed (`boot --image games --cpu rtl --type 'mem\|' --expect 'Largest executable program size'`, [report](boot-games-rtl-report.json)) | Pass. HIMEMX: "Fast A20 method used". The prompt at 2.40 s and MEM's output by 3.65 s: 573K conventional, 154K upper, 15,360K of XMS with 159K used (FreeCOM's swap) and 15,201K free. All 98,307,014 instructions retired on the RTL: 0 oracle steps, 1 protected-mode entry and 6 retirements with PE set (HIMEMX's INT 0Dh handler). FESENV, CTMOUSE and MEM ran through FreeCOM's XMS swap. The VGA replay matches Bochs's VGA in every event and frame. |
| `system_tb` (`pc386_system`, RTL CPU, no oracle), games image, `--seed 1 --exercise-input 0` ([report](system-games-report.json)) | Pass. The disk is a scratch copy of the games image (`abea6710…`) whose AUTOEXEC.BAT starts with the bench's write-test lines (`PATH C:\` then `VER`), then runs the image's own lines and MEM. HIMEMX: "Fast A20 method used". `C:\>` at 2.90 s after 47,715,125 instructions, MEM showing 15,360K of XMS with 15,201K free. Every check passes: the bench's disk write, the dirty map, the save, the record and its round trip. The input exercise is off because the disk has no INPUT.COM. |
| `system` suite, the bench's own boot fixture ([report](system-report.json)) | Pass. `C:\>` at 3.31 s after 55,718,724 instructions; the typed echo, INPUT.COM's mouse and key codes, the dirty map, the save, the record and its round trip all check. |
| `system` suite at 386DX-25 (`--preset 3`, [report](system-preset3-report.json)) | Pass. The governor held the CPU (`governed=1`); `C:\>` at 4.22 s, and every check passes. The #1372 bench gap (a hold inside a port-2 capture burst) did not occur on this run. |
| `sim-fes-pc386-fesenv` (`boot --image games`, oracle CPU, `set` typed, [report](boot-games-fesenv-report.json)) | Pass. `BLASTER=A220 I7 D1 T3` at the prompt (3.70 s); the VGA replay matches. CI's PR tier doesn't run this target, and it boots the changed image. |
| `tests.test_pc386_machine` | Pass: 33 tests on the merge and after the change, and the A20 tests again with the P1 fix's new test. |
| CI | Green on `6d4c86227`: consistency, fpga-producer, the pc386 simulation lane, integration, parent tests, simulation tools. |
| Codex | One P1 on `d3cb4c239` (an RTL A20 run could pass with oracle steps), fixed in `21fc68592` with a unit test; clean on `21fc68592`. |

## Review follow-up: conventional memory

The games profile now uses `BUFFERS=16` (down from 20). With the pinned
Verilator 5.032 and Bochs oracle CPU, the games-image MEM boot reports 588,544
free conventional bytes. Serpent Isle's published 573K requirement is
586,752 bytes, leaving 1,792 bytes; the simulation check requires at least
1,024 bytes of headroom. The exact report is
[`boot-games-memory-report.json`](boot-games-memory-report.json). The VGA
replay matched with zero event, read, VRAM or frame mismatches.

This is host simulation evidence only. The kit runs above used the earlier
20-buffer image; Serpent Isle has not been retried on a kit or with a checked
`SERPENT.CFG` here. Issue [#1426](https://github.com/DeanoC/fes/issues/1426)
remains open pending that game check.

## Seal

Quartus 17.0.2 sealed `3f2e47ebe` from a clean checkout (seed 1,
[`seal-summary.json`](seal-summary.json)). This is the first seal of main's
combined tree with #1373's CPU and #1391's BIOS.

- **Identity.** Build `14d7b6c398a3628781fb6d6011fe5472`, package
  `c7e06e042a045b9c453be36b1e4874497dba99132fe043d68f39545ca3b1727c`, RBF
  `3abade51…`.
- **Timing.** Every analysis passes on every clock:
  - setup +1.131 ns, on the 74.27 MHz pixel clock;
  - hold +0.092 ns;
  - recovery +7.422 ns;
  - removal +0.271 ns;
  - minimum pulse width +1.122 ns.

  The system clock is 61.44 MHz with +2.593 ns of setup slack, Fmax 73.08 MHz
  at the slow −40 °C corner (73.43 MHz at slow 100 °C).
- **Resources.** 28,211 ALMs, 25,344 registers, 257 RAM blocks, 32 DSP blocks.
- **Against #1373's merge seal** (build `530f136f`, package `604cab9b`): setup
  +1.049 → +1.131 ns, hold +0.080 → +0.092 ns, 28,311 → 28,211 ALMs. Of the
  109 pinned inputs only `firmware.lock` differs, #1391's BIOS; the RTL is
  the same.
- **Coverage.** All 109 pinned inputs hash the same at `6d4c86227` and at
  this record's commit. The later commits change the image script, the A20
  test, its suite, tests and documents, none of them sealed inputs.

## Kit

All on Kit B, core-only on its logged baseline (main `30595667`, software
`f786f7b3`, OS `f904961e`; nothing installed, no reboot), with package
`c7e06e04…` loaded through the agent as library games
([`kit-runs.json`](kit-runs.json)). Each claim is logged in
`kitB-STATUS.md`, and the kit was released with the baseline unchanged after
each.

**Scrub first.** A kit load keeps guest RAM
([#1408](https://github.com/DeanoC/fes/issues/1408)). Every claim first
booted a FreeDOS disk running SCRUBX with all regions (video, the UMB window,
1-16 MiB, conventional), as its own fresh library game, and then loaded the
game. What ran on the kit before each claim is in `kit-runs.json`. Run k1
followed the i386 Phase 2 session's MS-DOS runs, and its scrub found
conventional memory non-zero (`nz=4D1B4h`), as those runs leave it
([frame](kit-k1-scrub.png)); each later claim followed the previous one here.

**Fresh game ids.** Every load used a new library game id (`g1409-kN-…`), so
no disk delta from an earlier run applied.

| Run | Game, preset | Result |
| --- | --- | --- |
| k1 | The Black Gate, max | Pass. The kit boots the image with "Fast A20 method used" ([frame](kit-k1-boot-fast-a20.png)); MEM shows 573K conventional, 154K upper and 15,360K XMS ([frame](kit-k1-mem.png)). `ULTIMA7` plays the whole introduction with sound, through the Guardian (frame `050237f8849c`) and the moongate, to the main menu (frame `4248faccbc28`). Start New Game, a name, Journey Onward: the Avatar arrives in Trinsic and Iolo speaks (frame `3228bb170a72`). Each step from MAINMENU to INTRO and to the game exits Voodoo, whose exit is the XMS 04h that `/METHOD:KBC` used to stand in for. |
| k2, k3 | Serpent Isle, max | Fail, [#1426](https://github.com/DeanoC/fes/issues/1426). `SERPENT` comes back to the prompt at once. A second `MAINMENU` then reports "requires 1024000 bytes of extended memory! You only have 0 available" (frame `7e16e91a38ea`). k3 removed the eXoDOS `XMMHAND.DAT` leftover: no change. |
| k4 | Wolfenstein 3D, max | Pass. The sign-on shows all 1,000K of XMS available, the mouse and the Sound Blaster (frame `57a8838cbb59`). New game, E1L1: moves, turns and fires, ammo 8 → 7 (frame `4bf603c63c59`). F10 quits to DOS, and MEM shows 15,201K of XMS free again ([frame](kit-k4-mem-after-wolf3d.png)). Audio peak 22,957. |
| k5 | Ultima VI, max | Pass. Intro with music, menu, character creation (the gypsy's questions), into the game in Lord British's throne room (frame `ba063754f830`). Holding Down for 3 s and then Right for 3 s walked the Avatar out of the throne room into the next room (frame `9fa1f1749156`). |
| k6 | Ultima VI, 286-12 (preset 1) | Pass to the game. The preset was written to the fresh game id before its first load (the record reads back `preset: 1`). Menu (frame `32d393953a3a`), character creation and the game start as at max, with longer waits. The same held keys walked the Avatar about two tiles (frame `5b30a255fe3d`). |
| k7 | The Black Gate, 386DX-25 (preset 3) | Partial, [#1427](https://github.com/DeanoC/fes/issues/1427). The introduction plays through to the main menu, and Start New Game opens the character screen (frame `073a496c156b`). The screen takes keys (frame `9f1b72a8ad94`), but in 25 minutes no click on Journey Onward or Return to Menu landed. |
| k8 | Serpent Isle, diagnosis | `MAINMENU` alone on a fresh boot says "Type SERPENT to play Serpent Isle." with XMS untouched (frame `48e151e9ad32`), so k2/k3's message is a symptom: the first `SERPENT` run leaves the whole XMS block allocated. A scratch copy of the disk with `HIMEMX /METHOD:KBC` ("KBC A20 method used", [frame](kit-k8-si-kbc-boot.png)) fails the same way and leaves 0K of XMS free (frame `1849fa25be0d`). So dropping `/METHOD:KBC` is not the cause. |
| k9 | Serpent Isle, XMS capped | Scratch copies with `HIMEMX /MAX=6144` and `/MAX=2048` ("Maximum XMS: 2048K", [frame](kit-k9-si-max2048-boot.png)) fail the same way. Serpent Isle is said to fail on real hardware with more than about 7000K of XMS; that may matter once #1426 is fixed, but it is not what stops it here. |

**Measurements for #1368's 286 checks.** In k5 and k6 the same script held
Down for 3 s and then Right for 3 s at the game's start, in combat with the
gargoyles, while a 30 fps capture of the map view ran. Map scrolls counted
from the capture:
- max: 12 scrolls in 4.4 s;
- 286-12: 4 scrolls.

Each step waits for the party's and the gargoyles' combat turns, which run
several times slower at 286-12. This shows the preset slows Ultima VI's walk,
but it is not a comparison with a reference: no recording from period
hardware or a configured emulator exists on Powerboat. The speaker-pitch
check could not be measured: the AdLib music plays throughout the capture
(about −35 dBFS) and masks the speaker effects. Both #1368 items stay open.

**Cleanup.** Two cleanup claims, after k8 and after k9, each staged four
1 MiB filler images with distinct markers, so the agent's keep-four policy
evicted the game disk images. Each then probed every game and scrub disk's
hash: none is staged. The agent has no delete route for per-game data: the
disk deltas (`core-data/media/<id>`, whole 64 KiB slices of the disks that
were written) and game records (`core-data/games/<id>`) of these runs stay on
Kit B, listed per run in `kit-runs.json`.

**Frames.** Only frames of FreeDOS, HIMEMX, MEM and SCRUBX output are in this
record. Frames that show what a game drew are not committed: the table gives
each one's SHA-256 (the 1920x1080 HDMI grab), its file in the run directory
under `~/fes-scratch/games/runs/` on Powerboat, and what was on the screen.

| SHA-256 | Grab | On screen |
| --- | --- | --- |
| `050237f8849cab910bb5ab7f51579046997903b05a07a28a6e1e9ff7fc011013` | `g1409-k1/fr/i17.png` | The Black Gate introduction: the Guardian's face speaking, about 70 s in. |
| `4248faccbc28f63aafe55be1b099b56ac92e6fab93a5289acbcf311ca9ee542f` | `g1409-k1/fr/i36.png` | The Black Gate main menu after the whole introduction: View Introduction, Start New Game, Journey Onward, View Credits. |
| `3228bb170a72022abb455da647061c39fd4930bee2d644828ec363620b87bb00` | `g1409-k1/fr/h23.png` | The Black Gate new game: the Avatar beside the red moongate in Trinsic, with Iolo's portrait and his opening line. |
| `57a8838cbb5975d2de66cf6e5108b92ff05609e2486cde6fc02261fc36ea7096` | `g1409-k4/fr/w01.png` | Wolfenstein 3D sign-on: main memory to 256K, EMS none, XMS all ten bars (1,000K); Mouse and Sound Blaster lit. |
| `4bf603c63c59949210b1f8c906f1da7ccc1c1a69ce59765e9c50689bfc7a5003` | `g1409-k4/fr/p3.png` | Wolfenstein 3D E1L1 after moving, firing and turning: floor 1, score 0, 3 lives, health 100%, ammo 7. |
| `ba063754f8309b55e7c421889d545a3d6df1dc6646cdbf8abf636a3bb0104565` | `g1409-k5/fr/start.png` | Ultima VI at max: the game's start in Lord British's throne room, party of four at 90 HP, gargoyles present. |
| `9fa1f1749156332a2891d9659cc8d44f1f9d6519343e473c7e6824924f0659ab` | `g1409-k5/fr/after-walk.png` | Ultima VI at max after 3 s Down and 3 s Right: the Avatar in the room south of the throne room. |
| `32d393953a3a9fe98477412f04e0010081794130a6d53028eaf660871d76e41c` | `g1409-k6/fr/menu.png` | Ultima VI at 286-12: the main menu (Introduction, Create a Character, Transfer, Acknowledgements, Journey Onward). |
| `5b30a255fe3d3568663bb558feb034b41c224ca766eb32dfbadde9958ed3d068` | `g1409-k6/fr/after-walk.png` | Ultima VI at 286-12 after the same keys: the Avatar about two tiles south and east of the start, still in the throne room. |
| `073a496c156b1cfd2a047df33886d059ee9da1fb915607d8eaa47eec338817d4` | `g1409-k7/fr/f4.png` | The Black Gate at 386DX-25: the character screen (Name, Sex: Male, portrait, Journey Onward, Return to Menu) after the menu click. |
| `9f1b72a8ad944e9f6b015eae6217fcb9f459f7e351719b933fd48745242155c4` | `g1409-k7/fr/esc1.png` | The Black Gate at 386DX-25: the same screen 25 minutes later; the name field reads "Avatarx" (typed keys arrive), the cursor is over Return to Menu. |
| `48e151e9ad32038f1f339262fded109ebb5efe0147f4da96282f101b6940bb32` | `g1409-k8/fr/a-mainmenu2.png` | Serpent Isle: MAINMENU run alone on a fresh boot prints "Type SERPENT to play Serpent Isle." and returns to the prompt. |
| `1849fa25be0d79cc6353174feca1db0c3a4bef2194e9b6f09b0dc70f94a4eae4` | `g1409-k8/fr/b-mem.png` | Serpent Isle, /METHOD:KBC disk, after `serpent`: MEM in the game's loaded text font shows Extended (XMS) 15,360K used, 0K free. |
| `7e16e91a38ea57432ef9b32889cdece4839136b9eb4d93fb39e0f7884278f0b3` | `g1409-k3/fr/dir.png` | Serpent Isle: a second MAINMENU prints "Serpent Isle requires 1024000 bytes of extended memory! You only have 0 available.", then a DIR /W of C:\SERPENT. |

## Not done here

- **Serpent Isle** ([#1426](https://github.com/DeanoC/fes/issues/1426)): the
  machine sim with Bochs as the CPU does the same. MAINMENU sets up and frees
  its XMS block cleanly and exits with code 6. The program the loader runs
  next allocates the whole block, opens three data files read-write, and
  exits with code 1 without freeing the block or printing anything. Every
  XMS call succeeds, so neither the A20 method nor the CPU is involved. Next:
  this copy under DOSBox, then a copy installed from original media.
- **The Black Gate at 386DX-25 into a new game**
  ([#1427](https://github.com/DeanoC/fes/issues/1427)).
- **#1368's 286 checks against a reference**: there is no reference
  recording yet.
- **A20TEST under MS-DOS 6.22** (optional in the brief): not run.
