# Selected MMU transition checkpoints

The preceding [SCR-restoring board trial](../2026-11-09-de25-mmu-scr-trial/README.md)
passed provider setup with SCR `0x238`, then stopped after the MMU `before`
record. The number of subsequent register writes and the private-memory probe
outcome remain unknown. Its complete evidence is retained, both programming
reservations are consumed and the image is retired.

The selected format 3 profile adds 26 ordered boot checkpoints around the
existing admission callbacks, TLB/commit barriers, four register writes and
private witness clean/invalidate/readback. The observer requires the complete
sequence between the existing before/after register records. Every interrupted
prefix remains incomplete; malformed, missing, duplicate and out-of-order
steps reject. The joined strong-provider fixture verifies the actual emitted
sequence. No checkpoint is on the tape or ordinary SMC hot path.

The MMU engine, private memory probe, CPU guards, cold ownership, owned SiP and
tape implementation bytes remain identical to `ea0f6c8b`. The original one-second
tape deadline and device containment requirements remain. This change diagnoses
the boot stop; it does not yet claim to fix it.

Exact descriptors extracted from both the retired and newly built BL31 images
execute under emulated A53 EL3. Both raw binary images contain the same table
bytes as their respective ELFs. Twelve translation queries verify identity,
read/write permission, secure/non-secure outputs and unmapped holes. Actual
secure DDR write/read and stack access also pass. Three damaged descriptor
controls (writable table, missing private witness, secure shared DDR) reject.
QEMU does not reproduce Nano OCRAM, MMIO, cache coherency or FPGA behavior.

The matched BL31/SPL/U-Boot/image candidate is `59e96369a749fc7d`.
It reuses the receipt-verified SDK/entry and unchanged core payload. This build
has no new hardware acceptance. Software checks and retained artifacts are
recorded alongside this note; source and complete generated build/fixture inputs
are preserved in the retention archive before hardware use.
