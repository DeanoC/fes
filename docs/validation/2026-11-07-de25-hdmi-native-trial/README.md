# Bounded standard-package native operator

The independent operator now binds the [matched native chain](../2026-11-07-de25-hdmi-native-chain/README.md)
to one TRWU8CBL recovery/upload/native attempt. The package uses `fes.computer`
1.0 and `de25-native-computer-v1`; the application selects the native computer
constructor. This is preparation for a physical diagnostic, not full core-load,
HDMI, timing or native-network acceptance.

Native/firmware inputs: `137dc3e5f290d4be699c5369d90f6368a527fe90`.
Composition: `8f01ccb5c25464f524b9b9b8098e0571f139f155`.
FPGA producer: `6c6dd62b503f143448a65e17ba346e410187e960`.
Package: `03c8fa017f5fcfb40d23d54a449e8464d89fa9919dc6bae301fd8c833955290a`.
Build ID: `0792e51a2e51512bfb8fe44e93a69ddd`.

## Checked sequence

The operator requires the continuing claim, exact adapter and one-device JTAG
identity, free exclusive UART, clean source checkout and frozen input hashes.
After the reported power interruption, it must first boot the unchanged
qualified recovery and freshly admit the spare card, protected files, memory
ledger and Ethernet identity. Prior receipts cannot authorize a write.

One owned upload session writes eight fixed experimental files and freshly
reads each back. Factory boot files and the recovery prefix remain protected.
The new native chain then runs three serial Tenfoot sessions: ZX81 ROM,
`JP 0000` loop ROM, ZX81 ROM. The sequence checks distinct retained activation
receipts, observed fetch progress, SD reopening, USB reports and pause/resume,
firmware master-stop returns, and final owned Quit. It requires no physical
controller action and does not claim one.

The first physical failure stops outgoing commands. Exclusive attempt markers
prevent replay. Native failure collection remains receive-only for two seconds.
The original bounds remain: prompt 50 s, startup/launch 100 s, configuration
20 s, controls 5 s, native 450 s, native process 480 s, experiment 740 s and
host 850 s; console 1 MiB; USB drain 500 ms/4,096 steps. The 6 MHz programming
link must return to its original 15 MHz setting, with exact USB permissions,
owned private JTAG server shutdown and UART cleanup. The shared Quartus lock
is required for the physical command. The bottom kit is excluded.

## Off-board checks

Thirteen fixture/proof groups pass. These include 117 service-gate rejection
cases, 41 source/recovery mismatch rejections, 86 actual upload-runner cases,
357 upload-receipt cases, 17 one-shot device-admission cases and 25 CPU-data/
reservation rejections. Clock lifecycle/restore and fragmented boot/failure-tail
groups also pass. Models execute the real selected operator functions and
decoders; these checks perform no physical operations.

The complete typed native chain checker passes on the actual current files.
Source admission checks the committed chain record, exact native/firmware/
composition/FPGA identities, complete compiler-source closure, unchanged inputs
and permitted publication delta. Missing or conflicting sources reject. Nine
qualified safety helpers, including recovery, native execution, uploads,
private JTAG and service acceptance, are unchanged. All 11 selected DWC2 source
files match the previously qualified periodic-retirement build. Neither source
continuity nor an idle USB report proves a physical button or wire fault.

[operator-source.json](operator-source.json) freezes 38 helper sources, fixture
receipts and inputs. Raw evidence and operator files are in
`out/nano-hdmi-native-board-20261107/`. The checkout commit is bound separately
after this publication; the preflight records its exact USB node and all input
hashes without opening a device. Any changed checkout/helper/input requires a
fresh review and preflight, not a relaxed gate.

At publication, no UART, JTAG, programming or SD operation has occurred for
this candidate. The kit's CPU/FPGA/card state still requires live qualification.
The original factory card remains outside this work; restoration is waived and
the continuing claim is retained. Remaining full-system work includes physical
menu/core display, media, native networking and external timing/IO contracts.
