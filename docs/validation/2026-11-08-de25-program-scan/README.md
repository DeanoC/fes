# Fresh-scan programming producer and one recovery-only boot

`platform/embedded/nano/jtag_scan.cpp` replaces the staged probe source for new
host bring-up operations. It accepts one exact private top-TRWU8CBL cable,
validates6MHz while locked, performs one forced scan and one non-scanning fresh
identity read. Count1/4362C0DD/mask0/IR10 and successful unlock are mandatory.
The observed vendor chain type2 is not used as an identity proof. This follows
the successful separate scan-only observation and Intel's scan API sequence.

The unchanged `jtag_programming.ProgramAttempt` reserves each operation before
scanning, requires the exact successful transcript, rechecks the complete
claim/server/config/clock/probe/client identity, persists consumption before
programming and calls the programmer at most once. No diagnostic transcript or
cached listing can authorize it. Controlled actual-C++ tests cover success,
unknown/unrecognized pre-scan types, all API failures, bad identity, locked
clock, bad target/environment, durable one-use consumption and no replay.
The actual producer also compiles against pinned Intel headers/vendor libraries.

The separate frozen hardware procedure allows exactly one previously qualified
recovery image load. It uses adapter-only discovery, one fresh producer scan
per operation and the existing bounded traced programmer and serial receive
bodies. The pre-program console boundary is recorded in the actual program
callback, after scan/admission. Old queued prompts cannot satisfy the new
qualified SPL/U-Boot/MMC/DRAM/prompt sequence. Its image and unchanged compiled
recovery inputs are hash-bound; its loader has no automatic SD writes.

The procedure sends no UART commands or SD writes. It does not load native
firmware, repeat the retired7abe attempt, grant native acceptance, or resolve the
one-second tape timeout. All old reservations stay immutable. First failure is
terminal; one prior transport programming failure remains, with two the limit
before another repair. There is no automatic programming retry or fallback.

Injected wrapper tests cover scan failure, malformed transcript, claim loss,
program failure and program timeout, preserving consumption without replay.
The retained pump/wait/fuse/program-process bodies match the previous qualified
recovery operator. Independent cleanup verifies original15MHz readback, exact
USB permissions, private service gone and the UART free. Factory restore remains
waived by Deano; the spare card stays installed and bottom TRWU8J8G is untouched.

Prepared and result archives retain exact source, controlled checks, compiled
producer, image/input closure and execution evidence with full archive readback
and verified Git update bundles. Software, recovery boot and native core-load
acceptance remain separate evidence classes.
