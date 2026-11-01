# DE25 contained phase-two timing diagnostic, 2026-11-01

**STOPPED** on TRWU8CBL, native source `9ce1af2a6e65426a759d7240722845a425d820b3` and matched boot
`12c8c5878f222658`. The bounded attempt stopped at `configuration_poll`. The first raw trace is retained without a retry. A partial transfer or stalled identity access does not establish configuration completion or bridge reclosure. Runtime core acceptance remains unavailable. Native code raised `late FPGA firmware return phase=completion` after 20.121780 seconds, with 720,896 bytes completed, another 65,536-byte buffer outstanding, and all 2,027,520 source bytes retained. No identity or bridge-opening marker followed. There was no retry.

The [independent saved-trace audit](hardware.json) verifies the source, raw
trace, exact original 15 MHz JTAG clock, USB permissions and free UART.
The diagnostic, spare card and exclusive top-board claim remain retained under
Deano's instruction; the factory card remains outside the board.

## Timing observations

The [saved timing analysis](timing-analysis.json) separates architectural
counter time from cumulative secure SHA-384 work. The whole native deadline
remains 20 seconds. No completion after that deadline can authorize identity
or activation. The [first untimed stop](../2026-11-01-de25-contained-configuration/README.md)
and its raw trace remain retained separately. These measurements locate cost;
they do not establish physical configuration or runtime admission.

| Measurement | Seconds |
| --- | ---: |
| Native late return | 20.121780 |
| Cumulative secure SHA-384 work at the last marker | 16.837575 |
| First whole-image secure digest | 3.216320 |
| Time between first and last secure markers | 15.800850 |
| Secure digest work between those markers | 13.621255 |

Secure digest work observed before the last marker accounts for at least
83.68% of the native deadline interval. The final synchronous call also did
untraced work, so 16.837575 seconds is a lower bound. No clock/digest-accounting
error was found. The firmware keeps EL3 translation and data cache disabled;
no cache mode, hash check or deadline was changed in this experiment.

Next compare a narrowly scoped digest compiler optimization using the same
pinned algorithm, retaining every whole-image/chunk check and the first failed
candidate. Compilation or host digest agreement alone will not establish a
physical speedup.


## Exact inputs and requested scope

- Sealed package: `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63`.
- FPGA build ID: `e25bb65a1d14a2bbbddf4c770ea033e5`.
- Core: 2,027,520 bytes, SHA256 `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c`.
- FIT: 7,326,964 bytes, SHA256 `7a2e67852d114f4628fdd70e31b19ce780521845de3ea2626b2ce441b102af6a`.
- Fixed uncached configuration arena: `0x94800000..0x95000000`, outside Go,
  FIT, entry and U-Boot memory; 65,536-byte chunks with a short final chunk.
- One CPU; secondary CPU release remains locked. No native SD writes,
  data-bridge release, runtime admission or activation receipt was requested.

The qualified recovery performs one fixed `FES/UPLOAD.BIN` overwrite after
checking the board/card and protected recovery files, then independently reads
and compares it. This transport result is separate from native execution.
The secure configuration gate admits only one cold attempt, the fixed arena,
exact compiled bytes and unchanged closed ownership. It uses the pinned vendor
SHA-384 implementation and the existing contained four-call transfer engine.
It keeps ambiguous buffers until reboot and never opens data paths.

The [serial trace](native-console.log), [native receipt](native.json),
[upload receipt](upload.json) and [enclosing attempt](experiment.json) preserve
the observation boundaries. The prior [phase-one-only status](../2026-11-01-de25-fpga-ready/README.md)
and earlier [stopped identity access](../2026-11-01-de25-cpu-fpga-identity/README.md)
remain independent evidence; no reset polarity was changed.

## Software and retention

All 10 [affected software phases](software.json) passed on the exact source,
including 1256 parent tests and 1576 FPGA host tests.
The actual cold provider, dispatcher and pinned digest implementation ran with
injected hardware responses, including ownership, mutation and uncertain
completion failures. The [linked-firmware audit](linked-configuration.json)
checks seven required strong configuration/digest symbols and the enabled
fixed-source profile. Native command and cleanup fixtures cover 55 cases.
These checks are software evidence, separate from the hardware outcome above.

The sealed-package model still rejects this diagnostic target through the
existing production runtime (`unsupported_target`). This source-bound chain is
not signed/measured boot. Active master drain, runtime package admission,
repeatable launch/return, real emulator behavior, display and input acceptance
remain open.

[Retained builds](build-retention.json) contain 43,680 members,
each independently read back and compared. Archive SHA256:
`2fda8fcc333959103213b645228b7687102e6a67a27c95a258a33797fdc9c351`. The incremental source bundle was fetched and checked
against the documented prior shallow source-retention chain; it is not a
standalone complete-history backup. No claim credential is published.
