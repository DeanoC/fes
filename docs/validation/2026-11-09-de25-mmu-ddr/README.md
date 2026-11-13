# Uncached OCRAM and a private DDR cache witness

The preceding [hardware recording](../2026-11-09-de25-mmu-logging-trial/analysis.md)
identified a new CCU decode error, subtype illegal DII access, at the interrupted
OCRAM stack address after cache activation. Image2ed0757748552e49 and all its
operations remain consumed and retired.

The selected format7 policy follows the pinned vendor map: all OCRAM remains
secure, writable, execute-never Normal-NC. Stack/BSS reservations stay unchanged.
Code, tables and initialized private DDR data retain their prior cache and
permission attributes. The aligned64-byte witness moves from OCRAM BSS into
zero-initialized, uniquely file-backed private DDR data. The live adapter checks
the table/data boundary, private limit and complete witness extent before probing.
The image checker also rejects overlap with vector/logging lifetime state.

Actual linked tables differ from the preceding image in exactly16 OCRAM
cache/shareability entries. Every other table word, mapping extent, address,
security, execute and write permission is unchanged. The witness is at8004d180,
inside data8004d000..8004d1c0. The firmware binary is315840 bytes. The complete
FIT still passes its original8MiB upload/seal bound.

Evidence is retained under `out/nano-mmu-ddr-20261109`:

- Focused table/observer checks reject cached OCRAM entries, old/outside DDR
  witnesses, lifetime-state overlap and missing or incorrect boot progress.
- The actual provider model passes launch, tape, return and existing ownership
  failures. Eight relinked invalid memory layouts fail before the memory probe,
  leave its line untouched and deny every owned channel without endpoint I/O.
  A host-only large-address model and unused heap anchor avoid Linux heap/DMA
  collisions; neither is present in Nano firmware.
- Thirty-seven emulated assembly cases include the actual C line-probe algorithm
  and pinned vendor set/way cache helper. The explicit OCRAM-stack case retains
  virtual addresses and NC attributes but aliases its backing to model RAM,
  because QEMU virt has no writable Nano OCRAM. This is never a production map.
- The exact linked production tables pass13 translation/permission queries and
  three deliberate descriptor-failure controls under emulated EL3.
- The linked Nano ELF rejects168 mutations, including nonzero/uninitialized,
  wrong-size, old, shared, unaligned or overlapping witnesses and wrong cache types.
  Parent and complete native/producer/board/firmware software checks pass.
- The original recovery validator accepts unchanged selected sources and exact
  qualified recovery bytes. That existing loader is reused without a rebuild.

The memory-probe algorithm, CPU/owner checks, contained decode logging,
clean/invalidate operations, all reservations and the one-second tape deadline
remain. No ECC control, sticky status, interrupt mask or memory permission is
relaxed. These checks do not qualify physical cache or device behavior. A new
matched one-use trial must prove native boot and then the existing complete
launch/tape/return exercise. Full appliance acceptance remains open.
