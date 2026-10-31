# pc386 M4 shell sealed with Quartus 17.0.2, 29 October 2026

**`fes.pc386-diag` seals with Quartus 17.0.2 at its 61.44 MHz system clock,
with every clock and timing analysis met, once the ATA's IDENTIFY capacity
comes from a register ([#1245](https://github.com/DeanoC/fes/pull/1245)).**
Without that change Quartus fits the system clock at 54.07 MHz; nextpnr routes
the same shell at 40.4 ([M4 record](../2026-10-28-pc386-m4-diagnostic/summary.md#routed-timing)).
The pc386 bring-up now takes its timing from Quartus, with nextpnr a separate
later project ([#1235](https://github.com/DeanoC/fes/issues/1235)). This is the
build and timing record. The lane sealed the shell four times as M4 changed
it. The latest seal, with #1226's review fixes, passed every M4 diagnostic
program and 201 machine resets on Kit B, its I/O clocks equal to the bench's
command for command ([run q4](../2026-10-28-pc386-m4-diagnostic/summary.md#kit-run-of-the-review-fixes-29-october-2026)).

## Inputs

- FES branch `deano/pc386-quartus` with #1245 merged in, for each seal:

  | Seal | Revision | M4 shell of [#1226](https://github.com/DeanoC/fes/pull/1226) |
  | --- | --- | --- |
  | First | `d80a92fddc` | `1df09954e0` |
  | 720p timing reset | `7e5da17459` | `605ff7b09f`: the presenter's 720p timing follows the pixel PLL's lock alone |
  | Port fence | `d2f2565474` | `e920b2f90a`: ports 1 and 2 fenced on the bus master's machine reset |
  | Review fixes | `e10811e575` | `6ba525cf7a`: the review's bus master, fence, counter and tool fixes, with #1153 at `962f7cd0c0` |

- Quartus Prime Lite 17.0.2 (`quartus_sh` SHA-256 `71035cc1…`). The machine
  netlist comes from the pc386 lock's Yosys: `1f7d0273` (DeanoC/yosys#68's
  head) for the first three seals, and `0a7927bf`, the squash merge of #68 on
  Yosys main, for the review fixes.
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
| First seal, build `8a665015…` | 75.82 | 81.31 | 220.46 | 1.098 ns | 0.070 ns |
| 720p timing reset, build `9571d035…` | 76.01 | 84.65 | 191.98 | 1.559 ns | 0.094 ns |
| Port fence, build `f1d64e7d…` | 74.26 | 80.03 | 186.05 | 0.756 ns | 0.097 ns |
| Review fixes, build `fa9fcda1…` | 77.62 | 81.77 | 158.63 | 1.233 ns | 0.044 ns |
| nextpnr `bd7a1c0d`, M4 record | 40.4 | 55.8 | | | |

The scratch fit and the first seal have the same RTL; the build ID stamped
into the mailbox differs. Recovery, removal and minimum pulse width pass in
every fit too (6.694, 0.274 and 1.122 ns in the review fixes seal).

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
The review fixes seal:

| | |
| --- | --- |
| Package | `48fd1d4a420ac564f815720bf580ec44de6134d587bffe575b9569a205b32f4a` |
| Build ID | `fa9fcda12ddccefa8675c795d4168290` |
| RBF | 2,520,204 bytes, SHA-256 `19b717f5…` |
| Manifest | `fes.pc386-diag` "FES pc386 M4 diagnostic (Quartus)", `fes.computer` 1.0, the OSS package's five interfaces |
| Resources | 9,876 ALMs (24%), 13,131 registers, 53 RAM blocks (151,955 bits), 21 DSP blocks |

[`seal-summary.json`](seal-summary.json) is its build summary: timing,
clocks, guest rates, tool digests and every pinned input's hash. The earlier
seals were packages `ddf35306…` (build `8a665015…`), `42cb45ef…` (build
`9571d035…`) and `fc9c0dd0…` (build `f1d64e7d…`).

## On Kit B

M4's record has each run. All used the same Kit B baseline and M4's
diagnostic programs:

| Run | Seal | Result |
| --- | --- | --- |
| [q1](../2026-10-28-pc386-m4-diagnostic/summary.md#hardware-diagnostic-29-october-2026) | First | The six programs pass every command. HDMI re-locks for about 1.5 s after each machine reset |
| [q2](../2026-10-28-pc386-m4-diagnostic/summary.md#kit-re-run-with-the-timing-change-29-october-2026) | 720p timing reset | HDMI and audio hold through every reset. opl2 fails: a machine reset cut a port 2 write burst, and port 2 stopped serving the presenter |
| [q3](../2026-10-28-pc386-m4-diagnostic/summary.md#kit-run-with-the-port-fence-29-october-2026) | Port fence | The six programs and `resets` pass every command. After 201 machine resets the mode 13h capture is whole and every counter reads zero |
| [q4](../2026-10-28-pc386-m4-diagnostic/summary.md#kit-run-of-the-review-fixes-29-october-2026) | Review fixes | All eight programs pass every command and the checks the reviews added, including `copy`'s refused commands and timeouts. Every I/O command's clocks and every dump equal the bench's, and every counter reads zero after 201 resets |

## Checks

- `tests/test_build_fes_pc386_quartus.py`: 16 tests. They cover the source
  split, the Yosys script, identifier rewriting, the project, the timing gate
  on this shell's TimeQuest rows, the manifest and the build record. The gate
  also fails a slack or TNS printed as `-0.000`, and an illegal or
  unconstrained clock, whose registers no slack row covers.
- #1245's checks: the pc386 simulation lane and a FreeDOS boot identical clock
  for clock to the M2 record.
- `scripts/test_changed.py` against #1245's branch, at `e10811e575` (the
  review fixes seal's revision): 31 commands pass, in the parent and FPGA
  lanes, among them every core's simulation. Only docs changed after it.

## Not done

- The CPU in the shell (M5).
