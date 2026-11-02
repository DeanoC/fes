# DE25 bounded four-buffer configuration, 2026-11-02

**STOPPED** on TRWU8CBL, native source `f1776384dd3a85d1fb36d5d8c3a8267001607725` and matched boot
`493d2565fd4d38b5`. The first bounded attempt stopped at `configuration_poll`. The transfer stopped at 20.217864 seconds with 786,432 bytes completed, 1,048,576 accepted and all four buffers outstanding. Sampled CIN=COUT=5 and RIN=ROUT=2, with no incoming doorbell and an idle parser. Filling the window did not resolve the stall. The raw trace is retained without a retry. Configuration completion, live identity and bridge reclosure require their own positive evidence. Runtime core acceptance remains unavailable.

START reported 4 credits and 1048560 bytes per block, with next job 0. No transport failure record was observed. The saved analysis records the last transfer and queue observations separately. The [saved observations](transport-analysis.json) and
[raw serial trace](native-console.log) retain the exact first attempt.

This candidate fills up to four accepted writes, polls up to three exact ordered
completions and refills freed slots. The secure gate checks every pending chunk
and rejects a malformed batch before advancing any authority. A lookahead peer
reproduces the old serial-client deadlock and completes with this window.
Uncertain failures retain all possible outstanding buffers and the full source.
The fixed arena, 20-second deadline and held data paths remain unchanged.

This candidate appends a read-only observer to the exact pinned mailbox-v3
transport. It samples CIN/COUT, RIN/ROUT, the incoming doorbell and status, parser
state and descriptor counts. No FIFO contents or write-only register is read.
There are no added mailbox commands, clears, consumption, replays or resets.
Samples follow START, WRITE and completion polls 1, 2, 16 and 64 per buffer.
The completion path uses the existing header-returning API so that a consumed
SDM error 2 cannot be mistaken for MBOX_NO_RESPONSE. It clears the local header
on every read, including reads after an ACK. The actual-parser matrix covers
144 cases, including queued completions, lookahead, missing replies and this aliased error. The software repair does not establish
the physical cause before this matched trial.
These non-atomic observations grant no completion or bridge authority. The
original whole/chunk digest checks, cold ownership gate and 20-second transfer
limit remain in force.

## Inputs and boundaries

- Sealed package: `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63`.
- FPGA build: `e25bb65a1d14a2bbbddf4c770ea033e5`.
- Core: 2,027,520 bytes, SHA256 `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c`.
- FIT SHA256: `58436103f60ad379c0c554d625190ff80e705b171309aecda8b350831d508e69`.
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

## Status reads after the stop

Two separate non-programming JTAG status reads left the native image parked.
The [general status read](post-stop-config.log) returned six words with
`STATE=IDLE`, configuration firmware version `001a0300`, `nSTATUS=1`,
`CONF_DONE=0` and `INIT_DONE=0`. This confirms a responding SDM and incomplete
configuration at the time of that query; it is not the HPS transfer's status.
The [RECONFIG_STATUS request](post-stop-reconfig.log) returned `0x2ff`.
Altera's [command reference](https://docs.altera.com/r/docs/d710186/current/security-technical-reference-manual/reconfig_status-command)
defines that rejection for a requesting client that differs from the client
which started reconfiguration on the route. It supplies no phase-two state.
The next diagnostic needs a status capture from the HPS client.

The [independent cleanup audit](post-stop-audit.json) verifies both saved query
outcomes, restored 15 MHz clock receipts, live USB permissions and a free UART.
Neither query programmed a new image, reset the board, wrote the card, resumed
the stopped transfer or granted runtime admission. The query scripts ran on
source `b6301a49572c9b5856777ee299af45ae277ee9ac`; the parked native source
remained `f1776384dd3a85d1fb36d5d8c3a8267001607725`.

## Software and retention

All 10 [affected software groups](software.json) pass,
including 1264 parent tests and
1576 FPGA host tests. Actual contained-engine functions pass
144 register-boundary cases from every response-ring starting position: full
2,027,520-byte streams, command/response ring and job-ID wrap, delayed replies,
a missing reply, an empty error and SDM error 2. The observer changes neither registers nor
service state in these checks. The separate transport-error regression covers
rejected sends, timeouts and errors after an earlier acknowledgement.
The pre-fix behavioral failures are retained separately from the repaired runs.
The first window candidate exposed a same-phase scheduler progress bug in the
real Tenfoot integration. The endpoint now counts accepted or completed byte
progress while retaining the existing two-second controller deadline. Direct
launch, Tenfoot launch/return and wrong-identity recovery pass on this source.
Both supported firmware pins pass integration with the actual FPGA control RTL.
Native trace/cleanup helpers pass 66 cases. The linked image audit verifies the
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
`c5f6275dfc559f11dab78bf31e4be2bdb462a97ef5b6a27d75176941e1cce996`. Incremental source/evidence bundles require the documented
prior shallow retention chain; they are not complete-history backups. No claim
credential is published.
