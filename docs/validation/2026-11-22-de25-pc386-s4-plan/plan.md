# DE25 pc386 slice 4: platform, runtime and FogCast (plan)

Slice 4 of #2029. Lane plan #2030 (merged); Deano's decisions of 20 November
2026: P1 all pc386 memory in HPS DDR, P2 the image change approved for slice 4
(building and testing; flashing a kit needs his own OK for that image and kit),
P3 61.437908 MHz, P4 no recipe or support row before the kit pass. The shell
and producer are #2036 (`deano/de25-pc386-shell-s3`). This plan builds nothing.
References are to #2036's head `af58ebed2`.

## What the shell asks of the platform

#2036's board top presents `fes.fabric.de25-hps-window` 1.0
(`sources/mister-packages/packages/fabric/fes_fabric_de25_hps_window.yaml`,
`sources/misteross/docs/de25-hps-window.md`):

- one contiguous grant of 256 MiB (`0x10000000`), 4 KiB aligned, wholly below
  2^32, with a nonzero generation;
- a control block at lightweight-bridge offset `0x10400` (64 bytes): grant
  lo/hi, span, generation lo/hi, `CommandGrant`/`CommandClear`, status
  (granted, drained, held, fault), first fault;
- three read/write ports behind one F2SDRAM master, on the scanout's SMMU
  stream (13; `scanout/linux_dma.py` profile `scanout+window`, write credits
  one).

The package declares DE10 pc386's eleven interfaces at fes.computer 1.1, plus
`fes.fabric.de25-hps-window` 1.0 and `fes.fabric.de25-scanout` 1.3
(`producers/build_pc386_package.py`, `build_machine_package.py` `manifest`).

## What exists

**Kernel owner** (`image/kernel/de25-dma/`, built in by
`scripts/de25_kernel_platform.py`, `FES_DE25_DMA_PLATFORM=y`):

- `/dev/fes-de25-scanout`, owner ABI 3: `OWNER_ALLOC` (no size argument),
  `OWNER_REAP`, `OWNER_IDLE`, `OWNER_ABI`, `OWNER_INFO`; the grant fd takes
  `INFO`/`PUBLISH`/`POLL`/`REVOKE`/`DIAGNOSTICS`/`RETIREMENT` and `mmap`.
- Scanout only: 8 MiB, two slots, compiled in from the scanout contract;
  `dma_alloc_attrs(..., DMA_ATTR_FORCE_CONTIGUOUS)` under a 32-bit mask. The
  kernel writes the grant registers itself; no CPU or DMA address ever reaches
  userspace (`de25_dma.h`).
- SMMU v3, strict; the owner node `dma-owner@20010200` carries
  `iommus = <&smmu 13>`.
- CMA is the vendor default, 32 MiB: no `cma=` and no `linux,cma` node
  (`scripts/de25_boot_contract.py` pins bootargs exactly;
  `docs/validation/2026-10-31-de25-owner-binding/collection/receipt.json`
  shows `CmaTotal: 32768 kB`).
- KUnit: `scripts/de25_kernel_test.py` (QEMU arm64, 67 cases).

**Runtime** (`sources/libmister-runtime`):

- pc386's media logic is board-neutral already. `NativeHardware`
  zeroes guest RAM and VRAM (`ClearPc386GuestMemory`), writes the ATA image
  with its delta and CRC (`LoadAtaDisk`), and reads dirty chunks back at Stop
  (`FlushAtaDiskSave`), all through `DiskWindow::Access(offset, bytes, use)`
  over `MenuWindowOperations` (grant, map, revoke).
- On the DE10 that is `/dev/fes-window` over the boot-reserved
  `0x30000000` window. On the DE25 `disk_window_` is null,
  `LinuxDe25FpgaManager::BootHpsDdrLayout()` is false and
  `ReleaseHpsDdrPorts` is unsupported, so `fes.memory.hps-ddr` is refused.
- The DE25 admission table (`src/native/de25_core_contract.hpp`) gives each
  core at most five interfaces beside a fixed seven-interface shell that
  includes `fes.gamepad.ports`. pc386 needs seven of its own (mouse,
  hps-ddr, ata-disk, persistence, wall clock, pc386 machine, the window) and
  does not declare gamepad ports. #2069 (DE25 PC/XT, draft) adds a per-core
  ABI minor to the same table.
