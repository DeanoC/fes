# DE25 contained configuration transport diagnostic, 2026-11-01

**STOPPED** on TRWU8CBL, native source `d72926137ef5a66ea3c42b81b4c619c870a4616c` and matched boot
`bddc1e9a9ea2c8f9`. The first bounded attempt stopped at `configuration_poll`. The raw trace is retained without a retry. Configuration completion, live identity and bridge reclosure require their own positive evidence. Runtime core acceptance remains unavailable.

START reported 4 credits and 1048560 bytes per block, with next job 0. No transport failure record was observed. The native deadline stopped at 20.040471 seconds with 786,432 bytes completed, 65,536 bytes outstanding and all 2,027,520 source bytes retained. The [saved observations](transport-analysis.json) and
[raw serial trace](native-console.log) retain the exact first attempt.

The repair captures rejected asynchronous submissions before recording bytes as
sent. It propagates completion errors even when the response has no argument
words, or follows an earlier valid acknowledgement. Only the API's genuine
`MBOX_NO_RESPONSE` result remains pending; other errors retire this boot's
engine. The engine initiates no response flush, replay or restart. Uncertain
buffers stay owned until reboot. New limits/error records are passive
observations and grant no additional mailbox or bridge authority.

## Inputs and boundaries

- Sealed package: `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63`.
- FPGA build: `e25bb65a1d14a2bbbddf4c770ea033e5`.
- Core: 2,027,520 bytes, SHA256 `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c`.
- FIT SHA256: `37616c6e249f188261ddcc2aa62d6dcc281d4179aa58a62b1d41593c887d9814`.
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

All 11 [affected software groups](software.json) pass,
including 1259 parent tests and
1576 FPGA host tests. Actual contained-engine functions pass
seven injected transport outcomes: normal and delayed ACK, rejected send,
empty error reply, timeout, late error after ACK, and unexpected positive status.
The pre-fix behavioral failures are retained separately from the repaired runs.
Both supported firmware pins pass integration with the actual FPGA control RTL.
Native trace/cleanup helpers pass 58 cases. The linked image audit verifies the
strong configuration/digest implementation, transport failure latch and new
observations. The existing scalar digest specialization and all source/owner
checks remain unchanged.

The sealed model still rejects this target in the existing production runtime
with `unsupported_target`. This source-bound chain is not signed/measured boot.
Runtime admission, active-master drain, repeatable core launch/return and real
emulator/display/input acceptance remain open.

[Retained matched builds](build-retention.json) contain 43,680
members, each independently read back and compared. SHA256:
`c65fca744e8f42664850957f746e13195991f21ae8a5748f0b1169885bfea7a5`. Incremental source/evidence bundles require the documented
prior shallow retention chain; they are not complete-history backups. No claim
credential is published.
