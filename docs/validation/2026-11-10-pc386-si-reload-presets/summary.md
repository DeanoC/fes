# pc386 Serpent Isle black picture, across presets, 10 November 2026

Issue [#1494](https://github.com/DeanoC/fes/issues/1494): in #1485's Kit B run
`v1`, Serpent Isle showed only black after Journey Onward loaded a save,
while audio played. [#1506](../2026-11-05-pc386-si-reload/summary.md) didn't
reproduce it in three runs. This record repeats it on a fresh seal of main
with #1483's status ring and VRAM probes, at four governor presets.

**Result: reproduced, once in 21 Serpent Isle launches. It is a pc386
fault, not a speed artefact or a long load.**

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
  picture: the VGA's output state (attribute controller, pel mask, DAC) or the
  scanout.
- The other 20 Serpent Isle launches showed their pictures: `k1`'s first
  launch, which made the save, its other 7 relaunches, and `k2`'s 12. The load itself
  works: 7 of `k1`'s 8 Journey Onward loads put the party back on the ship
  within 3–6 s.
- The cause isn't pinned yet. A scratch seal that adds the VGA output state
  to the status ring is built and passes timing. Its kit run is pending (see
  [Next](#next)).

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
either. The bench's Journey Onward load (click at 64.5 s) was still running
when this record was written; it runs at about 2 minutes a simulated second.

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

- **The diagnostic seal.** A scratch Quartus 17.0.2 seal of main `65ec1f22a`
  carries the VGA's output state in the status ring's `capture_drops` word,
  which is always 0 on the kit. The word holds:
  - palette address source and the 3C0h flip-flop;
  - 3C3h video enable and SR01's screen-off bit;
  - the pel mask and the overscan colour;
  - the attribute index;
  - a count of DAC entries that aren't black, kept as the DAC is written.

  Package `49ec6936…`, setup +0.724 ns, hold +0.044 ns, 36,563 ALMs. It's
  not for merge. Run `k3` (`k1`'s flow, 16 relaunches alternating
  486DX2-66 and `max`) is ready. It couldn't start: at 20:29 and 20:36 EET
  Codex claimed both kits for #863's image work.
- **What it decides.** On a black launch, the snapshot says which of
  these holds:
  - palette address source off (a mode-set race, or a VGA fault in the
    attribute flip-flop);
  - the pel mask 0;
  - the screen switched off;
  - a black DAC.

  If none of them holds, the fault is in scanout. A fix will be proposed on
  #1494 before any RTL is written.

## Kit housekeeping

Runs `k1` and `k2` shared Kit B's claims and housekeeping with #1368's rerun; [its record](../2026-11-10-pc386-game-acceptance-rerun/summary.md#kit-housekeeping) has the table.
