# Bounded stack-free MMU transition capture

The preceding [board trace](../2026-11-09-de25-mmu-transition-trial/analysis.md)
stopped between the SCTLR write checkpoints. Its consumed image is retired.
The selected format 4 profile keeps a register-only exception vector installed
through the first translated C stack accesses and the existing private memory
test. It publishes a raw enabled record before returning to C, and restores and
reads back the original runtime vector before the final MMU register receipt.

The leaf routines require the existing EL3/A55/primary/DAIF/little-endian/cold
scope, exact MAIR/TCR/root and original runtime vector. An eight-byte initialized
private DDR slot consumes installation before the register write. Failed or
foreign restoration retires the platform MMU object; every owned channel then
rejects without device I/O. There is no cache-off fallback. Fault capture reads
architectural registers, switches nested exceptions to a permanent park vector,
uses bounded raw UART output without a stack or C, and parks. Source, linked
instructions, vector routes, fixed CPU/UART constants and finite bounds are
checked. Host observation requires both exact transition records, 26 steps and
both original register records; a fault never grants loader or bridge access.

Twelve actual assembly cases run under emulated A53 EL3: normal restore, replay,
foreign vector, failed restore, wrong CPU, non-secure cold entry, stack permission,
data translation, instruction permission, UART timeout/all-ones and a nested
UART fault. Three deliberate faults report exact ESR/ELR/FAR with M/C/WXN still
enabled; nested faults park after two aborts. Fixture substitutions are CPU/UART
constants and one extra PL011-only mapping. Nano mappings are unchanged, but
this is not Nano cache/device qualification. The canonical Nano object and
actual matched BL31 ELF separately reject damaged code, vector, guard, sink,
bound and saved-state inputs.

Candidate `07e1efeab6cc2b0d` includes the matched BL31/SPL/U-Boot/image, with
receipt-verified unchanged SDK/entry and core payload. Recovery firmware and
both loader variants are rebuilt and reproduce the qualified binaries; fresh
packages also reproduce the installed recovery manifest and image bytes.
No new physical acceptance is claimed. The MMU engine, memory probe, CPU/owner
and tape implementation bytes and the one-second tape deadline remain intact.
