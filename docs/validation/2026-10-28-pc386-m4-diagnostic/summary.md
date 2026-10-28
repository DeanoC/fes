# pc386 M4: the machine diagnostic shell, 28 October 2026

**The M4 shell, its bus master, its producer and its kit tool exist and pass
on the host bench, but the machine does not meet its clocks, so nothing is
sealed and no kit run happened.** This is the machine's first placement and
route. Two seeds routed to 40.4 and 38.6 MHz on the system clock (61.44 MHz
sealed; 56.65 MHz is the floor, twice the VGA's 28.322 MHz dot clock) and 55.8
and 52.3 MHz on the 74.25 MHz pixel clock. The misses are in M1 and M2 logic
that had never been timed: the AT core's I/O read path, the VGA scanout and
the presenter. Getting this far needed two toolchain-side fixes, recorded
below: a Yosys change so the producer's read audit fits its bound
(DeanoC/yosys#68, pinned), and #1153's M10K AT core so the shell places at
all. This record is a **host-only check** plus routed timing; there is no
hardware evidence.

## Inputs

- FES branch `deano/interesting-yalow-d6a1e4`, on main `463783281c`. The
  shell is [`cores/fes-pc386/`](../../../sources/misteross/cores/fes-pc386/README.md#m4-diagnostic-shell).
- Toolchain: [`cores/fes-pc386/toolchain.lock`](../../../sources/misteross/cores/fes-pc386/toolchain.lock):
  Yosys `1f7d0273` (DeanoC/yosys#68 on the i386 lock's `238eac9a`), nextpnr
  `bd7a1c0d`, Mistral `8fcc4cb4`, HIP router. Verilator 5.032 for the benches.
- The machine as M1-M3 left it, with #1153's AT core memories in M10K
  ([below](#placement-and-the-at-cores-memories)): the M2 VGA, presenter and
  plane, the M3 audio.

## What was built

`fes.pc386-diag` is a format-2 `fes.computer` 1.0 development package (fixed
720p60, 48 kHz audio, HPS DDR, the session-display plane). `pc386_bus_master`
stands in for the CPU: it takes commands from a ring in the HPS DDR window on
port 0 and issues each on the machine's I/O, INTA and MMIO ports. The 8237's
memory cycles and the ATA image port also use port 0. After each launch the
machine stays in reset until a command releases it. The system clock is
61.44 MHz: a multiple of the 12.288 MHz audio clock, above twice the
28.322 MHz dot clock the scanout needs, and every guest time base is an exact
rational enable of it (`scripts/pc386_diag.py` `rates()`).

The design's phase table says "mailbox-driven". The ring replaces the GP
mailbox for two reasons found in the runtime:

- A raw development RBF leaves the GP mailbox free, but the runtime then never
  brings up HDMI or audio and never releases the fpga2sdram ports, so VRAM,
  capture and the plane would have no DDR. The shell is therefore a package
  the runtime admits, brings up and releases (`/v1/development/core`).
- During a package session the GP mailbox is the runtime's: `FesGp::Exchange`
  toggles one request bit and poisons the link on a mismatch, so a second
  writer would break it, and `fes.computer` 1.0 has no opcode for arbitrary
  data.

The runtime does not enable the plane when it loads a session-display
package; it reads the plane's state after Start, and ENABLE comes only from a
host session-display request. Holding the machine, plane included, in reset
until the first command releases it therefore leaves the launch alone.

A diagnostic program is the command list plus what the host does between
commands: stage bytes into the window, mark a point, dump window bytes, dump
the newest capture. The same program file runs on the bench
(`cores/fes-pc386/sim/diag_tb.cpp`) and on a kit (`pc386-diag run`), and both
write the same results file.

## Yosys and the OPL2 import

The M3 audio had only been simulated. The locked Yosys could not read five
SystemVerilog forms the opl2_fpga import uses, nor one form in the mixer. The
import now carries local changes that keep the same registers, values and
widths ([its README](../../../sources/misteross/cores/fes-pc386/vendor/opl2_fpga/README.md)).
The opl2 suite (sample-exact against Nuked-OPL3), the audio suite and
`tests/test_pc386_machine.py` pass on them. Each form has a DeanoC/yosys
issue with a repro on `238eac9a`:

| Form | Issue | Where |
| --- | --- | --- |
| Initializer on an anonymous enum or packed struct variable | [#61](https://github.com/DeanoC/yosys/issues/61) | `channels.sv`, `mem_single_bank_reset.sv` |
| `'{default: v}` on a packed 2-D array | [#62](https://github.com/DeanoC/yosys/issues/62) | `pipeline_sr.sv` |
| Localparam with a packed 2-D type | [#63](https://github.com/DeanoC/yosys/issues/63) | `calc_envelope_shift.sv` |
| `$bits` of a type or a part select | [#64](https://github.com/DeanoC/yosys/issues/64) | `phase_generator.sv`, `calc_rhythm_phase.sv`, `envelope_generator.sv`, `control_operators.sv` |
| Unary minus before a size cast | [#65](https://github.com/DeanoC/yosys/issues/65) | `pc386_mixer.sv` |
| Concurrent assertion | [#66](https://github.com/DeanoC/yosys/issues/66) | `control_operators.sv` |

## Host-only checks

`python3 scripts/sim_fes_pc386.py --suite diag` runs `pc386_diag` (the
machine, its audio and the bus master) with the three DDR ports on the
window model and Kit B's measured read latency, and a host that runs each
program through the ring as the kit tool does
([`bench-report.json`](bench-report.json)). Every command's status and every
expected value pass:

| Program | Commands | Simulated | What it checks |
| --- | ---: | ---: | --- |
| smoke | 899 | 1 ms | POST code, both PIC masks, a CMOS byte, 8254 read-back status, an unclaimed port reads FFh, the counters at zero |
| mode13 | 872 | 110 ms | Mode 13h set from registers, a staged picture copied into VRAM, read back through the VGA (MMIO and a 16,000-dword copy out), captured double-scanned through one injective palette map; ![bench capture](bench-mode13.png) |
| sound (shortened) | 950 | 20 ms | DSP reset and version 2.01, single-cycle and auto-init DMA from guest RAM in the window, IRQ 7 as vector 0Fh, every DMA byte acknowledged |
| ata | 1,916 | 2 ms | An 8 MiB image staged in the window; IDENTIFY geometry; READ SECTORS equal to the image; WRITE SECTORS lands in the image and nowhere else; the chunk's dirty bit |
| vga-trace | 366,931 | 3.63 s | The vga suite's whole VGA trace (VGA BIOS text, fonts, mode 13h, palette cycling, mode Y flips, split and fade, mode 12h) replayed through the ring, every recorded read checked |

The vga-trace program is the vga suite's 13.48 million trace events with
retrace polling turned into polled reads (POLL_IN on the bit that changed) and
runs of dword writes into fills and copies. It ran on the bench before the
bus master's epoch change, which does not touch the VGA path. Its eleven
captures equal the vga suite's own captures of the same pictures, except in
the blinking cells of the three text pictures (486, 486 and 144 pixels; the
blink phase counts frames since reset, which differ). Every graphics picture
is identical.

I/O completion is fixed by the timing classes and shows in each result's
clock count: every isa8 cycle took 49 clocks at 61.44 MHz (720 ns is 44.2
clocks, plus the bus master's handshake), isa16 words 27, VGA port byte
writes 6 and word writes 9. The bench ran the vga-trace program before the
bus master's timing change below; the VGA path did not change.

## Synthesis and packing

Before #1153, with the AT core's memories in flip-flops,
`synth_intel_alm -nolutram` of the whole shell (outside the producer, 17
minutes) and `nextpnr-mistral` packing gave 45,442 of 83,820
LUT positions (54%), 25,328 flip-flops (15%), 36 M10K, 18 DSP blocks (13
9x9, 4 27x27, one 18x18), two PLLs. The PLLs solve exactly: the system PLL
gives 61.439999857 MHz on output 0 and 12.287999971 MHz on output 1 (VCO
430.08 MHz), the video PLL 74.249999832 MHz.

## The seal and the read audit

The first `make build-fes-pc386-diag` failed before routing with `read audit
exceeds bounded trace size: file=134297263 total=143835646 bytes`. The audit
traces every file the compiler opens. On the presenter alone, 187,373 of the
main Yosys process's 189,007 trace lines opened
`/sys/devices/system/cpu/online`: `ThreadPool::pool_size` called
`std::thread::hardware_concurrency()` on every call, glibc reads that file
each time, and `opt_clean` sizes a pool per pass, 2,273 passes here. The
bound is a seal check and stays.
[DeanoC/yosys#68](https://github.com/DeanoC/yosys/pull/68) reads the count
once ([#67](https://github.com/DeanoC/yosys/issues/67)); synthesis results
do not change. With it the presenter's main trace is 1,635 lines (315 KB) and
opens the file once. The pc386 lock pins #68's head `1f7d0273` until it
merges. The i386 lock still has `238eac9a`, so an audited i386 or M5 build
needs the same move.

## Placement, and the AT core's memories

No placement of the machine existed before this phase; M2 only packed it.
The first placement of the whole shell, with the AT core's memories in
flip-flops, ran 52 CPU minutes (one thread, Powerboat load average 20-58)
without finishing the placer's first iteration, and was stopped. Placing M2's
block netlists on their own measured the cause:

| Block | LUTs | FFs | First placer iteration |
| --- | ---: | ---: | ---: |
| AT core, memories in flip-flops | 33,053 | 14,510 | 840 s |
| VGA | 3,783 | 2,251 | 1 s |
| i386 CPU spike (another session, four threads), for scale | 19,200 | | 95 s |

Every word of the AT core's register memories has its own write enable, so
the placer must legalise thousands of distinct control sets. #1153 moves the
ATA sector buffer, the dirty map and CMOS into M10K (AT core 7,599 LUTs and
3,345 flip-flops). The machine fits without it but does not place in useful
time, so this branch includes #1153, merged with main: every M2 file takes
main's squashed version and #1153's own four commits apply on top. Its device
suite passes on that merge, and the diag suite passes on this branch with it.

## Routed timing

The second seal attempt (Yosys `1f7d0273`, #1153 in) synthesized under the
audit and placed in a minute: 20,168 LUT positions (24%), 14,157 flip-flops
(8%), 40 M10K, 18 DSP blocks. Its routes miss both fixed clocks after the
analogue signoff repair (the figures below are signoff figures). The producer
then tries its remaining seeds, which cannot close gaps this size; it fails
on its own and publishes nothing. Seed 1, failing setup endpoints at
61.44 MHz and 74.25 MHz ([`routed-timing.json`](routed-timing.json)):

| Block | Clock | Failing endpoints | Worst slack | About |
| --- | --- | ---: | ---: | ---: |
| AT core: I/O decoder, ATA, RTC | system | 295 | -8.45 ns | 40 MHz |
| INTA vector from the PICs | system | 8 | -7.33 ns | 42 MHz |
| VGA scanout (`f_expected`, `f_received`, `f_columns`, slot state) | system | 244 | -3.87 ns | 49 MHz |
| Bus master (before the change below) | system | 237 | -2.99 ns | 52 MHz |
| OPL2 | system | 5 | -0.43 ns | 60 MHz |
| Presenter weights (`wx`, `wy`, `h_a`) | pixel | 94 | -4.47 ns | 56 MHz |

The worst system path runs from the I/O decoder's `cycle_port` through the
ATA's register select and the AT core's read-data mux into the decoder's
`rdata` register, 28 cells. `pc386_io` holds the port stable for 18 phases
and samples at phase 8, so the read path has cycles to spare; the catch is
reads with side effects, which must return the value before the access. The
worst pixel path runs through the presenter's weight computation and a 27 x 27
multiplier, 74 cells. The pixel clock is fixed by 720p and the system clock
cannot go below 56.65 MHz, so each block needs RTL work, not a lower clock or
another seed.

The bus master's own misses are fixed on this branch. Its fetched command is
now compared with a registered sequence number a clock before the fields
load, waits count down instead of comparing `clock - started`, each command's
clocks count up, the READ word is selected a clock early, and POLL_IN compares
in its own step. Synthesized and placed alone in the M2 envelope, the old bus
master estimates 75.7 MHz and the new one 105.8 MHz. The diag suite passes on
it.

## Not done

- Timing closure of the AT core's read path, the VGA scanout and the
  presenter; then the seal and the kit run this branch prepares (HDMI captures
  of the VGA pictures, OPL2 and DSP audio, the port 2 counters and the image
  load time on hardware). Kit B was claimed once, for a read-only probe of the
  DDR window through `/dev/mem`, and released unchanged.
- The disk round trip with the runtime: it needs the M0 contracts
  (`fes.computer` 1.1, [#1220](https://github.com/DeanoC/fes/pull/1220)). The
  shell serves the ATA image from the window offset the runtime will write
  (0x08000000), which the HPS writes directly for now. A 1.1 shell needs its
  own mailbox path: ABI minor 1, identity word 16 with capability bits 16-19,
  and the disk opcodes; the shared 1.0 mailbox stays minor 0.
- The plane was not opened on the bench, so its underflow counter proves
  nothing yet; the presenter, capture and scanout counters are exercised.
