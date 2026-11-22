# Slice 4d: the kernel owner's window (design)

Slice 4d of #2029 (plan #2109). It lands on main on its own; the pc386
runtime backend that uses it is #2115.

This is the image change P2 approves, built and tested only. Building an
image for a kit or flashing one waits for Deano's OK at that time.
References are to `image/kernel/de25-dma/` at #2036's head `af58ebed2`.

## The owner today

- **One permanent platform device**, `dma-owner@20010200`, with
  `iommus = <&smmu 13>`. It is alone in its IOMMU group, with a DMA (or
  DMA_FQ) domain (`de25_dma_owner.c` `owner_validate`).
- **Exactly one `reg`:** the scanout control block, LW `0x20010200`, span
  `0x100` (`fes_de25_owner_probe`). It is reached only through the guarded
  transport (`de25_dma_guard.c`).
  - The guard is armed, with a fresh epoch, when the FPGA region's overlay
    carries a `fes,de25-linux-dma-scanout` child (`owner_notify`).
  - It is retired before any overlay change.
- **One lease (`owner->leased`) and one active allocation
  (`owner->active`).**
  - `OWNER_ALLOC` builds an 8 MiB grant file (`fes_de25_dma_create`):
    contiguous coherent memory under a 32-bit mask. `mmap` goes through
    `dma_mmap_attrs`, an eager VM_IO|VM_PFNMAP mapping, so revoke can zap
    it. No address ever reaches userspace.
  - `REAP` revokes with hardware proof: STOP, a drained snapshot, CLEAR,
    a cold snapshot, and the same build and epoch.
  - A failed proof keeps the allocation, and the module pinned, forever
    ("uncertain").

## The window, beside the scanout

