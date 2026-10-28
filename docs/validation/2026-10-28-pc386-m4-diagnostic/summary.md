# pc386 M4: the machine diagnostic shell, 28 October 2026

**The M4 shell, its bus master, its producer and its kit tool exist, and the
diagnostic programs pass on the host bench; the sealed build and the kit run
have not happened yet.** The producer's synthesis stopped at FES's compiler
read audit: the locked Yosys opens `/sys/devices/system/cpu/online` about
1.3 million times while synthesizing this shell, and its trace passed the
audit's 128 MiB bound (DeanoC/yosys#67). The fix is a one-line change to
Yosys; the shell is sealed and run on a kit once the pc386 lock can pin it.
This record is a **host-only check** plus diagnostic synthesis and packing.

## Inputs

- FES branch `deano/interesting-yalow-d6a1e4`, on main `463783281c`. The
  shell is [`cores/fes-pc386/`](../../../sources/misteross/cores/fes-pc386/README.md#m4-diagnostic-shell).
- Toolchain: [`cores/fes-pc386/toolchain.lock`](../../../sources/misteross/cores/fes-pc386/toolchain.lock),
  the i386 lock's pins (Yosys `238eac9a`, nextpnr `bd7a1c0d`, Mistral
  `8fcc4cb4`), HIP router. Verilator 5.032 for the benches.
- The machine as M1-M3 left it: the AT core with its memories in flip-flops
  (#1153, which moves them to M10K, is not in this build; the machine fits
  without it), the M2 VGA, presenter and plane, the M3 audio.

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
clock count: every isa8 cycle took 50 clocks at 61.44 MHz (720 ns is 44.2
clocks, plus the bus master's handshake), isa16 words 28, VGA port byte
writes 7 and word writes 10.

## Synthesis and packing

`synth_intel_alm -nolutram` of the whole shell on the locked Yosys (outside
the producer, 17 minutes) and `nextpnr-mistral` packing: 45,442 of 83,820
LUT positions (54%), 25,328 flip-flops (15%), 36 M10K, 18 DSP blocks (13
9x9, 4 27x27, one 18x18), two PLLs. The PLLs solve exactly: the system PLL
gives 61.439999857 MHz on output 0 and 12.287999971 MHz on output 1 (VCO
430.08 MHz), the video PLL 74.249999832 MHz.

## The seal and the read audit

`make build-fes-pc386-diag` failed before routing with `read audit exceeds
bounded trace size: file=134297263 total=143835646 bytes`. The audit traces
every file the compiler opens. On the presenter alone, 187,373 of the main
Yosys process's 189,007 trace lines open `/sys/devices/system/cpu/online`:
`ThreadPool::pool_size` calls `std::thread::hardware_concurrency()` on every
call, glibc reads that file each time, and `opt_clean` sizes a pool per pass,
2,273 passes here. The bound is a seal check and stays. The fix reads the
count once ([DeanoC/yosys#67](https://github.com/DeanoC/yosys/issues/67)); the
i386 lock has the same Yosys, so an audited i386 or M5 build meets the same
bound.

## Not done

- The seal, the routed timing of the 61.44 MHz system clock, and the kit run:
  HDMI captures of the VGA pictures, OPL2 and DSP audio, the port 2 counters
  and the image load time on hardware.
- The disk round trip with the runtime: it needs the M0 contracts
  (`fes.computer` 1.1). The shell serves the ATA image from the window, which
  the HPS writes directly.
- The plane was not opened on the bench, so its underflow counter proves
  nothing yet; the presenter, capture and scanout counters are exercised.
