# Bounded native SD writer preparation

Base: `0db693965`. The preceding physical inspection and independent layout
review supply the real spare-card plan. This preparation adds no hardware
attempt and does not change the last recorded kit state: SD-contained
generation 7, CPU park unobserved. No UART, JTAG or physical SD access is used
by this preparation. The positive manual launcher return and stable minute
remain separate evidence in the returned-menu observation.

The native trace decoder now explicitly selects the write plan. It requires
the full cold boot followed by exactly five ordered records: verified,
written/read back, contained, resumed/read back and final contained pass.
Every phase must match the expected card identity, capacity, last-eight-sector
extent, owner, generation and fixed pattern hash. Default read-only decoding
rejects these records. A partial or failed write never implies containment;
once VERIFIED appears, a missing WRITTEN record does not establish that no
write happened. The Go/C dispatcher and PIO model emit the records consumed by
the decoder interoperability test; their cold prefix is explicitly synthetic.

The separate `ScratchReadback` prototype accepts an injected exclusive recovery
console and claim callback. It verifies the qualified loader, live RAM/LMB map,
new volatile boot marker, card manufacturer/product/capacity summary, partition
layout, MBR, protected raw prefix, recovery FIT and recovery manifest. It reads
and hashes the exact eight scratch sectors, then checks the protected media
again. FAT reads have explicit size bounds. Raw MMC reads create no synthetic
LMB reservations. Hash output must contain one matching address range and
digest, rather than merely mention the expected digest.

The verifier sends no SD write or network command. Errors retire the instance,
retain the last attempted command and send no rollback, retry or recovery
command. A new object cannot adopt an existing boot marker or foreign memory
reservation. `mmcinfo` does not provide the full CID: the receipt explicitly
records that limitation. Native verification binds the full expected CID;
independent recovery checks the summary and protected media without claiming
a fresh full-CID measurement.

The real-card writer SDK, native entry, SD-only DDR BL31, U-Boot FIT and SOF
are built. The selected native build is `1aad09d24ae28f1c`; FIT size is
2,660,152 bytes, below the existing 8 MiB limit. Independent embedded-SPL
decoding, offline FAT readbacks and complete source/receipt validation pass.
Vendor packaging ran as uid 1000 under the shared Quartus lock. Normal native
storage remains read-only. The selected `sdwrite` executable alone consumes
the reviewed plan for sectors 7,774,200–7,774,207, outside the FAT partition
and declared 144 MiB boot image.

Files and logs are under `out/nano-sd-writer-20261110/`. The seven focused
recovery-readback tests cover success, wrong card/layout/protected data,
short/corrupt/interrupted reads, changed RAM reservations, lost claim and
receipt/session reuse. The 64 focused native trace tests include the actual
Go/C model interoperability case. The first readback test run caught a
mistyped hexadecimal LBA in the test expectation; its failed log is retained
alongside the corrected passing run.

[All seven selected checks pass](software-result.json): the parent runner
reports 1,869 tests and three existing skips, with one ancillary test run.
The native SD firmware lease, PIO, FAT and package-store race checks and
generated consumer consistency pass. Adding this validation document selects
no additional parent test modules. These are software and file-preparation
results; they do not establish native write success on the card.

The next physical procedure still needs fresh operator preparation/admission:
one qualified recovery boot and fixed FIT upload/readback, one matched native
write and final containment, then a separately consumed recovery programming
operation for the independent read-only verification. That second recovery
operation is planned only after native success; it is not a retry after a
failed gate. All stages need fresh scans and one-use programming grants.
No previous grant or terminal attempt may be replayed. The qualified fixed
3 MHz/auto-off policy and exact host cleanup remain required.

Only top TRWU8CBL and its installed disposable spare are in scope. The original
factory card stays protected, bottom kit is excluded and old-output cleanup
remains paused. No native SD write, ordinary FAT persistence/install, native
networking or full appliance acceptance is claimed by this preparation.
