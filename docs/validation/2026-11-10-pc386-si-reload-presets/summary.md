# pc386 Serpent Isle black picture, across presets, 10 November 2026

Issue [#1494](https://github.com/DeanoC/fes/issues/1494): in #1485's Kit B run
`v1`, Serpent Isle showed only black after Journey Onward loaded a save,
while audio played. [#1506](../2026-11-05-pc386-si-reload/summary.md) didn't
reproduce it in three runs. This record repeats it on a fresh seal of main
with #1483's status ring and VRAM probes, at four governor presets.

**Result: reproduced, 3 times in 56 Serpent Isle launches. It is a pc386
fault, not a speed artefact or a long load. The VGA's registers and DAC are
right during a black launch, so the fault is downstream of them.**

- In Kit B run `k1`, one launch at 486DX2-66 showed a flat black picture
  for the whole launch: introduction, menu, Journey Onward and 60 s after
  it, about 4.7 minutes in all.
- Underneath, the game ran normally:
  - The CPU kept running in Serpent Isle's code segments.
  - Sound Blaster DMA and OPL2 output advanced.
  - Scanout presented 60 frames a second with no underflow.
  - VRAM held the same pictures that the lit launches showed: the
    introduction's frame and the main menu, byte for byte at the sampled
    words.
- So the game drew its pictures, and what went wrong is between VRAM and the
  picture.
- Run `k3`, on a scratch seal that puts the VGA's output state in the status
  ring, had 2 black launches in 17. In the one with ring data, palette
  address source, video enable, the screen-off bit, the pel mask and the DAC
  were all as in a lit launch: the state word was identical. So the fault is
  downstream of those registers: scanout geometry (the CRTC isn't in the
  word), capture, the 720p presenter or the display plane.
- Runs `k4` and `k5` added 18 launches with capture-buffer samples and a
  DOS-prompt grab. None of them was black, so the capture buffers haven't
  been read during a black launch yet.
- Input still works in a black launch. `k3`'s first launch made its save
  blind, and the later launches list it.
- The other 53 Serpent Isle launches showed their pictures. The load itself
  works: all 22 Journey Onward loads in lit launches (`k1` 7, `k3` 15) put
  the party back on the ship within 3–6 s, as the Bochs-oracle machine does in about 5.5 s.
- The stage isn't pinned yet (see [Next](#next)).

The issue's title describes it too narrowly. The fault isn't tied to Journey
Onward or the save: it blacks out a whole launch from the introduction on.
`v1`'s black main menu, and the black picture after its load, fit the same
fault.

## Package and kit

- **Package.** `650bc477a12c458181f6eeca24d56fc71a0db37d1002f6a2d58869871a8c8f93`,
  the Quartus 17.0.2 seal of main `65ec1f22a` described in
  [#1368's rerun record](../2026-11-10-pc386-game-acceptance-rerun/summary.md).
  The status ring (#1483) is part of main.
- **Kit.** Kit B, core-only on its baseline (image `cd284ca3…`, software
  `7ddb0bfb…`). Claims, restore checks and releases are in Kit B's status
  file.
- **Disks.** Scratch copies of the FreeDOS games image `e69194ec…` plus
  Serpent Isle (`f32f047a…`, plain-SB `SERPENT.CFG` `13ad7bdb…`) or The Black
  Gate (`d9bfafd4…`), from Deano's eXoDOS 6.04 zips. Nothing from a game is
  committed.
- **Settings.** Sound Blaster 220h, IRQ 7, DMA 1. Presets by index:
  0 `max`, 3 386DX-25, 6 486DX-33, 7 486DX2-66.
- **Probes.** Every grab is reduced to a 64x36 grey mean and peak. A
  black grab (peak under 16) gets a status ring snapshot and a VRAM sample, and
  both again 1 s later. Some fixed grabs (the introduction, the menu and
  some load grabs) get them too. A VRAM sample is 512 words, one every 512
  bytes of VRAM's 256 KiB, read with `devmem` from DDR `0x30800000`, and
  hashed. A flat black HDMI grab is mean 6.6, peak 7 (frame `4c6d2c8d`).

## Run `k1`: save, then 8 reloads at four presets (19:12–19:47 EET)

1. At `max`: a new game, the ship scene, and a save to slot 1 ("fesk1")
   from the paperdoll's disk icon (frame `dac9c09d`), following #1520's
   `i1`.
2. Then, for each preset 0, 3, 6 and 7, twice: stop (no scrub, as in
   `v1`), set the preset, relaunch, `SERPENT`, the introduction, Esc to the
   menu, Journey Onward, then a grab every 3 s for 60 s, then `S` to list the
   save slots.

| Launch | Introduction | Load +3 s | Load +6 s | Slots |
| --- | --- | --- | --- | --- |
| `max`, 1 | shown | ship | ship | "fesk1" |
| `max`, 2 | shown | ship | ship | "fesk1" |
| 386DX-25, 1 | shown | ship | ship | "fesk1" |
| 386DX-25, 2 | shown | black | ship | "fesk1" |
| 486DX-33, 1 | shown | ship | ship | "fesk1" |
| 486DX-33, 2 | shown | ship | ship | "fesk1" |
| **486DX2-66, 1** | **black** | **black** | **black** | **black** |
| 486DX2-66, 2 | shown | black | ship | "fesk1" |

A black grab at +3 s followed by the ship at +6 s is the load in progress.

### The black launch (486DX2-66, 1)

All 26 grabs, from the introduction at 30 s after `SERPENT` to the slot list
about 4.7 minutes later, are the flat black frame `4c6d2c8d`. The 26 status
ring snapshots show, from 60 to 345 s of core time:

- **CPU.** Running, not halted or stopped, with no CPU reset. `CS:EIP`
  moved through segments `1A60`, `1F66`, `23D4`, `17A3`, `19F4` and `130E`.
  The lit launches sat in the same segments, for example `1A60:01C6` at the
  second 486DX2-66 launch's introduction. Retired instructions advanced.
- **Audio.** Sound Blaster DMA rose from 179,054 to 1,168,854 cycles, in
  bursts. OPL2 samples rose steadily.
- **Video.** `frames_shown` rose by 60 a second (12,653 frames in 210.9 s of core
  time from Load +3 s to the slot list). Scanout, capture, present
  and plane underflows all stayed 0.
- **VRAM.** It changed between the samples, and it matched the lit
  launches:
  - At Load +3 s, the second sample hashes `0de8e137`. That is the
    introduction's first scene, the same hash as the lit introductions of
    `max` launches 1 and 2 and the second 486DX2-66 launch.
  - At menu +6 s and +10 s, VRAM hashes `1f9b027f`. That is the main menu
    in the lit launches (`max` and 386DX-25, menu +6 s and +14 s).
  - In between, the hashes kept changing, as in a lit introduction.

So the game drew its introduction and menu into VRAM on time. 3 s after
the Journey Onward click, VRAM held the introduction again, so that click
didn't start a load. Why isn't known: the aim can't be checked in the dark.

The ring has no VGA register or DAC state, so these runs can't tell which
output state was wrong.

## Run `k2`: when does it start (20:00–20:26 EET)

15 launches: Serpent Isle 6 times at 486DX2-66 and 6 times at `max`, then
The Black Gate 3 times at 386DX-25. Each: stop, relaunch, a grab at the DOS
prompt, the game's command, grabs at +3, +8, +15 and +30 s, Esc, a menu grab,
then Alt-X to DOS. The driver was set to branch on a black launch (quit to
DOS, `VER`, the game again in the same boot).

**No launch was black.** Every Serpent Isle launch showed its introduction
by +30 s and its menu after Esc. Every Black Gate launch showed its
introduction. So the branch didn't run. With `k1` (its first launch, which
made the save, and 8 relaunches), that is 1 black launch in 21 Serpent Isle
launches. `k2` quit each launch to DOS before the stop,
while `k1` (and `v1`) stopped the machine in the game.

## The VGA traffic Serpent Isle makes (Bochs-oracle bench)

The machine simulation (`machine_tb`, Bochs-oracle CPU, the RTL VGA) ran the
same disk with the save. Its VGA port trace covers boot to the main menu:

- **Attribute controller (3C0h).** 129 writes, all from the VGA BIOS's
  mode sets. Each ends with 20h (palette address source on). Serpent Isle
  never writes 3C0h itself.
- **Pel mask (3C6h).** 3 writes, all FFh from the mode sets.
- **DAC.** 132,867 writes to 3C8h and 400,896 to 3C9h. The game mostly
  writes one entry at a time (an index, then red, green and blue). That fits
  fades and colour cycling.
- **Input status (3DAh).** About 19 million reads: it polls for retrace.
- **Around the mode set** that starts the game, only 2 3DAh reads fall
  near the attribute writes. Neither splits an index/data pair.

So in Serpent Isle the attribute controller's palette address source and
the pel mask are set only by the BIOS mode set. A wrong value would have to
come from that mode set, or from the VGA. The DAC is written continuously,
so a black DAC would need the game's own palette to be black or a DAC write
path fault.

**Bench timing.** The bench grabs a frame every simulated second. Esc at
40 s leaves 2 black frames (41 and 42 s), then the menu shows from 43 s. On
the kit the menu showed by Esc + 6 s in every lit launch. So a menu that
stays black for 10–20 s, as in `v1`, isn't the oracle machine's behaviour
either. Journey Onward (click at 64.5 s) shows the blue loading screen at
67 s and the ship from about 70 s, a 5.5 s load, as on the kit.

## Period hardware and emulators

Searched for the symptom on period systems (Origin's own documents and
period reports):

- **Origin's troubleshooting guide** for Serpent Isle lists EMS and
  memory managers, IRQ settings, the mouse driver (Microsoft's, version 7.0),
  the 386 B-step CPU, and Sound Blaster against Sound Blaster Pro.
- **Nothing about the symptom.** It has no entry for a black picture tied
  to CPU speed or the palette. The game's loading screen is blue, not black.
- **Speed.** Serpent Isle has a frame limiter.
- **Period reports.** One VOGONS report of a hang at The Black Gate's
  loading screen on a 486 was cured by more conventional memory and the
  Microsoft mouse driver. No report was found of a whole launch staying
  black while the game plays, or of a long black load after Journey Onward.

That fits a pc386 fault rather than behaviour period machines share. The
runs above also rule out a long load: on this package a load takes 3–6 s at
every preset.

## Next

- **Which stage.** The VGA's registers and DAC are ruled out. `k5` read the
  capture buffers but had no black launch, so this is still open.
  - If the capture buffer holds the picture in a black launch, the fault is
    in the presenter or the display plane.
  - If it is black or stale, it is in scanout or capture.
- **A faster sample.** `k5`'s sample reads 1,200 words through `devmem` and
  lands seconds after the HDMI grab, so it can't judge a black frame that
  lasts under a few seconds. A whole black launch is long enough. Reading
  fewer words, right after the grab, would close the gap.
- **More launches per run.** The rate is about 1 in 19, and the black
  launches were black from the introduction on. A run that stops after the
  menu grabs (no Journey Onward and 60 s of play) fits about twice as many
  launches into the same time.
- **Later diagnostic word.** `fes_display_plane`'s `ui_active`, `locked`,
  `displayed_sequence` and `faulted` outputs aren't connected in
  `pc386_machine.sv`, and the CRTC geometry isn't in the ring.
- No stage is named as the cause yet. A fix will be proposed on #1494
  before any RTL is written.

## Run `k3`: the VGA state in the ring (11 November, 23:58–00:57 EET)

- **Package.** `49ec69368d038d19c8abe9e7dc9cbcc78944322aa1fbd10faa2c841b05aa1941`,
  a scratch Quartus 17.0.2 seal of main `65ec1f22a` plus a diagnostic that
  isn't for merge (setup +0.724 ns, hold +0.044 ns, 36,563 ALMs). It puts
  the VGA's output state in the status ring's `capture_drops` word:
  - palette address source and the 3C0h flip-flop;
  - 3C3h video enable and SR01's screen-off bit;
  - the pel mask, the overscan colour and the attribute index;
  - the sequencer reset bits;
  - a count of DAC entries that aren't black, kept as the DAC is written.
- **Kit.** Kit B, core-only, on its new baseline (image `179affbb…`,
  software `7ddb0bfb…`).
- **Flow.** `k1`'s: at `max`, a new game and a save to slot 1 ("fesk3"),
  then 16 relaunches alternating 486DX2-66 and `max` (8 each), each with the
  introduction, Esc, Journey Onward and a grab every 3 s for 60 s.

**2 black launches in 17:**

- **The first launch, at `max`.** It made the save. All 18 of its grabs are
  the flat black frame `4c6d2c8d`. It has no ring snapshots (the save steps
  don't take them), so it is HDMI evidence only. The save landed anyway: the
  later launches' slot lists show "fesk3" (frame `p0n10-slots`). So mouse
  and keyboard input reached the game through a black launch.
- **Relaunch 12 (`p0n11`, `max`).** All 26 grabs are `4c6d2c8d`. Its 52
  ring snapshots, from 60 to 334 s of core time, all read:

  | Field | Value | In a lit launch |
  | --- | --- | --- |
  | Palette address source | 1 | 1 |
  | 3C0h flip-flop | 0 (index next) | 0 |
  | Video enable (3C3h) | 1 | 1 |
  | SR01 screen off | 0 | 0 |
  | Pel mask | FFh | FFh |
  | Sequencer reset | 3 (running) | 3 |
  | Overscan, attribute index | 0, 00h | 0, 00h |
  | Non-black DAC entries | 118–255 | 118–256 |

  The word at its menu (`p0n11-m6`) equals the word at the lit `p0n10`
  menu, 2952265216. VRAM at the menu is `1f9b027f` in both. The CPU, audio
  and `frames_shown` advanced as in `k1`'s black launch.
- **The click didn't load in either black launch with probes.** In
  `p0n11`, as in `k1`'s, VRAM went back to the introduction after the
  Journey Onward click (`c6370a05` at +6 s, CS `1A60`) and then to the menu.
  Input reached the game in the first launch, so this isn't lost input. Why
  the click missed isn't known.

**Five lit launches had a black grab at the introduction.** In `p7n01`,
`p0n03`, `p7n04`, `p7n05` and `p0n15`, the grab at about 59.5 s of core time
is `4c6d2c8d`, with VRAM `0de8e137` (the introduction's first scene) and a
non-black DAC count of 255. VRAM had changed 1 s later, and the menu showed
in each. Lit grabs at 59.35 s (`p7n00`) and 61.4 s (`p0n10`) show that
scene. The DAC count can't tell a dim palette from a black one, so this is
probably a fade in the introduction. It's left open, not counted as a
fault.

**`k3` against `k1` and `k2`.** `k3`'s first launch followed the stop of
the scrub image at the DOS prompt, not a stop in the game. So "`k2` quit to
DOS before each stop" doesn't explain why `k2` had none. Across the three
runs it is 3 black launches in 38: `k1` 1 of 9, `k2` 0 of 12, `k3` 2 of 17,
at 486DX2-66 once and at `max` twice.

## Runs `k4` and `k5`: capture-buffer samples (11 November, 01:21–02:45 EET)

- **Package and kit.** As `k3`: package `49ec6936…` on Kit B's baseline,
  core-only. No new seal.
- **What's new.** On every probed grab and every black grab, the driver
  read the three capture buffers the 720p presenter reads. Each sample is
  400 words per buffer: every 40th pixel of every 8th line, starting at host
  address `0x30A00000`, 2 MiB apart. The record keeps each buffer's word
  count, the count that aren't black, the brightest channel and a hash.
  Each launch also got a grab at the DOS prompt before `SERPENT`, and the
  setup launch a grab when its save was done.
- **`k4`** stopped after its setup launch: the driver's sample parser split
  on a hex digit. I stopped my own driver at 01:26 and cleaned its data.
  Its one launch was lit (peak 122). The parser was fixed and the run
  repeated as `k5`.
- **`k5`**: the same flow as `k3` (a save at `max`, then 16 relaunches
  alternating `max` and 486DX2-66, 8 each).

**No black launch in 17.** Every launch reached a lit picture (peak 254–255),
and every DOS-prompt grab was the same lit frame (peak 73). At the end of the
setup launch the three buffers held a picture: 391 of 400 samples not black,
brightest channel 255.

**The short black frames in lit launches.** 24 grabs had a peak under 16.
In each, the capture samples taken after it held a picture:

- **At the introduction (6 launches).** VRAM was `0de8e137`, as in `k3`'s
  five. The first buffer read had fewer lit samples and a dimmer brightest
  channel (300–313 lit, peak 154–243) than the two read after it (364–365 lit,
  mostly 243). That fits a fade-in still under way while the sample ran, so these
  are left as a fade.
- **3 s after Journey Onward (9 launches)**, where the load starts; **7
  grabs during play** with VRAM at the menu frame `1f9b027f`; and 2 others.
  These also come back as a picture, but the sample lands seconds after the
  grab, so they don't say which stage was black at the grab.

**Across the five runs** it is 3 black launches in 56: `k1` 1 of 9, `k2` 0
of 12, `k3` 2 of 17, `k4` 0 of 1, `k5` 0 of 17. At the earlier rate (3 in 38), 17
lit launches in a row happen about one run in four, so `k5` doesn't show the rate fell.

## Kit housekeeping

Run `k3` claimed Kit B at 23:58 EET, restored and released it at 00:57, and
removed its 5 FogCast data trees (2 game records, 2 media trees, the staged
Serpent Isle image) at 00:59, as logged in Kit B's status file. Runs `k1` and `k2` shared Kit B's claims and housekeeping with #1368's rerun; [its record](../2026-11-10-pc386-game-acceptance-rerun/summary.md#kit-housekeeping) has the table.

Run `k4` claimed Kit B at 01:21 EET and released it at 01:27, and its 5
data trees were removed at 01:27. Run `k5` claimed it at 01:28, restored and
released it at 02:44, and removed its 5 data trees at 02:45. Each is logged
in Kit B's status file. Neither run installed software or changed the image.
