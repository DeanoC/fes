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

At operator publication, no UART, JTAG, programming or SD operation had occurred
for this candidate. The physical result below consumes that operator's single
attempt. The original factory card remains outside this work; restoration is
waived and the continuing claim is retained. Remaining full-system work includes
physical menu/core display, media, native networking and external timing/IO
contracts.

## Physical result: stopped before native programming

Operator publication: `455554cebbf9eb63cd411a76c033c2f7bd52c51f`.
The actual read-only preflight passed in 76.859 s. Exact USB identity, the
one-device `4362C0DD` JTAG gate and the qualified recovery boot passed after the
power interruption. The one physical command stopped in 140.134 s, within its
unchanged 850 s bound. It was not replayed.

Four fixed files were individually written and read back successfully:
`FES/PKGTEST.BIN`, `FES/FIRMWARE.BIN`, `FES/PROGRAM.JSN` and `FES/FIRMALT.BIN`.
Each checked the protected prefix, recovery FIT and manifest. The fifth file,
alternate metadata for `FES/PROGALT.JSN`, reported all 3,263 bytes received into
RAM, then produced neither its fresh command-result marker nor another prompt.
The original 90 s transfer-command deadline expired. That file reached no SD
write, and selection, UI and boot uploads never started. The complete eight-file
group was not accepted and the final fresh-readback phase did not run. The spare
therefore contains a partial experimental catalog; it must not qualify native
boot from this attempt.

Native SOF programming, `fesboot`, application startup and launch/return were
never attempted. The new ordinary package remains hardware-unqualified. This
failure cannot be attributed to its FPGA endpoint, native constructor or CPU
handoff because those paths were not reached.

Cleanup independently passed: original 15 MHz JTAG clock read back, exact USB
permissions restored, owned private JTAG server gone and UART free. The bounded
console-park attempt also timed out. The recovery prompt is **not confirmed**;
CPU state remains unqualified. No automatic reset, retry or factory restore
followed. The continuing claim and spare card stay with this work, and the
bottom kit is untouched.

The retained source places the last message in `net_loop`'s success branch.
Environment updates, Ethernet/XGMAC/PHY shutdown, later net-loop bookkeeping,
netboot environment/autostart handling and interpreter return remain possible
stopping points. Existing XGMAC drain loops are already time-bounded in source;
the trace does not prove which call stalled or establish a fault PC. Linked
symbol addresses are recorded for follow-up, not treated as runtime PCs.

Next: add bounded before/after observations to that recovery return path,
validate the original calls and limits off-board, then build matched new recovery
inputs and a safe delivery plan. The consumed operator cannot authorize another
attempt. No transfer timeout, card/ownership gate or native admission policy was
relaxed. See [result.json](result.json) and the retained raw evidence under
`out/nano-hdmi-native-board-20261107/`.
