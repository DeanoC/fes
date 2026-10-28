# DE25 Linux DMA ownership and display diagnostic

This module implements Linux-owned coherent storage, revocable grant mappings
and the `fes.fabric.de25-scanout` display lifecycle. Its production platform
probe always returns `EOPNOTSUPP`. It registers no allocation device and accepts
no DT, module or userspace option that qualifies DE25 hardware. The actual
fabric device, SID, translation, physical firewall, ordered clock-domain access
and boot/reset epoch binding remain missing. Nothing installs or loads this
module into the DE25 image or runtime.

The implementation has a separate ABI from ARMv7 `fes-window`. It does not map
a reserved physical window, use `/dev/mem`, invoke PL310 cache operations, or
accept a caller-provided physical/DMA address.

## Allocation and ABI version 2

The private constructor receives the actual DMA device and an exclusively owned,
driver-controlled register transport. The KUnit suite is its only consumer. It
sets the device's 32-bit coherent DMA mask, allocates one 8 MiB region with
`dma_alloc_coherent`, clears it, and maps it with `dma_mmap_coherent`. CPU and DMA
addresses remain distinct kernel values. The complete allocation must fit the
32-bit DMA space and meet the generated grant alignment; low IOVAs remain valid.
Geometry comes from the generated C consumer of `fes.fabric.de25-scanout`:
two 4 MiB slots containing fixed 1280×720 BGRX8888 frames, stride 5120 bytes.

Each grant has a unique anonymous inode. `INFO` returns geometry, a nonzero
64-bit allocation generation, cached lifetime/frame state and the available
back slot, without addresses. `PUBLISH` accepts version 2, that generation, a
nonzero increasing 32-bit sequence, a slot and zero reserved bits. Old version 1
requests are refused. Generation allocation saturates rather than wrapping;
sequence exhaustion requires revocation and a new allocation.

Initially the logical front slot is zero and the first submission must use slot
one. The kernel constructs all grant, generation, sequence and slot register
writes, issues `dma_wmb`, orders those writes, pins the allocation, then submits.
The first SUBMIT configures and starts fetching the intended slot atomically.
There is one pending frame. Acceptance does not mean the frame was displayed,
and neither the previous front nor pending slot is available for reuse.

`POLL` requires version 2, the current allocation generation and zero reserved
bits. It obtains a frozen hardware snapshot: read the serial, issue SNAPSHOT,
order it, wait a bounded number of accesses for the serial to advance modulo
32 bits, read the frozen fields, and recheck the serial. It validates the full
64-bit generation, immutable DMA grant, accepted sequence, current/displayed
identities, pending slot and errors. Only the exact submitted sequence/slot
reported fully displayed, current, without pending or frame faults, releases
the previous front slot. `INFO` then reports that previous slot as the back
slot. Subsequent PUBLISH operations alternate slots while the allocation remains
exposed. INFO itself performs no register access and does not complete a frame.
A failed/contradictory snapshot prevents reuse and marks the grant uncertain.

The mapping covers the allocation, so the client must draw only into the
reported back slot and stop drawing it before PUBLISH. This ABI enforces slot
ownership at submission; it does not make individual front-slot PTEs read-only.
A complete frame acknowledgement never releases the allocation's DMA lifetime
pin: continuous refresh may still read the displayed slot.

## Mapping and transport boundaries

Shared, non-executable mappings may cover bounded portions of the grant. Only
an eager `VM_IO | VM_PFNMAP` mapping returned by the DMA API, with no mapper
fault handler, is accepted. Page-backed/`VM_MIXEDMAP` IOMMU mappings are refused:
page pins and aliases can outlive PTE revocation. The implementation does not
manufacture PFN flags or derive PFNs from a DMA address. Translation support
and revocable userspace mapping support are separate gates. The fresh mmap
callback clears the kernel's empty generic `vma_dummy_vm_ops` sentinel before
invoking the DMA API; real mapper-installed operations still cause refusal.

The private backend supplies a nonzero external reset epoch and bounded,
ordered `read32`, `write32` and `io_fence` operations. No physical MMIO backend
is implemented or admitted. A callback error after SUBMIT may already have
enabled DMA and retains exposure. Userspace cannot supply a drain proof.
The kernel caller must retain callback code/context, exclusive engine ownership
and device DMA/domain resources until final grant destruction, including an
uncertain exposure. `get_device` does not retain devm resources or a detached
translation domain. A constructor refuses an already configured engine; it
never clears a previous owner's grant.

## Revocation and lifetime

`REVOKE` rejects future mappings and publications. It synchronizes past every
mmap callback that installed PTEs before its VMA was linked into the grant inode:
each mm was recorded under the grant lock, and revocation takes each mm's mmap
write lock without holding the grant lock. It then removes mappings through the
unique inode. Revoked mappings fault with SIGBUS; mmap cannot recreate them.
Only afterward does the kernel issue STOP and request a frozen snapshot.

The proof must retain the old nonzero generation and immutable grant in the
same external epoch, report drained, have no running/pending work and zero
outstanding traffic. Incomplete drain returns `EAGAIN`; callers can retry.
Frame fault state does not replace this drain proof. Reset's zero generation
cannot prove an old operation drained. After consuming the old proof, the owner
issues CLEAR and verifies the empty, unconfigured, drained state before the
allocation becomes releasable and the engine can accept a new grant.

Exposed DMA holds an independent allocation reference and module pin alongside
the file, inode and device references. Errors, missing/changed epochs, failed
drain or failed cleanup retain these references and mark uncertainty. Last close
retries stop/drain; it cannot free uncertain exposed storage. No timeout, frame
completion or client-close path declares outstanding traffic gone. Actual reset
recovery and device detach qualification remain future work.

## Off-board kernel tests

The focused KUnit configuration includes `MMU`, `OF`, `KUNIT`, `CMA`, `DMA_CMA`,
32 MiB CMA, `FES_DE25_DMA=y` and `FES_DE25_DMA_KUNIT_TEST=y`. The runner builds a
separate pinned AArch64 test kernel; this is not the shipping kernel config.

The `fes-de25-dma` suite calls the actual allocator, file operations, DMA mmap
and release paths on a KUnit device using the kernel DMA API. A private scripted
register device exercises the same descriptor writes, serial handshake and
snapshot decoder used by the driver. Tests cover ordered first submission,
acceptance versus full-frame display, alternating slot reuse, contradictory
completion, frame faults, generation/reset mismatch, counter exhaustion,
bounded/unstable snapshots and failed-start/drain retention. VMA tests use a real
temporary mm and kernel uaccess for INFO/PUBLISH/POLL, revoke-before-stop,
post-revoke faults, refused writable page pins, denied remapping and the
mmap-callback/VMA-link race. Test hooks compile out when the test config is off.

These are kernel software checks with a scripted register device. They do not
emulate or qualify the DE25 fabric master, SMMU, firewall, DDR, clock crossing,
kernel boot, HDMI, runtime or interactive MENU. The separate RTL and generated
interconnect simulations cover their own artifacts. Loading against the board's
kernel additionally requires matching config and symbol-version evidence.
