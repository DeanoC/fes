# Pinned ARM64 mappings

This package checks the live EL1 translation state installed by the pinned
TamaGo ARM64 runtime. Both QEMU and the Nano use it for their explicitly
reserved uncached DMA windows. It supplies no physical RAM reservation,
firmware ownership or core-activation permission.

The Nano startup now calls allocation-free `ExcludeFirmwareRAM` before its
first `go_hwinit` checkpoint. This primary-CPU ARM routine checks the pinned
CPU state, table-page translations, low table pointers and all sixteen original
Device descriptors before removing `[0x50000,0x60000)`. It cleans the changed
PTEs, invalidates the EL1 TLB, and requires actual read/write level-3 translation
faults at both ends of every page. It restores PAR_EL1, uses no stack frame or
calls, and never reads or writes the excluded target memory. Failure halts the
Nano boot; the attempt cannot be replayed. CPU/DMA ownership is a boot obligation.

Before device mappings, `RequireFirmwareExclusion` independently checks those
invalid descriptors and live translations under the board's mapper lock. Every
leaf of the complete pinned table tree must also remain an identity mapping,
with two matching samples, so another virtual address cannot alias this region.
Every
subsequent mapper observation, including both sides of DMA publication, includes
the same negative mapping. Restored access or an uncertain observation retires
the mapper; no buffer is released. Attribute initialization accepts these exact
sixteen holes only after that proof; other missing pages still reject. The
read-only `Inaccessible` kind accepts only this complete fixed range and grants
no physical RAM ownership. `Observation.Bytes` includes inspected excluded bytes.

The source-bound Nano CPU-resource receipt is now version 2, naming
`firmware_base` and `firmware_bytes` as an exclusion and including them in the
reported observation. The current decoder requires both fields and rejects the
older resource-only contract. Earlier physical observations remain evidence
for their original inputs; they do not qualify this startup change.

For failed address translations, the check requires PAR_EL1.FST to identify a
level-3 translation fault, with F set and S/PTW clear; permission, external and
stage-2 faults cannot substitute for an invalid stage-1 leaf. See Arm's primary
[Cortex-A53 register reference](https://documentation-service.arm.com/static/5e9075f9c8052b1608761519).

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
