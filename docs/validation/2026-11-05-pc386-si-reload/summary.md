# pc386 Serpent Isle save, stop and relaunch, 5 November 2026

Issue [#1494](https://github.com/DeanoC/fes/issues/1494): in #1485's Kit B run
`v1`, Serpent Isle's Journey Onward after a stop and relaunch showed only black
for at least 60 s while audio played. This record repeats that run three times
on Kit B, with #1483's CPU status ring in the first.

**Result: not reproduced.** Every save loaded and played after a stop and a
relaunch. The disk delta and record round trip is byte-exact. The game's save
file matches the GAMEDAT files it was packed from. The same steps on `v1`'s
own package, and `v1`'s whole command sequence replayed with its pacing, also
show Serpent Isle's intro, menu and loaded game. No machine, CPU, VGA or
runtime defect was found, and nothing here changes RTL or code.

- **Run `k1`** (status-ring package): two saves, each followed by stop,
  relaunch and Journey Onward.
  - A save of the disk's existing GAMEDAT state (slot 1).
  - A save made during a new game's opening on the ship (slot 2). This is the
    state `v1` saved.
  - Both loaded within 6 s of the click and played on, with the cursor and
    party moving.
- **Run `k2`** (`v1`'s package, `499f4805…`): the same game record, launched
  again. Journey Onward loads the ship save and plays.
- **Run `k3`** (`v1`'s package): `v1`'s first 170 commands replayed in order,
  with `v1`'s gaps between commands. The Black Gate goes through to its view
  roll after Iolo's line ([#1493](https://github.com/DeanoC/fes/issues/1493)),
  the machine is stopped mid-roll, then a scrub, then Serpent Isle. Serpent
  Isle's intro and menu show at the points where `v1`'s grabs were black.

## Inputs

- **Packages.**
  - `k1`: `fes.pc386` `01e1eebb1ab03acd…`, #1483's status-ring seal (main
    `ee7ea9bb4` plus the ring).
  - `k2`, `k3`: `499f4805ca07ddc1…`, #1481's seal of main `ee7ea9bb4`. That is
    `v1`'s package.
  - Both include #1475's Sound Blaster ADPCM playback.
- **Disks.** #1485's games image with Serpent Isle (`6b4b0619…`, plain Sound
  Blaster `SERPENT.CFG` from #1443) and with The Black Gate (`eae30dea…`). Both
  use `MOUSEDEF.COM`. The game files come from Deano's eXoDOS 6.04 copy and
  are outside the repo.
- **Kit.** Kit B, core-only on baseline software `a8cd30c4` and OS `f904961e`.
  No install, image change or reboot. Each run claims, logs and releases the
  kit in Kit B's status file. Settings were `max`, 220h, IRQ 7, DMA 1, as in
  `v1`. A SCRUBX scrub ran before each run's first launch. Relaunches after a
  stop had no scrub, as in `v1`.
- **Runners.** Powerboat `~/fes-scratch/si1494`: `run-si.sh`, `hil_si.py` and
  `clean-si.sh`, copies of #1485's `u7menu` runners. Frames, audio, the pulled
  delta and the scratch tools `apply_delta.py` and `flexcheck.py` stay there.

## Evidence

Frames are listed by the first 12 hex digits of their SHA-256 and their mean
and peak grey level (64×36 grey thumbnail), never as images.

### The relaunch loads (k1, k2)

| Run, frame | What it shows | SHA-256 | Mean, peak |
| --- | --- | --- | --- |
| k1 `r-jo1` | Journey Onward after a relaunch, disk's existing GAMEDAT | `3c16410d2151` | 79.1, 216 |
| k1 `saved` | Save dialog, slot 1 written | `79d58f64f49c` | 76.4, 216 |
| k1 `l-jo1` | Relaunch, Journey Onward loads slot 1's state | `8eb68cfe0a19` | 78.9, 216 |
| k1 `n-jo2` | New game started (the ship) | `2c53dcab93fa` | 61.1, 127 |
| k1 `n-g1` | The ship conversation | `9c333708af74` | 31.0, 183 |
| k1 `n2-saved` | Slot 2 written during the ship scene | `00440e56e9fe` | 61.2, 141 |
| k1 `m-jo1` … `m-jo10` | Relaunch, Journey Onward loads the ship save, 6 s apart over 60 s | `dea1a2cb0c41` … `2c78b88cac8e` | 51.2 … 46.9 |
| k2 `menu1` | Main menu on `v1`'s package | `99817b25dc19` | 19.3, 227 |
| k2 `jo1`, `jo8` | Ship save loaded on `v1`'s package | `9ea14bee75dd`, `121a7e7fe902` | 51.1, 41.5 |

In `k1` the status ring showed the CPU at MAINMENU's code (CS 2539h) in the
menu, and in SI.EXE's segments (CS 471Dh, 248Dh, 2DDEh, 35D4h) while each load
played. There were no CPU resets, and the retired-instruction count kept
rising.

### The disk delta and the save files (k1)

The Stop after each save published the game's `fes.ata-disk.delta` manifest.
It was read from the kit read-only and applied to the base image on Powerboat.

- **First save.** Manifest `f8d387724cfd804f…`, 23 chunks of 64 KiB.
- **Second save.** Manifest `f52bfa17e3b76d78…`, 30 chunks.
- **Checks.** Each chunk file's SHA-256 matches its name and its manifest
  entry. The manifest's trailing SHA-256 matches its body. The base size is
  33,546,240 bytes.
- **The restored FAT volume.** It reads cleanly with mtools. `C:\SERPENT`
  holds `GAME00.U7` (442,296 bytes, `5f9931b593d1591d…`) and `GAME01.U7`
  (442,086 bytes, `ef9a6780527577663…`).
- **The save files.** Each one is a FLEX file (magic `FFFF1A00h`, 300 slots,
  177 non-empty). Every entry is byte-identical to the GAMEDAT file of its
  name, and no entry is missing from GAMEDAT.
- **Changed files.** Against the base image, the first save changed eight
  GAMEDAT files (`ACTION.DAT`, `FRAMES.FLG`, `GAMETIMN.DAT`, `MAPCOORD.DAT`,
  `PARTY`, `RANDSEED.DAT`, `U7IBUF.DAT`, `WEATHER.DAT`) and `XMMHAND.DAT`,
  and added `GAME00.U7`.

So the save and its delta survive Stop and relaunch intact. The game file
bytes stay on Powerboat.

### What run `v1`'s black frames were

- **They are real black output.** All of `v1`'s Serpent Isle "black" grabs,
  from the first launch's intro and menu to the reload's `si2-load*`, are the
  same flat-black frame, `4c6d2c8d8bbb`. Serpent Isle shows that frame between
  intro scenes on every run (`k1` `intro2`), and #1426 recorded it too. It is
  not a capture failure.
- **The game was running behind the black.** The reload's 5 s of audio in
  `v1` measured mean −46.4 dBFS and peak 2,687, in 100 ms RMS windows. A
  loaded game in `k1` measured −46.5 dBFS and peak 2,576. That is the game's
  music at the same level, not silence.
- **It was black from the first launch.** In `v1` the main menu was black 14 s
  after Esc. A blind click still opened the character screen, which showed.
  In `k1`, `k2` and `k3` the menu is fully shown 10 s after Esc
  (`92e9ceae831c`, `99817b25dc19`, `10562c0d0491`) and stays until its idle
  return to the intro, about 35 s later.
- **`k3` replays that history.** It sends `v1`'s commands with `v1`'s gaps.
  The Black Gate frames match `v1`'s brightness frame for frame and reach the
  roll (`v-bg-end`, `9d3b13add315`, picture torn below Iolo's line). After the
  stop and scrub, Serpent Isle's intro at 30 s (`f268560544dc`) and the menu
  at Esc + 14 s (`10562c0d0491`) are both shown where `v1` was black.

What made `v1`'s Serpent Isle output black on both launches is therefore not
known. It did not come from the package, the save, the disk delta, the game
record, the relaunch without a scrub, or the preceding Black Gate roll. The
roll itself is the VGA BIOS stuck in INT 10h AH=07h, which #1493's PR #1504
fixes.

## Not done

- **No bench comparison of a load.** Nothing failed on the kit, so there was
  no failing load to compare on the Bochs or RTL CPU. A Bochs bench run of
  Serpent Isle's intro and menu (scratch hooks from #1455) was started as a
  control and is not part of this result.
- **Unexplained `v1`.** If Serpent Isle shows black again on a kit, sample
  #1483's status ring and the DAC state at once. `v1`'s package had no status
  ring, so its CPU position is unknown.
- **#1427 seen at `max`.** On `k1`'s first new-game attempt, clicks on the
  character screen did nothing. The moves were 0.1 s apart and each click was
  two press and two release packets. That covered Journey Onward (several
  spots), Return to Menu, longer presses (up to 0.5 s) and Enter or Esc
  first. The cursor moved; only the clicks failed. After a stop and relaunch,
  `v1`'s exact sequence worked first time: name, moves 0.04 s apart, then two
  clicks. #1427 records this screen at 386DX-25.

## Kit hygiene

Every claim, release and baseline check is in Kit B's status file (`k1`
20:55–21:24, `k2` 21:24–21:28, `k3` 21:29–21:45 EET). The FogCast trees the
three runs created are listed in `runs/kall/remove.txt` on Powerboat (13 trees:
six game records, six media deltas and the staged Serpent Isle disk). They are
removed by `clean-si.sh` under its own claim, and its result is logged in the
status file.
