# DE25 Linux DMA ownership and display diagnostic

This module implements Linux-owned coherent storage, revocable grant mappings
and the `fes.fabric.de25-scanout` display lifecycle. Its production platform
probe returns `EOPNOTSUPP` in ordinary builds. The default-off
`FES_DE25_DMA_PLATFORM` built-in candidate connects a permanent device owner,
guarded transport and allocation control; it does not enable normal FES runtime
admission or qualify the physical stream/firewall. Nothing installs or loads
this module into the DE25 image or runtime.

The implementation has a separate ABI from ARMv7 `fes-window`. It does not map
a reserved physical window, use `/dev/mem`, invoke PL310 cache operations, or
accept a caller-provided physical/DMA address.

## Allocation and ABI version 2

The private constructor receives the actual DMA device and a driver-controlled
register transport. Its mandatory `acquire` callback must exclusively lease the
engine and retain context/MMIO/device-domain resources before the constructor
reads even the epoch or identity. A second allocation is refused while that
lease is held, including while the first engine is still unconfigured. The
matching `release` runs exactly once after storage and mapping references are
destroyed, or on a constructor error after successful acquisition. Acquisition
failure performs no register access and does not release another owner's lease.
The KUnit suite and default-off permanent owner candidate consume this
constructor. It sets the device's 32-bit coherent DMA mask, allocates one 8 MiB region through
`dma_alloc_attrs` with `DMA_ATTR_FORCE_CONTIGUOUS`, clears it, and maps it
with `dma_mmap_attrs` using that same attribute. Release passes the same attribute
to `dma_free_attrs`. The pinned IOMMU implementation uses contiguous backing
through this path, instead of a page array mapped by `vm_map_pages`. Allocation
can fail if suitable contiguous storage is unavailable; it does not fall back
to a mapping shape the driver cannot revoke. The strict I/O-PFN mapping checks
still apply. See [Linux DMA attributes](https://docs.kernel.org/core-api/dma-attributes.html#dma-attr-force-contiguous). CPU and DMA
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
ordered `read32`, `write32` and `io_fence` operations. The private
`de25_dma_guard` transport implements `readl`/`writel` with explicit barriers and
checks local guard identity, version, timeout, epoch, idle status and sticky
faults before and after each endpoint access. A timed-out proxy's dummy zero is
an error, never endpoint evidence. A faulted context stays poisoned even if
registers later appear healthy. Endpoint operations cannot address the guard's
epoch/configuration window. Claiming a fresh guard writes its nonzero epoch
once and verifies it; it never overwrites or adopts an existing owner's epoch.

This transport is not yet wired to an admitted physical provider. Its caller
must supply a mapped, live system-clock guard window and retain the full
resource/reconfiguration lease for all calls. It cannot make an unconfigured
fabric or dead HPS/bridge/guard clock safe to probe. Epoch selection belongs to
the kernel loader's fresh-configuration lifecycle; it cannot assert old DMA
drained. No userspace or DT flag bypasses device/domain qualification.
A callback error after SUBMIT may already have
enabled DMA and retains exposure. Userspace cannot supply a drain proof.
Callbacks are private to the same kernel module. The provider must implement
real context/device/domain retention and prevent detach/reconfiguration for the
whole acquired lease, including uncertain exposure. `get_device` does not
retain devm resources or a detached translation domain; adding lease callbacks
alone does not qualify a physical provider. A constructor also refuses an
already configured engine; it never clears a previous owner's grant.

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
drain or failed cleanup retain these references and mark uncertainty. Final close
makes one stop/drain attempt; it cannot free uncertain exposed storage. The
private device owner can obtain an opaque, independently referenced grant handle
while its file is live, before passing the file to a client. The handle retains
the allocation and module after the last client closes. It lets the owner retry
the same revocation path without retaining or dereferencing a dead file. A
successful old-epoch drain and cleanup is still required. Dropping the handle
never discards the separate exposure pin, and does not revoke a live client.
The owner must release its own handle after recovery; successful drain does not
release the backend lease while storage references remain. This is a private
kernel API, not a new userspace recovery override.
No timeout, frame completion or client-close path declares outstanding traffic gone. Actual reset
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
post-revoke faults, contiguous PFNs across the full allocation, an offset alias
that sees the same storage and is revoked together, refused writable page pins,
denied remapping and the mmap-callback/VMA-link race. A forced mapper refusal
verifies a newly recorded `mm` reference is removed on the error path. Test hooks
compile out when the test config is off.

Lease cases additionally check refusal of a second unconfigured allocation,
missing ownership callbacks, failed acquisition before all register effects,
constructor-error unwinding and retention through uncertain drain. Successful
drain alone cannot release the backend while its allocation still exists.
The release observer verifies that storage destruction precedes lease release.

Guard cases exercise the real `readl`/`writel` path on kernel test RAM and a
scripted raw-register device for fault/reset interleavings. They cover missing
identity/timeout metadata, foreign or ignored epoch writes, pre-existing busy
state, post-access response faults, reset during reads and sticky software
poisoning. A constructor case connects the guarded transport through the
exclusive backend lease to the actual DMA allocator. These tests do not model
ARM Device-memory ordering, physical HPS bridge liveness or FPGA transactions;
the separate RTL/generated-interconnect tests cover their own proxy behavior.

These are kernel software checks with a scripted register device. They do not
emulate or qualify the DE25 fabric master, SMMU, firewall, DDR, clock crossing,
kernel boot, HDMI, runtime or interactive MENU. The separate RTL and generated
interconnect simulations cover their own artifacts. Loading against the board's
kernel additionally requires matching config and symbol-version evidence.

The test dependency lock selects the authenticated AArch64 SDK from image
producer `0da63d1044cc7ecf9908efba52af17b3e55e6271`, including C++ runtime
support. This changes the SDK hash, not the pinned vendor kernel or builder.

The [contiguous allocation and revocation record](../../../docs/validation/2026-10-31-de25-contiguous-dma/README.md)
retains all 17 passing AArch64 KUnit cases and the matching prepared-kernel
module build. Physical stream/domain and normal-kit admission remain open.

## Permanent device owner candidate

`FES_DE25_DMA_PLATFORM` defaults off and requires a built-in driver, IOMMU API
and OF overlays. It implements `/dev/fes-de25-scanout` (root only, also requiring
CAP_SYS_RAWIO): OWNER_ALLOC returns a CLOEXEC grant file, and OWNER_REAP revokes
its grant through the existing checked drain path before releasing the owner's
recovery handle. Existing client storage references still hold the resource
lease, even after a successful reap. A failed reap retains the handle and lease.
An abandoned handle can be reaped by a later owner-control connection.

The base device must be permanent, outside overlays, with exactly one 256-byte
control resource at the generated architectural base plus peripheral offset,
exactly one `iommus` entry with stream ID 13, and a translated DMA/DMA-FQ domain
whose group contains only this device. `fes,fpga-region` points to a permanent
root-level `fpga-region` with a direct `fpga-mgr` and a fixed bridge topology.
An absent `fpga-bridges` list is supported, matching the pinned Terasic Agilex5
base tree and SDM-managed configuration sequencing. SMMU, region, manager and bridge platform suppliers and their bound ancestors must be
built-in drivers with sysfs bind/unbind disabled. The owner also disables these
attributes. The built-in candidate also checks parent and transitive supplier
bindings through the pinned driver-core device-link API under SRCU. It includes
the pinned kernel's private driver-core header only for this built-in option;
ordinary module builds do not depend on non-exported core APIs. Unbound or
busy suppliers defer, and removable/dynamic suppliers are refused. This preserves
ordinary driver-core default-domain ownership and
prevents supported unbind/removal from dismantling resources under allocations.
The current kernel does not meet that supplier contract and remains refused;
there is no module parameter or DT waiver. Kernel-internal forceful teardown or
privileged out-of-band JTAG is outside this supported lifecycle.

Probe reserves/maps the control resource without reading the fabric. A priority
100 OF-overlay notifier runs before the stock FPGA-region notifier. PRE_APPLY
and PRE_REMOVE reject while any allocation lease remains, including an empty
engine, a closed client with uncertain drain, or revoked storage still held by a
client. Otherwise they mark the owner unavailable before bridges can be disabled
or devices detached, and retire the old guard. A later failure leaves it
unavailable. Ancestor overlays cannot mutate its permanent topology; per-image
bridge, manager, compatible, status and range overrides are refused. Dedicated image overlays target the region itself.

Only POST_APPLY of an overlay that programs firmware and describes a
`fes,de25-linux-dma-scanout` child constructs and claims a new guard. Fresh
nonzero epoch tokens do not wrap. Unknown configuration is never probed or
adopted. This compatible describes hardware, as other Linux device bindings do;
it is not artifact authentication. The supported runtime must construct the
overlay from its admitted package and matching bitstream. Device-tree metadata
cannot prove a wrong or unconfigured bridge safe to probe.

The owner tests call the actual allocation/reap and notification decision paths
with scripted guard/endpoint registers and a test device. They verify early
exclusion and post-client-close recovery. They bypass physical binding only in
KUnit fixtures; they do not execute an actual OF overlay or qualify a domain,
stream, permanent supplier binding, physical clock/bridge, or board lifecycle.
Still required before activation: the pinned kernel supplier integration,
base-tree owner binding, admitted loader/package integration, complete overlay
and concurrent operation tests, and physical acceptance.
