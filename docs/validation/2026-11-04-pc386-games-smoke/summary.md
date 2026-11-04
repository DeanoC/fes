# pc386 first-acceptance games on main's seal, 4 November 2026

Trackers [#1267](https://github.com/DeanoC/fes/issues/1267) (M5) and
[#1368](https://github.com/DeanoC/fes/issues/1368) (checks owed after #1339).

**Result:** on Kit B, the sealed package of current main reached the goal of
every row it was run for. FreeDOS boots with HIMEMX, CuteMouse and 575K free.
Wolfenstein 3D plays, with digitised effects and AdLib music, and its save
loads after relaunch. Ultima VI plays its music, and at the 286-12 preset
its lute plays all 70 notes at #1472's pitches; its save loads after
relaunch. DOOM v1.9 starts DOS/4GW under HIMEMX with no EMM386 and no 387.
It shows the title and plays its demos, and a new game in E1M1 takes
keyboard and mouse. Its effects and music are audible, its save loads after
relaunch, and `-timedemo demo3` gives 3863 gametics in 5061 realtics
(26.7 fps). The Black Gate at 386DX-25 and Serpent Isle at `max` play
their introductions through to their main menus. A SCRUBX memory scrub ran
before every launch (#1433), and The Black Gate did not stall.
No new failure was found, so no issue was filed. What this run did not check
is listed under [Not done](#not-done).

## Inputs

- **Package.** `fes.pc386` `de3173afc9a209e77f2f87f2edc7fcccde0039567c5de26ea47c2f8d49d2fd00`,
  [#1475](https://github.com/DeanoC/fes/pull/1475)'s seal of `36ea8a2f3`
  (Quartus 17.0.2, [summary](seal-summary.json), copied from
  [#1475's record](../2026-11-04-pc386-sb-adpcm/summary.md)). Every analysis
  is met: setup +1.174 ns, hold +0.088 ns, recovery +6.903 ns, removal
  +0.440 ns, minimum pulse width +1.122 ns, at system 61.44 MHz, pixel
  74.27 MHz and audio 12.29 MHz. RBF `7696be9c…`, 29,211 ALMs.
  The package was not resealed: all 109 sealed inputs that the summary lists
  (RTL, vendor cores, constraints, `firmware.lock`, `toolchain.lock` and the
  build scripts) hash identically on main `2975760a8`, which holds #1475,
  #1452, #1458 and #1453.
- **Kit.** Kit B, run `s1`, claimed 15:35 and released 16:32 EET, core-only
  on its baseline: software `a8cd30c4` (main `bb51f445`), OS `f904961e`.
  Nothing was installed and there was no image or boot change and no reboot.
  The runner checked the snapshot keys before and after, including the
  `usb-storage.quirks` line. At 16:33 it removed the 16 FogCast data trees
  the run had created (game records, disk deltas, two staged disks), and
  kept the shared scrub image.
- **Disks.** All are outside the repo, because the games are user-supplied:
  - **Base.** Main's `build_pc386_freedos_image.py --profile games` builds
    `b23fba2e…`, with 2 KiB clusters (#1442). Each game disk is this image
    plus one eXoDOS 6.04 zip from Deano's share, copied in with mtools.
  - Wolfenstein 3D: `891779c4…`.
  - Ultima VI: `c6dd6e67…`, eXoDOS's config (VGA, AdLib).
  - Ultima VI with no music: `36ba1bba…`, #1362's `CONFIG.U6` (VGA, mouse,
    no music), so the speaker is alone in the capture.
  - DOOM: `750fbae4…` from `DOOM (1993).zip` `06ee36b4…`. It holds the 34
    top-level files of `DOOM\` (WAD 11,159,840 bytes); eXoDOS's GUS, SB16,
    SC55 and ULTRASND directories are left out. Its own `DEFAULT.CFG`
    selects Sound Blaster at 220h, IRQ 7 and DMA 1 for both music and
    effects (devices 3 and 3).
  - The Black Gate: `15d9ebe8…`.
  - Serpent Isle: `8066d4c6…`, with INSTALL's plain-SB `SERPENT.CFG` (#1443).
  - Scrub disk: `8538495a…` (SCRUBX, all regions), shared with #1433.

  [kit-runs.json](kit-runs.json) gives each zip's SHA-256, file counts and
  free space.
- **Presets.** Each game had a fresh library entry, and the settings were
  written before its first load. Ultima VI 286 (`s1-u6-286`): preset 1,
  286-12, record revision `060d4b94…`. The Black Gate (`s1-u7bg`): preset 3,
  386DX-25, revision `8c986c48…`. Every other entry ran at the default,
  preset 0, which is `max`. Sound Blaster is 220h, IRQ 7, DMA 1 everywhere.
- **Capture.** Video is Kit B's 4KPRO at 1920x1080, 30 fps, and audio is ALSA
  `hw:CARD=D4KPRO` at 48 kHz. Levels are AC RMS per second in dBFS, after
  removing the capture's constant DC offset (−88 LSB in this run). Silence
  reads −50.5 dB. Frame counts come from 480x270 grey video: a "new picture"
  is a frame that differs from the one before in more than 0.5% of the
  picture area by more than 24 levels. At 30 fps, the capture cannot show
  rates above 30.

## Results

| Row | Preset | Result |
| --- | --- | --- |
| Scrub | — | Each of the 9 scrubs ended `DONE flags=0000000F` (frame `60bd242c`). It zeroed UMB, EXT 1-16 MiB and the conventional memory above SCRUBX, which had held data from the run before. |
| FreeDOS | `max` | Pass on boot, memory, mouse and file persistence. The boot screen shows UMBM C800-EFFF and HimemX 3.38 (XMS 3.00, "Fast A20 method used"), then CuteMouse "Installed at PS/2 port" (frame `3c191304`). `MEM` shows conventional 575K free (588,544 bytes), XMS 15,201K free, and an 81K largest UMB (frame `3b7c7b39`). `BLASTER=A220 I7 D1 T3` (frame `e107197f`). `C:\SMOKE.TXT`, written before Stop, reads back after relaunch, and the Wolfenstein 3D save is listed (frame `7dc062ac`). The date and time are still not right: DOS shows 01-01-1980 12:00 am while the kit clock reads 1970-01-01 23:24 UTC (#1368's blocked item). |
| Wolfenstein 3D | `max` | Pass, but no level was played through. The signon detects the mouse and Sound Blaster (frame `ba22fda1`). Title, PC-13, menus, episode, skill, then E1L1 (frame `233c3029`). Movement is by keyboard. It fires with Ctrl and with the mouse button (ammo 8 → 5), and the mouse turns. **Frame rate:** over a 4 s held turn, every capture frame was a new picture, 30, 30, 30 and 29 a second. The capture's 30 fps is the limit; Deano: "30 is fine for now". **Tearing:** 0 partly updated frames in 360. **Audio:** music −38 to −44 dB through the menus and game; the gunshots go to −24 dB. **Save:** F2 in slot 1. After a Stop, a scrub and a relaunch, Load Game lists `smoke`, and the loaded game shows the same view with ammo 5 (frame `850d69e8`). |
| Ultima VI | `max`, eXoDOS config | Pass. **Music:** intro music at a steady −38 dB with peaks to −29 dB. Then menu (frame `e931dee6`), character creation and the game start. **Save:** Ctrl-S, then Y ("Save game? Y", frame `9a26bd2a`). After a Stop, a scrub and a relaunch, Journey Onward loads the party in the same room (frame `831cba17`). |
| Ultima VI, lute | 286-12, no music | Pass, with #1368's reference caveat. Same flow as #1472's k1: Use, the lute, then keys `6` ×30, `1` ×31, then `2`-`9` and `0`. All 70 keys played exactly one note, and there were 4 other effects. Each note is the routine's PIT carrier at 18.8-20.0 kHz, about 100 ms long. **Envelope pitch** (median, with range): `1` 502.0 Hz (499.3-506.8, n=31); `6` 839.6 Hz (837.1-841.3, n=31); `2` 565.7; `3` 634.9; `4` 674.2; `5` 747.2; `7` 951.0; `8` 999.4; `9` 1114.0; `0` 1232.7. These match [#1472's record](../2026-11-04-pc386-speaker-kit/summary.md) within 0.5% for every key except `9`, which is 1.3% high (one note each run). WAV `768084a6…`. The party walked from the throne room into the next room (frame `cd37c806`). The 14 s walk video `u6b-walk` is kept but not compared, because there is still no reference. |
| DOOM | `max` | Pass. See [DOOM](#doom). |
| Ultima VII: The Black Gate | 386DX-25 | Pass, intro to menu. `MEM` before start shows 575K and 15,201K free (frame `a52afd9a`). `ultima7` starts its Voodoo memory manager and plays the whole introduction: the title, the Guardian's speech, the monitor scene and the moongate. The main menu (View Introduction, Start New Game, Journey Onward, View Credits) is up 3 min 15 s after the command (frame `9f6deb7f`). Music and speech are audible, −23 to −38 dB through the intro. There was no stall (scrubbed). |
| Ultima VII Part Two: Serpent Isle | `max` | Pass, intro to menu. The introduction plays with its narration: the castle (frame `436cabae`), then the Guardian (frame `7dfdd661`). The main menu is up within 80 s of the command (frame `900b3954`). After about 20 s it goes back to the introduction, as the menu does when idle. Audio −21 to −41 dB with narration. |

### DOOM

- **Start-up.** `doom` at the prompt prints DOS/4GW's banner: "DOS/4GW
  Professional Protected Mode Run-time Version 1.95" (frame `f15c2f5a`).
  HIMEMX is loaded and no EMM386, and the machine has no 387.
  "DOOM System Startup v1.9" follows: DPMI memory `0xdba000`, `0x800000`
  allocated for the zone, "registered version". R_Init's dots run (frame `93f107b1`), and the title is up 17-21 s after the command (frame
  `5683426b`).
- **Demos.** The demo loop plays (frames `724854a5`, `07155d55`).
  - **Frame rate.** 376 new pictures in 20 s, 18.8 a second (13-25 per
    second), against the 30 fps capture. So at `max` the 35 fps cap is not
    the limit; the CPU is.
  - **Tearing.** The partial-frame heuristic flagged 17 frames. The 4
    checked by eye were sprite and muzzle-flash changes on a still
    background, not tears. DOOM page-flips, and its row asks for no tear
    check.
- **Play.** Esc, New Game, Knee-Deep in the Dead, Hurt Me Plenty, then E1M1
  (frame `ce3ead8e`).
  - Held arrow keys move and turn; mouse motion turns; Ctrl and the left
    button fire. Bullets went 50 → 48 (frame `d6e7508f`).
  - **Frame rate while moving.** 21, 23, 28, 20 and 19 new pictures a
    second.
- **Save and reload.** F2, slot 1, `smoke`, "game saved" (frame `764a824b`);
  then Quit Game. After a Stop, a scrub and a relaunch, `dir` shows
  `DOOMSAV0.DSG` (29,094 bytes). F3 lists `smoke`, and loading it gives a
  frame byte-identical to the one grabbed just before the save (`d6e7508f`
  both times).
- **`-timedemo demo3`.** "timed 3863 gametics in 5061 realtics" (frame
  `b4cfbb66`): 3863 × 35 / 5061 = 26.7 fps at `max`.
- **Audio.**
  - **Title.** −40 to −44 dB, the music only, as no effects play there.
  - **Demos.** −19 to −31 dB, effects and music.
  - The run only shows both audible. It does not separate the Sound
    Blaster's digitised effects from the OPL music in the mix.
- **Driver note.** DOOM ignored 60 ms key taps in its menus: F10 did
  nothing, and the typed `cls` and `doom -timedemo demo3` walked through
  the menus. Taps held 100-120 ms worked. Wolfenstein 3D and Ultima VI took
  60 ms taps. This is not filed, since it was not reproduced or bisected
  to the machine.

## Not done

- **FreeDOS.** Date and time (kits at 1970, #1368). The BLASTER change to
  240h/IRQ 5, and a game's sound after rerunning its setup there, were not
  rerun. `A20TEST` was not run; only HimemX's banner was seen.
- **Wolfenstein 3D.** No level was played through (#1368). The menus were
  driven by keyboard; the mouse was used in the game only. Rates above
  30 fps need a faster capture.
- **Ultima VI.** Pitch and walking speed against a period or emulator
  reference: there is still none (#1368).
- **DOOM.** The level was played for a few seconds, not through. Rates
  above 30 fps need a faster capture.
- **The Black Gate.** Only the intro to the menu: NPC pace, save and load
  across relaunch, and #1427's character-screen clicks at 386DX-25 were not
  retested.
- **Serpent Isle.** Only the intro to the menu, with no save and load.

## Frames

Only FreeDOS screens are committed. The game frames stay on Powerboat
(`~/fes-scratch/smoke/runs/s1/fr`), and their SHA-256s are in
[kit-runs.json](kit-runs.json).

| File | Frame | Content |
| --- | --- | --- |
| [kit-s1-scrub.png](kit-s1-scrub.png) | `60bd242c` | The first scrub's report, `DONE flags=0000000F`. |
| [kit-s1-freedos-boot.png](kit-s1-freedos-boot.png) | `3c191304` | FreeDOS boot: UMBM, HimemX 3.38 with fast A20, CuteMouse on PS/2. |
| [kit-s1-mem.png](kit-s1-mem.png) | `3b7c7b39` | `MEM`: 575K (588,544 bytes) conventional, 15,201K XMS free. |
| [kit-s1-set-date.png](kit-s1-set-date.png) | `e107197f` | `set` with `BLASTER=A220 I7 D1 T3`; `date` and `time` at 01-01-1980. |
| [kit-s1-relaunch-files.png](kit-s1-relaunch-files.png) | `7dc062ac` | After relaunch: `C:\SMOKE.TXT` reads `smoke-s1`; Wolfenstein 3D's save file is listed. |
