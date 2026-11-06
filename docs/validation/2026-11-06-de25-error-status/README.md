# Non-clearing early fault-status inventory

The preceding physical trial captured EL3 SError while the saved PC was in the
NOBITS invalidation loop. It did not identify the originating transaction. This
change leaves that operation and all runtime/ownership limits intact, then
collects a fixed inventory only after the original exception line is emitted.
No new hardware evidence is claimed by this file-chain validation.

The private inventory comprises seven mandatory architectural registers and
32 MMIO observations. MIDR/PFR0 precede interpretation of optional RAS features;
no optional RAS error record or selector is accessed. The MMIO list covers DSU
and OCRAM CCU error status/location, boot-region attributes, OCRAM ECC state and
all four OCRAM firewall regions. Status brackets the CCU location reads.
Returned values are raw observations, not evidence that a controller is healthy
or that an error was absent before reset. The original exception remains fatal.

The addresses/field access modes were checked against the retained official
[CS/R1 revB0 RC28 register map](https://docs.altera.com/v/u/resources/864455/agilex-5-cs-and-r1-suffix-opn-hps-register-map).
Read-to-clear fields, alias/injection registers, initialization controls as
writes, and RAM access ports are excluded. The implementation issues no status
clear, selector write, ECC change, memory initialization, RAM access or retry.
Its generated code marks each read before accessing it and emits a matching
value; END follows only after all fixed reads. A failed query takes the existing
park-only vectors, retaining the original exception and the last before marker.
The shared 384-byte budget resets for each fixed line, with the unchanged
100,000-poll UART wait bound. The number of reads is fixed at 39.

Eighty-four focused host tests pass, including every incomplete-read boundary,
missing/repeated/reordered/unknown observations, width errors, flood bounds and
rejection in a complete boot stream. Seventy-five actual AArch64 QEMU cases
pass: the existing 42 vector/logger/recursive-fault/byte-bound cases now include
the inventory, and 33 additional cases inject failure at every selected MMIO
read plus the first architectural read. The CPU registers are real simulator
observations; MMIO addresses/values are modeled in RAM. These runs do not prove
physical CSR access or diagnose the board's SError. A harness dictionary-shadow
error is retained with the corrected inputs and passing results.

Actual linked BL31 audits check the canonical 39 reads, the completed UART
marker before each access, the generated include binding, bounded messages,
no stack/calls/status writes/return, private vector tables, vendor-vector
restoration and fixed entry address. The normal guard remains 1,428 bytes;
private stack/BSS layout and vendor cache operations remain unchanged. Required
changed-input checks and matched BL31/SPL/U-Boot/composition results are recorded
in result.json. The native application/entry are byte-exact retained builds from
31d716957; all 2,098 source hashes match. The sealed FPGA payload is reused.

The next step is one fresh source-frozen bounded trial on TRWU8CBL. The consumed
acca image is not replayed. Capture the new inventory or its exact failed-read
boundary, stop on the first failed gate and preserve independent cleanup. No
new timeout, cache bypass, ECC disable, global OCRAM scrub or admission grant is
part of this change. The vendor's ECC initialization requirement remains an
investigation candidate, not a confirmed explanation of the captured SError.
