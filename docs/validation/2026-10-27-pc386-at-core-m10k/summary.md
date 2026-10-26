# pc386 AT core memories in M10K, in simulation and synthesis, 27 October 2026

**The pc386 AT core fits its area budget: at least 3,785 ALMs and 4 M10K,
down from 16,526 ALMs and 0 M10K, and the machine boots FreeDOS on exactly
the clocks it took before.** Three memories move from flip-flops to M10K
with registered reads: the ATA sector buffer (128 × 64), the ATA dirty map
(128 × 16) and the CMOS (64 byte pairs). The FreeDOS boot report is identical
to the [M2 record's](../2026-10-26-pc386-m2-vga/summary.md#freedos-boot) in
every field. That covers 430,003,773 clocks, the prompt at clock 240,003,758,
every I/O class count and time, the VGA counts, the screen and the replay.
The device checks and the presenter and plane benches pass unchanged. This is
a **host-only check** plus diagnostic synthesis: no route, package or kit.

## Inputs

- RTL at `45ad391f73` on `deano/pc386-at-m10k`, on the M2 branch
  (`deano/pc386-m2-vga` at `3e0d275abb`, PR
  [#1149](https://github.com/DeanoC/fes/pull/1149)). The synthesis summary's
  source digests are those files. Issue
  [#1152](https://github.com/DeanoC/fes/issues/1152).
- Firmware, the FreeDOS image, the oracle, Verilator and the DDR model as in
  the [M2 record](../2026-10-26-pc386-m2-vga/summary.md#inputs).
- Tools for synthesis: the i386 lock
  (`cores/fes-common/rtl/i386/toolchain.lock`): Yosys `238eac9a`, nextpnr-mistral
  `bd7a1c0d`, Mistral `8fcc4cb4`, from the authenticated cache.

## What changed

- **ATA sector buffer** ([`pc386_ata.sv`](../../../sources/misteross/cores/fes-pc386/rtl/pc386_ata.sv)):
  a 128 × 64 `pc386_ram`, 2 M10K. The data port reads the word at the guest's
  pointer, re-read every clock. The pointer moves only at a device cycle's
  access, at least 36 clocks apart, so the sampled word is the one the
  combinational read gave. The guest's 16-bit writes collect in a register
  and go into the buffer whole with the fourth write. The buffer therefore
  needs no 16-bit write enables, which under this Yosys would cost a block per
  lane. The store engine presents each word's address a clock before it
  issues the word, so the beat timing on the image port is unchanged. The
  fetch and the guest never write in the same clock unless a guest writes the
  data port during a read command; that word then follows a clock later.
- **Dirty map:** a 128 × 16 `pc386_ram`, 1 M10K. A sector store marks its
  chunk by read-modify-write over the two clocks after the store. A reset
  clears the map over 128 clocks, during which `dirty_word` reads 0.
  `dirty_word` is now the word at `dirty_index` two clocks earlier. It holds
  over a map read that met a write, which M10K leaves undefined.
- **CMOS** ([`pc386_rtc.sv`](../../../sources/misteross/cores/fes-pc386/rtl/pc386_rtc.sv)):
  64 pairs × 16 bits with byte enables, 1 M10K. 71h reads the pair at the
  index, re-read every clock. The checksum reads one pair ahead, and 2Eh–2Fh
  is written as one pair. `busy` therefore falls on the same clock as before,
  and the release of the CPU with it. The memory has one write port, taken in
  this order: setup, then the guest, then the century byte. The century byte
  waits for a free clock and gives way to a guest write of 32h.
- Unchanged, measured inside their rows: the 8042 RAM and FIFOs, and the DMA
  register files in the shared `fes_dma8237` (pcxt uses it too).

Recorded deviations, both protocol errors no driver makes:

- A data-port read during a write command returns no defined data.
- A guest write to CMOS while `busy` is lost. The testbenches release the
  CPU only after `busy` falls, and so will the runtime that the endpoint
  phase adds.

## Device checks and benches

- `python3 scripts/sim_fes_pc386.py --suite devices`
  ([`devices-report.json`](devices-report.json)): all 72 checks pass. They
  include WRITE SECTORS to the image and the dirty bit of its chunk, READ
  SECTORS back, the CMOS configuration bytes and checksum, and the
  wall-clock century.
- `--suite benches` ([`benches-report.json`](benches-report.json)): the
  presenter and plane benches pass.

## FreeDOS boot

`python3 scripts/sim_fes_pc386.py --suite boot` ([`boot-report.json`](boot-report.json)):
`END prompt`, `dir` at `C:\>`. The run retired 103,831,426 instructions over
430,003,773 clocks (4.30 s). The VGA replay matched: 85,234 events with no
replay, read, VRAM or frame mismatch. Every field of the report, the I/O
timing classes included, equals the M2 report.

## Machine-only synthesis

`python3 scripts/synth_fes_pc386.py --cache-root … --block at-core --block machine`
([`synthesis.json`](synthesis.json)). The flow is the
[M2 record's](../2026-10-26-pc386-m2-vga/summary.md#machine-only-synthesis):
`synth_intel_alm -family cyclonev -nolutram` inside the stimulus/signature
envelope, then nextpnr `--pack-only`. "ALMs at least" is half the packed LUTs.

| Block | Packed LUTs | ALMs at least | FFs | M10K | DSP | M2 (LUTs / ALMs / FFs / M10K) | Budget (ALMs / M10K / DSP) |
| --- | ---: | ---: | ---: | ---: | --- | --- | --- |
| AT core: PICs, PIT, DMA, ports, 8042, RTC, ATA, I/O decoder | 7,570 | 3,785 | 3,344 | 4 | 1 + 2 (27 × 27) | 33,053 / 16,526 / 14,510 / 0 | 4,800 plus the I/O decoder's share of 900 / 6 / 0 |
| Machine (AT core, VGA, presenter, port 2, plane) | 14,511 | 7,255 | 7,720 | 38 | 12 × 9 × 9, 4 × 27 × 27 | 40,034 / 20,017 / 18,886 / 34 | 16,100 / 142 / 16 for the whole machine, audio and endpoint included |

The M2 blocks are unchanged; their rows in the M2 record stand. Unflattened
per-module figures from the same Yosys, as a guide:

| Module | M2 LUTs | Now | Row (ALMs / M10K) |
| --- | ---: | --- | --- |
| ATA and dirty map | about 24,400 | about 1,450 LUTs (about 730 ALMs) and 3 M10K | 900 / 3 |
| RTC and CMOS | about 3,000 | about 900 LUTs (about 460 ALMs) and 1 M10K | 500 / 1 |
| 8042, keyboard, mouse, FIFOs | about 1,450 | unchanged, about 720 ALMs | 1,100 / 2 |
| DMA, both controllers | about 1,440 | unchanged, about 720 ALMs | 1,000 / 0 |

## Not covered

- A routed result or timing.
- The machine beside the CPU in the device.
- Kit hardware.
- The M5 endpoint's DiskDirty reads, which must allow the map's two clocks.
