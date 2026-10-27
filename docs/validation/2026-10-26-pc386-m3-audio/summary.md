# pc386 M3: PC speaker, OPL2, Sound Blaster 2.0 and mixer in simulation, 26 October 2026

**The pc386 audio plays Wolfenstein 3D and Ultima VI on the oracle-CPU
machine, and its OPL2 is sample-exact against Nuked-OPL3.** The imported
opl2_fpga core, after the local changes listed in its README, matched
Nuked-OPL3 on every one of 2,488,129 samples checked: random register
programs, a start-up exercise program, and both games' start-up as the
machine ran them. The Sound Blaster 2.0 DSP passes 87 directed checks of its
commands, DMA and IRQ at every base port and IRQ choice, and played
Wolfenstein 3D's digitized sounds by DMA with IRQs. Of phase M3's three exit clauses in the
[machine design](../../../sources/misteross/docs/pc386-machine-design.md#phases),
two are measured and pass (OPL2 sample-exact; DSP sequences); the third,
start-up audio that **sounds right**, is a listening judgement nobody has made
yet, so it stays open. This is a **host-only check**: no synthesis, routed
result, package or kit is involved.

## Inputs

- misteross on PR branch `deano/pc386-m3-audio`
  ([#1147](https://github.com/DeanoC/fes/pull/1147)), on FES `8bfd4ecc13`.
  The audio is in [`cores/fes-pc386/`](../../../sources/misteross/cores/fes-pc386/README.md#audio).
- [opl2_fpga MiSTer port](../../../sources/misteross/cores/fes-pc386/vendor/opl2_fpga/README.md)
  `451e5b0b` (LGPL-3.0) with eleven changed files; reference
  [Nuked-OPL3](../../../sources/misteross/cores/fes-pc386/vendor/nuked-opl3/README.md)
  `765ec962`, default build, OPL2 mode.
- Behavioural reference for the DSP: DOSBox-X `src/hardware/sblaster.cpp`
  (master, read 2026-10-26).
- Machine as in the [M1 record](../2026-10-26-pc386-m1-freedos-boot/summary.md):
  Bochs oracle `30cfd566`, the locked Bochs BIOS and VGABios, the FreeDOS 1.4
  fixture, 100 MHz, four clocks per instruction. Verilator 5.032.
- Sound Blaster setting 220h, IRQ 7, DMA 1 in the machine runs (CMOS 60h-62h
  say so); the device bench covers 220h and 240h with IRQ 2, 3, 5 and 7.

## OPL2 against Nuked-OPL3

[`opl2-report.json`](opl2-report.json): `python3 scripts/sim_fes_pc386.py --suite opl2`,
about 30 s. The bench drives the wrapper's AdLib port one byte per isa8 cycle
and steps Nuked-OPL3 at each RTL sample enable with the writes the wrapper
released before it.

| Program | Samples | Exact | Non-zero | Register writes |
| --- | ---: | ---: | ---: | ---: |
| Random seed 1: all nine channels keyed, then bursts to every register class | 19,995 | 19,995 | 19,991 | 213 |
| Random seed 2 | 19,995 | 19,995 | 19,990 | 227 |
| Random seed 3 | 19,995 | 19,995 | 19,991 | 224 |
| SOUND.COM's port writes captured from the machine ([fixture](../../../sources/misteross/cores/fes-pc386/sim/fixtures/sound-opl2.log)) | 98,134 | 98,134 | 83,656 | 125 |

Writes land at random points in the sample, so some arrive during the
72-clock computation and wait in the wrapper's queue. The machine runs below
also compare every OPL2 sample as they play. Reverting one local change (the
envelope step table) makes all three random programs fail from sample 11.

Upstream matched about 3% of samples on the first random programs. The changes that closed the gap,
in the order the bench found them: key-on phase reset, the reversed envelope
step table, the AM bit read for the wrong operator, both LFOs, the rhythm noise
generator and drum key-on, NTS latched per channel, and the mix (channels 6-8
one sample late, no doubling).

## Device bench

[`audio.log`](audio.log): `python3 scripts/sim_fes_pc386.py --suite audio`, 87
checks, all pass, about a minute. The AT core and `pc386_audio` run together,
so DMA goes through the AT core's 8237 and IRQs through its PICs.

- At 220h and 240h with IRQ 2, 3, 5 and 7 (eight builds of the setting): reset
  gives AAh 20 us after release; E1h returns 2.01; the other base reads FFh;
  F2h interrupts about 10 us later at vector 0Bh, 0Dh, 0Fh, or 71h for IRQ 2
  (the AT's IRQ 9); reading base+E acknowledges it; the OPL2 answers at base+8.
- Commands: write status AAh just after a byte and 2Ah 3 us later; E0h, D8h,
  E4h/E8h, 20h; an empty read buffer repeats its last byte; the DAC muted while
  the speaker is off; ADPCM 74h consumes its two argument bytes.
- 14h: 32 bytes in order through the DAC, 100 us apart at time constant 9Ch,
  IRQ 5 at the end, then no more DMA; the 8237 reaches terminal count.
- 1Ch with 48h: an IRQ every 16 samples at 20 kHz; D0h halts and D4h
  continues; DAh ends after the block.
- 91h: busy (AAh) during the block, a byte written then is dropped, IRQ 3 at
  the end. 90h stops on reset.
- 80h: IRQ after ten silent samples. 24h: 16 bytes of 80h written by DMA.
  E2h: DOSBox-X's identification bytes (76h, then DCh) written by DMA.
- AdLib detection at 388h (flags clear, timer 1 at FFh sets C0h within
  80.5 us); timer 1 from 00h overflows in 20.60 ms and timer 2 from F0h in
  5.15 ms; a masked timer sets no flag; the data port reads FFh.
- Mixer: a PIT channel 2 square wave at 1000.2 Hz with a peak of about 8192
  (-12 dBFS); the DC blocker takes a held speaker to near zero; a full-scale
  DSP byte reaches 16256 (-6 dBFS); an OPL2 note at its f-number frequency
  (440.0 Hz) with a single operator's peak of about 2048.

The M1 device bench (`--suite devices`) still passes with the AT core's sound
port added.

## Start-up exercise on the oracle machine

[`sound-report.json`](sound-report.json): `python3 scripts/sim_fes_pc386.py --suite sound`.
FreeDOS boots as in M1, `sound` is typed at the prompt and
[SOUND.COM](../../../sources/misteross/cores/fes-pc386/sim/sound.asm) does
what games do at start-up. It printed `ADLIB OK`, `SB DSP 2.01`, `SB IRQS 6`
and `SOUND DONE`, and the prompt returned at 6.87 simulated seconds.

| Section | Measured in the 48 kHz output | Expected |
| --- | --- | --- |
| PIT channel 2 speaker, divisor 1356 | 879.94 Hz, peak 9728 | 880 Hz |
| Speaker toggled by a CPU loop | 3091 Hz | set by the instruction rate |
| OPL2 phrase, drums, tremolo and vibrato | RMS 1269, peak 12457; every sample exact (341,375 of them over the whole run) | Nuked-OPL3 |
| DSP single-cycle DMA, 4000 bytes at 8 kHz | 1000.0 Hz, RMS 8986 | 1 kHz |
| DSP auto-init, 1000-byte blocks, then DAh | 1000.0 Hz, RMS 9066 | 1 kHz |

The DSP saw one reset and 13 command bytes, moved 9,000 bytes by DMA and
raised 6 IRQs. Every CPU I/O completed at exactly its timing class, as in M1
(2,064,435 isa8 bytes at 74 clocks, 145,408 isa16 words at 38). The checks
are the measured tones; nobody has yet listened to the capture and judged it.
It is `build/sim/fes-pc386/sound/sound.wav` after the run.

## Games on the oracle machine

The acceptance games are user-supplied. They were read from the eXoDOS 6.04
collection on the developer's host, unpacked into a scratch directory outside
the repository, and copied onto a copy of the FreeDOS fixture; nothing from
them is committed. Their WAVs and register logs stay in that scratch
directory. `python3 scripts/sim_fes_pc386.py --suite game --game <name>`.

**Ultima VI** ([`ultima6-report.json`](ultima6-report.json)): `Ultima VI - The False Prophet (1990).zip`,
SHA-256 `5ec54a04…881cb9`, 324 files; its own `CONFIG.U6` (`vma388`: VGA,
AdLib at 388h). `cd ultima6`, `ultima6`, no keys, 16 simulated seconds. The
intro's music wrote 3,813 OPL2 registers; all 795,454 OPL2 samples of the run
equal Nuked-OPL3's (513,701 non-zero). It does not use the Sound Blaster.
Every I/O completed at its class time.

**Wolfenstein 3D** ([`wolf3d-report.json`](wolf3d-report.json)): `Wolfenstein 3D (1992).zip`
(registered v1.4, `.WL6`), SHA-256 `f451d853…41ed37`, 11 files; its own
`CONFIG.WL6` (AdLib effects, AdLib music, Sound Blaster digitized sound).
Beside a resident FreeCOM it stops with "You do not have enough memory to run
Wolfenstein 3-D": the fixture has no HIMEMX, so DOS cannot load high. The game
disk therefore makes `WOLF3D.EXE` FreeDOS's shell (`SHELL=C:\WOLF3D.EXE`, game
files in `C:\`), with no `BLASTER` variable: the game probed the card itself.
Keys, blind: Enter every 1.5 s from 4 s through the signon, the PG-13 and
title screens and the menus into E1M1, then Ctrl and space every 0.8 s from
17.5 s. In 24 simulated seconds:

| | |
| --- | --- |
| OPL2 | 4,802 register writes; all 1,193,181 samples equal Nuked-OPL3's, 986,226 non-zero |
| Sound Blaster | 1 reset, 48 command bytes, 27,848 bytes by DMA, 7 IRQs |
| I/O | every access at its class time, after 28 byte and 36 word VGA port writes waited behind a full VGA FIFO (up to 91 and 110 clocks); 26 million VGA port accesses |

The DMA and IRQs are the game's digitized sounds playing through the DSP.
Nobody has yet listened to either game's capture and judged it against a
period recording.

## Recorded behaviour and deviations

These are in the design's audio section and the core README.

- OPL2 register writes reach the import between samples (the wrapper queues
  one that arrives during the 72-clock computation), as Nuked-OPL3 applies
  them.
- Register 01h bit 5 and CSM mode are not modelled, as in Nuked-OPL3.
- DSP reset takes 20 us (DOSBox-X), not the guide's "about 100 us"; the status
  ports read AAh and 2Ah as DOSBox-X records from an SB 2.0; 80h, E4h and E8h
  are implemented besides the design's table.
- The Bochs BIOS's INT 15h AH=86h never returns on this machine: the BIOS
  does not program PIT channel 1, which port 61h bit 4 follows. SOUND.COM
  times with PIT channel 0 instead. Filed as
  [#1150](https://github.com/DeanoC/fes/issues/1150).
- Wolfenstein 3D needs more conventional memory than the M1 fixture leaves
  beside FreeCOM; the game disk runs it as the shell instead (above). The
  design's acceptance setups load HIMEMX.
- The audio model costs about as much simulation time as the rest of the
  machine (2.4 million clocks per second alone), so the boot suite now takes
  about twice as long as in M1.

## Not covered

- Ultima VII (both parts): their Voodoo memory manager needs HIMEMX and 2 MiB
  of XMS, which the FreeDOS fixture does not carry. Their start-up audio is
  unchecked; it needs a fixture with HIMEMX.
- The games' play beyond start-up, and Ultima VI's speaker effects (in play,
  not at start-up) at the 286-class preset.
- A listener comparing the captures with period recordings, and tuning the
  mixer gains by ear, as the design asks.
- Synthesis: whether the locked Yosys frontend takes the import's
  SystemVerilog is phase M4's question.
- The shell, the I2S frame request and `fes.audio.pcm-s16-stereo-48k`; the
  benches make the 48 kHz request from a rational enable.

## Follow-up: DSP writes during busy, 27 October 2026

At FES commit `9d34913cf78354497c786dd4dbb9f1702e0939f6`,
`make sim-fes-pc386` passed the device, audio and OPL2 suites. The audio bench
passed 88 directed checks with no failures. Its added check issues an unpolled
speaker-on write less than 1 us after speaker-off and confirms D8h still reads
00h, so the write during the busy window was dropped. All four OPL2 comparisons
passed. This remains host-only simulation evidence.

## Follow-up: the M2 VGA beside the audio, 27 October 2026

With main merged in (M2's RTL VGA, [#1149](https://github.com/DeanoC/fes/pull/1149))
and the busy-write change above, the sound suite, both games, the vga suite
and the boot suite were run again; the reports and the numbers above are from
those runs. SOUND.COM, Ultima VI and the audio of Wolfenstein 3D behave as
before. Wolfenstein 3D's timing moved slightly with the RTL VGA, so it moved
27,848 bytes by DMA in the 24 s instead of 30,821, still with 7 IRQs.

Wolfenstein 3D fills the VGA FIFO now and then, as the
[M2 record](../2026-10-26-pc386-m2-vga/summary.md) found. A card with a full
write FIFO holds the bus until it has room, and the RTL VGA does the same. The
machine bench now counts the clocks a VGA port write is refused for a full
FIFO (`io_wait` in the reports) and takes them out of its time; what remains
must still be exactly the class time. 28 byte writes and 36 word writes
waited, and every VGA port write then took 4 or 7 clocks. The vga and boot
suites never filled the FIFO and pass unchanged.
