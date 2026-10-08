# Atari ST input/audio diagnostic and demo admission

This validates the original EmuTOS input/audio guest on Kit A and inspects the
user-selected BIG/TCB demo disks. It adds diagnostic source and evidence, with
no production RTL, runtime, FogCast, shared schema or media-contract change.
Base is FES `c3481d7f0dd11776488805382415803169edcba0` after #601 merged.
[Evidence](2026-10-07-atari-st-input-audio/evidence.json) binds the inputs,
measurements and captures. This is not general Atari ST game/demo compatibility.

## Original guest and host checks

`scripts/atari_st_io_diagnostic.py` generates a fresh 720 KiB FAT12 disk with
`AUTO/IOTEST.PRG`. The guest enters supervisor mode through GEMDOS, masks the
ACIA interrupt handler while retaining timer/VBL operation, and reads exact
IKBD bytes. Unexpected bytes print FAIL; missing bytes leave WAIT visible.
After eight PASS groups it repeats six three-second YM phases. It preserves
floppy port direction bits/latches and does not write the disk.

Hardware PRG SHA-256 is
`651061b0a8a6e01c6167f3d852f2cb3a8b4295332557276e4706c736ff7a4160`;
disk SHA-256 is
`853c9d8ed5effd652d73f0c6d10096885afbe2fdfe6516518757c64d5168821f`.
The [generated schedule](2026-10-07-atari-st-input-audio/schedule.json) supplies
all public API events and expected bytes. No proprietary ROM is bundled.
The separate stock EmuTOS 1.4 US 192 KiB test ROM has SHA-256
`8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc`.

The frozen guest simulation executes actual FX68K, stock EmuTOS, IKBD/ACIA,
YM and floppy/DMA RTL with bounded RAM/disk callbacks. It passed all 48 events,
eight guest groups, all six audio phases, 1,137,107 instruction fetches, 26
GEMDOS traps, 5,120 DMA words and 10,240 media exchanges. RAM discovery,
framebuffer configuration and ongoing MFP/VBL interrupts passed. The guest
left its disk unchanged. Simulation uses shortened 20-tick audio holds with
separately bound PRG/disk hashes, not the hardware fixture. Its independent PCM
oracle measured 440.079/661.447/880.196 Hz, active noise and initial envelope
steps, and zero settled silence. It does not model physical SDRAM or HDMI.

Validation also passed 16 Atari diagnostic Python tests, three MSA inspection
tests, the existing `sim-fes-atari-st-input-audio` suite, shared
`sim-fes-audio-output`, Python compilation and `git diff --check`. The focused
Make simulation now creates its output directory for a fresh worktree.
No synthesis, routing or cold image build ran for this diagnostic-only change.

## Exact hardware route and results

The unchanged, previously verified two-pass image from producer
`0cee5866db0e5d2d5ec426d5f3034a28b230a1bc` was reused:
`efe04797f2682c5c76372999a0628dc4f62742905e253dfc557311c0e0453075`.
At the base commit, FogCast, runtime and the ST core subtree matched that
producer. The installed image, read-only loop root, nine closed core packages,
four video parts and live executable hashes were rechecked. This adds evidence
for that exact artifact; it does not claim a newly rebuilt PR image.

The ST shell package is
`eba89c8b44c1650ca2ef77985ecf34f46ac27fce3e3cc33e84059b2a7bfb4e0d`,
using its built-in Direct output. Normal library Play linked the ROM and loaded
the original disk before CPU release. Launch took 55.380 seconds with the
isolated host's configured 60-second upload timeout. Events passed through the
normal attached host input stream and leased target/runtime route. Each next
group waited for visual confirmation of the preceding PASS and new WAIT.

| Guest group | Result |
| --- | --- |
| Shift+A make/break | PASS |
| Overlapping left/right Ctrl aliases | PASS |
| Overlapping left/right Alt aliases | PASS |
| Right Shift+B make/break | PASS |
| Escape and Backspace make/break | PASS |
| Player 0 / ST joystick 1: all directions, A/B fire and releases | PASS |
| Player 1 / ST joystick 0: all directions, A/B fire and releases | PASS |
| Simultaneous independent two-port input | PASS |

