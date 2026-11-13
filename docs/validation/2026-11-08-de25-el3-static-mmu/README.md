# DE25 static EL3 memory/cache table prototype

Scope: hardware-independent firmware layout work toward removing the initial
tape-transfer bottleneck. Base source is `cbfb81e6a5b90252e42c641af550581931202879`.
The existing board image remains unchanged. This is a static-link check, not an
enabled cache profile, speed measurement, boot result or ordinary core acceptance.

The latest native tape launch reached the original one-second limit. Its
ownership/USB-state samples identify substantial CPU-side overhead. The pinned
vendor `bl31_plat_enable_mmu` is empty, and current native guards require EL3
MMU/data-cache off. The new tool emits immutable tables as a separate file-backed
DDR section, avoiding dynamic table allocation in the constrained OCRAM area.

The linker determines code/data boundaries and table pointers from final symbols.
The verifier independently reconstructs every descriptor, checks every stored
byte and retains the existing cold-chain, load-segment and stack/BSS checks.
No current CPU guard or boot producer was changed.

| Region | Proposed EL3 mapping |
| --- | --- |
| Linked firmware code/constant data | Secure Normal WB/WA, read-only, executable |
| Six translation tables | Secure Normal WB/WA, read-only, execute-never |
| Initialized firmware data and private OCRAM `0x50000..0x60000` | Secure Normal WB/WA, writable, execute-never |
| Other OCRAM through `0x80000` | Secure Normal noncacheable, writable, execute-never |
| DDR `0x80200000..0xc0000000` | Non-secure Normal noncacheable, writable, execute-never |
| PSS and lightweight FPGA windows | Non-secure Device-nGnRnE, execute-never |
| MPFE, CCU, exact 1 MiB GIC window | Secure Device-nGnRnE, execute-never |
| Other addresses, including DDR above the matched 1 GiB | Unmapped |

Shared configuration, ROM, tape, reader and USB buffers remain noncacheable.
Identity mappings introduce no second address for private or shared memory.
This mapping does not establish physical firewall isolation or live ownership.

The attribute definitions follow the pinned TF-A v2 descriptor conventions,
including the EL3 AP[1] reserved bit and outer-shareable device/noncacheable
regions. Six compile-time assertions were cross-built with the actual selected
vendor headers and compile flags. See the primary [TF-A translation-library design](https://tf-a.docs.trustedfirmware.org/en/latest/components/xlat-tables-lib-v2-design.html)
and [Arm memory-management examples](https://documentation-service.arm.com/static/64d5f38f4a92140bb31d7f03).
The device MAIR byte deliberately uses `MAIR_DEV_nGnRnE`, preserving the
MMU-off device-write completion setting described by the primary [Arm memory-model guide](https://developer.arm.com/-/media/Arm%20Developer%20Community/PDF/Learn%20the%20Architecture/Armv8-A%20memory%20model%20guide.pdf?revision=58b1dd0a-3800-4218-b21a-f95a0332034c).
The first prototype used the vendor library's nGnRE default; that is superseded
before runtime enablement. This refinement changes the selected MAIR value, not
the already-linked table bytes or addresses. Final MAIR is `0x4400ff`; the
proposed 4 KiB, 32-bit identity regime uses TCR
`0x80803520`. Live CPU feature admission and register checks remain to implement.

The real selected AArch64 objects from the inlining build were relinked in a
new private directory; original object, linker, receipt and ELF hashes remained
unchanged. The diagnostic ELF is
`af9cbc0a423ed97b29598a767be3bc400cdcea3c79e929c18e365cf8452de434`.
The complete existing cold RAM/chain verifier passes on that ELF.

- Tables occupy `0x8003c000..0x80042000`: 24,576 file-backed bytes.
- Initialized data moves to `0x80042000`; the image ends at `0x80043000`, within
  the existing `0x80200000` exclusion limit.
- Private OCRAM still ends at `0x5e340`, leaving 7,360 bytes in its reservation.
- The descriptor verifier and independent host table walk cover permissions,
  all reachable leaves, identity addresses, region boundaries and unmapped holes.
- Negative tests reject descriptor corruption, wrong table placement, writable
  table sections, missing/truncated/overlapping load storage and changed physical
  addresses. A host ELF cannot qualify as a cold AArch64 image.

The first real-image inspection rejected the vendor's combined RWX load-container
flag. All descriptor bytes already matched. The verifier now checks the new
section's exact allocated/read-only storage and its independent runtime RO/XN
descriptors. It does not interpret the vendor's container flag as the future MMU
permission. A regression covers this distinction; original inspection failures
are preserved. Two standalone header compilation attempts lacked vendor include
paths; the completed check uses the selected build's complete compile flags.

Local evidence is under `out/nano-el3-static-mmu-20261108`: generated linker,
actual ELF/map, input hashes/link command, static table proof, selected-header
assertions and software check logs. The original object inputs remain in the
previously retained inlining build; no board or programmer operation occurred.

Final checks: eleven focused tests pass; the final parent run passes 1,763 tests
with three existing skips. Generated-consumer, default native runtime,
producer-identity and firmware/RTL integration checks passed before the single
MAIR-literal refinement. Those default integrations do not enable or consume
this new profile. The final literal has its own vendor-header cross-check,
real-ELF descriptor proof and fresh parent/focused runs. The broad FPGA sweep
reached its unchanged 300-second cap without assertion failures; its completed
module prefix is retained, and all 87 unfinished modules passed separately under
the same per-module cap. The complete catalog is covered, with no limit increase.
The original timeout remains recorded. See `checks.json` and `table-proof.json`
beside this note for the bounded software and static-layout results.

Next: select this profile through the matched producer chain, implement bounded
enable/admission and exact live register checks, and reconcile entry/exit cache
maintenance with that profile. Only then prepare a new bounded tape launch/return
test. No improvement or complete core-loading acceptance is claimed here.
