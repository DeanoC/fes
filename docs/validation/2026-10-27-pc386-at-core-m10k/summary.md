# pc386 AT core memories in M10K, in simulation and synthesis, 27 October 2026

**The pc386 AT core fits its area budget: at least 3,788 ALMs and 4 M10K,
down from 16,526 ALMs and 0 M10K, and the machine boots FreeDOS on exactly
the clocks it took before.** Three memories move from flip-flops to M10K
with registered reads: the ATA sector buffer (128 × 64), the ATA dirty map
(128 × 16) and the CMOS (64 byte pairs). The FreeDOS boot report is identical
to main's, with its flip-flop memories, in every field. That covers
430,003,773 clocks, the prompt at clock 240,003,758, every I/O class count
and time, the VGA counts, the screen and the replay. The device checks, whose
dirty-map checks now cover two map words, and the benches pass. This is a
**host-only check** plus diagnostic synthesis: no route, package or kit.

## Inputs

- RTL at `c2bc07352b` on `deano/pc386-at-m10k`. That is main at
  `eea51e94e0` merged in, with M2 (PR
  [#1149](https://github.com/DeanoC/fes/pull/1149)) and M3 (PR
  [#1147](https://github.com/DeanoC/fes/pull/1147)). Every run here used that
  commit, and the synthesis summary's source digests are its files. The
  baseline for the boot is main at `eea51e94e0`, the flip-flop memories, run
  the same day with the same inputs. Issue
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
  over a map read that met a write, which M10K leaves undefined. A mark
  therefore reaches `dirty_word` four clocks after the store goes idle, where
  the flip-flop map showed it at once.
- **CMOS** ([`pc386_rtc.sv`](../../../sources/misteross/cores/fes-pc386/rtl/pc386_rtc.sv)):
  64 pairs × 16 bits with byte enables, 1 M10K. 71h reads the pair at the
  index, re-read every clock. The checksum reads one pair ahead, and 2Eh–2Fh
  is written as one pair. `busy` therefore falls on the same clock as before,
  and the release of the CPU with it. The memory has one write port, taken in
  this order: setup, then the guest, then the century byte. The century byte
  waits for a free clock and gives way to a guest write of 32h.
- Unchanged, measured inside their rows: the 8042 RAM and FIFOs, and the DMA
  register files in the shared `fes_dma8237` (pcxt uses it too).

Recorded deviations, all protocol errors no driver makes:

- A data-port read during a write command returns no defined data.
- A guest write to CMOS while `busy` is lost.
- A 71h read during the checksum, or on the last configuration clock,
  returns the checksum's pair, not the index's.

The Bochs BIOS, FreeDOS and the games reach neither CMOS deviation. `busy`
lasts 190 clocks after a machine reset, and POST's first CMOS access comes
518 clocks or more in. A CPU-only reset does not reset the RTC. The
testbenches release the CPU only after `busy` falls, and so will the
runtime that the endpoint phase adds.

## Device checks and benches

- `python3 scripts/sim_fes_pc386.py --suite devices`
  ([`devices-report.json`](devices-report.json)): all 77 checks pass. They
  include WRITE SECTORS to the image, READ SECTORS back, the CMOS
  configuration bytes and checksum, and the wall-clock century. The dirty
  map checks store sectors into three chunks over two words of the map
  (the test image is 2,720 sectors for that), watching `dirty_word` on
  every clock. A later mark keeps the word's earlier bits. A mark reaches
  `dirty_word` four clocks after the store goes idle and nothing else shows
  before it. A mark in another word leaves `dirty_word` steady. Each word
  reads out exactly two clocks after `dirty_index`. A machine reset reads
  the map as zero after the clear. The suite builds the RAMs with
  `PC386_RAM_COLLISION`, which poisons a read of the word being written, as
  M10K leaves it. So a design that used such a read fails here, though in
  plain simulation it would read the old word. Removing the read-modify-write's
  OR, either half of the `dirty_word` hold, the clear, or a clock of index
  latency each fails a check. A sweep that stopped short of word 64 would
  not: the test image reaches words 0 and 1 only.
- `--suite benches` ([`benches-report.json`](benches-report.json)): the
  presenter, capture and plane benches pass.

## FreeDOS boot

`python3 scripts/sim_fes_pc386.py --suite boot` ([`boot-report.json`](boot-report.json)):
`END prompt`, `dir` at `C:\>`. The run retired 103,831,426 instructions over
430,003,773 clocks (4.30 s). The VGA replay matched: 85,234 events with no
replay, read, VRAM or frame mismatch. Every field of the report equals that
of the same boot on main at `eea51e94e0`, the I/O timing classes and M3's
audio counts included. The fields M2 recorded also equal the
[M2 report](../2026-10-26-pc386-m2-vga/summary.md#freedos-boot), which
this record's first version matched before main was merged in.

## Machine-only synthesis

`python3 scripts/synth_fes_pc386.py --cache-root … --block at-core --block machine`
([`synthesis.json`](synthesis.json)). The flow is the
[M2 record's](../2026-10-26-pc386-m2-vga/summary.md#machine-only-synthesis):
`synth_intel_alm -family cyclonev -nolutram` inside the stimulus/signature
envelope, then nextpnr `--pack-only`. "ALMs at least" is half the packed LUTs.

| Block | Packed LUTs | ALMs at least | FFs | M10K | DSP | M2 (LUTs / ALMs / FFs / M10K) | Budget (ALMs / M10K / DSP) |
| --- | ---: | ---: | ---: | ---: | --- | --- | --- |
| AT core: PICs, PIT, DMA, ports, 8042, RTC, ATA, I/O decoder | 7,577 | 3,788 | 3,345 | 4 | 1 + 2 (27 × 27) | 33,053 / 16,526 / 14,510 / 0 | 4,800 plus the I/O decoder's share of 900 / 6 / 0 |
| Machine (AT core, VGA, presenter, port 2, plane) | 14,663 | 7,331 | 7,758 | 38 | 12 × 9 × 9, 1 × 18 × 18, 3 × 27 × 27 | 40,034 / 20,017 / 18,886 / 34 | 16,100 / 142 / 16 for the whole machine, audio and endpoint included |

Both rows include what main brought in since M2: M3's sound port decode in
the AT core, and the VGA scanout changes. On the branch before that merge
(`59c1713d6e`) the rows read 7,599 / 3,799 / 3,345 / 4 and
14,560 / 7,280 / 7,753 / 38. Unflattened per-module figures from the same
Yosys, as a guide:

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
- The M5 endpoint's DiskDirty reads. They must allow the map's two clocks
  after the index, and four clocks after the stores drain for the last mark.
  The AT core exposes no store-idle signal; the endpoint's freeze, which
  drains an accepted write, is where that wait belongs.
