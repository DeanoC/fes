# DE25 private JTAG server tracing preparation

The [latest failed native programmer](../2026-11-14-de25-native-programming-stop/README.md)
connected to its private JTAG server, then reported a broken chain before any
native boot output. The existing programmer strace records executable/library
loads and its localhost connection; it does not capture the server's USB I/O.
That evidence cannot distinguish USB request failures from device configuration
state. The root cause remains open.

Source `ea378a1bb608d4be7fa23e516578479e3490884a` adds optional
`jtag_server_trace.JtagServerTrace`. It checks the existing top-only server's
PID/start ticks, UID, pinned executable hash, complete argument tail and absence
of another tracer. Only the absolute executable name and the vendor launcher's
`jtagd` spelling are accepted as argv zero. The caller retains the original
claim, clock, scan, image and one-use programming gates.

The host tracer records ioctl calls with timestamps, durations, decoded file
descriptors and 32-byte string limits. It adds no configuration/status/reset
command. The caller polls a fixed 120-second trace bound; file output is capped
at 8 MiB. The existing 90-second programmer bound is unchanged. Cleanup stops
only the owned tracer, checks that it detached from the same live server, and
records premature tracer death. Lost ownership stays failed. No receipt grants
programming, reset completion, memory reuse or recovery authority. Ptrace pauses
threads and changes host timing, so traced runs are diagnostic evidence.

Eleven injected tests pass across identity/record/UID/argument/ownership faults,
attachment failure/timeout, output/time exhaustion, early exit and detach
failure paths. The actual host fixture captures an ioctl to `/dev/null`, checks
live PID/start-tick/TracerPid values, detaches normally, and separately kills
its own tracer. The disposable process survives both and then exits cleanly.
Its vendor executable/argument/UID identity is injected; no USB device or board
is opened. [host-fixture.json](host-fixture.json) and
[host-abrupt-fixture.json](host-abrupt-fixture.json) retain those boundaries.

The fresh private operator wraps the exact admitted programmer call and polls
tracer liveness on both sides of each UART pump and after programming. Success
requires a complete owned trace and verified detachment. All 69 operator models
pass, including success/failure/attach rejection and the actual latest baseline
wire replay. Both complete native file admissions and eight real root-level
role/digest rejection controls pass. Final user then root default preflights
pass. The initial unexecuted revision and its user-preflight scratch-directory
permission failure are retained; that scratch ownership was corrected before
replacement by the vendor-launcher identity revision. Neither revision has an
attempt or execution approval at this preparation checkpoint.

The affected-test campaign passes 2,742 parent tests with 15 skips, plus planner,
whitespace, Go inventory and generated-consumer checks. That campaign began
before the argv-zero repair; the final repair separately passes all eleven
focused tests and the new 69-model operator campaign. Committed `make check`
passes: 69 generated consumers and 45 fixture copies match. This is combined
regression evidence, not an exact-final-source rerun of all 2,742 tests.
See [completed-checks.json](completed-checks.json).

[operator.json](operator.json) binds the fresh procedure
`e2bae5dd7ffd03fbfe5c67c9c6a5198e318a25f03c4678e461bde6a5086c9319`
and its 10,910 frozen files. Firmware `8394acfa5`, phase-1 image `5dec3d3168`
and FIT `3a70eb273c` remain unchanged. The prepared source bundle, both operator
revisions, current host fixture and completed checks are retained in
`/home/deano/fes-retained/de25-server-traced-loading-prepared-20261114-r1/prepared-procedure.tar.gz`,
SHA-256 `9eb5859bdbb605096bbdb6af4b12ddbf41d281c0bac74404271cecb959e1a534`.
Independent readback verifies 12,239 logical files / 5,119 unique blobs with
bytes, hashes and modes; [retention-proof.json](retention-proof.json).

No kit operation is part of this preparation record. Last verified TRWU8CBL
state remains original native baseline at Park after restoration `d958…`, spare
card installed and all our hardware handles closed. A fresh traced hardware
trial must be recorded separately. Physical private-server tracing, terminal
cold reset and repeated-loading acceptance remain open. QSPI, factory card and
bottom kit are outside this procedure.
