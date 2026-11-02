# DE25 contiguous DMA allocation and revocation

Source `6ae883d63f57dc81dc4cb02e8e34564316ebd929`, based on
`0aa9bf13a2a90b7fe614039cebf213bacee4a1be`. No board, SD or QSPI changes occur.

The pinned vendor kernel's default IOMMU coherent allocator may assemble a
page array and map it with `vm_map_pages`. The existing driver refuses that
mapping because page pins can outlive PTE revocation. The driver now requests
`DMA_ATTR_FORCE_CONTIGUOUS` through `dma_alloc_attrs`, passes the same attribute
to `dma_mmap_attrs` and `dma_free_attrs`, and retains its strict mapping checks.
It does not manufacture PFNs/VM flags, accept user DMA addresses or disable
translation/coherence requirements. Allocation may fail without suitable
contiguous backing. This follows the pinned implementation in
`drivers/iommu/dma-iommu.c` and [Linux DMA attributes](https://docs.kernel.org/core-api/dma-attributes.html#dma-attr-force-contiguous).

## Actual AArch64 kernel tests

The frozen source builds and boots the pinned Terasic kernel
`d7d192a9ddd955fe206aa350afed8f82e368a74c` under AArch64 QEMU. All 17 required
`fes-de25-dma` KUnit cases pass, without skips or kernel warnings/errors. The
new case walks every page of the full 8 MiB mapping to verify contiguous actual
PFNs, checks that a page-offset alias observes the same storage, then revokes
and verifies both mappings are inaccessible. Existing cases exercise refused
writable page pins, failed/uncertain DMA lifetime retention, complete-frame slot
ownership, reset/generation mismatches and the mmap/revoke race.

[kunit-checks.json](kunit-checks.json), [kunit-inputs.json](kunit-inputs.json),
[kunit.json](kunit.json), [raw boot output](kunit-raw.log) and
[runner output](kunit-run.log) retain source and output hashes and case results.
The 32 MiB CMA test configuration and actual DMA API are used with a KUnit
device and scripted registers. The virtual machine is `linux,dummy-virt`;
this does not exercise a DE25 fabric master, physical SMMU stream binding,
firewall or physical DDR/display. Production probe refusal remains tested.

The SDK is the authenticated image producer
`0da63d1044cc7ecf9908efba52af17b3e55e6271` archive, SHA-256
`7912076c08d93c8d8c28c0ef81bd406abf6b1acb9e01ccac37d44307e39759a7`.
Its retained image receipt and actual bytes match. The test lock advances to
that C++-capable SDK; the pinned vendor source and builder image are unchanged.
Compiler is Buildroot 2021.02.4 GCC 9.4.0; QEMU is 7.2.22. The separate KUnit
kernel is not the shipping image. The bounded job completes in 67.2 seconds.

## Matching prepared-kernel module

The same driver source builds against the already prepared
`6.12.11-fes-de25-platform` kernel. [module-build.json](module-build.json) binds
the source, SDK, command and resulting ELF64 little-endian AArch64 relocatable
module. Its exact vermagic is
`6.12.11-fes-de25-platform SMP preempt mod_unload aarch64`. The prepared Image,
configuration and Module.symvers hashes are checked before and after this build
and remain unchanged. The module build exits zero in 1.5 seconds. It is not
loaded, and it does not match the currently recorded vendor-kernel release.
The earlier prepared Image itself remains unbooted.

All six affected commands pass on the unchanged source head 6ae883d63,
including the parent regressions, whitespace and generated consistency checks.
The [result](affected-result.json), [job identity](affected-job.json) and
[compressed log](affected.log.gz) retain that exact-head run. Consistency checks
37 generated consumers and 40 fixture copies. This does not replace kernel or
physical hardware testing with Python/source tests.

The kernel test Image/configuration/objects and matching module, receipts and
logs are retained independently, read-only, at:

```
/home/deano/fes/out/artifacts/de25-contiguous-dma/6ae883d63f57dc81dc4cb02e8e34564316ebd929/
```

## Remaining normal-kit work

The production binding stays closed. The generated FPGA graph still has three
incoming F2SDRAM masters: scanout, the DDR probe and the external debug master.
A Linux device cannot claim isolated scanout ownership of their shared stream.
Real device/domain/context lifetime, bounded physical control qualification,
unsafe detach/reconfiguration refusal and reset/failed-load cleanup remain open.
Managed runtime/MENU and normal ZX81 catalog/session/persistence/update/recovery
acceptance are still required for a DE10-equivalent development kit.