**Same device.** The window's master drives SID 13 on the same F2SDRAM
adapter as the scanout (#2036, `scanout/linux_dma.py`). So the window's DMA
mapping must be made through the owner's device and domain. A second device
on SID 13 would join the same group, which `owner_validate` refuses.

**A second register block.** The owner node gains a second `reg`: the window
control block, LW `0x20010400`, span `0x40` (`fes.fabric.de25-hps-window`
`PeripheralOffset`/`ControlSpan`).

- Probe accepts either one `reg` (scanout only, as today) or exactly these
  two.
- **The window block has no hardware guard.** The scanout's guard is a
  timeout proxy inside the scanout block (guard registers at `0x80..0xff`
  of its `0x100` span), there because the scanout's registers live in the
  pixel clock, which can stop. The window block
  (`boards/de25nano/pc386/fes_de25_hps_window_control.v`, #2036) is plain
  registers in the board's 100 MHz peripheral clock, which runs whenever
  the LW bridge does. So the owner maps it directly. It uses the scanout
  guard's epoch, read before and after each window sequence, as its proof
  that the FPGA configuration is the one it granted to.
- The window is usable only when the overlay also carries a
  `fes,de25-linux-dma-window` child, and only while the scanout guard is
  armed.
- **The block answers SLVERR to anything it refuses:** a grant-register
  write while granted, or `CommandGrant`/`CommandClear` without their
  preconditions. On arm64 an SLVERR on a device write can raise an SError,
  which can take the kernel down. So the owner never issues a write the
  block could refuse. It reads status first and writes only when the
  preconditions hold, under the window lease, with the machine held by the
  runtime. The KUnit transport fails a test on any write the RTL would
  refuse.

**A second lease.** The window has its own `leased`/`active` pair, so a
pc386 session holds both the scanout grant (its session display) and the
window. `OWNER_IDLE` reports busy while either is held. Overlay changes are
refused while either is leased, as today.

**Allocation.** `OWNER_WINDOW_ALLOC` takes no argument and returns a
CLOEXEC window fd. The grant is published before the file exists, so a
refused window is freed at once, not by a deferred final `fput`. Under the window lease:

1. Read the window block's `Identity`/`Version`. Require held, drained, not
   granted and no fault. The runtime holds the machine (computer mailbox
   Hold) before it asks for a window, and keeps it held until the grant is
   confirmed.
2. Take the build identity from the scanout block's build words. The window
   block has none; both blocks are the same bitstream, under the same
   epoch.
3. `dma_alloc_attrs(dev, WindowSpan, ..., DMA_ATTR_FORCE_CONTIGUOUS)` under
   the 32-bit mask. Require 4 KiB alignment and the whole span below 2^32;
   the RTL refuses a span crossing 4 GiB too. Zero the memory.
4. Write `GrantLo`, `GrantHi`, `GrantSpan` and `GenerationLo/Hi` (a fresh
   nonzero generation). Fence, write `CommandGrant`, fence.
5. Read back. Require granted, drained, no fault, and registers that hold
   what was written. Otherwise clear (if granted) and fail.

**The window fd.**

- **`mmap`** at any page offset inside the span, shared, not exec.
  - It has the same `dma_mmap_attrs` eager-PFNMAP shape and the same per-mm
    bookkeeping as the scanout grant, so revoke can zap every mapping.
  - The runtime's range grants are `mmap`s at an offset; a range's revoke is
    `munmap`.
  - The mapping is the coherent DMA mapping, so there is no cache
    maintenance ioctl. The owner node declares no `dma-coherent`, and the
    platform verifier refuses one ("unproven coherence declaration",
    `image/kernel/de25-platform/verify_tree.c`, README "physical fabric
    coherence is not established"). So the mapping is uncached. 4d measures
    what a 128 MiB disk write and the guest-RAM clear cost under QEMU, and 4g
    measures them on the kit.
- **`WINDOW_INFO`** (`_IOWR`, with generation/version input like
  `DIAGNOSTICS`) returns the generation, version, span, status, first fault
  and build identity, read through the guard. No address. The runtime's
  `Check()` polls it.
- **`WINDOW_REVOKE` and the last close:**
  - Zap every mapping. Then require drained and no fault, write
    `CommandClear`, fence, confirm not granted, and free.
  - A fault, or ports that won't drain, leave the allocation exposed and the
    module pinned, as an uncertain scanout grant does today.
  - The contract makes a fault permanent until the FPGA is reprogrammed.
    Reprogramming goes through the owner's overlay notifier, which refuses
    while the window is leased. So a faulted window holds the kit until
    reboot. That is the safe outcome, and the runtime reports it.
- **`OWNER_REAP`** also retires the window, with the same proof.
- **One RTL comment disagrees.** `fes_de25_hps_window_control.v` still says
  "the runtime publishes the kernel's grant here". Editing it moves #2036's
  netlist identity, so it is left for #2036's next RTL change. The contract
  and its document already say the kernel publishes.

**ABI.** Additive; nothing in ABI 3 changes.

- `OWNER_ABI` still returns 3. The scanout's INFO and PUBLISH structures
  carry that number as their version, so bumping it would break every
  scanout client.
- `OWNER_WINDOW_ABI` (`0x15`) returns the window version (1). It fails with
  `ENODEV` without the window `reg`, and with `ENOTTY` on an older kernel.
  The runtime queries it before `OWNER_WINDOW_ALLOC` (`0x16`).
- The window fd takes `WINDOW_INFO` (`0x20`, 56 bytes) and `WINDOW_REVOKE`
  (`0x21`).
- The runtime mirrors the header, as it already does `de25_dma.h`
  (`scripts/consistency.py` `COMPONENT_FIXTURES`).
- The window constants come from a new `emit-c` row of the contract into
  `image/kernel/de25-dma/generated/`.
- **A failed publish.** If the block may still hold the grant and it
  cannot be cleared, the window is handed to the owner, so `REAP` can retry
  the clear.

## Storage

The owner allocates with `dma_alloc_attrs` and
`DMA_ATTR_FORCE_CONTIGUOUS`, from CMA. CMA on J8G is the vendor's 32 MiB
default. There are two ways to get 256 MiB, and both change the image:

- **A pool (planned).** A `reserved-memory` `shared-dma-pool`:
  - `reusable`, 272 MiB, `alloc-ranges` inside the F2SDRAM firewall's open
    region (below; `0x80100000..0xbfffffff`), which is also below 4 GiB;
  - named by `memory-region` on the owner node;
  - the scanout's 8 MiB then comes from the same pool;
  - bootargs unchanged.

  To verify in 4d: whether the pinned kernel's IOMMU DMA path takes a
  per-device CMA area with `DMA_ATTR_FORCE_CONTIGUOUS`. KUnit can't check
  that; it needs the QEMU boot of the platform kernel.
- **No pool (fallback).** Under the SMMU the fabric sees an IOVA, so
  physical contiguity is not a fabric need. A non-contiguous allocation
  mapped into one IOVA range removes the device-tree change. But
  `dma_mmap_attrs` would then map with `vm_map_pages`, a shape this owner
  refuses to expose, because revoke cannot zap page pins. It would need its
  own PFNMAP mapping path.

## The boot firmware (read only)

Nothing in the Linux boot chain was changed or written. What was read:

- **The F2SDRAM firewall.** It sits at the MPFE TBU output, so it judges the
  physical address after SMMU translation. Its registers are enable plus
  base/limit per region; there is no read/write or secure field, so writes
  face the same address check as the scanout's reads. Linux read it as
  region 0 enabled for `0x80100000..0xbfffffff`, and regions 1 and 2 off
  (`docs/validation/2026-11-01-de25-allocated-scanout/route-readonly.log`,
  TBU2NOC_FW, lines 15-27; layout in
  `platform/embedded/firmware/nano_ddr_firewall.h`). **So every physical page
  of the window must lie in that 1 GiB range.** Linux has 2 GiB, and today's
  CMA placement is not pinned anywhere, so the pool's `alloc-ranges` must pin
  it there.
- **The bridges.** The BSP boot script's `bridge enable` releases every
  bridge reset (RSTMGR reads 0 in the same log). It is not
  direction-specific.
- **The SMMU.** Stream 13 has only the owner's Linux DMA domain (strict, no
  bypass). The adapter drives SID 13 on AWUSER as on ARUSER
  (`scanout/linux_dma.py`).
- **The AXI write credit.** #2036 keeps the adapter's write credit at one
  for the scanout and the window together; 4g measures whether that starves
  the CPU.
- **Unread and hazardous:** the NoC/TCU/CCU firewall at `0x10d21400`. An
  earlier session (#1312's DMA bring-up, 1 November 2026) read it from Linux
  on J8G with `/dev/mem`, under its own kit claim; the board stopped
  responding and needed a power cycle
  (`docs/validation/2026-11-01-de25-allocated-scanout/README.md`,
  "Subsequent TCU firewall access failure", and `tcu-access-failure.json`).
  This slice did not touch the kit, and nobody reads that region from Linux
  again. Whatever the binary BL31 and SPL program there stays unknown.
  Fabric writes into HPS memory have never been tested on Kit D25
  (`docs/validation/2026-11-02-de25-contained-console/README.md`). The first
  write test is 4g.

## Tests

KUnit, in the existing suite and fixture (`de25_dma_test.c`, with its test
guard transport):

- probe accepts one or two `reg`s and nothing else;
- allocation refuses:
  - a missing window profile;
  - a wrong identity or version;
  - a block that is granted, faulted or not drained;
  - a second window;
  - a span above 2^32;
- the grant writes and the read-back checks happen in order;
- mmap bounds and offsets hold; revoke zaps, and a later access faults
  SIGBUS;
- revoke clears only when drained, and a fault or a drain timeout keeps the
  allocation pinned;
- `OWNER_IDLE` and overlay changes report busy while either lease is held;
- ABI 3's structures and ioctls are unchanged.

## Image items for Deano's OK at flash time

1. The kernel: this owner window (built in, `FES_DE25_DMA_PLATFORM`).
2. The device tree: the owner node's second `reg` and `memory-region`, and
   the `reserved-memory` pool. The owner node is injected by
   `scripts/de25_kernel_platform.py` (`dma-owner@...` fragment), so the
   change is there and in `verify_tree.c`.
3. The J8G image built with 1 and 2, with the matching runtime bundle.
4. The boot firmware: read here, not changed. Whether it admits fabric
   writes on SID 13 is checked on the kit in 4g.
