# Selective-tracing complete main-array passes

The first full-array attempt stopped at its600-second bound with4,239,360
verified prefix bytes and no acknowledged CLOSE. It remains failed and archived;
pass2 never started. Before this new pass1 may read, one fixed CLI CLOSE for that
owned uncertain session must be acknowledged after fresh service/scan gates.
There is no OPEN, data read or fallback inside that cleanup step. A failed CLOSE
stops the new reservation. Normal CLI-to-console admission was already qualified.

Selective seccomp tracing retains the exact requested exec/open/connect/mmap
records while avoiding stops on unselected syscalls. The restricted-UID offline
100000-syscall benchmark was about44times faster; this is an overhead hypothesis,
not hardware throughput proof. Actual Tcl full-reader models pass again using the
new guard. The owned data soft stop at450seconds denies further READ grants but
preserves one CLOSE while the same ownership remains valid. A lost owner blocks
that cleanup. The600-second hard console cap still handles unresponsive packets;
700/800 experiment/host bounds and native limits remain unchanged.

The three-window console/CLI cross-check passed and is archived. This operator
extends the same restricted packet path to all16MiB of TRWU8CBL's main QSPI
array, including unused FF regions. It admits two separately owned sessions,
each with4096 sequential1024-word READ3a packets at offsets0 through0xfff000.
No arbitrary address, shorter range, resume, retry, write, erase, generic device
opcode, controller handover, image programming or reset/reboot is permitted.

Each pass starts with CONFIG_STATUS, OPEN, SELECT chip0, AFh identity20bb18 and
two zero status bytes, and ends with an acknowledged unique CLOSE and unchanged
CONFIG_STATUS. A root fixed-sequence guard checks current ownership before and
after every exact packet. Uncertain data retires normal IO; at most one CLOSE
is allowed while ownership remains established. Uncertain CLOSE is not retried.
The Tcl body streams complete raw replies and a little-endian binary without
accumulating the full flash in memory. Python independently replays every request,
header, word count, data byte and termination. It records4096 chunk hashes and
one complete SHA256, and requires the previously qualified windows to match.

Each pass has a new private network namespace, top-filtered JTAG server and
sixty-second USB observation, with the existing source/claim/node/UART/clock,
vendor-first and fresh forced-scan gates. Root-owned immutable runtime inputs
and restricted UID65534 prevent the console from direct Altera USB writes.
Clock readback occurs at the boundaries of the exclusively owned private session.
The first server/namespace must be gone and full replay/cleanup must pass before
pass2 can start. Two complete16MiB outputs must have identical hashes. There is
no CLI transition within a console server. Prior mixed-transport error87 remains
a failed archived experiment; its exact cause is not asserted here.

Host-only limits are600seconds for the full console read,700experiment,800outer
and64MiB per output file. Other tool calls retain30seconds. The full vendor Tcl8
injected run took about102seconds;600 provides room for actual JTAG/USB transport
and ownership checks. Raw decimal reply journals fit below64MiB; the binary is
16MiB. Native FIT/configuration arenas, RAM maps and boot phase limits are unchanged.

The actual vendor Tcl8 model reads/serializes the complete16MiB, independently
checked through the final byte. Eighteen reader cases and nine root-gate cases
cover failures and boundaries; inherited close-uncertainty/release behavior was
already exercised in the archived three-window models. A separate full-size
fixture rejects first/middle/final-word corruption, missing final byte, missing
completion and extra trailing data. Restricted-user staging also passes.

Independent cleanup checks all namespace members/server absence,15MHz/auto1,
exact USB permissions, private config bytes/mode600 and free UART. CPU Park is
prior evidence, not reobserved. Both complete passes and source/raw evidence must
be retained before treating the main-array backup as complete. OTP/security are
excluded; exact flash die/package remains ambiguous. A usable rollback artifact
and explicit approval are still required before any boot-flash change. This
operator authorizes no write. Factory card/flash and the other kit are preserved.
