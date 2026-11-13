# DE25 header-aware contained configuration, 2026-11-01

**STOPPED** on TRWU8CBL, native source `8bc8fcf70756085553e46a4605e793a610335220` and matched boot
`32f5ecd1a46dcf8c`. The first bounded attempt stopped at `configuration_poll`. The raw trace is retained without a retry. Configuration completion, live identity and bridge reclosure require their own positive evidence. Runtime core acceptance remains unavailable.

START reported 4 credits and 1048560 bytes per block, with next job 0. No transport failure record was observed. The saved analysis records the last transfer and queue observations separately. The [saved observations](transport-analysis.json) and
[raw serial trace](native-console.log) retain the exact first attempt.

The transfer stopped at 20.097686 seconds with 786,432 bytes completed,
65,536 bytes outstanding and all 2,027,520 source bytes retained. Four sampled
pending observations report `CIN=COUT=0x19`, `RIN=ROUT=2`, incoming doorbell zero,
idle parser state, no active slot and zero local command/response descriptors.
The `job=13` field reports the next send ID after issuing job 12. No
transport-failure, programmed-core or identity marker follows. These non-atomic
samples locate the missing response after command consumption; they do not
identify an SDM/DMA/FPGA root cause. No retry was attempted.

This candidate appends a read-only observer to the exact pinned mailbox-v3
transport. It samples CIN/COUT, RIN/ROUT, the incoming doorbell and status, parser
state and descriptor counts. No FIFO contents or write-only register is read.
There are no added mailbox commands, clears, consumption, replays or resets.
Samples follow START, WRITE and completion polls 1, 2, 16 and 64 per buffer.
The completion path uses the existing header-returning API so that a consumed
SDM error 2 cannot be mistaken for MBOX_NO_RESPONSE. It clears the local header
on every read, including reads after an ACK. The actual-parser matrix covers
80 cases, including this aliased error. The software repair does not establish
the physical cause before this matched trial.
These non-atomic observations grant no completion or bridge authority. The
original whole/chunk digest checks, cold ownership gate and 20-second transfer
limit remain in force.

## Inputs and boundaries

- Sealed package: `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63`.
- FPGA build: `e25bb65a1d14a2bbbddf4c770ea033e5`.
- Core: 2,027,520 bytes, SHA256 `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c`.
- FIT SHA256: `8d7df62bfc75662739906a2b9b54ea9ed4c9eeedfa7aae4c4a6be7682fa4d709`.
- Fixed uncached configuration arena: `0x94800000..0x95000000`.
- Unchanged 20-second transfer deadline and complete whole-image/chunk checks.
- One CPU; secondary CPU release remains locked. No data release or activation.

The qualified recovery checks the spare card and protected boot files, writes
only the fixed `FES/UPLOAD.BIN`, then independently reads and compares it.
[Upload/readback](upload.json) remains separate from native execution.
The [independent audit](hardware.json) redecodes the first saved trace and checks
the restored original 15 MHz JTAG clock, USB permissions and free UART.
The spare diagnostic and exclusive top-board claim remain retained under
Deano's instruction; the original factory card remains safely outside the board.

## Software and retention

All 10 [affected software groups](software.json) pass,
including 1262 parent tests and
1576 FPGA host tests. Actual contained-engine functions pass
80 register-boundary cases from every response-ring starting position: full
2,027,520-byte streams, command/response ring and job-ID wrap, delayed replies,
a missing reply, an empty error and SDM error 2. The observer changes neither registers nor
service state in these checks. The separate transport-error regression covers
rejected sends, timeouts and errors after an earlier acknowledgement.
The pre-fix behavioral failures are retained separately from the repaired runs.
Both supported firmware pins pass integration with the actual FPGA control RTL.
Native trace/cleanup helpers pass 63 cases. The linked image audit verifies the
eight strong configuration/digest/observer symbols, the exact pinned
transport/header and appended observer policy. A strong-provider case exercises
90 busy polls and verifies the bounded sampler. Passive records cannot stand
in for configuration acknowledgements. The existing scalar digest specialization and all source/owner
checks remain unchanged.

The sealed model still rejects this target in the existing production runtime
with `unsupported_target`. This source-bound chain is not signed/measured boot.
Runtime admission, active-master drain, repeatable core launch/return and real
emulator/display/input acceptance remain open.

[Retained matched builds](build-retention.json) contain 43,681
members, each independently read back and compared. SHA256:
`2be8c9149d6e5ae72bfafa5363c25f6282e98bee3640977b52f0818b592d26fe`. Incremental source/evidence bundles require the documented
prior shallow retention chain; they are not complete-history backups. No claim
credential is published.
