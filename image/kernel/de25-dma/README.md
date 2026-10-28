# DE25 Linux DMA ownership diagnostic

This kernel module implements Linux-owned coherent storage, grant file
operations and DMA lifetime rules. Its production platform probe always returns
`EOPNOTSUPP`. It registers no allocation device and accepts no DT, module or
userspace option that qualifies DE25 hardware. The actual fabric device, SID,
translation, physical firewall and boot/reset epoch binding remain missing.
Nothing installs or loads this module into the DE25 image or runtime.

The implementation has a separate ABI from the ARMv7 `fes-window` driver. It
does not map a reserved physical window, use `/dev/mem`, invoke PL310 cache
operations, or accept a caller-provided physical/DMA address.

## Allocation and file operations

The private kernel constructor receives the actual DMA device and a
driver-controlled transport backend. The KUnit suite is its only current
consumer. It sets that device's 32-bit coherent DMA mask, allocates one 8 MiB
region with `dma_alloc_coherent`, and maps it with `dma_mmap_coherent`. CPU and
DMA addresses remain separate kernel values. The allocation must fit wholly
in the selected 32-bit DMA space and meet the shared grant alignment; lower
IOVAs remain valid. Geometry comes from the generated C consumer of
`fes.fabric.de25-ddr-probe`, including two 4 MiB slots and fixed 720p BGRX8888.

Each grant uses a unique anonymous inode. Shared, non-executable mappings may
cover bounded portions of that grant. `INFO` returns geometry, generation and
lifetime state, without addresses. `PUBLISH` takes only the current generation,
slot and nonzero sequence. It builds the descriptor from kernel-owned DMA
state and issues a DMA publication barrier before calling the backend. This
is a read-probe descriptor, not a display submission or frame-completion API.
There is one exposed operation per grant; this does not implement MENU scanout.

The mapping implementation currently accepts only an eager `VM_IO | VM_PFNMAP`
mapping returned by the DMA API, with no architecture fault handler. It refuses
page-backed/`VM_MIXEDMAP` IOMMU mappings: page pins and aliases can outlive PTE
revocation, and no scheme to revoke those aliases has been established here.
It does not manufacture PFN flags or derive PFNs from DMA addresses. DMA
translation support and revocable userspace mapping support are separate gates.

The private backend must supply a nonzero external reset epoch, an ordered
start operation, an idempotent stop-new-requests operation and a drain operation
that proves all accepted traffic retired in that same epoch. No DE25 hardware
backend currently supplies those operations. Userspace cannot assert a drain
proof. A failed start may already have exposed the allocation and therefore
pins it exactly like a successful start.

## Revocation and lifetime

`REVOKE` first rejects future mappings and publications. It synchronizes past
any mmap callback that installed PTEs before its VMA was linked into the grant
inode: every mm was recorded under the grant lock, and revocation takes each
mm's mmap write lock without holding that grant lock. It then removes mappings
through the unique inode. Revoked mappings fault with SIGBUS; mmap cannot
recreate them. Only afterward does it stop and drain the backend.

Exposed DMA holds a separate allocation reference and module pin, in addition
to the grant file and DMA device references. A failed stop/drain, unreadable
epoch, or changed epoch retains those references and marks the grant uncertain.
Last close retries the idempotent stop/drain; it cannot free exposed storage
without the proof. No timeout or client-close path declares outstanding traffic
gone. Actual reset recovery and device detach qualification remain future work.

## Off-board kernel tests

The focused KUnit configuration is:

```text
CONFIG_MMU=y
CONFIG_OF=y
CONFIG_KUNIT=y
CONFIG_FES_DE25_DMA=y
CONFIG_FES_DE25_DMA_KUNIT_TEST=y
```

The test kernel also needs its normal DMA allocation/mapping implementation;
the pinned AArch64 BSP enables DMA/CMA. Kernel integration sources this
directory's Kconfig and Makefile. The DE25 kernel test runner builds a separate
test kernel; its configuration is not the shipping kernel configuration.

The `fes-de25-dma` suite calls the actual allocator, file operations, DMA mmap
and release paths on a KUnit device with the kernel's DMA API. A private
scripted transport checks descriptor ownership and failed-start/failed-drain
retention. VMA tests use a real temporary mm and kernel uaccess to verify
coherent mappings, ioctl copying, revoke-before-stop ordering, denied remapping
and post-revoke faults. They verify the accepted mapping form and refusal of
writable page pinning. A controlled test-only pause reproduces the mmap
callback/VMA-link race with separate mapper and revoker kernel threads. Test
hooks compile out when `CONFIG_FES_DE25_DMA_KUNIT_TEST` is disabled.

These are kernel software checks on a test device. They do not emulate or
qualify the DE25 DMA master, SMMU, firewall, DDR, kernel boot, HDMI, runtime or
interactive MENU. Loading against the board's existing kernel additionally
requires matching kernel configuration and symbol-version evidence.
