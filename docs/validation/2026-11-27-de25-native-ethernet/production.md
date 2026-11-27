# Ethernet on the paired C64/MENU launcher

On TRWU8CBL the retained 4 GB C64 card uses PIO SD, launcher boot ID
`af4407e48b21634e`, FIT `be9a5f82a0ff769676e75a64583b19f68f7fbcb5896405c5a574512d91c6fcbb`
and BL31 `d63e6026c7bcd9741e1055ca1ea829164cdf5e14675b5431fd5ec9f74a31e413`.
The SPL authenticates the size and SHA-256 of the SD FIT before it passes
control to firmware. A production trial can preserve this card and QSPI by
booting a separately prepared PIO card with its matching volatile phase-one
SOF. The new card needs the matched firmware, Ethernet-enabled launcher and
provisioned FogCast target UUID and bearer. [C64 startup and handback](README.md) ·
[Native FIT check](../../../platform/embedded/nano/spl_cold.c)

## Memory ownership

The following half-open physical ranges come from the retained C64/MENU
entry, SDK and BL31 receipts under Powerboat's
`out/c64-picker-fault-requalification/` in the `de25-c64-selector-pr`
worktree. The entry receipt SHA-256 is
`172832a94221054f31860c1af7264bef57c5c650bb35cd566646036d17754101`.
The SDK receipt is `4e883e4b3bae89bc7455cdd4a3f2fa86a653b33c6bad1a8fa07ae49d69f86a73`
and the BL31 receipt is `22c1a200180d430323e12d3838e829f0595f50f404fd2ddf9aceccf0f0411028`.

| Range | Owner or reservation |
| --- | --- |
| `0x80000000–0x82000000` | Firmware and initial U-Boot reservation. The matched BL31 image ends at `0x8005f000`. |
| `0x82000000–0x84000000` | SPL FIT load and recovery scratch |
| `0x84000000–0x94000000` | Native runtime RAM, including the entry decoder |
| `0x94000000–0x94010000` | SDM SMMU tables, command queue and event queue |
| `0x94800000–0x95000000` | FPGA configuration DMA source |
| `0x95000000–0x95800000` | Reader and MENU reservation |
| `0x95800000–0x95a00000` | Tape and metadata |
| `0x95a00000–0x95c00000` | Optional SD ADMA allocation in the current producer |
| `0x95c00000–0x95e00000` | Candidate Ethernet pool, 2 MiB aligned |
| `0x96080000–0x98080000` | Maximum native entry staging envelope. This C64 build ends at `0x96861000`. |
| `0x99000000–0x9a000000` | Current matched producer's verified FIT copy |
| `0xb0000000–0xc0000000` | High SPL/U-Boot memory reservation |

A build can test the proposed Ethernet pool by adding its exclusion to the
entry producer and confirming disjointness in the emitted launch receipt,
EL1 uncached map and firmware map. The production XGMAC API currently uses
identity DMA addresses. Restrict SID 1 access to the actual descriptor and
packet pages within the pool. The standalone diagnostic's physical frame
and descriptor pages at `0x94800000` overlap the C64 configuration source.
[Saved FIT geometry](../../../platform/embedded/nano/fit_layout.py) ·
[Native entry exclusions](../../../platform/embedded/entry/build.py)

## Firmware and wire qualification

Bind the existing EL3 MAC lease to the accepted boot owner and epoch. Keep
one owner of the nonsecure SMMU command queue, stream table and invalidations
across MAC SID 1 and the SDM configuration stream. The protected control
pages must be accessible to the SMMU's nonsecure table walker while
remaining outside the client DMA mappings and EL1's writable regions.
Keep secure firmware metadata separate. The current SDM source gate checks
for a completely uninitialized MAC. Supporting an initialized, contained
MAC requires positive verification of its retired grant and the selected
SDM mapping under the existing firmware lock. [EL3 MAC lease](../../../platform/embedded/firmware/nano_mac.h) ·
[SDM ownership checks](../../../platform/embedded/firmware/nano_sdm_source_platform.inc) ·
[Arm SMMUv3 security states, section 3.10](https://documentation-service.arm.com/static/6813b2bbefb0f21122c144c2)

`ProgramService` already pauses the network before a program transition,
resumes it after a successful return to Ready or Running, and retains it
stopped at Park. Its secure Stop must establish device and interconnect
quiescence before SDM reconfiguration. Following the SDM's verified release,
each MAC restart needs a fresh Acquire and Confirm. The PIO card can serve
the launcher throughout without allocating an SD ADMA window.
[ProgramService network lifecycle](../../../platform/embedded/tamago/nativefabric/program_network.go)

After a qualified TX, the next bounded hardware test is an independent
host-to-CBL unicast frame with fresh bytes, a single RX descriptor and a
writable buffer inside the reserved pool. Compare the hardware length,
first/last/error bits and bytes with the emitted frame. Check the adjacent
guards. Run both directions, stop them, observe fresh channel stop
acknowledgements, drain both MTL queues, fence/revoke SID 1 and check for
late writes or SMMU events before reuse. Repeat after a fresh grant.
Full launcher validation also crosses MENU → C64 → MENU with PIO SD and USB
active as applicable, followed by mDNS discovery and authenticated target
status from a separate FogCast host.