- `PrepareDe25`/`hardware.cpp` advertises `fes.media.ata-disk` in the
  fes.computer 1.1 interface list only when `BootHpsDdrLayout() &&
  disk_window_`, so FogCast never sees the disk on the DE25 today.
- Wall clock, persistence words and the pc386 machine words go through
  `FesGpCoreDriver`, which already runs on the DE25 computer transport.

**FogCast** (`sources/FogCast`): the DE25 profile handling in
`internal/misterruntime/protocol_v2.go`, initial media in
`internal/misterruntime/initial_media.go` and `fogcast/core_initial_disk.go`.
The DE10 pc386 ATA disk path exists; the DE25 one does not.

**Diagnostics.** `scripts/pc386_cpu_status.py` reads the status ring with
`devmem` at physical `0x31000000`. On the DE25 the window has no fixed
physical address, and userspace never learns it.

## Design

### Who publishes the grant: the kernel

The owner never gives userspace a DMA address, so the runtime cannot write
`GrantLo`/`GrantHi`. The kernel owner allocates the window and publishes it to
the control block itself, as it does for the scanout. The contract text "the
runtime publishes the grant" (`de25-hps-window.md`) changes to "the kernel
owner publishes it"; the registers and the RTL do not change.

### Kernel owner: a window allocation beside the scanout (image)

- **Same device, same domain.** The window shares SID 13 with the scanout, so
  it must live in the same SMMU group, which is the owner device. A second
  platform device on SID 13 would join that group anyway. The owner gets a
  second allocation kind, not a second driver.
- **Storage.** A dedicated `reserved-memory` `shared-dma-pool` (reusable, so
  Linux may still use it as movable memory when no window is held), of
  272 MiB, named by `memory-region` on the owner node. That keeps the boot
  arguments unchanged and does not race the global 32 MiB CMA for a 256 MiB
  contiguous run. `dma_alloc_attrs(..., DMA_ATTR_FORCE_CONTIGUOUS)` under the
  32-bit mask, 4 KiB aligned; refuse anything that crosses 2^32 (the RTL
  refuses it too, 405a892c0). Zeroed at allocation.
- **Grant.** After the bitstream's identity is checked: read the block's
  `Identity`/`Version`, require held and drained, write lo/hi, span, a fresh
  nonzero generation, then `CommandGrant`; confirm `Granted`. `CommandClear`
  only when drained; REAP retires the window with the scanout. A latched
  fault is reported, not cleared: the contract makes it fatal until
  reprogramming, so the allocation is kept (leaked to the generation) until
  the overlay is removed rather than handed back while a port may still
  issue.
- **ABI 4, additive.** `OWNER_WINDOW_ALLOC` returns a window fd. On it:
  `WINDOW_INFO` (span, generation, cached status and fault; no address),
  `WINDOW_GRANT` (offset, length, page aligned, inside the span) returning a
  range fd that is `mmap`ped shared and revoked with `WINDOW_REVOKE` (PTE zap,
  then SIGBUS), and `WINDOW_SYNC` for CPU cache maintenance on a range if the
  mapping is cacheable. ABI 3's scanout structs stay byte-identical. The
  runtime mirrors the header.
- **MMIO.** The window block joins the guarded transport
  (`de25_dma_guard.c`) and the owner's `reg`.
- **Tests.** KUnit cases for: size, alignment and 4 GiB refusal; grant order
  and confirmation; clear only when drained; fault kept and allocation not
  freed; range grant bounds; revoke then SIGBUS; ABI 3 unchanged.

**Image and kernel changes, for Deano's OK at flash time** (P2 approves
building and testing):

1. Kernel: the de25-dma owner window allocation and ABI 4 (built-in module).
2. Device tree: the `reserved-memory` pool (272 MiB) and `memory-region`, and
   the window block in the owner's `reg`.
3. Image: the J8G image built with 1 and 2 and the matching runtime bundle.
   Bootargs and U-Boot are unchanged.
4. To verify on the kit, and an image change if it bites: whether the HPS
   DDR firewall or the F2SDRAM bridge configuration from the boot firmware
   admits fabric writes on stream 13. The owner README says the stream and
   firewall are not yet qualified. If writes are blocked, the fix is in the
   boot firmware and comes back to Deano as its own change.

### Runtime (no image)

