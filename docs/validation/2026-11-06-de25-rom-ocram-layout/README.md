# On-chip firmware state: private linker feasibility

The O2 hardware trial confirmed 7,824/8,192 ROM bytes before the unchanged
one-second deadline. Its [physical receipt](../2026-11-06-de25-rom-o2/physical-result.json)
and verified retained archive preserve the stopped procedure and cleanup.
The next investigation changes memory placement rather than ownership coverage.

Both pinned Agilex 5 vendor trees map 512 KiB OCRAM at physical address zero.
The legacy common-header 0xffe00000 values apply to other devices and cannot
select this board's range. The vendor TF-A linker supports
`SEPARATE_NOBITS_REGION`, placing stacks, BSS and translation-table storage in a
separate zero-initialized region. Its standard entry zeros BSS and selects the
new stack before the cold-provider capture. That ordering must remain part of
the handoff/ownership audit.

A private copy of the measured O2 firmware was rebuilt with that existing
linker mechanism and candidate range `[0x50000, 0x60000)`. Its stack and BSS use
57,024 bytes and fit in 64 KiB, with the ROM transport, USB/master state and cold
provider in that range. The candidate uses a zero-file-byte, non-executable
NOBITS load segment; executable firmware remains in DDR. No ownership C body,
MMIO sequence, cache control, frame size, deadline or polling limit changed.
This mechanically linked artifact was never uploaded, installed or programmed.

The range is **not reserved or hardware-admitted**. The matched SPL's maximum
load image ends at 0x40000 (its actual ELF image ends at 0x252a8); its stack top
is 0x71000, early allocation length is 0x2000, and the boot handoff is at
0x7f000. These observations do not prove the complete live stack, allocator,
firmware or SDM ownership envelope. SPL BSS remains in DDR. The sealed cold
record is deliberately in SPL's loaded OCRAM `.data` (0x21e90 in the measured
ELF), available before DDR BSS initialization; that range must also be preserved.
A full allocation/lifetime audit and an explicit matched reservation are still
required, including initial zeroization and native application exclusion.
No address is declared free merely because the observed symbols fit.

The existing production ELF validator rejects this prototype's non-DDR segment,
as required. The [receipt](result.json) hashes the pinned sources, source-only
experiment, actual ELF symbols and build evidence. It establishes linker
feasibility only: startup, ownership, memory latency, speedup, ROM completion,
normal package admission and DE10-equivalent readiness remain unproved.

Next audit every consumer of the candidate range, bind an explicit reservation
through the matched SPL/BL31/native memory contract, add rejection tests for
wrong ranges and overlaps, and only then admit the exact NOBITS layout for a
fresh bounded serial test. Preserve the present DDR-only rejection meanwhile.
