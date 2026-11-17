# DE25 contained load with scalar digest specialization, 2026-11-01

**STOPPED** on TRWU8CBL, native source `9237c375a7f20a1eb41ad60888461215fae4eb2b` and matched boot
`616ee4ddb494b9da`. The bounded attempt stopped at `configuration_poll`. The first raw trace is retained without a retry. A partial transfer or stalled identity access does not establish configuration completion or bridge reclosure. Runtime core acceptance remains unavailable. Native code raised `late FPGA firmware return phase=completion` after 20.084653 seconds, with 786,432 completed bytes, another 65,536-byte buffer outstanding and all 2,027,520 source bytes retained. No identity or bridge-opening marker followed, and there was no retry.

The [independent saved-trace audit](hardware.json) verifies the source, raw
trace, exact original 15 MHz JTAG clock, USB permissions and free UART.
The diagnostic, spare card and exclusive top-board claim remain retained under
Deano's instruction; the factory card remains outside the board.

## Scalar digest and timing observations

The cold profile specializes the exact pinned scalar SHA-384 source with
three GCC unrolling hints and `-O3` on `sha.o` alone. Removing the hints
reproduces the pinned source. The [linked-code audit](linked-configuration.json)
checks the generated source/policy and all 2,912 transform instruction words
against the independent off-board compilation. Cache/SIMD state, the algorithm
and every whole-image/chunk ownership check are unchanged.

The [saved timing analysis](timing-analysis.json) separates architectural
counter time from cumulative secure SHA-384 work. The whole native deadline
remains 20 seconds. No completion after that deadline can authorize identity
or activation. The [first untimed stop](../2026-11-01-de25-contained-configuration/README.md)
and its raw trace remain retained separately. The [instrumented baseline](../2026-11-01-de25-configuration-timing/README.md) measured at least 16.837575 seconds of secure digest work before its 20.121780-second stop. These measurements locate cost;
they do not establish physical configuration or runtime admission.

## Observed digest gain and completion stall

| Measurement | Value |
| --- | ---: |
| Previous first whole-image secure digest | 3.216320 s |
| Specialized first whole-image secure digest | 1.164858 s |
| First-digest speedup in this pair | 2.761x |
| Completed core prefix | 786,432 bytes |
| Same outstanding 65,536-byte buffer | 10.523879 s across 86 polls |
| Fixed native deadline / late return | 20 s / 20.084653 s |

The [stall analysis](stall-analysis.json) and raw trace show that the scalar
specialization reduces digest time but does not complete configuration. The
last buffer remains outstanding while repeated polls report no completion.
Its cause is not determined; no command-queue, bitstream or firmware failure
is inferred from BUSY alone. Next inspect the contained configuration engine
and pinned mailbox accounting, with injected transport failures and bounded
state observations. The earlier candidates remain retained.

## Exact inputs and requested scope

- Sealed package: `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63`.
- FPGA build ID: `e25bb65a1d14a2bbbddf4c770ea033e5`.
- Core: 2,027,520 bytes, SHA256 `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c`.
- FIT: 7,339,252 bytes, SHA256 `58a78d46c2c16986256fb9f3defe1a0c78e28b70ebb7bc924cc8908d8523baa4`.
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
including 1257 parent tests and 1576 FPGA host tests.
The actual cold provider, dispatcher and pinned digest implementation ran with
injected hardware responses, including ownership, mutation and uncertain
completion failures. The [linked-firmware audit](linked-configuration.json)
checks seven required strong configuration/digest symbols and the enabled
fixed-source profile. Native command and cleanup fixtures cover 55 cases. The specialized scalar
digest also matches 41 independent hashlib vectors, padding boundaries,
unaligned inputs and streaming splits, including a full core-sized input.
These checks are software evidence, separate from the hardware outcome above.

The sealed-package model still rejects this diagnostic target through the
existing production runtime (`unsupported_target`). This source-bound chain is
not signed/measured boot. Active master drain, runtime package admission,
repeatable launch/return, real emulator behavior, display and input acceptance
remain open.

[Retained builds](build-retention.json) contain 43,680 members,
each independently read back and compared. Archive SHA256:
`76483f1b28ec7227797d20443f92be8d53035929f2ab02a440373833e909b0d5`. The incremental source bundle was fetched and checked
against the documented prior shallow source-retention chain; it is not a
standalone complete-history backup. No claim credential is published.
