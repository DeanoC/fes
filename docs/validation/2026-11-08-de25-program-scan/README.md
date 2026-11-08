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

The bounded physical test passed on TRWU8CBL. One fresh producer scan and exact
identity were accepted by the unchanged one-use policy; the durable recovery
reservation and scan-consumption record preceded exactly one traced programmer
invocation. The trace confirms the exact private cable, selected SOF read,
private configuration/loopback port and executable mappings of both hash-bound
vendor clients. Quartus returned0 and reported successful configuration with
zero errors. The qualified SPL/U-Boot/MMC/1GiB/prompt sequence arrived after the
actual program boundary. The full operation took13.06 seconds.

Independent cleanup passed: original15MHz readback, exact same USB permissions,
private service gone and UART free. Post-run hashing verified every frozen
source/input/tool and the retired native reservation unchanged. No native image
was loaded, no UART commands/SD writes were sent, and the other kit was untouched.
The recovery prompt's unconfigured Ethernet/MAC notice is expected at this
point; no Ethernet initialization or network test was performed in this scope.

The first result-postprocessor invocation reached its completed checks but
refused to overwrite the host process receipt named hardware-result.json.
That receipt and frozen helper were preserved. A recorded postprocessor copy
changes only its first output filename to qualification-result.json. It reruns
the same read-only evidence checks; no hardware action or procedure was repeated.

This qualifies the new host programming path for this recovery image; it does
not establish repeatability across arbitrary board states or explain the earlier
error87. The main native tape timeout still needs finer ownership measurements
and a justified repair. The retired7abe/native and prior diagnostic reservations
remain consumed. Transport programming failures remain1/max2; no failed physical
programming operation occurred in this qualification.
