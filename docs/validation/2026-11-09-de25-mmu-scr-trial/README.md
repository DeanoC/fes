# Bounded SCR-restoring MMU trial

Candidate `7efa0304ddd339aa` is built from source
`4ac5c085c721c8d0acdab9d43bde86814b2eb14b`. The
[source repair and matched build](../2026-11-09-de25-scr-restore/README.md)
restore caller SCR and add RET to the pinned vendor lower-EL TLB helper.
Policy format 2 binds its exact 44-byte linked code. All CPU/ownership/tape
implementation bytes, invalidations and the one-second deadline remain intact.
QEMU helper controls, real ELF mutation checks, 1,776 parent tests and all
214 FPGA software modules pass. The unchanged SDK/entry is receipt-verified.

The preceding e4 image successfully programmed, but its real provider and cold
scope rejected leaked `SCR=0x239` before any MMU writes. Its recovery/native
reservations and complete negative evidence remain retained. This is a genuinely
changed image and a new one-use procedure; it does not retry e4 or reset an old
consumption. Both the completed previous recovery grant and the older mode-loss
reservation are hash-bound and rechecked.

Only exclusively assigned top `TRWU8CBL` is in scope. The qualified probe keeps
mode `0775`, with bytes/mode/access checked before reservations and each live
observation. Each recovery/native programming operation needs a separate durable
reservation and fresh forced exact-device scan. Recovery must pass before
thirteen fixed uploads and independent fresh SD reads. Native startup must pass
the provider, selected MMU registers and private memory diagnostic before the
launcher and complete three-launch/four-menu/seven-configuration exercise.

Original bounds remain native 450/process 480, upload 300/process 320,
recovery process 270, overall 800/host 890 seconds. The first failure stops
commands and permits only the existing two-second passive trace tail before
host cleanup. Nothing relaxes the one-second tape limit or live device guards.
UART Select provides actions; no physical keys are requested. The spare card
stays installed, factory restoration remains waived, and the bottom kit stays
outside scope.

This frozen procedure has no hardware result yet. Neither its software evidence
nor a bounded private-line diagnostic establishes general cache coherency,
normal package/core, physical display/input or appliance acceptance.
