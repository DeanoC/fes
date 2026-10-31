# DE25-Nano ZX81 diagnostic validation

This record covers an FPGA-only ZX81 BASIC machine with 16 KiB RAM, 720p HDMI,
KEY0 reset and KEY1 ENTER. It is a standalone SOF diagnostic with no HPS, DDR,
runtime transport, tape, expansion or audio source. No board or kit was programmed.

Scope base: `53dbe27886eb3fe750e45fa71ece89415c03f99f`. Compiled producer:
`1b40dc29379809e0d587fa9e8fcd8c4f16b33d99`. Current code and passing simulation:
`b5e8f7ca4b67c9b11685ce05f23e148b33e3e24b`.
[Source binding](source-binding.json) verifies that all 19 selected build inputs
remain byte-identical to the compiled producer. [Manifest](manifest.json) binds
the retained input, build, Quartus and simulation receipts.

The machine and renderer share the exact 74.25 MHz PLL output. The machine's
26/297 enable ratio preserves the 6.5 MHz ULA and 3.25 MHz CPU phases. Shared
RTL retains its original 1625/13056 defaults for existing 52.224 MHz consumers.
Explicit WAIT/NMI declarations repair Quartus Pro's handling of late net
initializers; the final compile contains neither the earlier redeclaration nor
undriven WAIT/NMI warnings. No shared protocol or package ABI changes.

## Simulation

Attempt 07 passes with the real machine, NMOS Z80, firmware and renderer:
178 complete 1650×750 HDMI frames, including four with both ink and paper;
KEY1 reaches the ROM keyboard scanner, and KEY0, PLL loss and configuration
reset recover with bounded reset release and real CPU instruction retirement.
The simulation binary takes 166.417 seconds. See [the passing log](simulation.log).
PLL, configuration and I2C boundaries are digital models; the RAM model is the
existing behavioral RTL. This does not prove physical PLL, M20K, I2C or HDMI behavior.

Earlier attempts exposed fixture problems: the initial working directory omitted
the ROM, and later picture/editor checks timed out before BASIC completed its
display-file expansion. A separate default-clock control passes BASIC and the normal
keyboard/tape-loader sequence, showing that D_FILE creation and HALT precede
editor readiness. Its log remains at
`out/de25-zx81-default-control/simulation.log` (copied as
[default-control.log](default-control.log)). [Attempt history](attempts.json)
identifies the retained failures and passing run. Failed attempt logs are retained;
they are not passing evidence.

## Quartus

Quartus Pro 26.3.0 Build 114 compiles the SCS device with zero errors and
41 warnings. SOF: 762,034 bytes, SHA-256
`9bd1f6dc7006af30790eb7cd7b26432b9b2a9c7325f33d5432ca8ab11e608a93`.
Retained artifact:
`out/de25-zx81-exact-clock/project/output_files/de25nano_zx81.sof`.
[Build receipt](build.json) and [report audit](quartus.json) retain exact identities.

All 17 multicorner summary rows have nonnegative slack and zero reported TNS.
These are worst-corner rows across the four analyzed models, not 17 rows per model.

| Analysis | Minimum slack (ns) |
| --- | ---: |
| Setup | 3.963 |
| Hold | 0.010 |
| Recovery | 12.580 |
| Removal | 0.024 |
| Minimum pulse width | 5.858 |

All seven reported clocks are constrained. Fitted resources are 2,286 ALMs,
1,033 registers, 458,752 memory bits and 140 M20Ks: RAM 8, ROM 4 and video 128.
There is one PLL, no HPS and no DSP. The fitted RAM table reports a single clock
for all memories; the video allocation rounds to 262,144×1 bits.

Overall Design Closure remains **FAIL**. The two failing Design Assistant classes
are missing output delay (29 violations) and missing input delay (four, including
the synchronized buttons). No violations are waived. STA separately reports two
unconstrained inputs, 30 outputs, nine paths from inputs and 3,787 paths to outputs.
All four found synchronizers are excluded from MTBF analysis, so no design MTBF
is calculated. Internal timing does not establish external IO or physical acceptance.

The retained ROM-depth warning concerns a 12 KiB MIF feeding a 16 KiB declaration.
The addressable ZX81 low 8 KiB matches the simulation HEX exactly, and the fitter
prunes the ROM to 8 KiB. The unreachable final 4 KiB differs: Quartus zero-fills
it while the HEX contains `0xff`; full 16 KiB equivalence is not claimed.
All prepared files remain unchanged except four standard Quartus QSF appends:
power-management format, its exponent, tool version and unused-pin behavior.
Removing those exact four lines restores the recorded prepared QSF hash.

Candidate `0c908ad444` was rejected for a rounded 52.222222 MHz machine clock
and undriven WAIT/NMI. Its compile was intentionally stopped; it provides no
accepted SOF. The final single-clock design preserves the machine frequencies
without accepting that rounding or relaxing timing constraints.

## Software checks and remaining acceptance

All eight planned software commands have passing coverage at code revision
`b5e8f7ca4b`: commands 0–5 completed locally, including 973 parent tests
(970 passed, three skipped) and generated consistency. Matching-tree CI run
[40771811312](https://github.com/DeanoC/fes/actions/runs/40771811312)
passes the FPGA producer suite (1,379 passed, 57 skipped) and the full
`make sim-fes-zx81` regression. Its checkout merge tree exactly equals the branch
code tree. The CI producer job skips the new board simulation because the required simulation tools
are unavailable; the separate passing local run supplies that evidence.

The duplicate local tail was deliberately retired after this coverage was
established. Its exit 143 is an intentional stop, not a test failure or a complete
local success. CI's own parent job was still running at the recorded observation;
no aggregate CI success is claimed. [Software checks](software-checks.json) records
the exact commands, skip counts, checkout identity and coverage split.
`make check` passes with 28 generated consumers, 37 fixture copies and no source-pin
copies. Shared definitions and wire/package ABIs are unchanged.

The [strict HIL plan](hil-plan.json) reports `OVERLAY core:ALL core:zx81` for the
shared ZX81 source changes. Its conservative consumer classification is retained;
no package installation or hardware result is inferred from these software checks.

Physical DE25 acceptance awaits the board. External HDMI timing, the vendor's
contradictory DJ24 clock-voltage documentation, M20K collision/startup behavior and
visible tearing remain open. The capture has no complete-frame ownership. Shared
RTL changes also retain the appropriate existing-consumer package/HIL gate.
This SOF is not a runtime package or a Linux boot image; serial, Ethernet and SSH
bring-up remain a separate target.
