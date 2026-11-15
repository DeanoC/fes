# Bounded refreshed native launch and return test

The consumed attempt stopped before native programming during the fourth
recovery upload. Three file writes/readbacks passed. The trace reached
`rx-drain-before`; source and linked-code checks then confirmed a 100-second
timeout expressed under a 100-ms name. The exact physical stall remains
unproven. See the result and timeout findings below; no retry occurred.

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

The frozen preparation itself granted no physical acceptance. This test does not
promote ordinary package admission or full core-load acceptance. HDMI display,
physical control use, media/native networking and four FPGA signoff findings
remain open. The original factory card stays safe aside and the bottom kit is
untouched. TRWU8CBL's continuing claim remains held.

Raw operator/evidence: `out/nano-current-launch-board-20261107/`.

## Physical stop and evidence boundary

The single bounded attempt stopped in the upload phase after 127.7
seconds. Recovery boot passed. Package, primary firmware and primary metadata
each completed all 34 return observations and their SD write/readback checks.
The fourth transfer, alternate firmware, reported 8,192 transferred bytes and
then produced only the first 16 return observations. The raw console contains
118 markers: three full traces and one valid incomplete prefix.

The last observation is `rx-drain-before`; `rx-drain-after` is absent. TX drain
and MAC disable returned. The intervening source comprises completion of the
UART before-marker helper, initial timer sampling, repeated timer evaluation,
and MTL receive-queue debug-register reads/queue-state tests. This narrows the
observation region; it does not identify the exact stalled instruction or prove
which timer/MMIO operation failed. The original vendor drain timeout remains
unchanged. The earlier intermittent stalls are not retrospectively attributed
to an exact cause by this result.

The fourth transfer's RAM hash was not reached, and its SD write was not
attempted. No final eight-file fresh-readback pass or native programming/boot
occurred. The native candidate therefore still has zero physical boot attempts;
this consumed operator has one recovery-upload failure. The full launch file
set is not admitted. No retry, fallback or native command followed the failure.

Independent cleanup passed: recorded 15 MHz readback, exact USB permissions,
owned private server gone and UART free. The board's recovery prompt remains
unconfirmed, so its continuing claim is retained. Factory card and bottom kit
were untouched. The raw trace, partial decoder output and exact driver region
are bound by `result.json`. Next is a contained receive-drain investigation
that preserves the existing timeout and all launch/admission gates.

## Confirmed timeout unit mismatch

The exact installed driver's `XGMAC_TIMEOUT_100MS` macro is 100000. All four
call sites use milliseconds: three compare against `get_timer`, and one passes
the value to `wait_for_bit_le32`. Their actual source documents milliseconds;
the compiled configuration has `CONFIG_SYS_HZ=1000`. Linked ARM inspection
confirms both stop loops compare against 99999 with the corresponding unsigned
branch, giving a 100000-ms API wait. The linked timer multiplies ticks by 1000,
divides by timer rate and subtracts the starting value.

The nominal 100-ms wait is therefore expressed as a 100-second API limit. The
90-second host deadline can expire before that limit. This is consistent with
the observed receive-drain prefix, but hardware timer/MMIO progress and the exact
stalled instruction remain unproven. The source error is confirmed separately
from that physical-cause inference.

Next is a targeted constant correction from 100000 to 100 milliseconds, with
all four caller APIs and stop operations preserved, independent busy/error-path
checks and a newly bound serial repair/test. The host deadline must stay at
90 seconds. This finding does not authorize native programming or reusing the
consumed operator against a partial launch-file set.
