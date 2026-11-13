# Refresh the ordinary-package native launch chain

After the [physical UART repair and repeated uploads](../2026-11-07-de25-recovery-console-repair-hardware/README.md)
passed, the ordinary-package native chain was refreshed against current sources.
No board operation occurred during these builds and checks.

Current source: `36eeaa8b8ff9f5573fe1f4807df5022e2be67ea5`.
Reused SDK/entry source: `137dc3e5f290d4be699c5369d90f6368a527fe90`.
Sealed FPGA producer: `6c6dd62b503f143448a65e17ba346e410187e960`.

The source audit checked 4,376 recorded input entries. Only the bootloader
builder differed, in the secure-firmware and U-Boot provenance maps. The normal
entry validator accepted the unchanged SDK/entry, while the normal full native
validator rejected the stale boot chain. Those gates were preserved. Secure
firmware and U-Boot were rebuilt with their original 480-second bounds, followed
by the matched HPS-first FPGA boot-file composition under the shared lock.

The new secure firmware's chain digest/owner seed reflect its updated producer
binding. Its 24 generic driver/PSCI/vendor CPU objects remain byte-identical to
the qualified diagnostic reference. The actual ELF retains the initialized,
64-byte-aligned 256-byte CPU array in sealed firmware DDR and the original BSS
bound. The new loader and extracted FIT FDT independently reserve the complete
two-MiB firmware interval. Source equality permits reuse of the completed ARM
call/cache model cases; they were not rerun or treated as physical proof.

The full normal native checker and source/resource/ROM/package agreement audit
pass for the rebuilt artifacts. The standard `fes.computer` 1.0 constructor,
retained-ROM provider, serial Tenfoot frontend and shared USB gamepad mode are
selected. The unchanged two-ROM catalogue passes the actual Go preflight without
firmware access. All eight fixed launch destinations, sizes and hashes are
recorded. The matched composition independently checks embedded SPL and the
unchanged sealed FPGA core payload; physical programming admission remains off.

Native FIT: 8,219,184 bytes, `a7d4b3f86ff99e2b27466968885d41ff408412befcca42b0e7d9c845c23d33b8`.
Native SPL: `15bba7971b7c10de7b854f307cf8dac8d9c015987d3451b969e5fd451feea2f2`.
Native HPS-first SOF: `e3988eed94059b92dfa88a154984a4839ac698029168901a9dfe43cb892e4cc7`.
These are experimental launch artifacts, distinct from the installed recovery
boot image. No SD file was changed by this milestone.

TRWU8CBL remains claimed at its last verified corrected recovery prompt. Next
is a newly frozen operator: fresh boot/card proof, all eight uploads and fresh
readbacks through the corrected trace-aware path, then one bounded native
launch/return test. The previous consumed operators must not be replayed.

This chain has not booted or activated a core on hardware. Four existing FPGA
signoff findings remain open (missing input/output delays and reset-domain
findings); no timing waiver or constraint relaxation was introduced. General
package loading, full launcher/display/input/media/network behavior and DE10
feature parity remain unproven. The original factory card and bottom kit remain
untouched. These results do not complete the active core-loading goal.

Raw evidence: `out/nano-current-launch-chain-20261107`.
Retention: `/home/deano/fes-retained/de25-current-launch-chain-20261107-r1`, layered on the original native SDK/entry
archive and the latest physical recovery archive.
