# Bounded SPL rejection and early EL3 RAM diagnostics

The corrected-wire image still stopped after SPL's final ATF descriptor in its
single physical attempt. Recovery, uploads/readbacks, programming and FIT
verification passed; no vendor secure startup appeared, and `fesboot` was
never sent. That trace does not distinguish SPL's final live exclusion from
the early EL3 guard. Preserve both failed images and their traces, with no retry.

This change adds evidence at that boundary. SPL retains the original consumed
allocation snapshot and its two results in its initialized data. Only its
failure branch prints these saved observations and parks. It never resamples,
retries or turns an earlier successful observation into another permission.
The successful final branch still requires the one-shot guard and has no
additional calls or stack writes afterwards.

The selected EL3 entry emits bounded ENTRY and PASS checkpoints and a rejection
phase: 1 arguments/context, 2 record fields, 3 CRC, 4 identity/owner, 5 linker
layout, 6 live containment. These messages use only the already configured
fixed SPL UART. Each message has a 384-byte budget; each ready/empty observation
has a 100,000-poll budget. All-ones or unusable UART status parks without a grant.
The assembly has no stack frame or function calls. Stores are limited to its
initialized DDR one-shot latch and the fixed UART data register. It does not
access candidate RAM, release a reset or enable a bus master.

Architecture checks now use immediate masks before any literal data read.
This matters for an unsupported big-endian EL3 entry: loading the rejection
mask from little-endian data before checking byte order could misread the mask.
Unsupported CPU state parks before the trace or latch access.

The native trace decoder validates checkpoint order around the checked ATF
observation and vendor capture. Its explicit `firmware_ram_trace=True` option
requires both checkpoints; Tenfoot forwards that selection before any App
input. Partial or absent checkpoints do not establish a boot. Rejection,
malformed, duplicated, out-of-order or late checkpoints fail closed. No
checkpoint supplies ownership or core-load acceptance independently of the
existing primary boot and live device proofs.

Focused host tests exercise cached first-failure observations, non-replay,
ordered decoding and the Tenfoot gate. Real AArch64 SPL translation units
compile against pinned headers; the guard remains a zero-frame leaf and the
64-byte caller's success branch has no extra calls or stack writes. These
objects are not themselves a linked boot chain.

The real completed C producer supplies 225 ARM control/corruption cases and
37 binding/live-state/replay/layout/CPU/UART fault cases in QEMU. Every rejection
phase is checked. Additional cases exercise big-endian, E0E, data-cache,
interrupt-mask and lower-EL entry, all-ones/stuck UART and an unterminated
message stopped at 384 bytes. These are modeled Nano registers and UART, not
hardware acceptance.

Validation is bound to implementation `f40a8acc496aaeeb6e69e4c8dc85e481efbaf420`:

- All ten required changed-input integration steps passed, including 1,510
  parent cases with two environment skips and 1,581 producer cases with 75
  tool/fixture skips. All 89 focused host cases passed. Seventeen early-entry
  constants compile against the actual C headers and reject deliberate drift.
- The real BL31 build and ELF/flat-binary audit passed. Its actual 1,428-byte
  early guard uses only the initialized DDR latch and five inlined UART store
  sites. All nine status-read sites and all data stores use the canonical UART
  addresses. Poll and message budgets are present in the machine code. CPU
  architecture checks precede its first literal data read; there are no guard
  stack writes or calls, and vendor initialization follows it.
- The matched SPL/U-Boot build passed. Both standalone translation-unit and
  final linked checks confirm the zero-byte guard frame, 64-byte caller and
  unchanged successful final branch. Only rejection calls the reporting sink.
- File-only Quartus composition ran under the shared lock. The 8,097,792-byte
  FIT remains under the unchanged 8 MiB limit, and the 1,658,880-byte sealed FPGA
  payload is unchanged. The new cold chain is
  `c0578d7ef93e00fdc8f19a03a3eac38bff63f6206e5e9f9d3d281911fb31025b`.

The [receipt](result.json) binds the source, tests, compiled objects, ELF,
branch/allocation and composition evidence under
`out/nano-firmware-entry-trace-20261106`. The retained native application/entry
from `31d716957759c3dede3330877dac2ce7bcd2d643` are reused byte for byte; their
complete selected source and artifact bindings remain checked. They were not
rebuilt or attributed to this firmware commit. The first private ELF audit
failure is preserved: it mistakenly treated a linker-boundary literal as a
UART address. The completed audit verifies the effective addresses of actual
UART reads/stores and the traced rejection branch.

No board operation followed the preceding corrected-wire stop. This diagnostic
image has zero hardware attempts. Prepare a fresh bounded procedure that
explicitly requires `firmware_ram_trace=True` in both boot decoding and Tenfoot
before further input. Keep the existing limits and stop at the first failure.
The continuing claim and spare remain retained, with the factory card and other
kit untouched. Physical entry, ROM completion/Run/return, normal package loading
and DE10-equivalent readiness remain unverified for this image.
