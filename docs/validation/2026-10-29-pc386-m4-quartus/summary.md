# pc386 M4 shell sealed with Quartus 17.0.2, 29 October 2026

**`fes.pc386-diag` seals with Quartus 17.0.2 at its 61.44 MHz system clock,
with every clock and timing analysis met, once the ATA's IDENTIFY capacity
comes from a register ([#1245](https://github.com/DeanoC/fes/pull/1245)).**
Without that change Quartus fits the system clock at 54.07 MHz; nextpnr routes
the same shell at 40.4 ([M4 record](../2026-10-28-pc386-m4-diagnostic/summary.md#routed-timing)).
The pc386 bring-up now takes its timing from Quartus, with nextpnr a separate
later project ([#1235](https://github.com/DeanoC/fes/issues/1235)). This is a
host build and timing record. The package has not been run on a kit.

## Inputs

- FES branch `deano/pc386-quartus` at `d80a92fddc`: the M4 shell of
  [#1226](https://github.com/DeanoC/fes/pull/1226) at `1df09954e0`, #1245
  merged in, and this lane.
- Quartus Prime Lite 17.0.2 (`quartus_sh` SHA-256 `71035cc1…`). The machine
  netlist comes from the pc386 lock's Yosys `1f7d0273`.
- Seed 1 for every fit below.

## How the lane builds the shell

[`scripts/build_fes_pc386_quartus.py`](../../../sources/misteross/scripts/build_fes_pc386_quartus.py)
(`make build-fes-pc386-diag-quartus`) takes the RTL, guest rates, core ID and
interfaces from the OSS producer. The locked Yosys elaborates the machine below
the board shell, `pc386_diag`, at the guest rates and writes it as Verilog.
Quartus reads that netlist and the shell's own files (`top.sv`, the PLLs, the
mailbox, audio output and HPS DDR) with `QUARTUS` defined. Each step answers a
failure Quartus gave without it:

| Step | Without it |
| --- | --- |
| Yosys elaborates `pc386_diag` | Quartus 17.0.2 rejects the OPL2 import's `signed'(...)` cast (`vibrato.sv`, error 10170) |
| Memories with more than one write port and no `ramstyle` become registers | Yosys writes `carrier_delay`'s reset clear and indexed write as two `always` blocks: "can't resolve multiple constant drivers" (error 10028) |
| Flattened names (`\machine.vga.line_data.memory[0][63]`) become plain identifiers | Analysis and Synthesis stops with an internal error, SGN `sgn_utility.cpp` line 4893, `dimension == 0` |
| Names escaped only as keywords (`\always_ff`), and strings, are kept | A rewritten `always_ff` attribute and enum strings no longer parse (error 10170) |
| Under `QUARTUS`, `top.sv` gives `pc386_diag` no parameters | The elaborated module has the rates built in: "cannot override parameters" (error 10111) |
| Under `QUARTUS`, the HDMI I2C pins come from the hard block | `MISTRAL_IO` is nextpnr's pad cell; RAM Tester's top does the same |

All the memories that become registers are register files of 32 words or
fewer (the DMA registers, CRTC, palette, keyboard FIFOs, `carrier_delay`).
Quartus would not infer RAM from them either, and the OSS lane makes them
registers too.

Two kinds of Quartus warning remain:
- **Eight wires are never assigned** (warning 10858). They are the
  out-of-range read entries of two arrays that became registers: the CRTC's 25
  registers behind a 5-bit index, whose read the RTL guards with
  `crtc_index <= 24`, and `carrier_delay`'s 3 entries, indexed only by
  channels 6 to 8. Quartus ties them to 0, and the RTL never reads them.
- **Two `ramstyle = "M10K"` memories are not inferred as M10K** (warning
  10999): the RTC RAM (64 x 16) and the VGA line buffer (16 x 64), whose
  byte-lane writes Quartus does not recognise in Yosys's form. They are about
  2,000 registers.

## Timing

Seed 1, worst-case Fmax at the slow 1100 mV 100 °C corner, and worst slack
over all corners:

| | System (61.44 MHz) | Pixel (74.25 MHz) | Audio (12.288 MHz) | Setup slack | Hold slack |
| --- | ---: | ---: | ---: | ---: | ---: |
| Before #1245 | 54.07 | 82.01 | 167.36 | −2.454 ns | 0.107 ns |
| With #1245, a scratch fit | 73.59 | 79.81 | 237.70 | 0.810 ns | 0.057 ns |
| The seal, build `8a665015…` | 75.82 | 81.31 | 220.46 | 1.098 ns | 0.070 ns |
| nextpnr `bd7a1c0d`, M4 record | 40.4 | 55.8 | | | |

The scratch fit and the seal have the same RTL; the build ID stamped into the
mailbox differs. In the seal, recovery, removal and minimum pulse width pass
too (6.769, 0.263 and 1.122 ns), as in every fit.

**Before #1245, 42 paths failed, all from one source.**
- They started at the ATA's logical geometry and ended at the AT decoder's
  read data or at the bus master's INTA vector, through 8 to 10 logic levels
  and 18.2 ns of data delay.
- The cause was IDENTIFY words 57 and 58, cylinders x heads x sectors per
  track: two multiplies inside the I/O read mux.
- #1245 registers the product, and the geometry changes only under BSY or on
  the first cycle after reset.
- The VGA scanout, presenter, OPL2 and bus master, which nextpnr misses, all
  meet timing on Quartus.

## The package

| | |
| --- | --- |
| Package | `ddf3530668b061fc368f6e5cd5e9947d6185f36ba42a4ae8879cee6796496ffe` |
| Build ID | `8a665015b07e3acd28010b028d31b39a` |
| RBF | 2,520,640 bytes, SHA-256 `a21be0a4…` |
| Manifest | `fes.pc386-diag` "FES pc386 M4 diagnostic (Quartus)", `fes.computer` 1.0, the OSS package's five interfaces |
| Resources | 9,700 ALMs (23%), 12,576 registers, 53 RAM blocks (151,955 bits), 21 DSP blocks |

[`seal-summary.json`](seal-summary.json) is the seal's build summary: timing,
clocks, guest rates, tool digests and every pinned input's hash.

## Checks

- `tests/test_build_fes_pc386_quartus.py`: 13 tests. They cover the source
  split, the Yosys script, identifier rewriting, the project, the timing gate
  on this shell's TimeQuest rows, the manifest and the build record.
- #1245's checks: the pc386 simulation lane and a FreeDOS boot identical clock
  for clock to the M2 record.
- `scripts/test_changed.py`: TC

## Not done

- A kit run of the package with the M4 kit tool's programs.
- The CPU in the shell (M5).
