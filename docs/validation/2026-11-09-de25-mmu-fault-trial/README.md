# Bounded stack-free transition capture trial

Candidate `07e1efeab6cc2b0d` is built from
`a475be5b2449ed88efb0592ef0b4c72ca0ac0f78` and the
[format 4 source/build checks](../2026-11-09-de25-mmu-fault-capture/README.md).
It adds a raw enabled record before returning to the C stack, keeps bounded
stack-free fault capture through the private memory test, and restores/readbacks
the original runtime vector before accepting the final register receipt.
Faults park; there is no cache-off fallback or new device/bridge authority.
The MMU engine, CPU/owner/memory/tape bytes and one-second tape limit remain.

Thirteen normal/fault/guard assembly cases pass, including the normal SP_EL0
path used by BL31. An [additional fault experiment](sp0-fault-observation.json)
rejects the initial hypothesis that MOV SP in an EL3 handler reads interrupted
SP_EL0: the raw `sp` field is the handler's SP_EL3. ESR/ELR/FAR remain correct;
SPSR identifies the interrupted mode. Interrupted SP_EL0 is not captured by this
version. This diagnostic limitation grants no authority and cannot admit a
failed boot. The original experiment and its rejected assertion remain retained.

Only exclusively assigned top TRWU8CBL is in scope. The preceding59e9 image
and its recovery/native reservations remain consumed and retired. Both old
phase receipts and the older mode-loss reservation stay hash-bound. The probe
keeps mode0775 and is checked before reservation and every live observation;
each recovery/native operation needs its own durable reservation and fresh
forced exact-device scan. Rebuilt recovery receipts reproduce its qualified
prefix/manifest/FIT/SOF bytes. Recovery must pass before thirteen fixed uploads
and fresh SD reads. Native startup requires provider success, both vector
transition records, all26 steps, both register records and private memory proof.
The complete run still requires three launches, four menus, seven configurations
and final Park; the first failure stops application commands.

Bounds remain recovery270, upload300/process320, native450/process480,
overall800/host890 seconds, and the existing two-second passive failure tail.
Cleanup restores the recorded clock and USB permissions and stops only the owned
private server. UART Select supplies actions; no physical key is requested.
The spare stays installed, factory restore remains waived, and bottom TRWU8J8G
is outside scope. No physical result exists at preparation time. These software
checks do not establish Nano cache, ordinary core loads or appliance acceptance.
