# Pinned ARM64 mappings

This package checks the live EL1 translation state installed by the pinned
TamaGo ARM64 runtime. Both QEMU and the Nano use it for their explicitly
reserved uncached DMA windows. It supplies no physical RAM reservation,
firmware ownership or core-activation permission.

`Inspect` requires the pinned 39-bit virtual/40-bit physical layout, enabled
MMU and caches, the expected TTBR0, and the Device/WB/optional uncached MAIR
attributes. It follows only the five table pages reserved before the linked
image. Before dereferencing a table page it queries the CPU's read and write
translations and requires identity, normal cached memory and shareability.
It then checks exact leaf addresses, access permissions, memory type and
execute permissions. Two complete descriptor samples must match; CPU state
must remain unchanged around each leaf. Range and traversal limits bound the
work, with at most 8,192 table reads and translation probes per sample.

The native implementation uses `AT S1E1R` and `AT S1E1W`, checks `PAR_EL1`,
and restores the previous PAR value. These instructions query translation
without reading or writing the target memory. They check EL1 stage 1;
stage 2 being disabled remains an obligation of the matched boot entry.
See the primary [Arm memory-management guide](https://developer.arm.com/-/media/Arm%20Developer%20Community/PDF/Learn%20the%20Architecture/LearnTheArchitecture-MemoryManagement-101811_0100_00_en.pdf)
and [Cortex-A55 register reference](https://documentation-service.arm.com/static/5e7e09f6a3736a0d2e862d2f).

`Mapper` serializes every managed mutation and observation. Before first
assigning MAIR slot 2 it traverses the complete bounded pinned tree twice and
requires that no active descriptor already uses that slot. It converts only
the selected identity-mapped Device blocks outside Go RAM. Each conversion
uses break-before-make, cache clean, barriers and TLB invalidation, followed
by another hardware translation check. Any uncertain change or failed
observation retires that mapper until reboot; buffers remain reserved.

The Nano transport brackets actual FPGA register operations and configuration
SMCs with identical mapping observations. Configuration copies check the
mapping before writing, after copying and before completion releases the
lease. The reader pattern also checks its full reserved mapping around the
copy. The cold loader reports a combined observation of Go RAM, control,
configuration DMA and optional reader memory before the first core transfer.
That report retains the existing secure owner and generation and grants no
ordinary runtime admission.

Host tests inject bad CPU state, aliasing, permissions, changing descriptors,
attribute conflicts, translation faults and uncertain publication. The QEMU
`mapcheck` program executes the actual assembly, verifies the heap, UART and
DMA mappings and checks an unaligned DMA copy. Neither establishes Nano
DDR-controller readiness, physical cache coherency or board acceptance.
The separate [Nano hardware observation](../../../../../docs/validation/2026-11-03-de25-cpu-mapping/README.md)
checks the real EL1 mappings and runs two guarded fixed-package transfers and
owned Run/returns. Its scope is recorded against exact native and FPGA inputs;
general RAM/clock qualification and normal runtime admission remain open.
