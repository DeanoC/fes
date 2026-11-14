# Bounded SPL/EL3 entry-diagnostic procedure

This is the first physical attempt of diagnostic firmware
`f40a8acc496aaeeb6e69e4c8dc85e481efbaf420` and cold chain
`c0578d7ef93e00fdc8f19a03a3eac38bff63f6206e5e9f9d3d281911fb31025b`.
The [build record](../2026-11-06-de25-firmware-entry-trace/README.md) binds the
matched SPL/BL31, bounded UART code, saved SPL rejection observations, actual
ARM cases and integration results. The native application/entry are the
byte-exact retained `31d716957759c3dede3330877dac2ce7bcd2d643` artifacts;
the later operator publication does not compile or replace them.

The private procedure is `out/nano-firmware-entry-trace-board-20261106`.
The [source freeze](operator-source.json) binds its helpers and fixtures.
Both native boot decoding and Tenfoot explicitly require
`firmware_ram_trace=True`. The final result gate rejects missing, incomplete
or invalid entry/pass checkpoints. Positive and damaged-record fixtures execute
the actual decoder and gate. Eight prior stopped procedures remain retained:
one programmer failure, one host setup failure, four ROM deadlines and two
pre-native startup timeouts. This new diagnostic input has zero hardware
attempts at preparation time; earlier input directories stay fused.

After exact-file/source/fixture checks and live read-only admission, permit one
attempt on the exclusively claimed top kit TRWU8CBL with its existing spare
card. Verify the exact adapter/UART, complete descriptor inventory, ownership,
route and source. Acquire the shared Quartus/JTAG lock as the normal user.
The private top-only service must observe the original 15 MHz, select temporary
6 MHz and freshly admit the exact chip before programming.

Qualify the unchanged historical recovery through its original producer and
unmodified validator. Use one owned Ethernet session for eight fixed file
writes/readbacks and eight fresh SD rereads, with protected root recovery
files, card geometry, PHY and upload-memory checks. Then program the exact
native chain. A SPL rejection must preserve its saved allocation view; EL3
ENTRY/PASS/rejection phases must remain diagnostic observations, never an
ownership grant. Stop immediately on a rejection or incomplete boot proof.
Only a verified native startup may continue to ZX81 ROM, JP-loop ROM and ZX81
ROM launch/return/Quit, with matching receipts and shared-USB pause/resume.
No physical controller input is required or claimed.

Keep prompt 50 s, startup/launch 100 s, configuration 20 s, control 5 s,
native 450 s, native process 480 s, procedure 740 s and host 850 s. ROM frames
remain 16 bytes, with the one-second/64-poll bounds. Stop first failure without
replay, preserve the raw tail, restore the original programming clock and exact
USB/private-service/UART state, and independently check cleanup. Cleanup is not
CPU halt proof.

Keep the claim and spare under the user's optional factory-restoration policy.
The factory card and other kit are outside scope. There is no flash write,
normal package admission, HDMI or native Ethernet acceptance, or DE10-equivalent
readiness claim. Record the physical outcome separately from the frozen
procedure and software evidence.


The single physical attempt on operator `e4b731aa6` stopped at the matched
native prompt timeout. Recovery, all eight uploads/fresh SD rereads,
programming and SPL FIT verification passed. Both EL3 `ENTRY` and `PASS`
checkpoints appeared after the exact SPL ATF descriptor, binding BL31 SHA
`2f09b64a864176eb9369380f950f8e30f078a71b1ad12a0d2d9b28ad2d88cd6a`.
The saved decoder independently reports a valid complete guard trace. This
proves execution crossed the final SPL exclusion and accepted the early EL3
checks. The final UART drain and subsequent vendor startup remain after the
last emitted marker; the exact next instruction is not observed. These
read-only checks do not prove writable OCRAM, vendor initialization, native
CPU exclusion or successful core loading.

No vendor output followed before the existing timeout, and `fesboot` was never
sent. There was no ROM load/Run/return and no retry. Independent cleanup verified
original 15 MHz, exact USB permissions, private service gone and free UART.
CPU/FPGA halt is not independently proved; retain the claim and spare. The
[physical receipt](physical-result.json) binds the raw trace and source
identities. Next add checkpoints around the unchanged vendor reset, CPU-data,
cache, BSS, stack and setup steps, preserving its calls and register contracts.
Do not replay this consumed diagnostic input.