[READY](2026-10-07-atari-st-input-audio/ready.png), individual `group-*.png`
captures and [IO PASS](2026-10-07-atari-st-input-audio/io-pass.png) are bound in
the evidence. This qualifies host-delivered input, not physically attached
USB keyboards/controllers or a Stop/relaunch while keys remain held.

HDMI capture contains stereo signed 16-bit PCM at 48 kHz. The
[lossless recording](2026-10-07-atari-st-input-audio/hdmi-audio.flac) decodes
byte-for-byte to the captured samples. Independent windowed spectral analysis
measured the tones below, broadband nonzero noise and changing envelope
amplitude. Settled guest silence was zero. After Stop, the
[silence recording](2026-10-07-atari-st-input-audio/stop-silence.flac) contained
139,180 zero samples per channel after discarding the initial 100 ms capture
settling interval; its first 2 ms contained buffered audio.

| YM channel | Programmed pitch | Captured median |
| --- | ---: | ---: |
| A | 440.141 Hz | 440.195 Hz |
| B | 661.376 Hz | 661.319 Hz |
| C | 880.282 Hz | 880.370 Hz |

The recording's right channel lags the left by exactly one sample. Compensating
that lag makes the entire recording bit-identical between channels. Its origin
is unqualified: attribution to HDMI transport versus the capture device needs
a separate measurement. The existing coherent stereo/serializer simulation
passes. This diagnostic establishes audible PCM content and settled Stop mute,
not zero channel skew, analog DAC fidelity or a compiler defect.

The first hardware attempt stopped before input injection because the private
host had remote input disabled. The exact owned session was stopped and the
normal menu restored; enabling input only in the private configuration allowed
the successful run above. The installed-image helper was also corrected to
wait for asynchronous lease cleanup before its final idle check.

## BIG and TCB demo admission

The offline `scripts/atari_st_demo_media.py` inspector validates complete
classic MSA track blocks and RLE runs against the
[documented MSA format](https://github.com/hatari/hatari/blob/main/src/floppies/msa.c).
It reports geometry and decoded raw hashes without truncation or padding.

| Independently acquired image | Tracks × sides × sectors | Raw bytes | Current admission |
| --- | --- | ---: | --- |
| [BIG original](https://no-fragments.atari.org/no_fragments_01/MSA/T/TEX/BIG_DEMO.MSA) | 80 × 1 × 10 | 409,600 | Unsupported geometry |
| [BIG_100 archive](https://fujiology.org/ST/T/TEX/BIG_100.ZIP) | 80 × 1 × 10 | 409,600 | Unsupported geometry |
| [TCB Cuddly](https://fujiology.org/ST/T/TCB/CUDDLY.ZIP) | 82 × 2 × 10 | 839,680 | Unsupported geometry and capacity |
| [TCB CuddlySS](https://fujiology.org/ST/T/TCB/CUDDLYSS.ZIP) | 82 × 2 × 10 | 839,680 | Unsupported geometry and capacity |

The original BIG and Cuddly boot sectors independently declare matching
ten-sector geometry and have executable `$1234` checksums. CuddlySS's actual
header still declares two sides. The current media contract and WD1772/DMA
addressing admit only 80 × 2 × 9 × 512-byte raw ST disks. Merely decompressing
MSA does not fix that mismatch. The demos were not launched; their RAM/ROM,
loader, fullscreen, border and raster compatibility remain unqualified.
Downloaded demo/ROM binaries stay outside Git.

The next compatibility step is explicit geometry/capacity support across the
shared media contract, runtime/host admission and WD1772/DMA buffer indexing,
then the original BIG/TCB loaders and effects. Current reduced native timing
and fixed-resolution video remain independent border/raster limitations; this
disk-admission result is not evidence of a nextpnr defect.

## Restoration and integration

Managed rollback confirmed the actual initial image
`5032c2d2282da26e79e6f7efc47f9f2a1aa9abca4aad4ca6f19817e82d18ce4a`.
The normal host and original autostart are active/enabled, its API serves all
4,253 library entries, and the
[populated idle menu](2026-10-07-atari-st-input-audio/final-populated-menu.png)
was inspected on HDMI. The target is ready/idle with a free lease. The exact
task-owned private host and credential copy were removed; owner configuration,
protected images, saves and unrelated work were preserved.

Integration needs review/merge of the diagnostic source and this evidence.
No shared consumers or compiled production contracts change.
