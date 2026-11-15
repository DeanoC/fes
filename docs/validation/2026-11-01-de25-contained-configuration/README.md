# DE25 contained phase-two configuration, 2026-11-01

**STOPPED** on TRWU8CBL, native source `283d91acfd62f29643107064d1044624ab6b3385` and matched boot
`6bf7a619b72b8bca`. The bounded attempt stopped at `configuration_poll`. The first raw trace is retained without a retry. A partial transfer or stalled identity access does not establish configuration completion or bridge reclosure. Runtime core acceptance remains unavailable. The native code raised `panic: late FPGA firmware return` at its fixed 20-second deadline. The trace confirms 786,432 bytes completed and a further 65,536-byte buffer issued. No identity probe or bridge-opening marker followed. The last outstanding completion is unknown; the buffer stays retained until reboot. Existing records do not separate digest work from firmware transport latency; see the [failure analysis](failure-analysis.json).

The [independent saved-trace audit](hardware.json) verifies the source, raw
trace, exact original 15 MHz JTAG clock, USB permissions and free UART.
The diagnostic, spare card and exclusive top-board claim remain retained under
Deano's instruction; the factory card remains outside the board.

## Exact inputs and requested scope

- Sealed package: `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63`.
- FPGA build ID: `e25bb65a1d14a2bbbddf4c770ea033e5`.
- Core: 2,027,520 bytes, SHA256 `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c`.
- FIT: 7,326,676 bytes, SHA256 `0527f520628b6185d24e2101199d1d1ca3489b65dc1e7b9ee123e4c01439cf38`.
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

All 12 [affected software phases](software.json) passed on the exact source,
including 1255 parent tests and 1576 FPGA host tests.
The actual cold provider, dispatcher and pinned digest implementation ran with
injected hardware responses, including ownership, mutation and uncertain
completion failures. The [linked-firmware audit](linked-configuration.json)
checks seven required strong configuration/digest symbols and the enabled
fixed-source profile. Native command and cleanup fixtures cover 51 cases.
These checks are software evidence, separate from the hardware outcome above.

The sealed-package model still rejects this diagnostic target through the
existing production runtime (`unsupported_target`). This source-bound chain is
not signed/measured boot. Active master drain, runtime package admission,
repeatable launch/return, real emulator behavior, display and input acceptance
remain open.

[Retained builds](build-retention.json) contain 43,680 members,
each independently read back and compared. Archive SHA256:
`ff48202d270b7433aca4a54cfe56c8c347e6d8c577af750e2945af5f41b2f2d3`. The incremental source bundle was fetched and checked
against the documented prior shallow source-retention chain; it is not a
standalone complete-history backup. No claim credential is published.