- **R1. Admission.** Generalise the DE25 table: each core names its own
  shell (pc386: video, keyboard, audio, session display, display plane, no
  gamepad) and any number of interfaces, and the format/ABI-minor it takes.
  Add `fes.pc386` at fes.computer 1.1 with its eleven interfaces, the window
  and scanout 1.3. Admission still refuses it with a named error until R2's
  window backend reports ready, so this lands safely before the kernel. The
  generalisation overlaps #2069's `abi_minor`; the pc386 row names
  `fes.fabric.de25-hps-window`, which is #2036's contract, so that row
  stacks on #2036.
- **R2. The window backend.** `DiskWindow` already takes
  `MenuWindowOperations`. Add a DE25 window context over an injected
  `De25WindowOperations` (the ABI-4 calls) that allocates per session after
  identity, gives `DiskWindow` its range grants, and retires at Stop. The
  DE25 FPGA manager answers `fes.memory.hps-ddr` from it (in place of
  `BootHpsDdrLayout`) and `ReleaseHpsDdrPorts` becomes "grant confirmed and
  ports out of hold". The `fes.media.ata-disk` advertisement gate in
  `hardware.cpp` takes the same answer. Tests use a scripted fake owner, as
  `tests/support/de25_menu_operations.hpp` does. The Linux operations that
  call the real ioctls land with the kernel (they need its header).
- **R3. Diagnostics.** A runtime-side status-ring read through a window range
  grant, replacing `devmem` on the DE25 (`pc386_cpu_status.py --de25`).

- **Floppies.** pc386 floppies (A:/B: at window offset `0x01800000`) are
  #2101's work (F3, draft) and go through the same `DiskWindow`. Slice 4
  takes them as #2101 lands; it does not build its own floppy path.

### FogCast (no image)

- **F1.** The DE25 profile admits `fes.pc386`; the initial-disk and save
  paths take the DE25 pc386 package; the client proof receipt is
  regenerated. Go tests.

### Kit D25 run

After the kernel, image and runtime pieces, and Deano's OK for the image on
J8G: claim D25, install the image and bundle, launch the #2036 package, boot
FreeDOS from the ATA disk, save a change, relaunch and read it back, capture
HDMI and audio, restore the baseline image and bundle, release. That run is
the merge gate for #2036 and for slice 4's DE25 pieces.

## Slices and order

| | PR | Needs | Base |
| --- | --- | --- | --- |
| 4a | R1 admission table: per-core shell, any number of interfaces, the pc386 row (refused until a window backend exists) | #2069's `abi_minor` (stack or wait) | #2036 |
| 4b | Contract: the grant is the kernel's; C and C++ headers generated for the window | nothing | #2036 (contract lives there) |
| 4c | R2 runtime window backend over a fake owner, and the hps-ddr / ata-disk gates | 4b | 4b |
| 4d | Kernel owner ABI 4, DT pool, KUnit | 4b; image change | 4b |
| 4e | F1 FogCast | 4a, and #2101 for its DE10 pc386 media paths | stack |
| 4f | R3 status-ring read through the runtime | 4c | 4c |
| 4g | Image build + Kit D25 run | 4a–4f, Deano's flash OK | stack |

4a, 4b, 4c, 4e and 4f need neither a kit nor an image change. 4d is code and
KUnit only until 4g. The yaml description is not emitted into
`fes_de25_hps_window.vh`, so 4b does not move #2036's `BUILD_ID`; 4b checks
that by re-emitting.

## Open

- **The kernel storage design is unverified** and is checked in 4d under
  QEMU/KUnit before anything is flashed:
  - a `memory-region` on the owner node also redirects the scanout's 8 MiB
    allocation into the pool (acceptable, but it must be measured);
  - whether the pinned kernel's IOMMU DMA path honours a per-device pool
    with `DMA_ATTR_FORCE_CONTIGUOUS`;
  - under the SMMU the fabric sees an IOVA, so physical contiguity is a
    driver choice, not a fabric need. The alternative is no DT pool and a
    non-contiguous allocation mapped into one contiguous IOVA range; it
    removes the device-tree change from the image list, at the cost of a
    mapping shape the owner's revoke rules must still cover.
  - Allocating and zeroing 256 MiB on every launch costs launch time; 4d
    measures it under QEMU and 4g on the kit.
- Whether a coherent (uncached) mapping is fast enough to write 128 MiB of
  disk at launch; if not, a cacheable mapping with `WINDOW_SYNC`.
- F2SDRAM write credits stay one in #2036's profile; the kit run measures
  whether that starves the CPU.
- The native (Linux-free) runtime (#1867/#1952) gets no window here.
