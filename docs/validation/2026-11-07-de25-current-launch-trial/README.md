# Bounded refreshed native launch and return test

This operator consumes the [refreshed native artifacts](../2026-11-07-de25-current-launch-chain/README.md)
and the [physically verified corrected recovery](../2026-11-07-de25-recovery-console-repair-hardware/README.md).
The original SDK/entry remain bound to source `137dc3e5f290d4be699c5369d90f6368a527fe90`;
secure firmware/U-Boot were rebuilt from source `36eeaa8b8ff9f5573fe1f4807df5022e2be67ea5`.
This freeze records preparation, not a hardware result. All prior consumed
operators remain terminal.

One fresh attempt performs a corrected recovery boot, eight fixed SD uploads
and eight fresh readbacks in one owned trace-aware session, followed by the
matched native HPS-first boot. Native programming requires all eight valid,
complete 34-marker transfer traces and the accepted file/card/boot protection
receipts. The ordinary `fes.computer` package and two retained ROM choices
are exercised through the real serial Tenfoot App in three request-driven
launch/return cycles: ZX81 ROM, JP0000 loop, ZX81 ROM. The final Quit must
complete owned master stop and return. No physical controller press is needed
or claimed; shared USB gamepad reports must still be observed.

Prompt/startup/launch/configuration/control/native/process/outer/host bounds
remain 50/100/100/20/5/450/480/740/850 seconds, with the original bounded console
and two-second passive failure tail. First failure stops all subsequent
commands and phases. No replay, retry, fallback, reset recovery, factory-card
write or automatic rollback is supplied. The continuing top-kit claim and
shared JTAG lock are mandatory. Exact top adapter/device and 6 MHz are required
before programming; exit must restore 15 MHz, exact USB permissions, stop only
the owned private JTAG service and close UART/TFTP resources.

The native service, CPU-data/reservation gate, private transport, clock policy,
recovery boot helper, service result gate and upload receipt gate are unchanged
qualified source bytes. The changed source admission checks 4,419 recorded
entries across 2,205 unique compiled files and rejects any operator code delta.
The new controller allows only the current source-bound trace recovery with
its known FIT, manifest and protected prefix. The corrected loader is distinct
from the native experimental FIT; recovery root boot files remain protected.

Thirteen fixture groups passed, including the actual eight-write/eight-readback
wrapper over the real UploadSession model, the current source/reservation
admissions, native progress/error/passive-tail handling and actual master
try/finally with missing/invalid trace and phase/cleanup faults. Boot/device I/O
remain modeled in those host fixtures. Initial local fixture setup/name
mismatches stopped before device access; their failed logs remain retained.
The final individual receipts and exact helper hashes are frozen alongside the
complete launch plan and historical qualification records.

The live preflight and physical attempt remain pending. This test does not
promote ordinary package admission or full core-load acceptance. HDMI display,
physical control use, media/native networking and four FPGA signoff findings
remain open. The original factory card stays safe aside and the bottom kit is
untouched. TRWU8CBL's continuing claim remains held.

Raw operator/evidence: `out/nano-current-launch-board-20261107/`.
