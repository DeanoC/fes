# Bounded guarded-OCRAM trial procedure

This procedure prepares the first physical test of the
[matched guarded firmware chain](../2026-11-06-de25-guarded-ocram-chain/README.md).
Firmware/SPL implementation is `b642fc3c9cc1c8d2b7d7f7af24507110932ef01b`.
The native application/entry are reused byte for byte from
`31d716957759c3dede3330877dac2ce7bcd2d643`; their selected sources and retained
artifacts are checked independently. Neither is attributed to the later
operator publication. Twenty firmware driver objects, including the ROM,
ownership, reset and USB paths, are byte-identical to the preceding O2 build.
The profile-bearing cold-platform object and early entry use the newly checked
generated sources and actual ELF/instruction evidence.

The private procedure is `out/nano-firmware-ocram-board-20261106`. Its
[source freeze](operator-source.json) binds the helpers and off-board fixtures.
Existing launch/return, upload, receipt, transport, programmer admission and
passive-tail fixtures passed. New fixtures execute the actual distinct-source
admission and original recovery validator, accepting the genuine sources and
rejecting 24 source/receipt/history mismatches.

The first file-only candidate check caught a stale producer reference in the
unchanged qualified recovery's receipt. That stopped check is preserved. The
second check passed with the original producer, validator and PHY profile
pinned to their actual Git bytes and original receipt. Only that stored
historical recovery uses this private source context. Its strict validator is
unmodified, and every other input still requires an exact current hash. The
new native chain remains bound to its current producer. No recovery image or
boot file is silently regenerated, and the old failed physical inputs stay
fused.

After a clean committed-source and live read-only preflight, allow one attempt
on the exclusively claimed top kit TRWU8CBL with its existing spare test card:

1. Check exact USB/UART identity, ownership, every process's UART descriptors,
   route, source, fixtures and artifact hashes. Acquire the shared Quartus/JTAG
   lock as the normal user before any hardware operation.
2. Start a top-only private JTAG service, require the original 15 MHz setting,
   select temporary 6 MHz, and require fresh matching chip admission before
   programming. Qualify the existing recovery prompt and programmer exit.
3. Use one owned Ethernet session for eight fixed file writes/readbacks and
   eight fresh SD rereads. Preserve and check the root recovery boot files,
   card geometry, PHY and upload-memory ownership before native programming.
4. Program the exact composed native chain, require its boot/CPU proofs and
   shared USB pause, then run ZX81 ROM, JP-loop ROM and ZX81 ROM sessions with
   matching activation receipts and owned return/Quit. Physical controller
   action is not requested or claimed in this attempt.
5. Stop on the first failed gate without replay. Retain the raw trace and
   resources. Restore the original JTAG clock, exact USB permissions, private
   service and UART state, and independently check that cleanup. Host cleanup
   does not prove the CPU or FPGA halted.

Bounds remain prompt 50 s, startup/launch 100 s, configuration 20 s, control
5 s, native 450 s, native process 480 s, procedure 740 s and host wrapper
850 s. ROM frames remain 16 bytes, with the existing one-second/64-poll limits.
Six earlier stopped procedures remain recorded: one programmer failure, one
host setup failure and four native ROM deadlines. This new compiled firmware
has zero physical attempts at preparation time.

Keep the continuing claim and test card under the user's optional-restoration
policy. Do not touch the factory card or other kit. There is no flash write,
ordinary package admission, HDMI acceptance, native Ethernet acceptance or
DE10-equivalent readiness claim. A physical result must be recorded separately;
the source freeze and passing fixtures alone do not establish a successful
boot or core load.


The single physical attempt on operator
`01bcdbd55` stopped at the matched native prompt timeout. Recovery, all eight
uploads and fresh SD rereads, programming and SPL FIT verification passed.
The final SPL ATF trace binds the expected BL31 SHA, but no vendor secure
startup appeared and `fesboot` was never sent. The trace does not distinguish
SPL's final exclusion rejection from the early EL3 guard. There was no native
startup, ROM completion, Run or return. No retry was made. Independent cleanup
verified 15 MHz, exact USB permissions, the private service stopped and a free
UART. CPU/FPGA halt is not independently proved; retain the claim and spare.
The [physical receipt](physical-result.json) binds the raw trace and results.

Source investigation found a guaranteed producer/consumer mismatch:
`FES_NANO_FABRIC_ABSENT` is 1, while the early assembly required zero. The
hand-packed QEMU control also used zero, so its 251 passing cases did not prove
compatibility with the real C handoff producer. Preserve those results with
that limitation. Correct the enum binding and generate the next ARM test's
record through the actual C producer before constructing another candidate.
The exact first physical failing instruction remains unobserved.
