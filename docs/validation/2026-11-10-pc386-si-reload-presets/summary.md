# pc386 Serpent Isle black picture, across presets, 10 November 2026

Issue [#1494](https://github.com/DeanoC/fes/issues/1494): in #1485's Kit B run
`v1`, Serpent Isle showed only black after Journey Onward loaded a save,
while audio played. [#1506](../2026-11-05-pc386-si-reload/summary.md) didn't
reproduce it in three runs. This record repeats it on a fresh seal of main
with #1483's status ring and VRAM probes, at four governor presets.

**Result: reproduced, 10 times in 299 launches. It isn't a speed artefact or
a long load. In `k6`'s black launch, the capture buffers held the same
picture as in the lit launches, and the picture was black from the DOS
prompt on. In `k8`'s six black launches, the core still drove lit pixels to
the HDMI transmitter pins: the 720p presenter's output and the display
plane's output both had lit pixels in every sampled frame, and the plane was
closed. So the black comes after the core's video output: the pin timing
into the ADV7513, the ADV7513 and its per-launch setup, the HDMI link, or
the Powerboat grabber.**

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
  address source, video enable, the screen-off bit, the pel mask, the
  sequencer and the count of non-black DAC entries were all as in a lit
  launch: the state word was identical. That rules out those bits only.
  The word leaves out the CRTC geometry, and the DAC count can't tell a dim
  palette from a black one or say which entries scanout uses. So the
  remaining candidates are the CRTC, the DAC's values, capture, the 720p
  presenter and the display plane.
- Runs `k4` and `k5` added 18 launches with capture-buffer samples and a
  DOS-prompt grab. None of them was black.
- Run `k6` (41 short launches, a capture sample during every grab) had one
  black launch. It was black at the DOS prompt, before `SERPENT` ran. At
  every grab its three capture buffers matched the lit launches either side
  at all 48 sampled pixels per buffer. The presenter showed 60 frames a
  second with no underflow. See [Run `k6`](#run-k6-capture-samples-during-the-grab-11-november-07190840-eet).
- Input still works in a black launch. `k3`'s first launch made its save
  blind, and the later launches list it.
- The other 93 launches showed their pictures. The load itself
  works: all 22 Journey Onward loads in lit launches (`k1` 7, `k3` 15) put
  the party back on the ship within 3–6 s, as the Bochs-oracle machine does in about 5.5 s.
- Runs `k7` (101 launches, 0 black) and `k8` (101 launches, 6 black) stopped
  every launch at the DOS prompt. `k8`'s scratch seal adds the presenter's and
  display plane's state to the ring. In all six black launches it read the
  same as lit: presenter and plane output lit, plane closed (`ui_active` 0,
  `locked` 1, `faulted` 0), presenter showing a 720x400 picture. See
  [Runs `k7` and `k8`](#runs-k7-and-k8-presenter-and-display-plane-state-11-november-09131200-eet).
- Which stage after the core's pins isn't pinned yet (see [Next](#next)).

The issue's title describes it too narrowly. The fault isn't tied to Journey
Onward, the save or Serpent Isle: it blacks out a whole launch from the DOS
prompt on. All 299 launches ran Serpent Isle's disk image, so the count stays
per launch of that image.

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

- **Which stage after the core.** In a black launch the core drives lit
  pixels with DE into `HDMI_TX_D` (`top.sv` wires the plane's output
  straight to the pins). What's left:
  - the pin timing into the ADV7513. Every core drives `HDMI_TX_CLK` from
    `pixel_clk` with no output-delay constraints, and #1003 found a
    placement-specific dropout at the ADV7513's sampling edge (fixed by
    the runtime's `0xBA` clock delay in #1011);
  - the ADV7513 and the runtime's setup of it at each load;
  - the HDMI link;
  - the Powerboat grabber. Every black observation comes from it, and
    `4c6d2c8d` is any flat-black 720p grab (the PC/XT records' `empty.png`
    has it too), so the grab can't tell black video from a lost signal.
- **Next diagnostics, cheapest first, proposed on #1494 before any of them:**
  1. Host only: Quartus timing of the `HDMI_TX_D`/`DE`/`HS`/`VS` pads against
     `HDMI_TX_CLK` for main's seal, `d1` and `d2`, to see whether they sit
     near the ADV7513's sampling edge.
  2. Powerboat, read-only: the grabber's signal status and detected timings
     at a black grab (`v4l2-ctl --query-dv-timings`), and HDMI audio across
     a black launch: audio means the link is up with black video.
  3. Kit, read-only: the ADV7513's registers (including `0xBA` and the
     status registers) in a black launch and a lit one, if the kit has an
     I2C read tool. Nothing installed.
  4. Any ADV7513 register write (a `0xBA` sweep) needs Deano's OK.
- **The intro grab at 486DX2-66.** It is black in 13 of 20 lit `k6`
  launches there (1 of 20 at `max`). The capture samples show a fade in
  some, so it is probably the grab landing on a scene change. Open, not
  counted.
- No stage is named as the cause yet, and no RTL fix is indicated in the
  core. A fix will be proposed on #1494 before any RTL is written.

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

## Run `k6`: capture samples during the grab (11 November, 07:19–08:40 EET)

- **Package and kit.** As `k3` (package `49ec6936…`), Kit B's baseline,
  core-only.
- **Flow.** The setup launch made a save at `max`. Then 40 relaunches
  alternated 486DX2-66 and `max`, each with a DOS-prompt grab, `SERPENT`,
  an introduction grab, Esc and menu grabs at +6, +10 and +14 s, then stop.
  No Journey Onward.
- **Sample.** Every grab starts a read of the three capture buffers while
  the HDMI grab runs: 48 pixels a buffer (8 lines × 6 pixels), about 1 s
  for all three. A black grab also gets `k5`'s 400-pixel sample, VRAM and
  the ring, twice, 1 s apart.

**1 black launch in 41: relaunch 33 (`p7n32`, 486DX2-66).**

- All five grabs are the black frame `4c6d2c8d`, from the DOS prompt on.
  The 18 black grabs in `k6` all have that hash, the same frame as `k1`'s
  and `k3`'s black launches.
- At the DOS prompt and at the menu, the fast samples have the same hashes
  as the lit launches before and after it (`p0n31`, `p7n33`):
  `1b6a5ad9`/`6f9b4838`/`6f9b4838` at the prompt, `b3d3e215`/`29ec6e20`/
  `29ec6e20` at the menu. Those are steady pictures, so the second or so
  between grab and sample doesn't matter. At the introduction, the samples
  hold its picture (42 of 48 lit, brightest channel 158), as in the lit
  launches.
- VRAM went from the DOS screen to the introduction (`0de8e137`) and the
  menu (`1f9b027f`) as in a lit launch. The VGA word was 2952265216 at the
  menu, the lit value.
- The presenter's `frames_shown` rose 60 a second, with
  `present_underflows`, `scanout_underflows` and `plane_underflows` all 0.
- Its load took 12.56 s, inside the run's 11.22–13.46 s spread. Input
  attached as in the other launches.
- By the second probe at +10 s, VRAM had left the menu for the
  introduction's segment (CS `1A60`). The probes stretched the menu grabs
  to about 27 s apart. That fits the menu going back to the introduction
  when left idle, and would explain why the Journey Onward click missed in
  `k1`'s and `k3`'s probed black launches. Not tested.

**The rest:** 40 lit launches. Every DOS-prompt grab is the same lit frame,
and every menu grab is lit.

**Across the six runs** it is 4 black launches in 97: `k1` 1 of 9, `k2` 0
of 12, `k3` 2 of 17, `k4` 0 of 1, `k5` 0 of 17, `k6` 1 of 41.

## Runs `k7` and `k8`: presenter and display-plane state (11 November, 09:13–12:00 EET)

- **Kit.** Kit B, core-only on its baseline (image `179affbb…`, software
  `7ddb0bfb…`). No install, no image change, no reboot.
- **Flow.** Each run: a setup launch, then 100 relaunches alternating 486DX2-66
  and `max`. Each launch stops after a DOS-prompt grab 22 s after load
  (about 47 s a launch). Every grab also reads the ring, the three capture
  buffers (48 pixels each) and the two display-plane slots in DDR (48 words
  each, `0x30000000` and `0x30400000`). The runtime's status socket can't be
  read from the kit (it has no `nc`, `socat` or Python), so the plane's
  state comes from the ring.
- **`k7`** ran `k3`'s package `49ec6936…`: **0 black in 101.** Every grab was
  the same lit DOS frame.
- **`k8`** ran a new scratch Quartus 17.0.2 seal (`d2`, package
  `ecef12e2…`, main `65ec1f22a` plus `k3`'s VGA word plus this one, not for
  merge; worst setup slack +0.960 ns, hold +0.082 ns). It puts in the ring's
  `samples` word: presenter output lit, plane output lit, the plane's
  `ui_active`, `locked` and `faulted`, and the presenter's `showing`, shown
  buffer, width and height. "Lit" means at least one DE pixel with non-zero
  RGB in the last frame.

**`k8`: 6 black launches in 101:** `p0n49`, `p0n61`, `p7n68`, `p7n90`,
`p7n94`, `p7n98` (2 at `max`, 4 at 486DX2-66). Each grab is the flat-black
frame `4c6d2c8d`.

- In all six, the ring read just after the grab and twice more 1 s apart
  says: presenter output lit, plane output lit, `ui_active` 0, `locked` 1,
  `faulted` 0, `showing` 1, 720x400. That is the same as all 95 lit
  launches. Only the shown buffer changes, cycling 0–2 as in lit launches.
- The capture-buffer samples have the lit DOS-prompt hashes (`1b6a5ad9`/
  `6f9b4838`/`6f9b4838`).
- Both plane slots read fully transparent (alpha 0 at all 48 words) at
  every grab, black or lit, in both runs.
- The plane's output is `video`, which `top.sv` drives straight onto
  `HDMI_TX_D`/`DE`/`HS`/`VS`. So in a black launch the core puts lit
  pixels on the HDMI pins. The flag doesn't count pixels or check sync and
  DE geometry, so it shows the presenter and plane aren't blanking the
  picture, not that every pixel is right.

**By seal** (observations, not a significant split): main's seal 1 of 21
(`k1`, `k2`), `d1` 3 of 177 (`k3`–`k7`), `d2` 6 of 101 (`k8`). `k8`'s six
are all in relaunches 49–98. A placement-dependent rate would fit the pin
timing candidate in [Next](#next), but these counts don't show one.

**Across the eight runs** it is 10 black launches in 299.

## Kit housekeeping

Run `k3` claimed Kit B at 23:58 EET, restored and released it at 00:57, and
removed its 5 FogCast data trees (2 game records, 2 media trees, the staged
Serpent Isle image) at 00:59, as logged in Kit B's status file. Runs `k1` and `k2` shared Kit B's claims and housekeeping with #1368's rerun; [its record](../2026-11-10-pc386-game-acceptance-rerun/summary.md#kit-housekeeping) has the table.

Run `k4` claimed Kit B at 01:21 EET and released it at 01:27, and its 5
data trees were removed at 01:27. Run `k5` claimed it at 01:28, restored and
released it at 02:44, and removed its 5 data trees at 02:45. Each is logged
in Kit B's status file. Neither run installed software or changed the image.

Run `k6` claimed Kit B at 07:19 EET, restored and released it at 08:38, and
removed its 5 data trees at 08:40, as logged in Kit B's status file. It
installed no software and changed no image.

Run `k7` claimed Kit B at 09:13 EET, restored and released it at 10:33, and
removed its 3 data trees at 10:34. Run `k8` claimed it at 10:35, restored
and released it at 11:58, and removed its 3 data trees at 12:00. Each is
logged in Kit B's status file. Neither installed software or changed the
image.
