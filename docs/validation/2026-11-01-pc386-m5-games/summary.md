# pc386 M5: FreeDOS, Wolfenstein 3D and Ultima VI on a kit, 1 November 2026

This record covers the last slice of [#1267](https://github.com/DeanoC/fes/issues/1267)
([#1339](https://github.com/DeanoC/fes/issues/1339)), the part that today's CPU can do.
On Kit B, the sealed `fes.pc386` package ran FreeDOS, Wolfenstein 3D and Ultima VI at `max`.
Each ran from a FreeDOS image in the acceptance setup: HIMEMX with `DOS=HIGH`, CuteMouse, MEM,
and `BLASTER` taken from the machine setting. Input came through the FogCast agent's
remote-input stream.

- **FreeDOS.** It boots from the ATA image to a prompt, HIMEMX and CuteMouse load,
  MEM reports 549K free, and a file written before Stop is there after the relaunch.
  `BLASTER` reads `A220 I7 D1 T3` by default and `A240 I5 D1 T3` once the setting
  moves to 240h and IRQ 5. Wolfenstein 3D's digitised sound plays at 240h and IRQ 5.
  The date and time do not match the kit: the kit's clock reads 1970, and the
  contract clamps that to 1980-01-01.
- **Wolfenstein 3D.**
  - The setup is Sound Blaster and AdLib. The title screen and the menus work by keyboard
    and by mouse.
  - AdLib music and Sound Blaster digitised effects are audible.
  - Turning gives a new frame in every 30 fps capture, with no torn frames.
  - A saved game loads after a relaunch.
  - Nobody played a level through.
- **Ultima VI.** The setup is VGA and AdLib, and its music plays. A character
  created on the kit and saved in game loads after a relaunch. The 286-preset checks
  (speaker pitch, walking speed) wait for the governor.

Planning turned up one finding. HimemX's XMS block move runs SMSW, then enters unreal
mode, and `fes_i386_cpu` has neither yet. Two programs would use those moves:
- FreeDOS 1.4's FreeCOM swaps itself through XMS.
- Wolfenstein 3D's page manager caches pages in XMS when it finds free XMS.

Either would stop the CPU. So the image runs HIMEMX with `/MAX=0`: HIMEMX manages
the HMA and A20 and offers no extended memory. That limit goes when the CPU has
protected mode, which Ultima VII needs anyway.

This record has host-only checks (the image builder's tests, an oracle and an RTL
boot of the image in simulation) and exact-artifact hardware evidence on Kit B.
The package is `457444bf…`, PR [#1335](https://github.com/DeanoC/fes/pull/1335)'s
seal, and the record says below why a rerun is owed once #1335 lands.

## Inputs

- **Branch.** FES branch `deano/pc386-m5-games`, cut from main `58edb6b3b`. The image
  change is `aa2779c75`. This PR changes no RTL.
- **Package.** `fes.pc386` `457444bf9ae851f7d115e0df1a0cead19f1b84cbfa6b27f04e93f635f806df52`
  (build `4681e16d…`). It is #1335's seal of `f2b12145a`, the keyboard and mouse
  slice with main `58edb6b3b` merged in. Its record is
  `docs/validation/2026-11-01-pc386-m5-input/summary.md`, added by [#1335](https://github.com/DeanoC/fes/pull/1335).
  - #1335 is still open. Its head is now `bcb9f9098`, which adds review fixes in
    `pc386_kbc.sv` and `pc386_hid_keyboard.sv` after that seal.
  - The package for main's pc386 tree once #1335 lands will therefore be a new
    seal. The kit runs below are owed again on that package.
- **Kit.** Kit B on its baseline: image `f904961e`, kit-software bundle `86cc27b6`,
  both from main `58edb6b3b`. All five runs were core-only: no install, image
  change, reboot or host change.
- **FreeDOS.** The FreeDOS 1.4 packages are pinned in
  `sources/misteross/cores/fes-pc386/sim/freedos.lock`. The games image
  (`build_pc386_freedos_image.py --profile games`) is
  `876986c069891f872ef6d808548dde4521fd5fbad103ceefa99f39d4c5687d7f`, and two
  builds made the same bytes. The boot fixture the simulation suites use is
  unchanged at `b9d36549…`.
- **Games.** The games are user-supplied, from Deano's eXoDOS 6.04 zips on Powerboat's
  games share ([`disk.json`](disk.json)).
  - `Wolfenstein 3D (1992).zip` has SHA-256 `f451d853…` and gives 11 files in `C:\WOLF3D`.
  - `Ultima VI - The False Prophet (1990).zip` has SHA-256 `5ec54a04…` and gives 324
    files in `C:\ULTIMA6`.
  - These are the zips the game census used
    ([record](../../../sources/misteross/docs/validation/2026-10-28-pc386-game-census.md)).
  - A Powerboat script copies the image and adds the games with mtools. The result is
    the kit disk, `6138390a…` (32 MiB, 65/16/63).
  - No game file, frame or recording is in this repository. The frames here are
    FreeDOS screens.
- **Tooling.** The kit runner lives on Powerboat in `~/fes-scratch/m5g`: `run-m5g.sh`,
  `hil_m5g.py`, `make_disk.py` and `analyze_m5g.py`. It extends #1335's m5i runner.
  - The driver holds the agent's kit lease, stages the disk, and runs commands I sent
    while I read its HDMI grabs.
  - Commands cover library launch and Stop, the machine settings, keys and mouse
    through `/v1/input/attach` and `CONNECT /v1/input/stream`, frame grabs, and ALSA
    and video capture.
  - Kit B's capture card is a 4KPRO: HDMI at 1920x1080 YUYV, 30 fps only, and audio
    from `hw:CARD=D4KPRO`.

## The games image

`build_pc386_freedos_image.py` now has two profiles. `boot` is the existing
simulation fixture, byte for byte. `games` adds HIMEMX 3.38, CuteMouse 2.1b4 and
MEM 1.12, from their FreeDOS 1.4 package commits, in `C:\FDOS\BIN`, with
`FESENV.COM` ([`fesenv.asm`](../../../sources/misteross/cores/fes-pc386/sim/fesenv.asm)):

```
FDCONFIG.SYS                                   AUTOEXEC.BAT
FILES=20                                       @ECHO OFF
BUFFERS=20                                     PROMPT $P$G
DOS=HIGH                                       PATH C:\FDOS\BIN;C:\
DEVICE=C:\FDOS\BIN\HIMEMX.EXE /MAX=0 /VERBOSE  FESENV > C:\FDOS\FESENV.BAT
SHELL=C:\COMMAND.COM C:\ /E:1024 /P=...        CALL C:\FDOS\FESENV.BAT
                                               CTMOUSE
                                               VER
```

FESENV reads CMOS 60h-62h, where the machine mirrors the latched Sound Blaster
setting. It prints `SET BLASTER=A2x0 Iy D1 T3`, or a `REM` line when the bytes hold
no valid setting.

**Why `/MAX=0`.** The code that rules out a normal HIMEMX setup:
- HimemX's EMB move (`HimemX.asm`, `@@move_ok_to_start`) runs `SMSW` to see whether
  the CPU is in protected mode. In real mode it then switches to unreal mode
  (`lgdt`, `mov cr0`) for the copy. `fes_i386_cpu` holds only CR0.MP and TS, and its
  stop test treats SMSW as a stop (`sim/rtl_stop_test.asm`). HimemX's start-up
  (A20 method, E820/E801/88h, INT 15h and 2Fh hooks) is real-mode only.
- FreeDOS 1.4's `COMMAND.COM` is the `XMS_Swap` build. Its `XMSinit` (`shell/cswapc.c`)
  allocates an XMS block at start-up and copies the message segment there. It then
  swaps itself through XMS for every external program. It does neither when the
  allocation fails.
- Wolfenstein 3D's page manager caches pages in XMS when HIMEMX offers free XMS,
  and moves them with the same call.

With `/MAX=0` HimemX keeps the HMA (so `DOS=HIGH` works) and offers no extended
memory, so none of these moves happens. MEM then reports 15,360K of XMS, all used.

**Memory.** With `DOS=HIGH` and FreeCOM resident, MEM reports 549K free
(561,712 bytes, the largest executable). `MEM /C` lists SYSTEM 14,032 bytes, HIMEMX 2,192,
COMMAND 73,040 and CTMOUSE 3,104
([`kit-m5g2-mem-c.png`](kit-m5g2-mem-c.png)). The CPU design's game table gives
575 KiB for Wolfenstein 3D. The game runs from this prompt in 549K, and its sign-on
screen shows about 256K of main memory for its own use. Upper memory blocks (Jemm's
UMBM over C8000-EFFFF, which is RAM) would free FreeCOM's 71K if a later title needs
it.

## Simulation

| Check | Result |
| --- | --- |
| `sim_fes_pc386.py --suite boot --image games --type 'mem\|'`, oracle CPU | Pass ([`sim-boot-games-report.json`](sim-boot-games-report.json)): the prompt at 3.7 s simulated. The screen shows "Always on A20 method used", the kernel's 46 buffers in the HMA, CuteMouse "Installed at PS/2 port", and MEM's 549K. The VGA replay matches Bochs (0 mismatches). |
| The same on the RTL CPU (`--cpu rtl`) | Pass, no stop, oracle step or protected-mode step ([RTL boot](#rtl-boot)) |
| `tests/test_pc386_machine.py` | 26 tests pass, including the new profile test |
| Both images rebuilt | Byte-identical: boot `b9d36549…` (unchanged), games `876986c0…` |

### RTL boot

`sim_fes_pc386.py --suite boot --image games --cpu rtl --type 'mem|'` passes
([`sim-boot-games-rtl-report.json`](sim-boot-games-rtl-report.json)). `fes_i386_cpu`
alone, with no stop, no oracle step and no protected-mode step, retired 98,946,812 instructions
from reset to the prompt at 3.65 s simulated. On the way it ran HimemX, CuteMouse's
INT 15h C2xx set-up, FESENV and MEM, and the screen ends as the oracle's and the
kit's do. The boot's VGA trace replays against Bochs's VGA with 0 mismatches.
The run took about 70 minutes of wall time on a shared Powerboat.

## Kit B

Five runs, each claimed in Kit B's status file. Each was released FREE with the
baseline keys unchanged: image, previous image, pending, idle RBF, launcher, images,
quirk, boot ID, and active bundle `86cc27b6` with nothing pending. The agent was idle
with a free lease. [`kit-runs.json`](kit-runs.json) has each run's launches, Stops,
settings writes, captures and wrapper log.

| Run | EET | Library games | What |
| --- | --- | --- | --- |
| m5g1 | 09:08-09:13 | `pc386-m5g-freedos` | FreeDOS boot, MEM, SET, DATE and TIME against the kit clock, a file written, Stop, relaunch |
| m5g2 | 09:15-09:17 | `pc386-m5g-wolf3d` | MEM /C; Wolfenstein 3D's sign-on, title and menu. The driver stopped on its own bug at the first mouse command (a field-name clash). Its cleanup stopped the machine and released the lease. |
| m5g3 | 09:18-09:30 | `pc386-m5g-wolf3d` | Menus by keyboard and mouse, the sound setup, title and menu music, a new game, five shots, a turn captured, save, Stop, relaunch, load |
| m5g4 | 09:31-09:47 | `pc386-m5g-sb240`, `pc386-m5g-ultima6` | The setting moved to 240h and IRQ 5, then SET and Wolfenstein 3D's digitised sound there; Ultima VI's intro music, character creation, save, Stop, relaunch, Journey Onward |
| m5g5 | 10:03-10:06 | `pc386-m5g-wolf3d-level` | An attempt at E1L1 on the easiest difficulty |

Each launch took 9.1-9.4 s through `/v1/library/core/load`. The runtime admitted the
package with `fes.keyboard.hid`, `fes.mouse.relative`, `fes.media.ata-disk`,
`fes.pc386.machine`, `fes.persistence.words` and `fes.rtc.wall-clock` among its
interfaces. The prompt was stable 32.5-32.8 s after each launch.

## Checks

Results against the per-game table in
[`pc386-machine-design.md`](../../../sources/misteross/docs/pc386-machine-design.md#per-game).
Audio levels are with the capture's DC offset removed (see findings), from
[`metrics.json`](metrics.json). Its burst lists are the 100 ms windows above
−40 dBFS by raw RMS; the −48 dBFS that the offset alone gives sits below that.

### FreeDOS

| Check | Result | Evidence |
| --- | --- | --- |
| Boots from the ATA image to a prompt | Pass | m5g1 to m5g5, every launch ([`kit-m5g1-boot.png`](kit-m5g1-boot.png)) |
| HIMEMX loads and its A20 test passes | Pass | HimemX 3.38 loads with `Interface : XMS 3.00 80386 4G`. Its A20 test found A20 on, as the Bochs BIOS leaves it, so HimemX chose its "Always on" method. The kernel moved into the HMA with its 46 buffers ("FreeDOS is resident in the high memory area"), which needs A20. The 8042 and port 92h A20 gate's own checks are in the devices suite. |
| CuteMouse finds the PS/2 mouse | Pass | "CuteMouse v2.1 beta4 [FreeDOS] / Installed at PS/2 port" at every boot. Through its INT 33h, the mouse moves Wolfenstein 3D's menu cursor and a click selects. Ultima VI shows its pointer. |
| `MEM` shows the free conventional memory each game needs | Pass | 549K, 561,712 bytes ([`kit-m5g1-mem.png`](kit-m5g1-mem.png)). Both games run from that prompt. |
| Date and time match the kit | Fail, blocked by the kit's clock | The kit reads 1970-01-01 17:49 UTC. Image `f904961e` predates [#1279](https://github.com/DeanoC/fes/pull/1279)'s NTP, and the kits have no RTC. The `fes.rtc.wall-clock` contract clamps a time before 1980 to 1980-01-01 00:00:00, a Tuesday. DOS showed `Tue 01-01-1980` and 00:00:46 46 s after release, and 00:00:40 after the relaunch ([`kit-m5g1-clock-file.png`](kit-m5g1-clock-file.png)). The wall-clock path does what the contract says, but the kit has no real time to give it. Rerun on an image with #1279. |
| A file written before Stop is there after relaunch | Pass | `echo … >C:\M5G.TXT`, Stop, relaunch, `type` ([`kit-m5g1-relaunch.png`](kit-m5g1-relaunch.png)) |
| `BLASTER` matches the setting | Pass | `BLASTER=A220 I7 D1 T3` with no record ([`kit-m5g1-set.png`](kit-m5g1-set.png)). `/v1/library/core/settings` wrote `0,0,0,0,576,5,1` to game `pc386-m5g-sb240`, moving revision `absent` to `a89af05d…`. The next launch showed `BLASTER=A240 I5 D1 T3` ([`kit-m5g4-set-240.png`](kit-m5g4-set-240.png)). |
| …and a game's sound still works there once its setup has been rerun | Pass | Wolfenstein 3D at 240h and IRQ 5. Its sign-on lights Sound Blaster. In its sound setup (the game has no separate setup program; it reads `BLASTER` at every start), effects and music were set to None and digitised sound to Sound Blaster. Five shots made five bursts 1.2 s apart, up to −19.7 dBFS. Ammo went from 8 to 3. |

### Wolfenstein 3D

| Check | Result | Evidence |
| --- | --- | --- |
| Sound Blaster and AdLib selected in setup | Pass | The eXoDOS `CONFIG.WL6` selects AdLib effects, AdLib music and Sound Blaster digitised sound, as the game's sound menu showed. The sign-on detected the mouse and the Sound Blaster. |
| Title | Pass | Sign-on, PC-13 screen and title |
| Menus with mouse and keyboard | Pass | Down moved the cursor from New Game to Sound. Mouse moves of 48 counts moved it up one item and down one item (the first downward move, just after a key, did not register), and a left click opened the sound menu. Keyboard drove the sound, episode, difficulty, save and load menus. |
| A level played through | Not done | m5g3 played into E1L1: a new game, five shots, a turn, the player killed once and respawned. m5g5 tried the level on the easiest difficulty, steering from grabs. The first guard behind the first door killed the player before the next step. The game runs in real time, and each remote step (send keys, grab, look) took about 30 s. A play-through needs a person at a controller, or a scripted route. |
| Digitised effects (Sound Blaster DMA) audible | Pass | At 220h and IRQ 7, with effects and music set to None and digitised sound to Sound Blaster, five shots made five bursts 1.2 s apart. The loudest 100 ms window was −19.4 dBFS, against −96 dBFS between shots. |
| AdLib music audible | Pass | Title −45.2 dBFS median and menu −43.2 dBFS, against −101.6 dBFS on the silent sign-on. Music is set to AdLib, so the OPL2 is its only source. |
| Page flips without tearing | Pass | A 4.5 s left turn captured at 30 fps gave 135 turning pairs and 0 torn. In a pure turn every band of wall rows shifts by the same amount, and a frame torn across a flip splits into two plateaus a frame's turn apart. One pair had a single outlying band, where the top wall edge slants; both of its frames look clean. |
| Frame rate recorded from captured frames | Partial: at least 30 fps | Every one of the 134 consecutive transitions during the turn was a new frame. The 4KPRO's only mode is 1920x1080 at 30 fps, so the game's rate above 30 cannot be measured on Kit B. Kit A's capture was claimed by another session. |
| A saved game loads after relaunch | Pass | Saved to slot 1 as "M5G", then Stop. After the relaunch, `SAVEGAM0.WL6` (19,273 bytes) is on the disk and Load Game lists "M5G". The loaded state is Floor 1, 2 lives, ammo 8; a new game starts with 3 lives. Against the frame before saving, 0.06% of pixels differ, all in the status bar's face. |

### Ultima VI

| Check | Result | Evidence |
| --- | --- | --- |
| VGA and AdLib selected | Pass | The eXoDOS `CONFIG.U6` reads `vma388`. The intro and the game draw in 256-colour VGA, and the music plays on the AdLib at 388h, the only music device Ultima VI has here. |
| Music plays | Pass | The intro's music starts 1.7 s into the recording and runs at −38.4 dBFS median for 20 s. Its zero-crossing rate moves between about 1,000 and 2,200 per second, so it is changing notes, not a held tone. In game the level is −36.1 dBFS. |
| PC speaker effects at the reference pitch under the 286 preset | Pending | Needs the governor |
| Walking speed matches the reference | Pending | Needs the governor |
| A saved game loads after relaunch | Pass | The eXoDOS save holds no character, so one was created on the kit: name Avatar, the gypsy's questions, the arrival at Lord British's castle. Three steps south, Ctrl-S, Y, Stop, at party HP 90, 84, 75 and 90. After the relaunch, Journey Onward shows the same HP, position and date. The party panel is identical, and 1.3% of the map area differs, where sprites animate. |

## Kit-path issues

- **[#1332](https://github.com/DeanoC/fes/issues/1332)** (Home's computer shelf hid
  `fes.computer` 1.1 packages) is fixed on main by #1286. `shelfPlayableABI` now
  takes every minor that `protocol.ComputerABI` accepts, and
  `shelf_computers_test.go` has a 1.1 case. `go test ./fogcast -run Shelf` passes on
  this branch. No kit run went through Home's shelf: that needs a library entry on
  the shared host, and FogCast cannot remove one. The issue is left open for its
  owner to close.
- **[#1331](https://github.com/DeanoC/fes/issues/1331)** (tenfoot cannot send F1-F11,
  the keypad, Print Screen or Pause) is fixed in its own draft PR,
  [#1340](https://github.com/DeanoC/fes/pull/1340): `nativeHIDUsage` maps through
  `hidkeys.FromLinuxKey`. These runs sent keys through the agent's input stream, so
  they did not depend on it.

## Findings

- **No protected mode, so no XMS moves.** HIMEMX runs with `/MAX=0` until
  `fes_i386_cpu` has SMSW and protected mode (Phase 2), as above. Ultima VII's 2 MiB
  of XMS needs the same work.
- **The kit's clock.** The kits boot at 1970, and their image predates #1279's NTP,
  so every pc386 launch starts at 1980-01-01 00:00. Moving the kit images past #1279
  (an image change, Deano's call) unblocks the date check.
- **A20 "Always on".** The Bochs BIOS leaves A20 enabled, so HimemX's start-up
  finds it on, picks its "Always on" method and never drives the 8042 or port 92h
  itself. Forcing a `/METHOD` would not prove more while A20 starts enabled.
- **Mixer balance.** Wolfenstein 3D's AdLib music sits about 25 dB below its
  digitised effects. The kit level matches the simulation's: the census measured
  Ultima VI's OPL2 music at about −39.5 dBFS RMS through the same mixer. So this is
  the design's starting gains (OPL2 and DSP both at half scale), whose tuning by ear
  is already open in the design.
- **The capture's DC offset.** Every Kit B recording sits at −186 LSB. The mixer
  ends in a DC blocker, so the offset comes from the HDMI audio capture path, not the
  core. The levels here remove it.
- **[#1330](https://github.com/DeanoC/fes/issues/1330) and [#1150](https://github.com/DeanoC/fes/issues/1150)
  did not come up.** No keys went in while CuteMouse initialised at boot, and neither
  game's mouse reset hung. Nothing here called INT 15h AH=86h.
- **Driving menus.** Wolfenstein 3D drops keys that arrive while a menu fades in.
  Two runs landed on New Game that way and backed out. This is the game's behaviour,
  not the machine's.

## Left on Kit B

FogCast's data directories on Kit B keep these:
- the staged disk `6138390a….img`, which holds the user-supplied games;
- each library game's record and disk delta: `pc386-m5g-freedos`,
  `pc386-m5g-wolf3d`, `pc386-m5g-sb240`, `pc386-m5g-ultima6` and
  `pc386-m5g-wolf3d-level`.

The runs added 10, 8, 5, 20 and 8 paths. Each run's `kit-new-paths.txt` on Powerboat
lists them.

## Not in this record

- Ultima VII: The Black Gate and Part Two: Serpent Isle. They need protected mode
  (i386 Phase 2).
- The 286 and 386 presets, and Ultima VI's speaker pitch and walking speed. They
  need the governor.
- The final kit rerun on the package of main's pc386 tree once #1335 has landed.
  This PR changes no RTL.
