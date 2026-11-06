# pc386 first-acceptance games on main's seal, 4 November 2026

Trackers [#1267](https://github.com/DeanoC/fes/issues/1267) (M5) and
[#1368](https://github.com/DeanoC/fes/issues/1368) (checks owed after #1339).

**Result:** Kit B run `s2` used a Quartus 17.0.2 seal of main `ee7ea9bb4`,
which holds #1473's KF8253 fix, and every row run reached its goal:

- FreeDOS boots with HIMEMX, CuteMouse and 575K free.
- Wolfenstein 3D plays with digitised effects and AdLib music, and its save
  loads after relaunch.
- Ultima VI plays its music, and its save loads after relaunch. At the
  286-12 preset its lute plays all 70 notes at #1472's pitches.
- DOOM v1.9 starts DOS/4GW under HIMEMX with no EMM386 and no 387. It shows
  its title, plays its demos and plays E1M1 by keyboard and mouse. Its
  effects and music are audible and its save loads after relaunch.
  `-timedemo demo3` gives 3863 gametics in 5062 realtics (26.7 fps).
- The Black Gate at 386DX-25 and Serpent Isle at `max` play their
  introductions to their main menus.

A SCRUBX memory scrub ran before every launch (#1433), and The Black Gate
did not stall. No new failure was found, so no issue was filed. What was not
checked is under [Not done](#not-done).

An earlier run, `s1`, used #1475's package `de3173af…`, which does not
cover #1473. Its results were the same; see [Run s1](#run-s1-superseded).

## Inputs

- **Package.** `fes.pc386`
  `499f4805ca07ddc13f04e35146fc822e340c49babc3694fdae0b813fce933d2c`, built
  by `make build-fes-pc386-quartus SEED=1` (Quartus 17.0.2) from a clean
  worktree of main `ee7ea9bb4` ([summary](seal-summary.json); build
  `61d5f899…`, RBF `4b093c3d…`). Firmware came from `firmware.lock` with
  `build_pc386_firmware.py --download`. The BIOS, VGA BIOS and ROM are the
  same as #1475's.
  - **Timing.** Every analysis is met: setup +1.081 ns, hold +0.061 ns,
    recovery +8.159 ns, removal +0.768 ns, minimum pulse width +1.122 ns.
    Clocks: system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.
    29,166 ALMs.
  - **Against #1475's seal.** Of the sealed inputs, only
    `cores/fes-common/vendor/kf8253/KF8253_Counter.sv` (#1473) differs.
- **Kit.** Kit B, run `s2`, claimed 16:53 and released 17:48 EET, core-only
  on its baseline: software `a8cd30c4` (main `bb51f445`), OS `f904961e`.
  Nothing was installed, and there was no image or boot change and no
  reboot. The runner checked the snapshot keys before and after, including
  the `usb-storage.quirks` line. At 17:49 it removed the 16 FogCast data trees
  the run had created (game records, disk deltas, two staged disks), and kept
  the shared scrub image.
- **Disks.** All are outside the repo, because the games are user-supplied.
  - **Base.** Main's `build_pc386_freedos_image.py --profile games` builds
    `b23fba2e…`, with 2 KiB clusters (#1442). Each game disk is this image
    plus one eXoDOS 6.04 zip from Deano's share, copied in with mtools.
    Copies made at different times differ in their file times.
  - Wolfenstein 3D: `d3cf4713…`.
  - Ultima VI: `574ea0cc…`, eXoDOS's config (VGA, AdLib).
  - Ultima VI with no music: `e829656b…`, #1362's `CONFIG.U6` (VGA, mouse,
    no music), so the speaker is alone in the capture.
  - DOOM: `5ed3d78f…` from `DOOM (1993).zip` `06ee36b4…`. It holds the 34
    top-level files of `DOOM\` (WAD 11,159,840 bytes); eXoDOS's GUS, SB16,
    SC55 and ULTRASND directories are left out. Its own `DEFAULT.CFG`
    selects Sound Blaster at 220h, IRQ 7 and DMA 1 for both music and
    effects (devices 3 and 3).
  - The Black Gate: `7ca7d0eb…`.
  - Serpent Isle: `91b130b9…`, with INSTALL's plain-SB `SERPENT.CFG` (#1443).
  - Scrub disk: `8538495a…` (SCRUBX, all regions), shared with #1433.

  [kit-runs.json](kit-runs.json) gives each zip's SHA-256, file counts and
  free space.
- **Presets.** Each game had a fresh library entry, and the settings were
  written before its first load:
  - Ultima VI 286 (`s2-u6-286`): preset 1, 286-12, record revision
    `c961578a…`.
  - The Black Gate (`s2-u7bg`): preset 3, 386DX-25, revision `61d6ba26…`.
  - Every other entry ran at the default, preset 0, which is `max`.
  - Sound Blaster is 220h, IRQ 7, DMA 1 everywhere.
- **Capture.** Video is Kit B's 4KPRO at 1920x1080, 30 fps, and audio is ALSA
  `hw:CARD=D4KPRO` at 48 kHz.
  - **Levels.** AC RMS per second in dBFS, after removing the capture's
    constant DC offset (−88 LSB). Silence reads −50.5 dB.
  - **Frame counts.** These come from 480x270 grey video. A "new picture" is
    a frame that differs from the one before in more than 0.5% of the
    picture area by more than 24 levels. At 30 fps, the capture cannot show
    rates above 30.

## Results

### Scrub

All 9 scrubs ended `DONE flags=0000000F` (frame `d336412a`). Each zeroed the
UMBs, EXT 1-16 MiB and the conventional memory above SCRUBX, which had held
data from the previous launch.

### FreeDOS (`max`)

Pass for boot, memory, mouse and persistence:

- **Boot.** The screen shows UMBM C800-EFFF and HimemX 3.38 (XMS 3.00,
  "Fast A20 method used"), then CuteMouse "Installed at PS/2 port" (frame
  `975a0e59`).
- **Memory.** `MEM` shows 575K conventional free (588,544 bytes), 15,201K
  XMS free and an 81K largest UMB (frame `14cedcc6`).
- **Sound Blaster.** `BLASTER=A220 I7 D1 T3` (frame `f8e47dc4`).
- **Persistence.** `C:\SMOKE.TXT`, written before Stop, reads back after
  relaunch, and the Wolfenstein 3D save is listed (frame `7c2e1eae`).
- **Not right: date and time.** DOS shows 01-01-1980 12:00 am. The kit clock
  reads 1970-01-02 00:42 UTC (#1368's blocked item).

### Wolfenstein 3D (`max`)

Pass, without a level played through.

- **Start-up.** The signon detects the mouse and Sound Blaster (frame
  `f0fad8ff`). Then the title, PC-13, menus, episode and skill lead to E1L1
  (frame `546414d0`).
- **Play.** Movement is by keyboard. Ctrl and the mouse button fire
  (ammo 8 → 5), and the mouse turns.
- **Frame rate.** Over a 4 s held turn, every capture frame was a new
  picture: 30, 30, 30 and 29 a second. The capture's 30 fps is the limit;
  Deano: "30 is fine for now".
- **Tearing.** No partly updated frames in 360.
- **Audio.** Music at −40 dB through the menus and game, with gunshots to
  −24.7 dB.
- **Save.** F2 in slot 1. After a Stop, a scrub and a relaunch, Load Game
  lists `smoke`. The loaded game shows the same view with ammo 5 (frame
  `aa32e3d8`).

### Ultima VI

- **At `max`, eXoDOS config: pass.**
  - **Music.** The intro music holds a steady −38 dB, with peaks to
    −28 dB.
  - **Into the game.** Menu (frame `32d39395`, byte-identical to #1409's
    k6 menu), character creation and the game start.
  - **Save.** Ctrl-S, then Y (frame `61edcdd8`). After a Stop, a scrub and
    a relaunch, Journey Onward loads the party in the same room (frame
    `d2631b2f`).
- **Lute at 286-12, no music: pass, with #1368's reference caveat.**
  - **Method.** The same flow as #1472's k1: Use the lute, then keys `6`
    ×30, `1` ×31, then `2`-`9` and `0`.
  - **Notes.** All 70 keys played exactly one note, and there were 2 other
    effects. Each note is the routine's PIT carrier at 18.8-20.0 kHz, about
    100 ms long.
  - **Envelope pitch** (median, with range):
    - `1` 502.2 Hz (499.4-506.1, n=31)
    - `6` 840.0 Hz (837.4-844.0, n=31)
    - `2` 565.4, `3` 636.3, `4` 675.2, `5` 750.3
    - `7` 949.4, `8` 998.4, `9` 1104.1, `0` 1231.4
  - **Against #1472.** Every key is within 0.5% of [#1472's
    record](../2026-11-04-pc386-speaker-kit/summary.md). WAV `762bc558…`.
  - **Walk.** The party walked out of the throne room (frame `f551b9c7`).
    The walk video is kept but not compared, because there is still no
    reference.

### DOOM (`max`)

Pass:

- **Start-up.** `doom` at the prompt prints DOS/4GW's banner: "DOS/4GW
  Professional Protected Mode Run-time Version 1.95" (frame `8f5305c4`).
  HIMEMX is loaded and no EMM386, and the machine has no 387.
  "DOOM System Startup v1.9" follows (frame `ddaad11b`):
  - DPMI memory `0xdba000`, with `0x800000` allocated for the zone;
  - "registered version";
  - R_Init completes;
  - `I_StartupMouse` reports "Mouse: detected";
  - `I_StartupSound` and `I_StartupTimer` run.

  The title is up about 20 s after the command.
- **Demos.** The demo loop plays (frames `9a1f7bcb`, `f477fda7`, `4199c7ae`).
  - **Frame rate.** 402 new pictures in 20 s, 20.1 a second (9-25 per
    second), against the 30 fps capture. So at `max` the 35 fps cap is not
    the limit; the CPU is.
  - **Tearing.** DOOM page-flips, and its row asks for no tear check. The
    partial-frame heuristic flagged 3 frames. In run s1 the flagged frames
    checked by eye were sprite changes on a still background.
- **Play.** Esc, New Game, Knee-Deep in the Dead, Hurt Me Plenty, then E1M1
  (frame `ce3ead8e`, byte-identical to s1's).
  - **Controls.** Held arrow keys move and turn, mouse motion turns, and
    Ctrl and the left button fire. Bullets went 50 → 48 (frame `6fa142b2`).
  - **Frame rate while moving.** 23, 26, 28, 20 and 19 new pictures a
    second.
- **Save and reload.**
  - **Save.** F2, slot 1, `smoke`, "game saved" (frame `4c635ee7`), then
    Quit Game to the prompt.
  - **Relaunch.** After a Stop, a scrub and a relaunch, `dir` shows
    `DOOMSAV0.DSG` (25,350 bytes). F3 lists `smoke`, and loading it puts the
    player back where the game was saved, with 48 bullets and 100% health
    (frame `4e8a4fc4`).
- **`-timedemo demo3`.** "timed 3863 gametics in 5062 realtics" (frame
  `1a8f9914`): 3863 × 35 / 5062 = 26.7 fps at `max`.
- **Audio.**
  - **Title.** −40 to −44 dB, the music only, as no effects play there.
  - **Demos.** −22 to −31 dB, effects and music.
  - The run only shows both audible. It does not separate the Sound
    Blaster's digitised effects from the OPL music in the mix.
- **Driver note (run s1).** DOOM ignored 60 ms key taps in its menus: F10
  did nothing, and typed text walked through the menus. Run s2 held every
  DOOM key 120 ms, and all of them worked. Wolfenstein 3D and Ultima VI take
  60 ms taps, but Wolfenstein 3D's menus drop the first tap after a fade.
  This is not filed, since it was not traced to the machine.

### Ultima VII: The Black Gate (386DX-25)

Pass, intro to menu.

- **Memory.** `MEM` before start shows 575K and 15,201K free (frame
  `170f2e23`).
- **Introduction.** `ultima7` starts its Voodoo memory manager and plays
  the whole introduction: the title, the Guardian's speech (frame
  `e29208e6`), the monitor scene and the moongate.
- **Menu.** View Introduction, Start New Game, Journey Onward and View
  Credits are up 3 min 10 s after the command (frame `b2ba3056`).
- **Audio.** Music and speech at −33 dB median, with peaks to −18.8 dB.
- **No stall** (scrubbed first).

### Ultima VII Part Two: Serpent Isle (`max`)

Pass, intro to menu.

- **Introduction.** It plays with its narration: the castle (frame
  `bab5c909`), then the Guardian (frame `adc1b734`).
- **Menu.** Up by 90 s after the command (frame `92e9ceae`). It goes back
  to the introduction after about 20 s idle.
- **Audio.** Narration and music at −32 dB median, with peaks to −15.9 dB.

## Not done

- **FreeDOS.**
  - Date and time: the kits are at 1970 (#1368).
  - The BLASTER change to 240h/IRQ 5, and a game's sound after rerunning
    its setup there, were not rerun.
  - `A20TEST` was not run; only HimemX's banner was seen.
- **Wolfenstein 3D.** No level was played through (#1368). The menus were
  driven by keyboard; the mouse was used in the game only. Rates above
  30 fps need a faster capture.
- **Ultima VI.** Pitch and walking speed against a period or emulator
  reference: there is still none (#1368).
- **DOOM.** The level was played for a few seconds, not through. Rates
  above 30 fps need a faster capture.
- **The Black Gate.** Only the intro to the menu. NPC pace, save and load
  across relaunch, and #1427's character-screen clicks at 386DX-25 were not
  retested.
- **Serpent Isle.** Only the intro to the menu, with no save and load.

## Run s1 (superseded)

Run `s1`, from 15:35 to 16:32 EET the same day on Kit B, used #1475's
package `de3173af…` (seal of `36ea8a2f3`). Its 109 sealed inputs hash
identically on main `2975760a8`, but not on `ee7ea9bb4`, which adds #1473.
The 16 trees it left were removed at 16:33.

It ran the same checks, with the same outcome, and different-dated copies
of the same disks. The numbers were within the run-to-run spread:

- Wolfenstein 3D: 30/30/30/29 new frames a second, 0 torn.
- Lute: 70/70 notes, `6` 839.6 Hz, `1` 502.0 Hz, `9` 1114.0 Hz.
- DOOM:
  - demos at 18.8 new pictures a second;
  - timedemo 3863 gametics in 5061 realtics;
  - the loaded save's frame byte-identical to the frame before saving.
- The Black Gate: the menu at 3 min 15 s.

[kit-runs.json](kit-runs.json) keeps its hashes under `s1`.

## Frames

Only FreeDOS screens are committed. The game frames stay on Powerboat
(`~/fes-scratch/smoke/runs/s2/fr`), and their SHA-256s are in
[kit-runs.json](kit-runs.json).

| File | Frame | Content |
| --- | --- | --- |
| [kit-s2-scrub.png](kit-s2-scrub.png) | `d336412a` | The first scrub's report, `DONE flags=0000000F`. |
| [kit-s2-freedos-boot.png](kit-s2-freedos-boot.png) | `975a0e59` | FreeDOS boot: UMBM, HimemX 3.38 with fast A20, CuteMouse on PS/2. |
| [kit-s2-mem.png](kit-s2-mem.png) | `14cedcc6` | `MEM`: 575K (588,544 bytes) conventional, 15,201K XMS free. |
| [kit-s2-set-date.png](kit-s2-set-date.png) | `f8e47dc4` | `set` with `BLASTER=A220 I7 D1 T3`; `date` and `time` at 01-01-1980. |
| [kit-s2-relaunch-files.png](kit-s2-relaunch-files.png) | `7c2e1eae` | After relaunch: `C:\SMOKE.TXT` reads `smoke-s2`; Wolfenstein 3D's save file is listed. |
