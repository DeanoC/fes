# DE25 kernel DMA ownership source review

This review covers the kernel ownership diagnostic and generated C constants
at final candidate `45c2dfc247eccaae2556798a039a9390eaa15a6c`. The reviewed
functional files match that commit; driver and internal-header bytes are
unchanged from `d3b657ff249c52aaa95f08004daeaf7219d25258`. It supplies source
reasoning and bounded QEMU kernel software evidence, not loadable-module or
physical DMA qualification. Full test results belong to the accompanying
validation records. No hardware, kit,
harness, FPGA database or producer input was changed by this review.

The Linux primary sources below are pinned to Terasic revision
`d7d192a9ddd955fe206aa350afed8f82e368a74c`, matching the preserved BSP kernel's
`6.12.11-gd7d192a9ddd9` banner. The retained SDK's 5.10 UAPI headers alone are
not a prepared kernel module build tree. Kernel source/configuration and
generated build headers remain distinct from the userspace SDK.

## Canonical constants and admission boundary

[`emit-c`](../../../sources/mister-packages/cmd/mister-packages/main.go),
lines 176–188, accepts only `kind: fabric` and loads it through existing strict
fabric validation. The
[C emitter](../../../sources/mister-packages/internal/emitc/emitc.go), lines
13–31, emits guarded, header-independent unsigned 32-bit `#define` values
using the existing uppercase fabric macro naming rule. It rejects normalized
macro and guard collisions. The kernel consumes the canonical
[`fes_de25_ddr_probe.h`](https://github.com/DeanoC/fes/blob/45c2dfc247eccaae2556798a039a9390eaa15a6c/image/kernel/de25-dma/generated/fes_de25_ddr_probe.h),
not a hand-maintained copy of C++ constants. Emission adds no SoC map, GP ABI,
runtime capability, device binding or DMA admission.

[`de25_dma.c`](../../../image/kernel/de25-dma/de25_dma.c), lines 331–379,
receives a device and private transport backend, sets the device's 32-bit DMA
mask, allocates one 8 MiB coherent region, and bounds the complete DMA handle
and allocation without interpreting the handle as a CPU physical address.
Only the KUnit test currently constructs such a backend. Production probe at
lines 382–392 always returns `-EOPNOTSUPP`; no userspace, DT or module flag
opens it. INFO/PUBLISH expose generation, geometry, slot and sequence, never
CPU physical or DMA addresses. This is one read-probe operation per grant,
not an interactive MENU scanout or displayed-frame completion interface.

The pinned [DMA allocator](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/kernel/dma/mapping.c#L591-L624)
selects direct or IOMMU operations from the supplied device. Its
[mmap dispatcher](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/kernel/dma/mapping.c#L537-L562)
uses that same device and requires backing to survive the userspace mapping.
A DMA handle allocated for a test/root/misc device does not establish a handle
for the actual fabric SID/domain. Future hardware admission needs the real
device, mask, translation or bypass binding, physical firewall permission and
reset lifetime proof. Neither coherent allocation nor a successful software
test establishes a coherent F2SDRAM interconnect.

## Reference and backend lifetime

The driver retains a device object reference, a unique anonymous inode and a
base grant reference. The pinned
[anonymous-file implementation](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/fs/anon_inodes.c#L78-L124)
also obtains the file-operations owner's module reference. Its
[unique-inode variant](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/fs/anon_inodes.c#L184-L218)
isolates revocation to this grant. The kernel's
[mmap path](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/mmap.c#L1456-L1460)
takes a file reference for each VMA, so an ordinary FD close cannot destroy
storage still mapped by that file.

Before the potentially side-effecting start callback, driver lines 127–143
acquire a separate exposure reference and module pin. A failed start retains
them. Revocation at lines 153–205 drops them only after stop and drain succeed
with the original nonzero epoch visible both before and after. Timeout, error,
changed epoch or unknown epoch leaves exposure pinned and marks it uncertain.
Last close at lines 308–316 retries revoke; it does not reinterpret failure as
drain. Final destruction at lines 63–82 releases DMA storage, recorded mm
references, inode and device only after the exposure reference is gone.

[`de25_dma_internal.h:27–35`](../../../image/kernel/de25-dma/de25_dma_internal.h)
explicitly requires the caller to retain callback code/context and device
DMA/domain resources until final grant destruction, including uncertain
exposure. `get_device()` alone does not preserve devm state, a backend context
or an IOMMU domain across device detach. That caller contract has no DE25
hardware implementation yet; the closed production probe prevents treating
these references as completed detach/reset qualification.

## Mapping form, page pins and revocation

Driver lines 250–271 accept only the genuine eager `VM_IO | VM_PFNMAP` mapping
returned by `dma_mmap_coherent`, with no mapper-installed `vm_ops`. They do
not manufacture PFN flags or infer PFNs from a DMA address. This restriction
resolves a review finding: pinned
[`iommu_dma_mmap`](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/drivers/iommu/dma-iommu.c#L1669-L1697)
can instead call `vm_map_pages`, which installs ordinary pages with
[`VM_MIXEDMAP`](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/memory.c#L2172-L2185).
That form can also leave `vm_ops` empty but admit page pins/aliases surviving
PTE zapping. It is unsupported here. A device's translation support and its
revocable mapping support are separate gates.

For the selected pinned ARM64 PFN mapping path:

- [`remap_pte_range`](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/memory.c#L2545-L2563)
  installs special PTEs. [Fast GUP](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/gup.c#L2925-L2935)
  cannot pin those PTEs, and [slow/remote GUP](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/gup.c#L1270-L1278)
  rejects `VM_IO | VM_PFNMAP` before permission overrides.
- Shared mappings avoid private COW aliases. `VM_DONTCOPY` prevents fork
  inheritance; `VM_DONTEXPAND` prevents expansion. Pinned
  [mprotect](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/mprotect.c#L796-L810)
  retains non-access flags and cannot grant execution after `VM_MAYEXEC` is
  cleared. Existing mappings remain associated with their unique inode.
- The driver's fault-only operations supply no remote-access callback. The
  kernel's [remote-access fallback](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/memory.c#L6600-L6615)
  therefore cannot bypass failed GUP through a driver access operation.

Revocation first marks the grant closed under its lock. Each mmap records its
mm before installing PTEs. The pinned kernel
[calls the driver's mmap](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/mmap.c#L1440-L1458)
before [linking the VMA into the file mapping](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/mmap.c#L1528-L1531).
The driver therefore takes each recorded mm's mmap write lock after closing
the grant, without holding its grant lock. This waits past an in-flight
callback and VMA link while avoiding the reverse lock order. It then calls
`unmap_mapping_range(..., 0, 0, 1)` before backend stop/drain. New mmap fails;
the installed fault handler returns SIGBUS and cannot recreate revoked PTEs.
This is ordinary process-mapping revocation, not authority over privileged
kernel/raw physical access.

The empty-default `vm_ops` refusal found during kernel testing is also resolved
in source: [vma initialization](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/include/linux/mm.h#L848-L853)
sets the [zero-initialized dummy table](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/init-mm.c#L20).
The driver clears that placeholder before calling the DMA mapper, then checks
the mapper's result. It discards no DMA-installed operations and references no
unexported dummy-table symbol. The KUnit sources include real mapping-form,
writable-pin refusal and pre-link race checks.

The D3 run reported eight passing cases but its raw log contained three rwsem
warnings: the test called `pin_user_pages()` after releasing its mmap read
lock. Candidate `45c2dfc247` changes that one test call to
`pin_user_pages_unlocked(address, 1, &pinned, FOLL_WRITE)`. The pinned
[unlocked API](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/gup.c#L3621-L3631)
sets `locked=0`, preserves `FOLL_PIN`, and delegates to the helper whose
[lock contract and acquisition](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/gup.c#L1704-L1741)
and [release](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/mm/gup.c#L1845-L1853)
manage the mmap lock internally. This fixes the test's lock ownership without
weakening the writable-pin rejection assertion or changing the driver. D3's
case count is not a clean-warning result.

The final `45c2dfc247` run completed with all eight required named cases PASS
and no `WARNING:`, `BUG:`, `Oops:` or kernel-panic marker in the raw kernel
log. Independent read-only verification matched all nine exported artifact,
log and configuration hashes; all ten driver input hashes against the
committed Git blobs, current files and retained read-only FES snapshot; and
the runner hash against that same commit/snapshot. Both exported driver/test
objects are ELF64 `EM_AARCH64`. The run uses Buildroot GCC 9.4.0 and QEMU
7.2.22. Its retained `out/de25-kunit-45c2dfc247/checks.json` has SHA-256
`2af7c3c5b255852434a50da041dfbb08520f198d7fdea9eada28cf54668538f3`.
This validates the actual kernel software paths on a KUnit test device; it
does not qualify module loading on the vendor kernel or the DE25 fabric,
SMMU, firewall, DDR visibility, reset epoch or hardware binding.

## HPS warm reset remains an external gate

In retained producer `8f54e4f89e5b12ae18df70c542bc5846d0452ce6`, generated
`hps_subsys/ip/hps_subsys/agilex_hps/intel_agilex_5_soc_1410/synth/agilex_hps_intel_agilex_5_soc_1410_4wddr4q.v:551–552`
directly connects exposed `reset_req`/`reset_ack` to hard
`h2f_pending_rst_req_n`/`f2h_pending_rst_ack_n`. Both are active low despite the
exposed names lacking `_n`. That file's SHA-256 is
`eab3a8ae71ae29077952b2eab1188f98de0c7248fcb27961621e875011479184`.
Its `qsys_top/qsys_top.sopcinfo:18617–18635` declares no associated clock or
reset for the conduit. Request ingress needs an explicit clock-domain crossing;
no 100 MHz synchronous relationship is supplied by that interface.

Installed official component source
`/data/altera_pro/26.3/ip/altera/intel_hps/sm/common/interface_ports.tcl:350–351`
requires acknowledgement only after the HPS soft logic is in reset. The
[component reset manual](https://docs.altera.com/r/docs/813752/26.1/hard-processor-system-component-reference-manual-agilextm-5-socs/reset-signals)
describes this as fabric readiness for warm reset. Probe-local IDLE proves
neither global quiescence nor DDR retention: other JTAG/F2H/F2SDRAM masters,
HPS-origin bridges and their soft slaves require their own fencing/drain/reset
coordination. The combined builder
[`attach_video:83–92`](../../../sources/misteross/boards/de25nano/combined/build.py)
preserves the debug paths, leaves request unconnected and explicitly holds
ACK at `1`, inactive. It implements no warm-reset grant barrier.

Agilex 5 TRM 814346 version 26.1.1,
[§8.6.2.3 HPS idle sequence](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/hps-idle-sequence?contentId=DxY_zh3eoV_L7B2PT07HQg),
allows progression when enabled incoming/outgoing fence/drain completes **or
times out**. Withholding ACK is not proof that reset cannot proceed. Its
[warm-reset event](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/hps-warm-reset-event?contentId=FNLIcgldFJ03BVnetw_jXA)
also warns that HPS user clocks stop. A future admitted backend must stop new
requests, retire accepted responses, coordinate all relevant reset boundaries
and prevent restart until an external allocation epoch authorizes it. Hard
reset, timeout, deasserted request or reset-local IDLE cannot prove prior traffic
gone or authorize allocation reuse/freeing.

## Reviewed FES source identities

SHA-256 values for the reviewed producer's Git blobs:

| File | SHA-256 |
| --- | --- |
| `image/kernel/de25-dma/de25_dma.c` | `f8f30e21f96b9694b2e376e4713414f41179a377fbba91e906eb26a0f9070fff` |
| `image/kernel/de25-dma/de25_dma_internal.h` | `6677e55814c4ffecbdc71c00c4c5012a584be879f4a68724c09a9c0f39fbbd81` |
| `image/kernel/de25-dma/de25_dma.h` | `909e486737681ff284f325740b0f6c56c221a6f1370d8dfd2dee27e5cda851a4` |
| `image/kernel/de25-dma/de25_dma_test.c` | `5c4ca977d01abed08f541ed2478265e17b7e49978f31c858faa2bf422481803f` |
| `image/kernel/de25-dma/generated/fes_de25_ddr_probe.h` | `ab89732c139512edbdd0610783f1001dfd2ede9839d92ef767b1d5a34f2fc5cf` |
| `sources/mister-packages/internal/emitc/emitc.go` | `96eaa01253c122a32c6664be0e4df7393c26b2692a09db0cac9bef88c625662b` |

The source review finds no remaining blocker within this deliberately closed
software diagnostic. It does not admit a production device, device detach,
reset recovery, boot firmware pairing, framebuffer scanout or physical DDR.
