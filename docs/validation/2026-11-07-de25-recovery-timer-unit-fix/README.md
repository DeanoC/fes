# Correct recovery Ethernet timeout units

The [consumed native-launch attempt](../2026-11-07-de25-current-launch-trial/README.md)
stopped during its fourth recovery upload before native programming. Source
and linked inspection confirmed `XGMAC_TIMEOUT_100MS=100000` at four millisecond
callers, permitting a 100-second API wait beyond the 90-second observer. The
physical stop is consistent with that mismatch; exact timer/MMIO progress
remains unproven. The failed attempt stays terminal and retained.

Implementation: `6d3a8be82ce66f35b820627b0e56ebbfddbd9e1e`.

The explicit trace recovery profile now corrects the one header constant to
100 milliseconds. The caller APIs, register operations, driver stop order,
90-second host deadline and all other hardware bounds stay intact. The v2
profile identity explicitly includes the functional correction. Its transform
pins the actual header after PHY preparation and independently reconstructs
the original header and all original network sources before delegating the
unchanged base/PHY checks. Foreign or self-consistently tampered inputs reject.
This is an explicit diagnostic profile; no production default is promoted.

The prior host operation-equivalence fixture assumed the intended constant.
A new regression instead injects the actual before/after vendor-header values
into the real vendor stop/success/return blocks. Permanently busy TX and RX
queues consume over 90,000 modeled milliseconds with the original header and
at most 202 combined modeled milliseconds after correction. Idle behavior and
the original disable sequence remain intact. Returned timeout paths do not
prove idle DMA. Host callbacks model timer and register I/O.

All 33 focused recovery tests and 1,607 parent tests (three skips) pass, along
with selected planner/whitespace/generated checks. The fresh U-Boot build passes
its unchanged 480-second bound. Actual ARM disassembly shows both stop-loop and
send-loop comparisons for 100 ms, with the timer scaled to milliseconds. Both
device trees select UART1. Strict BL31, SPL and the qualified SOF are unchanged;
no FPGA/PFG build was required. The exact original-source inverse passes.

The explicit regular-file package validates, while the generic uploader and
current profile admission of the older installed image reject as intended.
Compared with that installed-image reference, only the fixed FIT mirror differs
in the protected prefix. This is a file-only comparison, not a fresh card check.

Candidate FIT: 954,248 bytes, `3722e24c551a794dd970e665cc4932b96cb62aefa353f39079285458d67d48e5`.
Candidate manifest: `95aca2b3116755247a122a9a4357c1efbcfb106adb4dfa5fe94e489cc1889664`.
Candidate prefix: `80e36789a8de046c8e47899bbb6567d5afa58e3325c10d3944c7151a210d595c`.

All 2,205 native compiled inputs remain unchanged and the normal native FIT/
SPL/entry checker passes against the previously refreshed native artifacts.
Only the recovery profile/package/tests/documentation changed; the native
firmware and bootloader producer code did not. No native rebuild is required.
The native candidate still has zero physical boot attempts.

The kit has not received this correction. It remains claimed with the recovery
prompt unconfirmed after the failed transfer. Next is an exact current-image
UART repair, preserving original R1107A and consumed R1107B rollback files and
using fresh backup names. That controller, its failure models and a new frozen
operator are required before any write. Existing consumed operators and the
earlier original/v1 repair transitions cannot be replayed against this state.
The factory card and bottom kit remain untouched. Native launch/return, general
admission, full display/input/media/network behavior and four FPGA signoff
findings remain open; the full core-loading goal remains active.

Raw evidence: `out/nano-recovery-timer-unit-fix-20261107`.
Retention: `/home/deano/fes-retained/de25-recovery-timer-unit-fix-20261107-r1`, layered on the physical-stop archive.
