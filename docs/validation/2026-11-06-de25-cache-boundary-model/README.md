# Real cache helper after the physical NOBITS boundary stop

The single TRWU8CBL trial recorded in the sibling vendor-startup-trace-trial
stopped after nobits-cache-before. This follow-up performs no board operation.
It delegates the simulator fixture's two cache calls to the actual pinned vendor
cache helper instead of the injected cache stub. Other platform functions and
the memory environment remain models.

All 13 AArch64 QEMU cases pass, including complete startup with both real cache
calls, seven injected platform-call stops, UART failures, register preservation,
the original warm entry, and a direct real-helper call over `[0x50000,0x5dec0)`.
The last case executes cache instructions only; it performs no OCRAM data read,
write, BSS clear or selected-stack access. QEMU maps that low address region as
its own boot ROM, so this is not a Nano SRAM-access test.

An ELF extraction audit proves the fixture helper's 56 instruction bytes match
inv_dcache_range in the physical trial's BL31 exactly. That function reads the
cache-line size, invalidates successive addresses, executes DSB SY and returns;
it has no stack accesses or platform function call in this selected build.
The fixture changes the symbol name only, leaving those instruction bytes intact.

The physical stop is not reproduced in QEMU. No cache-maintenance removal,
policy relaxation, timing extension or hardware retry is justified by this model.
The next diagnostic should preserve the vendor cache operation and capture a
bounded stack-free early exception or stalled boundary before BSS/stack setup.
The preceding UART drain and following UART startup remain part of the
unobserved interval; the actual failing instruction remains unknown.

The actual firmware remains implementation `7e90d783e`; this follow-up changes
no firmware, native application, FPGA producer, runtime or ownership contract.
The kit's original 15M/permissions/private-service/UART cleanup remains verified,
its claim and spare card retained, and the factory card and other kit untouched.
