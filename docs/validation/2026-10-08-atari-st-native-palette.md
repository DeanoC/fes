# Atari ST native low-resolution palette capture

This follow-up to [task #621](https://github.com/DeanoC/fes/issues/621) and
[PR #630](https://github.com/DeanoC/fes/pull/630) starts at merged main
`582d3c608eb7bd0a89ca7940b9cbb6fce2c7eb39`. The production implementation is
`22e3994166ac18b1aa0b898785bc949dbe980ad5`. The [evidence record](2026-10-08-atari-st-native-palette/evidence.json)
binds the captured input hashes, completed simulations and companion files.
The earlier [BIG loader hardware record](2026-10-08-atari-st-big-loader.md)
applies only to its earlier FPGA package.

## Ordinary raster capture

The low-resolution renderer now captures live palette selection at nominal
8 MHz before output scaling. Alternating RAM row caches feed three 320×200
RGB333 banks. Publication/release toggles transfer ownership between system
and pixel clocks; only complete native frames become available at output SOF.
The reader pins its displayed bank until a later SOF. A stopped reader makes
the producer skip frames. Missing memory rows become whole black lines;
HOLD discards incomplete captures and invalid bases cannot wrap into RAM.
Medium/high modes retain the indexed renderer and frame-held palette.

The reduced I/O model now uses ordinary PAL and NTSC display porches,
following [Hatari 2.5.0's ordinary timing table](https://github.com/hatari/hatari/blob/v2.5.0/src/video.c).
It samples mode settings at line/frame boundaries. The first BIG run with
immediate mode changes captured only nine complete native frames during the
seventh simulated second; immediate mode changes could interrupt DE in that model. Boundary sampling
produces fifty complete frames during that interval. This is deliberately an
approximation, not original GLUE latch positions or opened-border behavior.

## Digital checks

The complete ST simulation aggregate passes. The default production adapter
runs with independent 52.224/74.25 MHz clocks and exact per-pixel expectations
for within-line palette changes and fixed HDMI timing. Its 178,966,052 checks
cover ownership, 50-to-60 Hz repeats, delayed memory, whole-line black recovery,
HOLD, invalid bases, medium/high mode recovery and a paused consumer. The run
publishes eleven captures, repeats six output frames and records 203 missing
native rows: three from injected latency and 200 from the invalid base.

The I/O test passes 653,108 assertions, including the ordinary vertical and
horizontal porches, 200 color/400 monochrome Timer B edges per frame and brief
mid-line mode writes that retain the PAL frame period. Nineteen producer,
nine demo-helper, three memory-helper, one capture-helper and fourteen
compiler read-audit tests pass.
Parent consistency passes with eighteen generated consumers and thirty-four
fixture copies. Shared ABI, generated contracts, video socket layout and
physical fences are unchanged.

The assembled stock EmuTOS 192US 1.4 test uses real FX68K/chipset/memory RTL,
physical SDRAM commands and independent pixel clocks. Over eight simulated
seconds it passes 417,792,000 system and 594,018,699 pixel clocks, with 479
complete native/HDMI frames, zero skipped frames and zero native/indexed
underruns. CPU/video latency maxima are 63/64 system clocks. RAM sizes to
512 KiB, screen base is `$78000`, and timer/VBL processing continues under
contention. This is a digital SDRAM model, not physical-kit acceptance.

## Original BIG disk and TOS 1.00

The unchanged 409,600-byte disk has SHA-256
`608c4beff1780552e8ae6ee5a1efff27198fb6a3ca6274597f1cb528c3970edb`.
The independently supplied 192 KiB TOS 1.00 ROM has SHA-256
`5771f9fd1391d3ae0b513ab3fd67aec30fb1843753e7e0345f92615e72c36797`.
Neither ROM nor raw RAM is published.

The frozen twelve-second CPU/chipset diagnostic selects B through the existing
HID/IKBD path at eight seconds. All twenty-one captured compiled/helper inputs
match the implementation commit. Frozen sources, working inputs, executable,
generated model, microcode, ROM and disk remain unchanged during execution.
It finishes with 599 complete native captures, zero capture underruns, zero
external bus faults and no CPU halt. Native capture uses model RAM and a
system-clock consumer; it does not exercise the physical SDRAM arbiter or HDMI
clock crossing. Those are separately exercised by the tests above.

The seventh-second menu capture has 32 distinct RGB colours, compared with
15 in the same-second static RAM/palette reconstruction. The final B scroller
capture has 39 colours and visible rainbow bands. These establish native
palette capture in this model, not complete BIG compatibility or a pixel-exact
original-machine comparison. The earlier independent Hatari capture remains
a visual reference; scrolling phases are not synchronized for exact equality.

![Native menu capture](2026-10-08-atari-st-native-palette/big-menu.png)

![Native B scroller capture](2026-10-08-atari-st-native-palette/big-scroller.png)

## FPGA and hardware qualification

The first selected-source synthesis passes and maps 418 M10Ks in total,
including 216 for the three RGB banks. Its route misses clock timing: an
intermediate report gives 47.04 MHz pixel and 44.82 MHz system. The critical
paths run through coordinate arithmetic/native DE validity into RGB RAM.
Registered write data and a two-pixel read-address forecast shorten those RAM
paths. The full native pixel test, ST aggregate and assembled EmuTOS regression
pass after this change. Every native BIG snapshot matches the preceding
capture byte for byte.

A subsequent completed route reaches 66.15 MHz pixel and 49.91 MHz system,
still below 74.25/52.224 MHz. Its limiting paths are publication ordering and
row-validity selection into RGB data. The current source uses bounded
three-bit publication ordering and registers black-line validity separately
from RGB. The native pixel, aggregate and assembled EmuTOS simulations pass with these
changes. Every native BIG snapshot also matches the preceding run byte for byte.

The revised synthesis initially exceeds the compiler read-audit trace cap.
The allowance is now bounded at 64 MiB per process and 256 MiB total, with
expanded error diagnostics. A live overflow regression confirms cancellation
of the compiler process. Source exclusion and complete-trace checks remain
required. Fresh FPGA route, final clock timing and exact-shell Direct/Scanlines
qualification are in progress. No new hardware
transition has occurred; the normal populated menu remains available. The
earlier hardware package does not qualify this production change.

Opened borders, exact GLUE/MMU/shifter timing, mid-line base changes, later BIG
sections, audio fidelity and full current-image integration remain open. Keep
#621 open until its intended borderless effects and correct sound are qualified.
